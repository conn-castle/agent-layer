package sync

import (
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
	root := t.TempDir()
	return &config.ProjectConfig{Config: cfg, Env: config.WithBuiltInEnv(map[string]string{}, root), Root: root}
}

func wantBuiltInDispatchInvocation(t *testing.T, command string, args []string, root string) {
	t.Helper()
	if command != "/bin/sh" {
		t.Fatalf("built-in dispatch command = %q, want /bin/sh", command)
	}
	want := []string{"-c", `AL_MCP_WORKING_DIR=$PWD; export AL_MCP_WORKING_DIR; cd "$1" && exec al dispatch mcp-server`, "agent-layer-mcp", root}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("built-in dispatch args = %q, want %q", args, want)
	}
}

// TestClaudeReceivesTheBuiltInDispatchServer proves the generated Claude MCP
// configuration declares the canonical stdio server. Claude has no per-server
// timeout field, so the definition must carry nothing beyond the standard
// stdio keys — Agent Layer must not reach for the client-wide MCP_TOOL_TIMEOUT.
func TestClaudeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatalf("build claude mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("claude mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in claude server = %#v, want an stdio server", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if len(server.Env) != 0 || server.URL != "" {
		t.Fatalf("built-in claude server carries unexpected fields: %#v", server)
	}
}

// TestAntigravityReceivesTheBuiltInDispatchServer proves Antigravity receives
// the same canonical definition using only its documented schema keys; it has
// no tool-timeout field and none may be invented.
func TestAntigravityReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildAntigravityMCPConfig(project)
	if err != nil {
		t.Fatalf("build antigravity mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("antigravity mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if server.ServerURL != "" || len(server.Headers) != 0 {
		t.Fatalf("built-in antigravity server carries unexpected fields: %#v", server)
	}
}

// TestCopilotReceivesTheBuiltInDispatchServer proves a shared-skill consumer
// cannot be left with Agent Dispatch instructions but no matching MCP tools.
func TestCopilotReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildCopilotMCPConfig(project)
	if err != nil {
		t.Fatalf("build copilot mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("copilot mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in copilot server = %#v", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if len(server.Tools) != 1 || server.Tools[0] != "*" {
		t.Fatalf("built-in copilot server tools = %v, want [\"*\"]", server.Tools)
	}
}

// TestVSCodeReceivesTheBuiltInDispatchServer proves the editor's shared-skill
// projection and MCP configuration expose one coherent Agent Dispatch surface.
func TestVSCodeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildVSCodeMCPConfig(project)
	if err != nil {
		t.Fatalf("build vscode mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("vscode mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in vscode server = %#v", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
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
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if server.ToolTimeoutSec != minutes*60 {
		t.Fatalf("codex tool_timeout_sec = %d, want %d", server.ToolTimeoutSec, minutes*60)
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
