package projection

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
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
	clearLauncherEnv(t)
	installedDir := t.TempDir()
	installed := writeExecutable(t, filepath.Join(installedDir, "al"), "#!/bin/sh\n")
	t.Setenv("PATH", installedDir)
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
	want := []string{"-c", `AL_MCP_WORKING_DIR=$PWD; export AL_MCP_WORKING_DIR; cd "$1" || exit; if [ -n "$2" ]; then AL_DEV_EXECUTABLE=$2; AL_DEV_BYPASS_VERSION_DISPATCH=1; AL_DEV_SOURCE_ROOT=$1; export AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; exec "$2" dispatch mcp-server; fi; unset AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; if [ -x "$3" ]; then case ":$PATH:" in *":$4:"*) ;; "::") PATH=$4 ;; *) PATH=$PATH:$4 ;; esac; AL_INSTALLED_EXECUTABLE=$3; export PATH AL_INSTALLED_EXECUTABLE; exec "$3" dispatch mcp-server; fi; exec al dispatch mcp-server`, "agent-layer-mcp", root, versiondispatch.DevelopmentExecutable(root), installed, installedDir}
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
//
// No al is on the generating PATH, so nothing is recorded and the launcher
// keeps resolving al through the client's PATH.
func TestEffectiveDispatchInvocationSelectsRepoBeforeAl(t *testing.T) {
	clearLauncherEnv(t)
	t.Setenv("PATH", "")
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

func TestDispatchLauncherDevelopmentExecutableSelection(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	for name, marker := range map[string]string{"al": "installed", "source al": "development"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '"+marker+"\\n'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		} // #nosec G302 -- test-owned executable.
	}
	for _, tc := range []struct{ name, bypass, executable, want string }{
		{"development", "1", filepath.Join(bin, "source al"), "development\n"},
		{"ordinary ignores stale executable", "", filepath.Join(bin, "source al"), "installed\n"},
		{"direct development client uses PATH", "1", "", "installed\n"},
		{"missing explicit executable fails", "1", filepath.Join(bin, "missing"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(versiondispatch.EnvInstalledExecutable, "")
			t.Setenv("PATH", bin)
			t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", tc.bypass)
			t.Setenv("AL_DEV_EXECUTABLE", tc.executable)
			t.Setenv("AL_DEV_SOURCE_ROOT", root)
			server, _ := RootedBuiltInDispatchServer(dispatchCallerConfig(ClientClaude), ClientClaude, root)
			cmd := exec.Command(server.Command, server.Args...) // #nosec G204 -- generated launcher under test.
			cmd.Env = []string{"PATH=" + bin}
			out, err := cmd.Output()
			if tc.want == "" {
				if err == nil {
					t.Fatal("missing development executable must fail")
				}
				return
			}
			if err != nil || string(out) != tc.want {
				t.Fatalf("output %q, error %v; want %q", out, err, tc.want)
			}
		})
	}
}

// TestReceivedMCPServersFollowSyncClientGating proves doctor and warning
// accounting see a user server only when sync writes it into the generated MCP
// config of at least one enabled client.
func TestReceivedMCPServersFollowSyncClientGating(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name    string
		clients []string
		enabled *bool
		agents  func(*config.AgentsConfig)
		want    bool
	}{
		{name: "no enabled client", want: false},
		{name: "unrestricted server", agents: func(a *config.AgentsConfig) { a.Grok.Enabled = &on }, want: true},
		{name: "disabled server", enabled: &off, agents: func(a *config.AgentsConfig) { a.Claude.Enabled = &on }, want: false},
		{name: "limited to a disabled client", clients: []string{ClientCodex}, agents: func(a *config.AgentsConfig) { a.Claude.Enabled = &on }, want: false},
		{name: "codex config written for VS Code", clients: []string{ClientCodex}, agents: func(a *config.AgentsConfig) { a.VSCode.Enabled = &on }, want: true},
		{name: "codex mcp_servers passthrough replaces projected servers", clients: []string{ClientCodex}, agents: func(a *config.AgentsConfig) {
			a.Codex.Enabled = &on
			a.Codex.AgentSpecific = config.ProviderPassthrough{config.CodexMCPServersKey: map[string]any{}}
		}, want: false},
		{name: "claude via the Claude VS Code surface", clients: []string{ClientClaude}, agents: func(a *config.AgentsConfig) { a.ClaudeVSCode.Enabled = &on }, want: true},
		{name: "vscode is not written for Claude VS Code", clients: []string{ClientVSCode}, agents: func(a *config.AgentsConfig) { a.ClaudeVSCode.Enabled = &on }, want: false},
		{name: "muse", clients: []string{ClientMuse}, agents: func(a *config.AgentsConfig) { a.Muse.Enabled = &on }, want: true},
		{name: "vscode", clients: []string{ClientVSCode}, agents: func(a *config.AgentsConfig) { a.VSCode.Enabled = &on }, want: true},
		{name: "copilot", clients: []string{ClientCopilot}, agents: func(a *config.AgentsConfig) { a.CopilotCLI.Enabled = &on }, want: true},
		{name: "grok", clients: []string{ClientGrok}, agents: func(a *config.AgentsConfig) { a.Grok.Enabled = &on }, want: true},
		{name: "codex mcp_servers passthrough also applies for VS Code", clients: []string{ClientCodex}, agents: func(a *config.AgentsConfig) {
			a.VSCode.Enabled = &on
			a.Codex.AgentSpecific = config.ProviderPassthrough{config.CodexMCPServersKey: map[string]any{}}
		}, want: false},
		{name: "one of several clients enabled", clients: []string{ClientCopilot, ClientAntigravity}, agents: func(a *config.AgentsConfig) { a.Antigravity.Enabled = &on }, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := tt.enabled
			if enabled == nil {
				enabled = &on
			}
			cfg := config.Config{MCP: config.MCPConfig{Servers: []config.MCPServer{{
				ID:        "user-server",
				Enabled:   enabled,
				Clients:   tt.clients,
				Transport: config.TransportStdio,
				Command:   "user-server",
			}}}}
			if tt.agents != nil {
				tt.agents(&cfg.Agents)
			}

			received := len(ReceivedMCPServers(cfg)) == 1
			if received != tt.want {
				t.Fatalf("ReceivedMCPServers includes server = %v, want %v", received, tt.want)
			}
			if got := slices.Contains(EffectiveEnabledServerIDs(cfg), "user-server"); got != tt.want {
				t.Fatalf("EffectiveEnabledServerIDs includes server = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEffectiveEnabledServersSkipUnresolvableUnreceivedServer proves a server
// no enabled client receives cannot fail resolution for the servers that are
// received, even when its placeholder has no value.
func TestEffectiveEnabledServersSkipUnresolvableUnreceivedServer(t *testing.T) {
	on := true
	cfg := dispatchCallerConfig(ClientClaude)
	cfg.MCP.Servers = []config.MCPServer{
		{ID: "codex-only", Enabled: &on, Clients: []string{ClientCodex}, Transport: config.TransportHTTP, URL: "https://example.com/mcp?token=${AL_UNSET_TOKEN}"},
		{ID: "shared", Enabled: &on, Transport: config.TransportStdio, Command: "shared"},
	}

	resolved, err := ResolveEffectiveEnabledMCPServers(cfg, map[string]string{})
	if err != nil {
		t.Fatalf("resolve effective enabled servers: %v", err)
	}
	var ids []string
	for _, server := range resolved {
		ids = append(ids, server.ID)
	}
	if strings.Join(ids, ",") != BuiltInDispatchServerID+",shared" {
		t.Fatalf("resolved server IDs = %v, want [%s shared]", ids, BuiltInDispatchServerID)
	}
}

func TestEffectiveEnabledServerIDs(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name        string
		servers     []config.MCPServer
		enableCodex bool
		enableGrok  bool
		want        []string
	}{
		{
			name:    "empty",
			servers: nil,
			want:    []string{},
		},
		{
			name: "omits servers no enabled client receives",
			servers: []config.MCPServer{
				{ID: "server-a", Enabled: &enabled},
			},
			want: []string{},
		},
		{
			name:        "preserves order and includes built-in dispatch server",
			enableCodex: true,
			servers: []config.MCPServer{
				{ID: "server-a", Enabled: &enabled},
				{ID: "server-b", Enabled: &enabled},
				{ID: "server-c", Enabled: &disabled},
				{ID: "server-d"},
				{ID: "server-e", Enabled: &enabled, Clients: []string{"grok"}},
			},
			want: []string{"server-a", "server-b", BuiltInDispatchServerID},
		},
		{
			name:       "includes built-in for another dispatch client",
			enableGrok: true,
			want:       []string{BuiltInDispatchServerID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{MCP: config.MCPConfig{Servers: tt.servers}}
			if tt.enableCodex {
				cfg.Agents.Codex.Enabled = &enabled
			}
			if tt.enableGrok {
				cfg.Agents.Grok.Enabled = &enabled
			}
			if got := EffectiveEnabledServerIDs(cfg); !slices.Equal(got, tt.want) {
				t.Fatalf("EffectiveEnabledServerIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEveryProjectedDevelopmentLauncherWorksWithoutParentSelection(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo $() `false` ; space")
	devDir := filepath.Join(base, "dev $() `false` ; space")
	releaseDir := filepath.Join(base, "release")
	for _, dir := range []string{filepath.Join(root, ".agent-layer"), devDir, releaseDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Re-exec the test helper as a source binary, including the production
	// ConfigureDevelopmentEnvironment used by al before its nested command.
	source := filepath.Join(devDir, "al")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "al"), []byte("#!/bin/sh\n[ -z \"$AL_DEV_EXECUTABLE$AL_DEV_BYPASS_VERSION_DISPATCH$AL_DEV_SOURCE_ROOT\" ] || exit 2\nprintf release\n"), 0700); err != nil { // #nosec G304 G306 -- test-owned synthetic fixture paths and executables.
		t.Fatal(err)
	}
	clearLauncherEnv(t)
	t.Setenv("PATH", releaseDir)
	t.Setenv("AL_DEV_EXECUTABLE", source)
	t.Setenv("AL_DEV_SOURCE_ROOT", root)
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	on := true
	cfg := dispatchCallerConfig(ClientClaude, ClientCodex, ClientAntigravity, ClientCopilot, ClientVSCode)
	cfg.Agents.Grok.Enabled = &on
	cfg.Agents.Muse.Enabled = &on
	for _, client := range mcpClients {
		server, ok := RootedBuiltInDispatchServer(cfg, client, root)
		if !ok {
			t.Fatalf("missing %s", client)
		}
		for _, arg := range server.Args {
			if strings.Contains(arg, "${") {
				t.Fatalf("%s client-interpretable argv: %s", client, arg)
			}
		}
		cmd := exec.Command(server.Command, server.Args...)
		cmd.Env = []string{"PATH=" + releaseDir, "AL_TEST_PROJECTED_SOURCE=1"}
		out, err := cmd.Output()
		if err != nil || string(out) != "source" {
			t.Fatalf("%s: %q %v", client, out, err)
		}
	}
	// A deliberate release projection drops the development selection, even
	// when the host itself later inherits an unrelated development environment.
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "")
	server, _ := RootedBuiltInDispatchServer(cfg, ClientCodex, root)
	for _, arg := range server.Args {
		if strings.Contains(arg, "${") {
			t.Fatalf("release client-interpretable argv: %s", arg)
		}
	}
	cmd := exec.Command(server.Command, server.Args...)
	cmd.Env = []string{"PATH=" + releaseDir, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_DEV_EXECUTABLE=" + source, "AL_DEV_SOURCE_ROOT=" + root}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "release" {
		t.Fatalf("release projection: %q %v", out, err)
	}
}

func clearLauncherEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		versiondispatch.EnvInstalledExecutable, versiondispatch.EnvShimActive,
		versiondispatch.EnvDevelopmentExecutable, versiondispatch.EnvDevelopmentBypassVersionDispatch, versiondispatch.EnvDevelopmentSourceRoot,
	} {
		t.Setenv(name, "")
	}
}

func writeExecutable(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test-owned executable fixture.
		t.Fatal(err)
	}
	return path
}

// TestReleaseLauncherRunsRecordedEntryWithoutClientPath reproduces a GUI
// client whose PATH lacks the al install directory (#513). The launcher runs
// the entry sync resolved, appends its directory once so nested al calls find
// the same entry, and exports it for in-server regeneration.
func TestReleaseLauncherRunsRecordedEntryWithoutClientPath(t *testing.T) {
	clearLauncherEnv(t)
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(base, "install $() `false` ; space")
	installed := writeExecutable(t, filepath.Join(installDir, "al"), "#!/bin/sh\nprintf '%s\\n' \"$PATH\" \"$PWD\" \"$AL_INSTALLED_EXECUTABLE\" \"$@\"\n")
	t.Setenv("PATH", installDir)
	server, _ := RootedBuiltInDispatchServer(dispatchCallerConfig(ClientCodex), ClientCodex, root)
	for _, arg := range server.Args {
		if strings.Contains(arg, "${") {
			t.Fatalf("client-interpretable argv: %s", arg)
		}
	}
	for _, tc := range []struct{ clientPath, wantPath string }{
		{"/usr/bin:/bin", "/usr/bin:/bin:" + installDir},
		{"/usr/bin:" + installDir + ":/bin", "/usr/bin:" + installDir + ":/bin"},
		{"", installDir},
	} {
		cmd := exec.Command(server.Command, server.Args...) // #nosec G204 -- generated launcher under test.
		cmd.Dir = base
		cmd.Env = []string{"PATH=" + tc.clientPath}
		out, err := cmd.Output()
		want := strings.Join([]string{tc.wantPath, root, installed, "dispatch", "mcp-server", ""}, "\n")
		if err != nil || string(out) != want {
			t.Fatalf("client PATH %q: output %q, error %v; want %q", tc.clientPath, out, err, want)
		}
	}
}

// TestReleaseLauncherFallsBackWhenRecordedEntryIsGone proves a moved or
// uninstalled al degrades to today's PATH lookup instead of failing outright.
func TestReleaseLauncherFallsBackWhenRecordedEntryIsGone(t *testing.T) {
	clearLauncherEnv(t)
	root := t.TempDir()
	installed := writeExecutable(t, filepath.Join(t.TempDir(), "al"), "#!/bin/sh\nprintf recorded\n")
	fallbackDir := t.TempDir()
	writeExecutable(t, filepath.Join(fallbackDir, "al"), "#!/bin/sh\nprintf fallback\n")
	t.Setenv("PATH", filepath.Dir(installed))
	server, _ := RootedBuiltInDispatchServer(dispatchCallerConfig(ClientCodex), ClientCodex, root)
	run := func() string {
		cmd := exec.Command(server.Command, server.Args...) // #nosec G204 -- generated launcher under test.
		cmd.Env = []string{"PATH=" + fallbackDir}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("launcher: %v", err)
		}
		return string(out)
	}
	if got := run(); got != "recorded" {
		t.Fatalf("recorded entry output = %q", got)
	}
	if err := os.Chmod(installed, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != "fallback" {
		t.Fatalf("non-executable recorded entry output = %q, want fallback", got)
	}
	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != "fallback" {
		t.Fatalf("missing recorded entry output = %q, want fallback", got)
	}
}

func TestInstalledExecutableSelection(t *testing.T) {
	base := t.TempDir()
	exported := writeExecutable(t, filepath.Join(base, "exported", "al"), "#!/bin/sh\n")
	first := writeExecutable(t, filepath.Join(base, "first", "al"), "#!/bin/sh\n")
	second := writeExecutable(t, filepath.Join(base, "second", "al"), "#!/bin/sh\n")
	invoked := writeExecutable(t, filepath.Join(base, "invoked", "al"), "#!/bin/sh\n")
	wrapped := writeExecutable(t, filepath.Join(base, "wrapped", "al-real"), "#!/bin/sh\n")
	goRun := writeExecutable(t, filepath.Join(base, "go-build123", "b001", "exe", "al"), "#!/bin/sh\n")
	writeExecutable(t, filepath.Join(base, "relative", "al"), "#!/bin/sh\n")
	placeholder := writeExecutable(t, filepath.Join(base, "x${HOME}", "al"), "#!/bin/sh\n")
	if err := os.MkdirAll(filepath.Join(base, "directory", "al"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writeExecutable(t, filepath.Join(base, "plain", "al"), ""), 0o644); err != nil { // #nosec G302 -- test-owned fixture mode under test.
		t.Fatal(err)
	}
	// Executable by group and others but not by this user.
	if err := os.Chmod(writeExecutable(t, filepath.Join(base, "others", "al"), ""), 0o655); err != nil { // #nosec G302 -- test-owned fixture mode under test.
		t.Fatal(err)
	}
	// Package managers such as Homebrew install al as a symlink into a
	// versioned directory; the symlink itself is the stable entry.
	linked := filepath.Join(base, "linked", "al")
	if err := os.MkdirAll(filepath.Dir(linked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, linked); err != nil {
		t.Fatal(err)
	}
	devLink := filepath.Join(base, "dev", "al")
	if err := os.MkdirAll(filepath.Dir(devLink), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, devLink); err != nil {
		t.Fatal(err)
	}
	path := func(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }
	dir := filepath.Dir
	for _, tc := range []struct {
		name, exported, path, dev, argv0, shim, want string
		unprivileged                                 bool
	}{
		{name: "exported entry wins over PATH", exported: exported, path: path(dir(first)), want: exported},
		{name: "exported wrapper target falls back to PATH", exported: wrapped, path: path(dir(first)), want: first},
		{name: "stale exported entry falls back to PATH", exported: filepath.Join(base, "gone", "al"), path: path(dir(first)), want: first},
		{name: "first PATH entry", path: path(dir(first), dir(second)), want: first},
		{name: "relative and empty PATH entries skipped", path: path("", ".", "relative", dir(second)), want: second},
		{name: "development executable skipped", path: path(dir(first), dir(second)), dev: first, want: second},
		{name: "development executable symlink skipped", path: path(dir(first), dir(second)), dev: devLink, want: second},
		{name: "exported development executable skipped", exported: first, path: path(dir(second)), dev: first, want: second},
		{name: "placeholder path rejected", exported: placeholder, path: path(dir(placeholder), dir(second)), want: second},
		{name: "directories and non-executables skipped", path: path(filepath.Join(base, "directory"), filepath.Join(base, "plain"), dir(second)), want: second},
		{name: "entry this user cannot execute skipped", path: path(filepath.Join(base, "others"), dir(second)), want: second, unprivileged: true},
		{name: "symlinked entry recorded unresolved", path: path(dir(linked)), want: linked},
		{name: "absolute invocation path", path: "/nonexistent", argv0: invoked, want: invoked},
		{name: "relative invocation path", argv0: "./invoked/al", want: invoked},
		{name: "bare invocation name ignored", argv0: "al"},
		{name: "other invocation name ignored", argv0: filepath.Join(base, "invoked", "al-other")},
		{name: "version-dispatched invocation ignored", argv0: invoked, shim: "1"},
		{name: "go run invocation ignored", path: "/nonexistent", argv0: goRun},
		{name: "nothing qualifies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unprivileged && os.Geteuid() == 0 {
				t.Skip("root may execute files with any execute bit")
			}
			clearLauncherEnv(t)
			t.Chdir(base)
			t.Setenv(versiondispatch.EnvInstalledExecutable, tc.exported)
			t.Setenv(versiondispatch.EnvDevelopmentExecutable, tc.dev)
			t.Setenv(versiondispatch.EnvShimActive, tc.shim)
			t.Setenv("PATH", tc.path)
			args := os.Args
			t.Cleanup(func() { os.Args = args })
			os.Args = []string{os.Args[0]}
			if tc.argv0 != "" {
				os.Args[0] = tc.argv0
			}
			if got := InstalledExecutable(); got != tc.want {
				t.Fatalf("InstalledExecutable() = %q, want %q", got, tc.want)
			}
		})
	}
}
