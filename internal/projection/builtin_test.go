package projection

import (
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func dispatchCallerConfig(enabled ...string) config.Config {
	on := true
	cfg := config.Config{}
	for _, client := range enabled {
		switch client {
		case ClientCodex:
			cfg.Agents.Codex.Enabled = &on
		case ClientClaude:
			cfg.Agents.Claude.Enabled = &on
		case ClientAntigravity:
			cfg.Agents.Antigravity.Enabled = &on
		case ClientCopilot:
			cfg.Agents.CopilotCLI.Enabled = &on
		case ClientVSCode:
			cfg.Agents.VSCode.Enabled = &on
		}
	}
	return cfg
}

// TestBuiltInDispatchServerReachesEveryEnabledCaller proves every client that
// consumes the shared Agent Dispatch skill receives the MCP tools it requires,
// while disabling a caller removes its built-in server.
func TestBuiltInDispatchServerReachesEveryEnabledCaller(t *testing.T) {
	clients := []string{ClientCodex, ClientClaude, ClientAntigravity, ClientCopilot, ClientVSCode}
	cfg := dispatchCallerConfig(clients...)
	for _, client := range clients {
		if _, ok := BuiltInDispatchServer(cfg, client); !ok {
			t.Fatalf("enabled caller %q did not receive the built-in server", client)
		}
	}
	if _, ok := BuiltInDispatchServer(config.Config{}, ClientCodex); ok {
		t.Fatal("a disabled caller must not receive the built-in server")
	}
}

// TestBuiltInDispatchServerFollowsTheClaudeVSCodeSurface proves Claude's shared
// project MCP configuration keeps working when only the Visual Studio Code
// surface is enabled.
func TestBuiltInDispatchServerFollowsTheClaudeVSCodeSurface(t *testing.T) {
	on := true
	cfg := config.Config{}
	cfg.Agents.ClaudeVSCode.Enabled = &on
	if _, ok := BuiltInDispatchServer(cfg, ClientClaude); !ok {
		t.Fatal("claude_vscode-only config did not receive the built-in server")
	}
}

// TestBuiltInDispatchServerCarriesTheConfiguredHardTimeout proves the
// configured minute setting is what clients with a per-server timeout project.
func TestBuiltInDispatchServerCarriesTheConfiguredHardTimeout(t *testing.T) {
	cfg := dispatchCallerConfig(ClientCodex)
	server, ok := BuiltInDispatchServer(cfg, ClientCodex)
	if !ok {
		t.Fatal("expected the built-in server")
	}
	if server.ToolTimeoutSeconds != 40*60 {
		t.Fatalf("default tool timeout = %ds, want %ds", server.ToolTimeoutSeconds, 40*60)
	}
	if server.Command != "al" || len(server.Args) != 2 || server.Args[0] != "dispatch" || server.Args[1] != "mcp-server" {
		t.Fatalf("built-in invocation = %q %v, want al dispatch mcp-server", server.Command, server.Args)
	}
	minutes := 55
	cfg.Dispatch.MCPToolTimeoutMinutes = &minutes
	server, _ = BuiltInDispatchServer(cfg, ClientCodex)
	if server.ToolTimeoutSeconds != 55*60 {
		t.Fatalf("configured tool timeout = %ds, want %ds", server.ToolTimeoutSeconds, 55*60)
	}
}

// TestEffectiveServersKeepUserServersIntact proves adding the derived server
// neither replaces nor reorders user-configured entries away from their ID
// ordering, and that the reserved ID appears exactly once.
func TestEffectiveServersKeepUserServersIntact(t *testing.T) {
	on := true
	cfg := dispatchCallerConfig(ClientClaude)
	cfg.MCP.Servers = []config.MCPServer{
		{ID: "zeta", Enabled: &on, Transport: config.TransportStdio, Command: "zeta-server"},
		{ID: "alpha", Enabled: &on, Transport: config.TransportStdio, Command: "alpha-server"},
		{ID: "disabled", Enabled: new(bool), Transport: config.TransportStdio, Command: "nope"},
	}
	resolved, err := EffectiveMCPServers(cfg, map[string]string{}, ClientClaude, nil)
	if err != nil {
		t.Fatalf("resolve effective servers: %v", err)
	}
	var ids []string
	for _, server := range resolved {
		ids = append(ids, server.ID)
	}
	want := []string{BuiltInDispatchServerID, "alpha", "zeta"}
	if len(ids) != len(want) {
		t.Fatalf("effective server ids = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("effective server ids = %v, want %v", ids, want)
		}
	}
	listed := EffectiveServerIDs(cfg, ClientClaude)
	if len(listed) != len(want) {
		t.Fatalf("effective server ids for permissions = %v, want %v", listed, want)
	}
}

// TestEffectiveEnabledServersCountTheBuiltInServerOnce proves warning and
// doctor accounting measures the dispatch tools without double counting them
// when several callers are enabled.
func TestEffectiveEnabledServersCountTheBuiltInServerOnce(t *testing.T) {
	cfg := dispatchCallerConfig(ClientCodex, ClientClaude, ClientAntigravity)
	resolved, err := ResolveEffectiveEnabledMCPServers(cfg, map[string]string{})
	if err != nil {
		t.Fatalf("resolve effective enabled servers: %v", err)
	}
	count := 0
	for _, server := range resolved {
		if server.ID == BuiltInDispatchServerID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("built-in server appeared %d times, want exactly once", count)
	}
	if resolved, err = ResolveEffectiveEnabledMCPServers(config.Config{}, map[string]string{}); err != nil || len(resolved) != 0 {
		t.Fatalf("no enabled caller must yield no servers, got %v (err=%v)", resolved, err)
	}
}
