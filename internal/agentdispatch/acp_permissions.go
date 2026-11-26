package agentdispatch

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const acpRPCMethodNotFound = -32601

// acpLaunchConfig holds provider-agnostic ACP session parameters and
// permission policy derived from Agent Layer project configuration.
type acpLaunchConfig struct {
	WorkDir           string
	ApprovalsMode     string
	AllowCommands     bool
	AllowMCP          bool
	CommandsAllow     []string
	ProjectMCPServers []string
	SessionMCPServers []string
	MCPServers        []acpMCPServerSpec
	HandshakeNonce    string
	Authenticate      bool
}

type acpPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  acpPermissionTool  `json:"toolCall"`
	Options   []acpPermissionOpt `json:"options"`
}

type acpPermissionTool struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Args     map[string]any `json:"args"`
	RawInput map[string]any `json:"rawInput"`
}

type acpPermissionOpt struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

func (c *acpClient) handleAgentRequest(msg acpJSONRPCMessage) error {
	if msg.Method != acpMethodRequestPerm {
		return c.sendError(msg.ID, acpRPCMethodNotFound, fmt.Sprintf("unsupported ACP client method %q", msg.Method))
	}
	var req acpPermissionRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return c.sendResponse(msg.ID, map[string]any{
			jsonOutcomeKey: map[string]any{jsonOutcomeKey: dispatchStateCancelled},
		})
	}
	if req.SessionID != "" && c.sessionID != "" && req.SessionID != c.sessionID {
		return c.sendResponse(msg.ID, map[string]any{
			jsonOutcomeKey: map[string]any{jsonOutcomeKey: dispatchStateCancelled},
		})
	}
	if c.checkCancelled() {
		return c.sendResponse(msg.ID, map[string]any{
			jsonOutcomeKey: map[string]any{jsonOutcomeKey: dispatchStateCancelled},
		})
	}
	allow := acpShouldApproveTool(c.provider, c.cfg, req.ToolCall)
	if !allow {
		label := strings.TrimSpace(req.ToolCall.Name)
		if label == "" {
			label = strings.TrimSpace(req.ToolCall.Title)
		}
		if label == "" {
			label = strings.TrimSpace(req.ToolCall.Kind)
		}
		if label == "" {
			label = "tool"
		}
		c.deniedTools = append(c.deniedTools, label)
	}
	optionID := selectACPPermissionOption(req.Options, allow)
	if optionID == "" {
		return c.sendResponse(msg.ID, map[string]any{
			jsonOutcomeKey: map[string]any{jsonOutcomeKey: dispatchStateCancelled},
		})
	}
	return c.sendResponse(msg.ID, map[string]any{
		jsonOutcomeKey: map[string]any{
			jsonOutcomeKey: "selected",
			"optionId":     optionID,
		},
	})
}

// selectACPPermissionOption selects ONLY once-scoped ACP permission options so
// invocation-level decisions never persist grants to provider state.
func selectACPPermissionOption(options []acpPermissionOpt, allow bool) string {
	wantKind := "reject_once"
	if allow {
		wantKind = "allow_once"
	}
	for _, opt := range options {
		if opt.Kind == wantKind && opt.OptionID != "" {
			return opt.OptionID
		}
	}
	return ""
}

// acpShouldApproveTool enforces Agent Layer's approvals.mode, commands.allow,
// project MCP server allowlists, and unconditional session-scoped MCP server
// approval on ACP session/request_permission callbacks.
func acpShouldApproveTool(provider string, cfg acpLaunchConfig, tool acpPermissionTool) bool {
	if cfg.ApprovalsMode == config.ApprovalModeYOLO {
		return true
	}
	// Check tool.Kind first so execute/edit/delete/move calls can never bypass
	// command or edit policy by spoofing an MCP-shaped title or payload.
	kind := strings.ToLower(strings.TrimSpace(tool.Kind))
	switch kind {
	case "read", "search", "think":
		return true
	case "edit", "delete", "move":
		return cfg.AllowCommands
	case "execute":
		if !cfg.AllowCommands {
			return false
		}
		command := extractACPExecuteCommand(tool)
		if command == "" {
			return false
		}
		return acpCommandAllowed(command, cfg.CommandsAllow)
	case "", "other", "mcp":
		if server, isMCP := extractACPMCPServerName(provider, tool, cfg.SessionMCPServers, cfg.ProjectMCPServers); isMCP {
			if slices.Contains(cfg.SessionMCPServers, server) {
				return true
			}
			if cfg.AllowMCP && slices.Contains(cfg.ProjectMCPServers, server) {
				return true
			}
			return false
		}
		return false
	default:
		return false
	}
}

func extractACPMCPServerName(provider string, tool acpPermissionTool, sessionServers []string, projectServers []string) (string, bool) {
	allServers := make([]string, 0, len(sessionServers)+len(projectServers))
	allServers = append(allServers, sessionServers...)
	allServers = append(allServers, projectServers...)
	// Longest server name first so "foo_bar" or "workflow__admin" matches before "foo" or "workflow".
	slices.SortFunc(allServers, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})

	switch provider {
	case AgentGrok:
		// Only inspect structured Grok UseTool payloads; never fall back to
		// free-form tool.Title which may hold shell commands or arbitrary text.
		if tool.RawInput == nil {
			return "", false
		}
		if variant, hasVariant := tool.RawInput["variant"].(string); hasVariant && variant != "UseTool" {
			return "", false
		}
		toolName, ok := tool.RawInput["tool_name"].(string)
		if !ok || toolName == "" {
			return "", false
		}
		for _, server := range allServers {
			prefix := server + "__"
			if strings.HasPrefix(toolName, prefix) && len(toolName) > len(prefix) {
				return server, true
			}
		}
		if idx := strings.Index(toolName, "__"); idx > 0 {
			return toolName[:idx], true
		}
		return "", false

	case AgentAntigravity:
		// Lazy MCP bridge tools carry ServerName in args / rawInput.
		switch tool.Name {
		case "call_mcp_tool", "list_resources", "read_resource":
			for _, m := range []map[string]any{tool.Args, tool.RawInput} {
				if m == nil {
					continue
				}
				if srv, ok := m["ServerName"].(string); ok && strings.TrimSpace(srv) != "" {
					return strings.TrimSpace(srv), true
				}
				if srv, ok := m["serverName"].(string); ok && strings.TrimSpace(srv) != "" {
					return strings.TrimSpace(srv), true
				}
			}
			return "", true
		}
		// Eager MCP tools are named mcp_<server>_<tool> in structured tool.Name.
		// Never fall back to free-form tool.Title.
		if strings.HasPrefix(tool.Name, "mcp_") {
			rest := strings.TrimPrefix(tool.Name, "mcp_")
			for _, server := range allServers {
				if strings.HasPrefix(rest, server+"_") && len(rest) > len(server)+1 {
					return server, true
				}
			}
			if idx := strings.IndexByte(rest, '_'); idx > 0 {
				return rest[:idx], true
			}
			return "", true
		}
		return "", false
	}
	return "", false
}

func extractACPExecuteCommand(tool acpPermissionTool) string {
	for _, m := range []map[string]any{tool.RawInput, tool.Args} {
		if m == nil {
			continue
		}
		for _, key := range []string{jsonCommandKey, "CommandLine", "cmd"} {
			if val, ok := m[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
		if nested, ok := m["tool_input"].(map[string]any); ok {
			for _, key := range []string{jsonCommandKey, "cmd"} {
				if val, ok := nested[key].(string); ok && strings.TrimSpace(val) != "" {
					return strings.TrimSpace(val)
				}
			}
		}
	}
	return ""
}

func acpCommandAllowed(command string, allowlist []string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || containsUnsafeShellComposition(trimmed) {
		return false
	}
	for _, pattern := range allowlist {
		p := strings.TrimSpace(pattern)
		if p == "" {
			continue
		}
		if trimmed == p || strings.HasPrefix(trimmed, p+" ") || strings.HasPrefix(trimmed, p+"\t") {
			return true
		}
	}
	return false
}

func containsUnsafeShellComposition(cmd string) bool {
	for _, token := range []string{"$(", "${", "$'", "$\"", "`", "&", ";", "|", "\n", "\r", "\x00", ">", "<"} {
		if strings.Contains(cmd, token) {
			return true
		}
	}
	return false
}

func buildACPLaunchConfig(
	target targetMeta,
	project *config.ProjectConfig,
	workDir string,
	staged stagedSessionMCP,
	sessionMCP []SessionMCPServer,
	runDir string,
) *acpLaunchConfig {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	clientID := projection.ClientGrok
	authenticate := true
	if target.Name == AgentAntigravity {
		clientID = projection.ClientAntigravity
		authenticate = false
	}
	var mcpSpecs []acpMCPServerSpec
	if len(sessionMCP) > 0 {
		mcpSpecs = buildACPMCPServerSpecs(runDir, staged, sessionMCP)
	}
	absWorkDir := workDir
	if abs, err := filepath.Abs(workDir); err == nil {
		absWorkDir = abs
	}
	return &acpLaunchConfig{
		WorkDir:           absWorkDir,
		ApprovalsMode:     project.Config.Approvals.Mode,
		AllowCommands:     approvals.AllowCommands,
		AllowMCP:          approvals.AllowMCP,
		CommandsAllow:     slices.Clone(approvals.Commands),
		ProjectMCPServers: projection.EffectiveServerIDs(project.Config, clientID),
		SessionMCPServers: sessionMCPServerNames(sessionMCP),
		MCPServers:        mcpSpecs,
		HandshakeNonce:    staged.Nonce,
		Authenticate:      authenticate,
	}
}
