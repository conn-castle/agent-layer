package projection

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestEffectiveDispatchServerCarriesRepoRoot proves generated MCP invocations
// do not depend on a client's choice of server working directory. The root is
// what lets the global shim find and honor this repository's version pin.
func TestEffectiveDispatchServerCarriesRepoRoot(t *testing.T) {
	cfg := dispatchCallerConfig(ClientClaude)
	root := "/workspace/older-pinned-repo"
	resolved, err := EffectiveMCPServers(cfg, map[string]string{
		config.BuiltinRepoRootEnvVar: root,
	}, ClientClaude, nil)
	if err != nil {
		t.Fatalf("resolve effective servers: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("effective servers = %#v, want one built-in server", resolved)
	}
	if resolved[0].Command != "/bin/sh" {
		t.Fatalf("built-in command = %q, want /bin/sh", resolved[0].Command)
	}
	want := []string{"-c", `AL_MCP_WORKING_DIR=$PWD; export AL_MCP_WORKING_DIR; cd "$1" && exec al dispatch mcp-server`, "agent-layer-mcp", root}
	if len(resolved[0].Args) != len(want) {
		t.Fatalf("built-in args = %q, want %q", resolved[0].Args, want)
	}
	for i := range want {
		if resolved[0].Args[i] != want[i] {
			t.Fatalf("built-in args = %q, want %q", resolved[0].Args, want)
		}
	}
}

// TestEffectiveDispatchInvocationSelectsRepoBeforeAl proves the wrapper's
// observable boundary: the unchanged legacy al arguments run from the pinned
// repository even when the MCP client starts elsewhere.
func TestEffectiveDispatchInvocationSelectsRepoBeforeAl(t *testing.T) {
	cfg := dispatchCallerConfig(ClientClaude)
	root := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	fakeAL := filepath.Join(binDir, "al")
	if err := os.WriteFile(fakeAL, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$AL_MCP_WORKING_DIR\" \"$@\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fakeAL, 0o700); err != nil { // #nosec G302 -- test-owned executable fixture.
		t.Fatal(err)
	}
	resolved, err := EffectiveMCPServers(cfg, map[string]string{
		config.BuiltinRepoRootEnvVar: root,
	}, ClientClaude, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(resolved[0].Command, resolved[0].Args...) // #nosec G204 -- command and arguments are the fixed built-in invocation under test.
	originalWorkingDir := t.TempDir()
	cmd.Dir = originalWorkingDir
	cmd.Env = []string{"PATH=" + binDir + ":/usr/bin:/bin"}
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run built-in invocation: %v", err)
	}
	physicalWorkingDir, err := filepath.EvalSymlinks(originalWorkingDir)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{root, physicalWorkingDir, "dispatch", "mcp-server", ""}, "\n")
	if string(output) != want {
		t.Fatalf("built-in invocation output = %q, want %q", output, want)
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

func TestEffectiveServersDoNotDuplicateReservedID(t *testing.T) {
	on := true
	cfg := dispatchCallerConfig(ClientCodex)
	cfg.MCP.Servers = []config.MCPServer{{
		ID:        BuiltInDispatchServerID,
		Enabled:   &on,
		Transport: config.TransportStdio,
		Command:   "configured-server",
	}}

	resolved, err := EffectiveMCPServers(cfg, map[string]string{}, ClientCodex, nil)
	if err != nil {
		t.Fatalf("resolve effective servers: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != BuiltInDispatchServerID {
		t.Fatalf("effective servers = %#v, want one reserved ID", resolved)
	}
	if ids := EffectiveServerIDs(cfg, ClientCodex); len(ids) != 1 || ids[0] != BuiltInDispatchServerID {
		t.Fatalf("effective server IDs = %v, want one reserved ID", ids)
	}

	resolved, err = ResolveEffectiveEnabledMCPServers(cfg, map[string]string{})
	if err != nil {
		t.Fatalf("resolve effective enabled servers: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != BuiltInDispatchServerID {
		t.Fatalf("effective enabled servers = %#v, want one reserved ID", resolved)
	}
	if ids := EffectiveEnabledServerIDs(cfg); len(ids) != 1 || ids[0] != BuiltInDispatchServerID {
		t.Fatalf("effective enabled server IDs = %v, want one reserved ID", ids)
	}
}
