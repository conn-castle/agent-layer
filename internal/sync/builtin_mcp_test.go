package sync

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

func builtInProject(t *testing.T) *config.ProjectConfig {
	t.Helper()
	on := true
	cfg := config.Config{}
	cfg.Agents.Claude.Enabled = &on
	cfg.Agents.Codex.Enabled = &on
	cfg.Agents.Antigravity.Enabled = &on
	cfg.Agents.CopilotCLI.Enabled = &on
	cfg.Agents.VSCode.Enabled = &on
	return &config.ProjectConfig{Config: cfg, Env: map[string]string{}, Root: t.TempDir()}
}

// TestClaudeReceivesTheBuiltInDispatchServer proves the generated Claude MCP
// configuration declares the canonical stdio server. Claude has no per-server
// timeout field, so the definition must carry nothing beyond the standard
// stdio keys — Agent Layer must not reach for the client-wide MCP_TOOL_TIMEOUT.
func TestClaudeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	cfg, err := buildMCPConfig(builtInProject(t))
	if err != nil {
		t.Fatalf("build claude mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("claude mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio || server.Command != "al" {
		t.Fatalf("built-in claude server = %#v, want an stdio `al` server", server)
	}
	if strings.Join(server.Args, " ") != "dispatch mcp-server" {
		t.Fatalf("built-in claude server args = %v", server.Args)
	}
	if len(server.Env) != 0 || server.URL != "" {
		t.Fatalf("built-in claude server carries unexpected fields: %#v", server)
	}
}

// TestAntigravityReceivesTheBuiltInDispatchServer proves Antigravity receives
// the same canonical definition using only its documented schema keys; it has
// no tool-timeout field and none may be invented.
func TestAntigravityReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	cfg, err := buildAntigravityMCPConfig(builtInProject(t))
	if err != nil {
		t.Fatalf("build antigravity mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("antigravity mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Command != "al" || strings.Join(server.Args, " ") != "dispatch mcp-server" {
		t.Fatalf("built-in antigravity server = %#v", server)
	}
	if server.ServerURL != "" || len(server.Headers) != 0 {
		t.Fatalf("built-in antigravity server carries unexpected fields: %#v", server)
	}
}

// TestCopilotReceivesTheBuiltInDispatchServer proves a shared-skill consumer
// cannot be left with Agent Dispatch instructions but no matching MCP tools.
func TestCopilotReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	cfg, err := buildCopilotMCPConfig(builtInProject(t))
	if err != nil {
		t.Fatalf("build copilot mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("copilot mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio || server.Command != "al" ||
		strings.Join(server.Args, " ") != "dispatch mcp-server" {
		t.Fatalf("built-in copilot server = %#v", server)
	}
	if len(server.Tools) != 1 || server.Tools[0] != "*" {
		t.Fatalf("built-in copilot server tools = %v, want [\"*\"]", server.Tools)
	}
}

// TestVSCodeReceivesTheBuiltInDispatchServer proves the editor's shared-skill
// projection and MCP configuration expose one coherent Agent Dispatch surface.
func TestVSCodeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	cfg, err := buildVSCodeMCPConfig(builtInProject(t))
	if err != nil {
		t.Fatalf("build vscode mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("vscode mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio || server.Command != "al" ||
		strings.Join(server.Args, " ") != "dispatch mcp-server" {
		t.Fatalf("built-in vscode server = %#v", server)
	}
}

// TestCodexReceivesTheBuiltInDispatchServerWithItsHardTimeout proves Codex's
// native per-server tool timeout is projected from the canonical configuration
// value, parsed structurally rather than matched as file text.
func TestCodexReceivesTheBuiltInDispatchServerWithItsHardTimeout(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	minutes := 12
	project.Config.Dispatch.MCPWaitTimeoutMinutes = new(int)
	*project.Config.Dispatch.MCPWaitTimeoutMinutes = 5
	project.Config.Dispatch.MCPToolTimeoutMinutes = &minutes

	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		Features struct {
			CodeMode struct {
				DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
			} `toml:"code_mode"`
		} `toml:"features"`
		MCPServers map[string]struct {
			Command        string   `toml:"command"`
			Args           []string `toml:"args"`
			ToolTimeoutSec int      `toml:"tool_timeout_sec"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v\n%s", err, managed.Content)
	}
	server, ok := decoded.MCPServers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("codex config is missing %q: %#v", projection.BuiltInDispatchServerID, decoded.MCPServers)
	}
	if server.Command != "al" || strings.Join(server.Args, " ") != "dispatch mcp-server" {
		t.Fatalf("built-in codex server = %#v", server)
	}
	if server.ToolTimeoutSec != minutes*60 {
		t.Fatalf("codex tool_timeout_sec = %d, want %d", server.ToolTimeoutSec, minutes*60)
	}
	namespaces := decoded.Features.CodeMode.DirectOnlyToolNamespaces
	if len(namespaces) != 1 || namespaces[0] != codexAgentLayerToolNamespace {
		t.Fatalf("Codex direct-only tool namespaces = %v, want [%q]", namespaces, codexAgentLayerToolNamespace)
	}
}

// TestCodexPreservesUserDirectToolNamespaces proves Agent Layer adds its
// namespace without replacing or duplicating a user's Codex-native settings.
func TestCodexPreservesUserDirectToolNamespaces(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	project.Config.Agents.Codex.AgentSpecific = map[string]any{
		"features": map[string]any{
			"code_mode": map[string]any{
				"direct_only_tool_namespaces": []any{"mcp__history", codexAgentLayerToolNamespace},
			},
		},
	}

	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		Features struct {
			CodeMode struct {
				DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
			} `toml:"code_mode"`
		} `toml:"features"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v\n%s", err, managed.Content)
	}
	namespaces := decoded.Features.CodeMode.DirectOnlyToolNamespaces
	if len(namespaces) != 2 || namespaces[0] != "mcp__history" || namespaces[1] != codexAgentLayerToolNamespace {
		t.Fatalf("Codex direct-only tool namespaces = %v, want preserved user namespace plus Agent Layer", namespaces)
	}
}

// TestCodexRemovesAgentLayerDirectNamespaceWhenDispatchIsNotProjected proves
// switching to a VS Code-only projection cannot leave a stale Codex routing
// override in the shared project config.
func TestCodexRemovesAgentLayerDirectNamespaceWhenDispatchIsNotProjected(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	withDispatch, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build Codex config with dispatch: %v", err)
	}

	off := false
	project.Config.Agents.Codex.Enabled = &off
	withoutDispatch, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, false)
	if err != nil {
		t.Fatalf("build VS Code-only Codex config: %v", err)
	}
	merged, err := mergeCodexConfig(filepath.Join(project.Root, ".codex", "config.toml"), withDispatch.Content, withoutDispatch)
	if err != nil {
		t.Fatalf("merge VS Code-only Codex config: %v", err)
	}

	var decoded map[string]any
	if err := toml.Unmarshal([]byte(merged), &decoded); err != nil {
		t.Fatalf("parse merged Codex config: %v\n%s", err, merged)
	}
	if _, ok := valueAtPath(decoded, []string{codexFeaturesKey, codexCodeModeKey, codexDirectOnlyToolNamespacesKey}); ok {
		t.Fatalf("stale Agent Layer direct-only namespace remains after dispatch removal:\n%s", merged)
	}
}

// TestUserServersNeverReceiveTheDispatchToolTimeout proves the per-server
// timeout stays scoped to the built-in server: a user's stdio server keeps the
// client default.
func TestUserServersNeverReceiveTheDispatchToolTimeout(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	on := true
	project.Config.MCP.Servers = []config.MCPServer{
		{ID: "user", Enabled: &on, Transport: config.TransportStdio, Command: "user-server"},
	}
	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		MCPServers map[string]struct {
			ToolTimeoutSec int `toml:"tool_timeout_sec"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v", err)
	}
	if decoded.MCPServers["user"].ToolTimeoutSec != 0 {
		t.Fatalf("user server received tool_timeout_sec = %d, want none",
			decoded.MCPServers["user"].ToolTimeoutSec)
	}
}
