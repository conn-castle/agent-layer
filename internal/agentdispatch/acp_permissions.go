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

const (
	acpRPCMethodNotFound = -32601
	jsonCmdKey           = "cmd"
)

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
	ToolCallID string         `json:"toolCallId"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Args       map[string]any `json:"args"`
	RawInput   map[string]any `json:"rawInput"`
}

func mergeACPPermissionTool(base, override acpPermissionTool) acpPermissionTool {
	merged := base
	if override.ToolCallID != "" {
		merged.ToolCallID = override.ToolCallID
	}
	if override.Name != "" {
		merged.Name = override.Name
	}
	if override.Kind != "" {
		merged.Kind = override.Kind
	}
	if override.Title != "" {
		merged.Title = override.Title
	}
	// If the permission request supplies either Args or RawInput, treat its
	// input as authoritative across both fields so stale cached RawInput can
	// never shadow a request-supplied Args command (or vice versa).
	if len(override.Args) > 0 || len(override.RawInput) > 0 {
		merged.Args = override.Args
		merged.RawInput = override.RawInput
	}
	return merged
}

func trimACPPermissionToolForCache(tool acpPermissionTool) acpPermissionTool {
	trimmed := acpPermissionTool{
		ToolCallID: tool.ToolCallID,
		Name:       tool.Name,
		Kind:       tool.Kind,
		Title:      tool.Title,
	}
	if len(tool.Args) > 0 {
		trimmed.Args = pickAuthToolFields(tool.Args)
	}
	if len(tool.RawInput) > 0 {
		trimmed.RawInput = pickAuthToolFields(tool.RawInput)
	}
	return trimmed
}

func pickAuthToolFields(src map[string]any) map[string]any {
	var dst map[string]any
	copyString := func(key string) {
		if val, ok := src[key].(string); ok && val != "" {
			if dst == nil {
				dst = make(map[string]any, 4)
			}
			dst[key] = val
		}
	}
	for _, key := range []string{jsonCommandKey, "CommandLine", jsonCmdKey, "variant", "tool_name", "ServerName", "serverName"} {
		copyString(key)
	}
	if nested, ok := src["tool_input"].(map[string]any); ok {
		var nestedDst map[string]any
		for _, key := range []string{jsonCommandKey, jsonCmdKey} {
			if val, ok := nested[key].(string); ok && val != "" {
				if nestedDst == nil {
					nestedDst = make(map[string]any, 2)
				}
				nestedDst[key] = val
			}
		}
		if len(nestedDst) > 0 {
			if dst == nil {
				dst = make(map[string]any, 1)
			}
			dst["tool_input"] = nestedDst
		}
	}
	return dst
}

func acpToolLabel(tool acpPermissionTool) string {
	for _, candidate := range []string{tool.Name, tool.Title, tool.Kind} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return "tool"
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
	tool := req.ToolCall
	if tool.ToolCallID != "" && c.toolCalls != nil {
		if cached, ok := c.toolCalls[tool.ToolCallID]; ok {
			tool = mergeACPPermissionTool(cached, tool)
			delete(c.toolCalls, tool.ToolCallID)
		}
	}
	allow := acpShouldApproveTool(c.provider, c.cfg, tool)
	if !allow {
		c.deniedTools = append(c.deniedTools, acpToolLabel(tool))
	}
	optionID := selectACPPermissionOption(req.Options, allow)
	if optionID == "" {
		if allow {
			c.deniedTools = append(c.deniedTools, acpToolLabel(tool)+" (provider offered no once-only approval option)")
		}
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
	case "edit", "delete", "move", "fetch", "switch_mode":
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
		for _, key := range []string{jsonCommandKey, "CommandLine", jsonCmdKey} {
			if val, ok := m[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
		if nested, ok := m["tool_input"].(map[string]any); ok {
			for _, key := range []string{jsonCommandKey, jsonCmdKey} {
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
