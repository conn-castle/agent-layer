package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/musepolicy"
)

func writeMCPConfigFile(t *testing.T, dir string, name string, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// isolateSessionMCPCacheDir redirects os.UserCacheDir (XDG_CACHE_HOME on Linux,
// HOME/Library/Caches on macOS) into a test-owned temporary directory so tests
// never leave durable session MCP specs or Muse grants in the user's cache dir.
func isolateSessionMCPCacheDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	return home
}

func supportedVersionsLookup(_ string, agent string) (string, error) {
	return supportedProviderVersions[agent], nil
}

func sessionMCPFakeWorkerLauncher(t *testing.T) workerLauncher {
	t.Helper()
	return func(string, string, string) (launchedWorker, error) {
		readPipe, writePipe, err := os.Pipe()
		if err != nil {
			return launchedWorker{}, err
		}
		t.Cleanup(func() {
			_ = readPipe.Close()
			_ = writePipe.Close()
		})
		return launchedWorker{
			gate:          writePipe,
			pid:           os.Getpid(),
			startIdentity: processStartIdentity(os.Getpid()),
		}, nil
	}
}

func TestBuildOptionsReportsSessionMCPServerSupport(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	options, err := BuildOptions(OptionsRequest{
		Root:          root,
		Env:           []string{"AL_NO_NETWORK=1"},
		LookPath:      alwaysFound,
		VersionLookup: supportedVersionsLookup,
	})
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}
	wantSupport := map[string]bool{
		AgentCodex:       true,
		AgentClaude:      true,
		AgentMuse:        true,
		AgentAntigravity: true,
		AgentGrok:        true,
	}
	for _, option := range options.Agents {
		want, ok := wantSupport[option.Agent]
		if !ok {
			t.Fatalf("unexpected agent %q in options", option.Agent)
		}
		delete(wantSupport, option.Agent)
		if option.SessionMCPServers.Supported != want {
			t.Errorf("agent %s session_mcp_servers.supported = %t, want %t", option.Agent, option.SessionMCPServers.Supported, want)
		}
		if !want && strings.TrimSpace(option.SessionMCPServers.Reason) == "" {
			t.Errorf("unsupported agent %s omitted session_mcp_servers.reason", option.Agent)
		}
		if want && option.SessionMCPServers.Reason != "" {
			t.Errorf("supported agent %s had unexpected session_mcp_servers.reason %q", option.Agent, option.SessionMCPServers.Reason)
		}
	}
	if len(wantSupport) != 0 {
		t.Fatalf("BuildOptions omitted expected agents: %v", wantSupport)
	}
}

func TestResolveSessionMCPServersStrictValidation(t *testing.T) {
	dir := t.TempDir()
	validA := writeMCPConfigFile(t, dir, "a.json", `{
		"mcpServers": {
			"z-tool": {"type": "stdio", "command": "/bin/z", "args": ["--a", "${LIT}"], "env": {"PATH": "/custom/bin", "TOK": "v"}},
			"a-tool": {"command": "/bin/a"}
		}
	}`)
	validB := writeMCPConfigFile(t, dir, "b.json", `{"mcpServers": {"m-tool": {"command": "/bin/m"}}}`)

	servers, err := resolveSessionMCPServers([]string{validA, validB}, nil)
	if err != nil {
		t.Fatalf("resolve valid configs: %v", err)
	}
	wantNames := []string{"a-tool", "m-tool", "z-tool"}
	if got := sessionMCPServerNames(servers); !slices.Equal(got, wantNames) {
		t.Fatalf("resolved names = %v, want %v", got, wantNames)
	}
	if servers[2].Env["PATH"] != "/custom/bin" || servers[2].Args[1] != "${LIT}" {
		t.Fatalf("server fields not preserved verbatim: %#v", servers[2])
	}

	invalidCases := []struct {
		name string
		json string
	}{
		{"empty object", `{}`},
		{"empty mcpServers", `{"mcpServers": {}}`},
		{"unknown top-level key", `{"mcp_servers": {"a": {"command": "/bin/a"}}}`},
		{"trailing data", `{"mcpServers": {"a": {"command": "/bin/a"}}} {}`},
		{"duplicate server in file", `{"mcpServers": {"a": {"command": "/bin/a"}, "a": {"command": "/bin/b"}}}`},
		{"duplicate env key", `{"mcpServers": {"a": {"command": "/bin/a", "env": {"K": "1", "K": "2"}}}}`},
		{"http transport rejected", `{"mcpServers": {"a": {"type": "http", "command": "/bin/a"}}}`},
		{"unknown server field", `{"mcpServers": {"a": {"command": "/bin/a", "url": "https://example.com"}}}`},
		{"missing command", `{"mcpServers": {"a": {"args": ["--x"]}}}`},
		{"command with newline", `{"mcpServers": {"a": {"command": "/bin/a\nbad"}}}`},
		{"invalid server name dot", `{"mcpServers": {"a.b": {"command": "/bin/a"}}}`},
		{"invalid server name leading dash", `{"mcpServers": {"-a": {"command": "/bin/a"}}}`},
		{"invalid server name double underscore", `{"mcpServers": {"a__b": {"command": "/bin/a"}}}`},
		{"invalid server name adjacent hyphen underscore", `{"mcpServers": {"a-_b": {"command": "/bin/a"}}}`},
		{"invalid env key", `{"mcpServers": {"a": {"command": "/bin/a", "env": {"BAD-KEY": "1"}}}}`},
		{"nul in env value", `{"mcpServers": {"a": {"command": "/bin/a", "env": {"K": "a\u0000b"}}}}`},
		{"nul in arg", `{"mcpServers": {"a": {"command": "/bin/a", "args": ["a\u0000b"]}}}`},
		{"null type rejected", `{"mcpServers": {"a": {"type": null, "command": "/bin/a"}}}`},
		{"empty type rejected", `{"mcpServers": {"a": {"type": "", "command": "/bin/a"}}}`},
		{"null arg element rejected", `{"mcpServers": {"a": {"command": "/bin/a", "args": [null]}}}`},
		{"null env value rejected", `{"mcpServers": {"a": {"command": "/bin/a", "env": {"K": null}}}}`},
		{"case-aliased top-level key rejected", `{"McpServers": {"a": {"command": "/bin/a"}}}`},
		{"case-aliased server command rejected", `{"mcpServers": {"a": {"Command": "/bin/a"}}}`},
		{"case-aliased server args rejected", `{"mcpServers": {"a": {"command": "/bin/a", "Args": ["--x"]}}}`},
	}
	for _, tc := range invalidCases {
		path := writeMCPConfigFile(t, dir, strings.ReplaceAll(tc.name, " ", "_")+".json", tc.json)
		_, err := resolveSessionMCPServers([]string{path}, nil)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		requireDispatchExitCode(t, err, ExitUsage)
	}

	// Empty path, directory path, and duplicate across files all fail with ExitUsage.
	for _, paths := range [][]string{{""}, {"   "}, {dir}, {validA, validA}} {
		_, err := resolveSessionMCPServers(paths, nil)
		requireDispatchExitCode(t, err, ExitUsage)
	}
}

func TestValidateSessionMCPServersCollisionsAndPolicyConflicts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	enabled := true
	disabled := false
	project.Config.MCP.Servers = []config.MCPServer{
		{ID: "proj-enabled", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/p"},
		{ID: "proj-disabled", Enabled: &disabled, Transport: config.TransportStdio, Command: "/bin/d"},
		{ID: "docs.internal", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/docs", Clients: []string{"claude"}},
		{ID: "muse-srv", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/ms", Clients: []string{"muse"}},
		{ID: "foo_bar", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/fb"},
		{ID: "grok__admin", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/ga"},
	}
	project.Config.Agents.Codex.AgentSpecific = config.ProviderPassthrough{
		config.CodexMCPServersKey: map[string]any{"codex-custom": map[string]any{"command": "/bin/c"}},
	}
	project.Config.Agents.Claude.AgentSpecific = config.ProviderPassthrough{
		claudePermissionsPassthroughKey: map[string]any{"deny": []any{"mcp__denied_srv__*"}},
	}

	codexTarget, _ := lookupTarget(AgentCodex)
	claudeTarget, _ := lookupTarget(AgentClaude)
	museTarget, _ := lookupTarget(AgentMuse)
	grokTarget, _ := lookupTarget(AgentGrok)
	agyTarget, _ := lookupTarget(AgentAntigravity)

	cases := []struct {
		name    string
		target  targetMeta
		servers []SessionMCPServer
		code    int
	}{
		{"collides with agent-layer on grok", grokTarget, []SessionMCPServer{{Name: "agent-layer", Command: "/bin/w"}}, ExitUsage},
		{"collides with agent-layer on antigravity", agyTarget, []SessionMCPServer{{Name: "agent-layer", Command: "/bin/w"}}, ExitUsage},
		{"collides with agent-layer", codexTarget, []SessionMCPServer{{Name: "agent-layer", Command: "/bin/w"}}, ExitUsage},
		{"collides with agent_layer normalized", codexTarget, []SessionMCPServer{{Name: "agent_layer", Command: "/bin/w"}}, ExitUsage},
		{"collides with enabled project server", claudeTarget, []SessionMCPServer{{Name: "proj-enabled", Command: "/bin/w"}}, ExitUsage},
		{"collides with disabled project server", claudeTarget, []SessionMCPServer{{Name: "proj-disabled", Command: "/bin/w"}}, ExitUsage},
		{"collides with codex agent_specific mcp_servers", codexTarget, []SessionMCPServer{{Name: "codex-custom", Command: "/bin/w"}}, ExitUsage},
		{"collides with claude normalized namespace", claudeTarget, []SessionMCPServer{{Name: "docs_internal", Command: "/bin/w"}}, ExitUsage},
		{"collides with muse normalized namespace", museTarget, []SessionMCPServer{{Name: "muse_srv", Command: "/bin/w"}}, ExitUsage},
		{"grok session prefix of project server with double underscore", grokTarget, []SessionMCPServer{{Name: "grok", Command: "/bin/w"}}, ExitUsage},
		{"antigravity session prefix of project server", agyTarget, []SessionMCPServer{{Name: "foo", Command: "/bin/w"}}, ExitUsage},
		{"antigravity project prefix of session server", agyTarget, []SessionMCPServer{{Name: "proj-enabled_extra", Command: "/bin/w"}}, ExitUsage},
		{"antigravity ambiguous session server pair", agyTarget, []SessionMCPServer{{Name: "wf", Command: "/bin/a"}, {Name: "wf_sub", Command: "/bin/b"}}, ExitUsage},
		{"duplicate normalized session servers", museTarget, []SessionMCPServer{{Name: "wf-a", Command: "/bin/a"}, {Name: "wf_a", Command: "/bin/b"}}, ExitUsage},
		{"claude permissions.deny conflict", claudeTarget, []SessionMCPServer{{Name: "denied_srv", Command: "/bin/w"}}, ExitConfig},
	}
	for _, tc := range cases {
		err := validateSessionMCPServersForProject(project, tc.target, tc.servers)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		requireDispatchExitCode(t, err, tc.code)
	}
	for _, target := range []targetMeta{codexTarget, claudeTarget, museTarget, grokTarget, agyTarget} {
		if err := validateSessionMCPServersForProject(project, target, []SessionMCPServer{{Name: "wf", Command: "/bin/w"}}); err != nil {
			t.Errorf("%s valid session MCP server rejected: %v", target.Name, err)
		}
	}
}

func TestBuildProviderCommandSessionMCPDeliveryAcrossApprovalsModes(t *testing.T) {
	origExec := sessionMCPExecutable
	sessionMCPExecutable = func() (string, error) { return "/opt/al-test", nil }
	t.Cleanup(func() { sessionMCPExecutable = origExec })

	servers := []SessionMCPServer{
		{Name: "beta-srv", Command: "/bin/beta", Args: []string{"--id", "2"}, Env: map[string]string{"PATH": "/b/bin"}},
		{Name: "alpha-srv", Command: "/bin/alpha", Args: []string{"--id", "1"}, Env: map[string]string{"TOK": "secret"}},
	}

	for _, mode := range []string{config.ApprovalModeNone, config.ApprovalModeCommands, config.ApprovalModeMCP, config.ApprovalModeAll, config.ApprovalModeYOLO} {
		for _, dispatchMode := range []string{dispatchModeFresh, dispatchModeResume} {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			project := loadApprovalsTestProject(t, root)
			project.Config.Approvals.Mode = mode
			project.CommandsAllow = []string{"git status"}
			enabled := true
			project.Config.MCP.Servers = []config.MCPServer{
				{ID: "proj-mcp", Enabled: &enabled, Transport: config.TransportStdio, Command: "/bin/proj"},
			}

			// Claude: --mcp-config without --strict-mcp-config; session --allowedTools in all non-YOLO modes without adding project MCP in none/commands.
			claudeTarget, _ := lookupTarget(AgentClaude)
			claudeRun, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], dispatchMode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(claudeRun.Dir) })
			claudeCmd, err := buildProviderCommandForSession(claudeTarget, project, root, nil, []byte("hi"), "", "", false, dispatchMode, runtimeSessionID, servers, claudeRun, io.Discard)
			if err != nil {
				t.Fatalf("claude %s/%s: %v", mode, dispatchMode, err)
			}
			claudeRTDir, err := sessionMCPRuntimeDirPath(claudeRun.Dir)
			if err != nil {
				t.Fatal(err)
			}
			wantMCPFile := filepath.Join(claudeRTDir, sessionMCPClaudeFile)
			if !argPairPresent(claudeCmd.Args, "--mcp-config", wantMCPFile) {
				t.Errorf("claude %s/%s missing --mcp-config %s in %v", mode, dispatchMode, wantMCPFile, claudeCmd.Args)
			}
			if _, statErr := os.Stat(filepath.Join(claudeRun.Dir, sessionMCPClaudeFile)); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("claude %s/%s unexpectedly wrote claude-mcp.json inside workspace run dir", mode, dispatchMode)
			}
			if slices.Contains(claudeCmd.Args, "--strict-mcp-config") {
				t.Errorf("claude %s/%s unexpectedly passed --strict-mcp-config", mode, dispatchMode)
			}
			if mode == config.ApprovalModeYOLO {
				if slices.Contains(claudeCmd.Args, "--allowedTools") {
					t.Errorf("claude yolo/%s should not pass --allowedTools: %v", dispatchMode, claudeCmd.Args)
				}
			} else {
				for _, rule := range []string{"mcp__alpha-srv__*", "mcp__beta-srv__*"} {
					if !argPairPresent(claudeCmd.Args, "--allowedTools", rule) {
						t.Errorf("claude %s/%s missing --allowedTools %s in %v", mode, dispatchMode, rule, claudeCmd.Args)
					}
				}
				projectMCPAllowed := mode == config.ApprovalModeMCP || mode == config.ApprovalModeAll
				if got := argPairPresent(claudeCmd.Args, "--allowedTools", "mcp__proj-mcp__*"); got != projectMCPAllowed {
					t.Errorf("claude %s/%s project MCP allow = %t, want %t", mode, dispatchMode, got, projectMCPAllowed)
				}
			}
			// Verify staged Claude MCP config JSON points to wrapper without leaking env secrets.
			claudeJSON, err := os.ReadFile(wantMCPFile) // #nosec G304 -- path is staged in this test's dispatch run directory.
			if err != nil {
				t.Fatalf("read staged claude session-mcp.json: %v", err)
			}
			if strings.Contains(string(claudeJSON), "secret") {
				t.Fatalf("staged claude session-mcp.json leaked caller env value: %s", claudeJSON)
			}

			// Codex: sorted -c mcp_servers.<name>=... with required=true and default_tools_approval_mode="approve".
			codexTarget, _ := lookupTarget(AgentCodex)
			codexRun, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchMode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(codexRun.Dir) })
			codexCmd, err := buildProviderCommandForSession(codexTarget, project, root, nil, []byte("hi"), "", "", false, dispatchMode, runtimeSessionID, servers, codexRun, io.Discard)
			if err != nil {
				t.Fatalf("codex %s/%s: %v", mode, dispatchMode, err)
			}
			// Staged spec must live outside the workspace checkout.
			if _, statErr := os.Stat(filepath.Join(codexRun.Dir, "session-mcp-spec.json")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("codex %s/%s wrote session-mcp-spec.json inside workspace run dir", mode, dispatchMode)
			}
			alphaIdx, betaIdx := -1, -1
			for i := 0; i+1 < len(codexCmd.Args); i++ {
				if codexCmd.Args[i] == "-c" && strings.HasPrefix(codexCmd.Args[i+1], "mcp_servers.alpha-srv=") {
					alphaIdx = i
					if !strings.Contains(codexCmd.Args[i+1], `required=true`) || !strings.Contains(codexCmd.Args[i+1], `default_tools_approval_mode="approve"`) || !strings.Contains(codexCmd.Args[i+1], `"--digest"`) {
						t.Errorf("codex %s/%s malformed alpha-srv arg: %s", mode, dispatchMode, codexCmd.Args[i+1])
					}
				}
				if codexCmd.Args[i] == "-c" && strings.HasPrefix(codexCmd.Args[i+1], "mcp_servers.beta-srv=") {
					betaIdx = i
				}
			}
			if alphaIdx < 0 || betaIdx < 0 {
				t.Errorf("codex %s/%s missing session MCP -c flags in %v", mode, dispatchMode, codexCmd.Args)
			} else if alphaIdx > betaIdx {
				t.Errorf("codex %s/%s session MCP args not sorted deterministically: %v", mode, dispatchMode, codexCmd.Args)
			}
			if codexCmd.Args[len(codexCmd.Args)-1] != "-" {
				t.Errorf("codex %s/%s trailing arg = %q, want '-'", mode, dispatchMode, codexCmd.Args[len(codexCmd.Args)-1])
			}
			if strings.Contains(strings.Join(codexCmd.Args, " "), "secret") {
				t.Errorf("codex %s/%s leaked caller env secret on argv: %v", mode, dispatchMode, codexCmd.Args)
			}

			// Muse: sorted -c mcpServers.<name>=... with mode="required" and without changing approval flags.
			museTarget, _ := lookupTarget(AgentMuse)
			museRun, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchMode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(museRun.Dir) })
			museCmd, err := buildProviderCommandForSession(museTarget, project, root, nil, []byte("hi"), "", "", false, dispatchMode, runtimeSessionID, servers, museRun, io.Discard)
			if err != nil {
				t.Fatalf("muse %s/%s: %v", mode, dispatchMode, err)
			}
			museAlphaIdx, museBetaIdx := -1, -1
			for i := 0; i+1 < len(museCmd.Args); i++ {
				if museCmd.Args[i] == "-c" && strings.HasPrefix(museCmd.Args[i+1], "mcpServers.alpha-srv=") {
					museAlphaIdx = i
					if !strings.Contains(museCmd.Args[i+1], `"mode":"required"`) || !strings.Contains(museCmd.Args[i+1], `"--digest"`) {
						t.Errorf("muse %s/%s missing mode=required or --digest in %s", mode, dispatchMode, museCmd.Args[i+1])
					}
				}
				if museCmd.Args[i] == "-c" && strings.HasPrefix(museCmd.Args[i+1], "mcpServers.beta-srv=") {
					museBetaIdx = i
				}
			}
			if museAlphaIdx < 0 || museBetaIdx < 0 {
				t.Errorf("muse %s/%s missing session MCP -c flags in %v", mode, dispatchMode, museCmd.Args)
			} else if museAlphaIdx > museBetaIdx {
				t.Errorf("muse %s/%s session MCP args not sorted deterministically: %v", mode, dispatchMode, museCmd.Args)
			}

			// Grok & Antigravity: ACP transport with sorted --verify-handshake wrapper specs and zero secrets on wire.
			for _, acpAgent := range []string{AgentGrok, AgentAntigravity} {
				acpTarget, _ := lookupTarget(acpAgent)
				acpRun, err := newDispatchRun(root, acpAgent, supportedProviderVersions[acpAgent], dispatchMode)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(acpRun.Dir) })
				acpCmd, err := buildProviderCommandForSession(acpTarget, project, root, nil, []byte("hi"), "", "", false, dispatchMode, runtimeSessionID, servers, acpRun, io.Discard)
				if err != nil {
					t.Fatalf("%s %s/%s: %v", acpAgent, mode, dispatchMode, err)
				}
				if !acpCmd.UseACP || acpCmd.ACP == nil {
					t.Fatalf("%s %s/%s did not configure ACP launch", acpAgent, mode, dispatchMode)
				}
				if acpAgent == AgentGrok {
					if mode == config.ApprovalModeYOLO {
						if !slices.Contains(acpCmd.Args, "--always-approve") || argPairPresent(acpCmd.Args, "--permission-mode", "default") {
							t.Errorf("grok yolo/%s unexpected permission args: %v", dispatchMode, acpCmd.Args)
						}
					} else if !argPairPresent(acpCmd.Args, "--permission-mode", "default") {
						t.Errorf("grok %s/%s missing --permission-mode default: %v", mode, dispatchMode, acpCmd.Args)
					}
				}
				if len(acpCmd.ACP.MCPServers) != 2 || acpCmd.ACP.MCPServers[0].Name != "alpha-srv" || acpCmd.ACP.MCPServers[1].Name != "beta-srv" {
					t.Errorf("%s %s/%s ACP MCPServers not sorted: %#v", acpAgent, mode, dispatchMode, acpCmd.ACP.MCPServers)
				}
				for _, spec := range acpCmd.ACP.MCPServers {
					if spec.Command != "/opt/al-test" || !slices.Contains(spec.Args, "--verify-handshake") || len(spec.Env) != 0 {
						t.Errorf("%s %s/%s invalid ACP MCP spec %#v", acpAgent, mode, dispatchMode, spec)
					}
				}
			}
		}
	}
}

func TestClaudeSessionMCPNoSilentDegradation(t *testing.T) {
	okInit := `{"type":"system","subtype":"init","session_id":"` + runtimeSessionID + `","mcp_servers":[{"name":"agent-layer","status":"connected"},{"name":"wf-a","status":"connected"},{"name":"wf-b","status":"connected"}]}`
	okResult := `{"type":"result","session_id":"` + runtimeSessionID + `","is_error":false,"result":"done","permission_denials":[]}`

	runStream := func(stream string, expected []string) []providerEvent {
		var events []providerEvent
		err := readStructuredEventsWithSessionMCP(strings.NewReader(stream), io.Discard, AgentClaude, runtimeSessionID, false, expected, func(event providerEvent) error {
			events = append(events, event)
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("readStructuredEventsWithSessionMCP: %v", err)
		}
		return events
	}

	// All connected -> completes cleanly.
	events := runStream(okInit+"\n"+okResult+"\n", []string{"wf-a", "wf-b"})
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventFailure }) {
		t.Fatalf("connected servers failed unexpectedly: %#v", events)
	}
	if !slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("connected servers did not complete: %#v", events)
	}

	// Failed status -> fails immediately with actionable error naming server and status.
	failedInit := `{"type":"system","subtype":"init","session_id":"` + runtimeSessionID + `","mcp_servers":[{"name":"wf-a","status":"connected"},{"name":"wf-b","status":"failed"}]}`
	events = runStream(failedInit+"\n"+okResult+"\n", []string{"wf-a", "wf-b"})
	var failReason string
	for _, e := range events {
		if e.Kind == eventFailure {
			failReason = e.Reason
			break
		}
	}
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("failed server unexpectedly completed: %#v", events)
	}
	if !strings.Contains(failReason, "wf-b") || !strings.Contains(failReason, "failed") {
		t.Fatalf("failed server reason = %q, want mention of wf-b and failed", failReason)
	}

	// Missing server in init -> fails immediately.
	missingInit := `{"type":"system","subtype":"init","session_id":"` + runtimeSessionID + `","mcp_servers":[{"name":"wf-a","status":"connected"}]}`
	events = runStream(missingInit+"\n"+okResult+"\n", []string{"wf-a", "wf-b"})
	failReason = ""
	for _, e := range events {
		if e.Kind == eventFailure {
			failReason = e.Reason
			break
		}
	}
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("missing server unexpectedly completed: %#v", events)
	}
	if !strings.Contains(failReason, "wf-b") || !strings.Contains(failReason, "not initialized") {
		t.Fatalf("missing server reason = %q, want mention of wf-b and not initialized", failReason)
	}

	// Nested array under status must not spoof a scalar connected status.
	nestedStatusInit := `{"type":"system","subtype":"init","session_id":"` + runtimeSessionID + `","mcp_servers":[{"name":"wf-a","status":["connected"]}]}`
	events = runStream(nestedStatusInit+"\n"+okResult+"\n", []string{"wf-a"})
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("nested array status unexpectedly completed: %#v", events)
	}
	if !slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventFailure && strings.Contains(e.Reason, "wf-a") }) {
		t.Fatalf("nested array status did not fail wf-a: %#v", events)
	}

	// Required session MCP server appearing after >256 project servers in Claude init must still be retained.
	var manyServers strings.Builder
	manyServers.WriteString(`{"type":"system","subtype":"init","session_id":"` + runtimeSessionID + `","mcp_servers":[`)
	for i := range claudeMCPServersLimit + 5 {
		if i > 0 {
			manyServers.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&manyServers, `{"name":"proj-%d","status":"connected"}`, i)
	}
	manyServers.WriteString(`,{"name":"wf-late","status":"connected"}]}`)
	events = runStream(manyServers.String()+"\n"+okResult+"\n", []string{"wf-late"})
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventFailure }) ||
		!slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("required session MCP server after index %d was dropped: %#v", claudeMCPServersLimit, events)
	}

	// Result without any init event -> fails instead of silently succeeding.
	events = runStream(okResult+"\n", []string{"wf-a"})
	if slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventComplete }) {
		t.Fatalf("missing init event still completed: %#v", events)
	}
	if !slices.ContainsFunc(events, func(e providerEvent) bool { return e.Kind == eventFailure && strings.Contains(e.Reason, "wf-a") }) {
		t.Fatalf("missing init event did not report wf-a failure: %#v", events)
	}
}

func TestStartReservationAndContinuePersistSessionMCPServers(t *testing.T) {
	isolateSessionMCPCacheDir(t)
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	cfgDir := t.TempDir()
	mcpCfg := writeMCPConfigFile(t, cfgDir, "wf.json", `{
		"mcpServers": {
			"wf-tools": {"command": "/bin/wf", "args": ["--run", "1"], "env": {"PATH": "/wf/bin", "WF_ID": "abc"}}
		}
	}`)

	// 1. Reserve a conversation.
	reservation := reserveInTest(t, root)

	// 2. Starting the reservation with a colliding session MCP config fails BEFORE claiming it.
	badCfg := writeMCPConfigFile(t, cfgDir, "bad.json", `{
		"mcpServers": {
			"agent-layer": {"command": "/bin/wf"}
		}
	}`)
	err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentGrok, Prompt: "Hello",
		Reservation: &reservation.Handle, MCPConfigs: []string{badCfg},
		Env: []string{}, LookPath: alwaysFound, VersionLookup: supportedVersionsLookup,
	})
	requireDispatchExitCode(t, err, ExitUsage)
	stillReserved, err := loadRunRecord(root, reservation.InvocationID)
	if err != nil || stillReserved.State != dispatchStateReserved {
		t.Fatalf("failed pre-launch validation modified reservation: %#v err=%v", stillReserved, err)
	}

	// 3. Starting the reservation with a valid supported agent persists SessionMCPServers.
	var startOut bytes.Buffer
	err = Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "First turn",
		Reservation: &reservation.Handle, MCPConfigs: []string{mcpCfg},
		Stdout: &startOut, Env: []string{}, LookPath: alwaysFound, VersionLookup: supportedVersionsLookup,
		launchWorker: sessionMCPFakeWorkerLauncher(t),
	})
	if err != nil {
		t.Fatalf("reserved Start with --mcp-config: %v", err)
	}
	session, err := loadSession(root, reservation.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := hydrateSessionMCPServers(root, &session); err != nil {
		t.Fatal(err)
	}
	wantServers := []SessionMCPServer{{Name: "wf-tools", Command: "/bin/wf", Args: []string{"--run", "1"}, Env: map[string]string{"PATH": "/wf/bin", "WF_ID": "abc"}}}
	if !reflect.DeepEqual(session.SessionMCPServers, wantServers) {
		t.Fatalf("persisted SessionMCPServers = %#v, want %#v", session.SessionMCPServers, wantServers)
	}

	// 4. Mark invocation failed (with provider session ID + confirmed termination) and Continue:
	// session servers must remain attached and be re-validated on Continue.
	now := time.Now().UTC()
	session.ProviderSessionID = runtimeSessionID
	session.State = sessionStateDurable
	session.ActiveRunID = ""
	session.ActiveClaimKnown = true
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	if _, err := updateRunEvidence(filepathForRun(root, reservation.InvocationID), func(r *RunRecord) error {
		r.State = dispatchStateFailed
		r.CompletedAt = &now
		r.TerminalReason = "simulated failure"
		r.TerminalExitCode = ExitTargetFailure
		r.ProviderSessionID = runtimeSessionID
		r.LaunchFenced = true
		r.TerminationConfirmed = true
		r.TerminationConfirmedAt = &now
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var contOut bytes.Buffer
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: reservation.Handle, Prompt: "Retry turn",
		Stdout: &contOut, Env: []string{}, LookPath: alwaysFound, VersionLookup: supportedVersionsLookup,
		launchWorker: sessionMCPFakeWorkerLauncher(t),
	}); err != nil {
		t.Fatalf("Continue after failure: %v", err)
	}
	afterContinue, err := loadSession(root, reservation.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := hydrateSessionMCPServers(root, &afterContinue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterContinue.SessionMCPServers, wantServers) {
		t.Fatalf("SessionMCPServers changed after Continue: %#v", afterContinue.SessionMCPServers)
	}

	// 4b. Deleting a conversation's private session MCP file must NOT brick
	// repo-wide operations (listSessions, Reserve, Start, expired pruning),
	// while Continue on that damaged conversation still fails closed.
	if err := removePrivateSessionMCPServers(root, afterContinue.SessionMCPID); err != nil {
		t.Fatal(err)
	}
	if sessions, listErr := listSessions(root); listErr != nil || len(sessions) == 0 {
		t.Fatalf("listSessions failed after missing private spec: %v", listErr)
	}
	if err := pruneDispatchEvidence(root, time.Now(), 24*time.Hour); err != nil {
		t.Fatalf("pruneDispatchEvidence failed after missing private spec: %v", err)
	}
	// Mark the continued run terminal so Continue reaches hydrateSessionMCPServers and fails closed.
	var contRes Result
	if err := json.Unmarshal(contOut.Bytes(), &contRes); err != nil {
		t.Fatal(err)
	}
	if _, err := updateRunEvidence(filepathForRun(root, contRes.InvocationID), func(r *RunRecord) error {
		r.State = dispatchStateFailed
		r.CompletedAt = &now
		r.TerminalReason = "simulated failure 2"
		r.TerminalExitCode = ExitTargetFailure
		r.ProviderSessionID = runtimeSessionID
		r.LaunchFenced = true
		r.TerminationConfirmed = true
		r.TerminationConfirmedAt = &now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, RunRecord{Name: reservation.Handle, ID: contRes.InvocationID}); err != nil {
		t.Fatal(err)
	}
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: reservation.Handle, Prompt: "Should fail closed",
		Env: []string{}, LookPath: alwaysFound, VersionLookup: supportedVersionsLookup,
		launchWorker: sessionMCPFakeWorkerLauncher(t),
	}); err == nil {
		t.Fatal("Continue succeeded despite missing private session MCP spec")
	}

	// 5. Verify generated repo projections contain NO session MCP server entries.
	for _, rel := range []string{".mcp.json", filepath.Join(".codex", "config.toml"), filepath.Join(".claude", "settings.json")} {
		data, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- path is within the test repository.
		if err == nil && strings.Contains(string(data), "wf-tools") {
			t.Errorf("repo projection %s was polluted with session MCP server wf-tools", rel)
		}
	}

	// 6. Parallel conversation in the same checkout gets its own independent servers (or none).
	var otherOut bytes.Buffer
	if err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Unrelated conversation",
		Stdout: &otherOut, Env: []string{}, LookPath: alwaysFound, VersionLookup: supportedVersionsLookup,
		launchWorker: sessionMCPFakeWorkerLauncher(t),
	}); err != nil {
		t.Fatalf("parallel Start without session MCP: %v", err)
	}
	var otherResult Result
	if err := json.Unmarshal(otherOut.Bytes(), &otherResult); err != nil {
		t.Fatal(err)
	}
	otherSession, err := loadSession(root, otherResult.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if otherSession.SessionMCPID != "" || otherSession.SessionMCPDigest != "" {
		t.Fatalf("unrelated parallel session inherited SessionMCP metadata: id=%q digest=%q", otherSession.SessionMCPID, otherSession.SessionMCPDigest)
	}
	if err := hydrateSessionMCPServers(root, &otherSession); err != nil {
		t.Fatalf("hydrate unrelated session: %v", err)
	}
	if len(otherSession.SessionMCPServers) != 0 {
		t.Fatalf("unrelated parallel session inherited SessionMCPServers: %#v", otherSession.SessionMCPServers)
	}
}

func TestDispatchEnvironmentStripsSessionMCPEnvForNestedDispatches(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	env := dispatchEnvironment([]string{"KEEP=yes", "AL_SESSION_MCP_0_0=secret"}, project, run, 1)
	if slices.Contains(env, "AL_SESSION_MCP_0_0=secret") {
		t.Fatalf("dispatchEnvironment leaked AL_SESSION_MCP_* into child env: %v", env)
	}
	if !slices.Contains(env, "KEEP=yes") {
		t.Fatalf("dispatchEnvironment dropped unrelated env var: %v", env)
	}
}

func TestRunSessionMCPServerEnvWorkDirAndClosedFence(t *testing.T) {
	runDir := t.TempDir()
	workDir := t.TempDir()
	customBin := t.TempDir()

	// Create an executable inside customBin (NOT on process PATH).
	toolPath := filepath.Join(customBin, "custom-wf-mcp")
	if err := os.WriteFile(toolPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { // #nosec G306 -- synthetic executable test fixture.
		t.Fatal(err)
	}
	staged, err := stageSessionMCPSpec(runDir, workDir, []string{"AMBIENT_CALLER=kept-through-muse", "AL_SESSION_MCP_LEAK=strip-me"}, []SessionMCPServer{{
		Name:    "wf",
		Command: "custom-wf-mcp",
		Args:    []string{"--literal", "${KEEP_ME}"},
		Env:     map[string]string{"PATH": customBin, "CUSTOM_VAR": "val-${LITERAL}"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(runDir) })
	t.Cleanup(func() { _ = removeSessionMCPArtifacts(runDir) })

	origExec := sessionMCPExec
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sessionMCPExec = origExec
		_ = os.Chdir(origWD)
	})

	var gotPath string
	var gotArgv []string
	var gotEnv []string
	var gotWD string
	sessionMCPExec = func(argv0 string, argv []string, envv []string) error {
		gotPath = argv0
		gotArgv = slices.Clone(argv)
		gotEnv = slices.Clone(envv)
		gotWD, _ = os.Getwd()
		return nil
	}

	// Wrong digest must fail closed without execing.
	if err := RunSessionMCPServer(runDir, "wf", staged.SpecPath, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("mismatched digest should have failed closed")
	}

	if err := RunSessionMCPServer(runDir, "wf", staged.SpecPath, staged.Digest); err != nil {
		t.Fatalf("RunSessionMCPServer: %v", err)
	}
	if gotPath != toolPath {
		t.Errorf("resolved binary = %q, want %q", gotPath, toolPath)
	}
	if !slices.Equal(gotArgv, []string{"custom-wf-mcp", "--literal", "${KEEP_ME}"}) {
		t.Errorf("argv = %v", gotArgv)
	}
	realWantWD, _ := filepath.EvalSymlinks(workDir)
	realGotWD, _ := filepath.EvalSymlinks(gotWD)
	if realGotWD != realWantWD {
		t.Errorf("working dir = %q, want %q", realGotWD, realWantWD)
	}
	if !slices.Contains(gotEnv, "PATH="+customBin) || !slices.Contains(gotEnv, "CUSTOM_VAR=val-${LITERAL}") {
		t.Errorf("env missing caller overrides: %v", gotEnv)
	}
	if !slices.Contains(gotEnv, "AMBIENT_CALLER=kept-through-muse") {
		t.Errorf("ambient caller env from BaseEnv lost: %v", gotEnv)
	}
	if slices.Contains(gotEnv, "AL_SESSION_MCP_LEAK=strip-me") {
		t.Errorf("internal AL_SESSION_MCP_* leaked into server env: %v", gotEnv)
	}

	// Process ownership must have been recorded in the private runtime dir before exec.
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := readSessionMCPProcsLocked(rtDir)
	if err != nil || len(doc.Processes) != 1 || doc.Processes[0].Server != "wf" || doc.Processes[0].PID != os.Getpid() {
		t.Fatalf("recorded session MCP processes = %#v err=%v", doc, err)
	}

	// Once fenced closed, wrapper must refuse to exec.
	if err := publishBytes(filepath.Join(rtDir, sessionMCPClosedFile), []byte("closed\n"), os.Rename); err != nil {
		t.Fatal(err)
	}
	called := false
	sessionMCPExec = func(string, []string, []string) error { called = true; return nil }
	if err := RunSessionMCPServer(runDir, "wf", staged.SpecPath, staged.Digest); err == nil || called {
		t.Fatalf("fenced wrapper should have refused without execing (err=%v, called=%t)", err, called)
	}
}

func TestTerminationConfirmedWaitsForTrackedSessionMCPProcesses(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Spawn a real detached process in its own PGID simulating a session MCP
	// server that outlived a dead provider.
	sleeper := exec.Command("sleep", "60")
	sleeper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sleeper.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = sleeper.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		select {
		case <-reaped:
		default:
			_ = syscall.Kill(-sleeper.Process.Pid, syscall.SIGKILL)
			<-reaped
		}
	})
	startID := processStartIdentity(sleeper.Process.Pid)
	if startID == "" {
		t.Fatal("could not capture sleeper start identity")
	}
	procsDoc := sessionMCPProcsDocument{
		Processes: []sessionMCPProcess{{
			Server:               "wf",
			PID:                  sleeper.Process.Pid,
			ProcessGroupID:       sleeper.Process.Pid,
			ProcessStartIdentity: startID,
		}},
	}
	rtDir, err := ensureSessionMCPRuntimeDir(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(run.Dir) })
	if err := writeJSONAtomic(filepath.Join(rtDir, sessionMCPProcsFile), procsDoc); err != nil {
		t.Fatal(err)
	}

	// Simulate provider already stopped & cancelled, unconfirmed.
	now := time.Now().UTC()
	if _, err := updateRunEvidence(run.Dir, func(r *RunRecord) error {
		r.State = dispatchStateCancelled
		r.CompletedAt = &now
		r.TerminalReason = terminalReasonCancelledByCaller
		r.TerminalExitCode = ExitTargetFailure
		r.LaunchFenced = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// While sleeper is alive, observeTerminationInDir and Inspect MUST NOT confirm termination!
	rec, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed, _, _ := observeTerminationInDir(run.Dir, rec); confirmed {
		t.Fatal("termination confirmed while session MCP server process was still running")
	}
	var inspectOut bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, InvocationID: run.Record.ID, Stdout: &inspectOut}); err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(inspectOut.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.TerminationConfirmed {
		t.Fatalf("Inspect confirmed termination while session MCP server was alive: %#v", inspected)
	}

	// Cancel must terminate the surviving session MCP process group and confirm termination.
	var cancelOut bytes.Buffer
	if err := Cancel(CancelRequest{Root: root, Handle: session.Name, Stdout: &cancelOut}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	var cancelled Result
	if err := json.Unmarshal(cancelOut.Bytes(), &cancelled); err != nil {
		t.Fatal(err)
	}
	if !cancelled.TerminationConfirmed {
		t.Fatalf("Cancel did not confirm termination after killing session MCP process: %#v", cancelled)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled session MCP process to be reaped")
	}
	if !providerProcessGroupDead(sleeper.Process.Pid) {
		t.Fatal("session MCP process group still alive after termination_confirmed")
	}
}

func TestMuseExecuteDispatchWritesAndCleansSessionMCPGrant(t *testing.T) {
	isolateSessionMCPCacheDir(t)
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	rawCfg, err := os.ReadFile(cfgPath) // #nosec G304 -- test repository config.toml.
	if err != nil {
		t.Fatal(err)
	}
	updatedCfg := strings.Replace(string(rawCfg), "[agents.muse]\nenabled = false", "[agents.muse]\nenabled = true", 1)
	updatedCfg = strings.Replace(updatedCfg, `mode = "all"`, `mode = "none"`, 1)
	if err := os.WriteFile(cfgPath, []byte(updatedCfg), 0o600); err != nil { // #nosec G703 -- test repository config.toml.
		t.Fatal(err)
	}
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = config.ApprovalModeYOLO
	target, _ := lookupTarget(AgentMuse)
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionMCPServers = []SessionMCPServer{{Name: "wf-muse", Command: "/bin/wf"}}
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	// Pre-start failure (nonexistent command) must write the private Muse grant before launch,
	// then downgrade the session AND remove the Muse grant + private runtime dir.
	evalMuseMCP := func(sessionID string) (string, error) {
		var out bytes.Buffer
		in := fmt.Sprintf(`{"hook_event_name":"PermissionRequest","cwd":%q,"session_id":%q,"tool_name":"mcp__wf_muse__ping"}`, root, sessionID)
		err := musepolicy.HandleMCP(root, strings.NewReader(in), &out)
		return out.String(), err
	}
	var observedSessionID string
	var grantDecisionAtLaunch string
	err = executeDispatch(dispatchExecution{
		Root: root, WorkDir: root, Project: project, Target: target,
		Version: supportedProviderVersions[AgentMuse], Prompt: []byte("hi"),
		Mode: dispatchModeFresh, Run: run, Session: session,
		Stdout: io.Discard, Stderr: io.Discard, Env: []string{}, Depth: 1,
		NewCommand: func(_ string, args ...string) *exec.Cmd {
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--session-id" {
					observedSessionID = args[i+1]
					break
				}
			}
			if observedSessionID != "" {
				if out, hErr := evalMuseMCP(observedSessionID); hErr == nil {
					grantDecisionAtLaunch = out
				}
			}
			return exec.Command("/nonexistent-muse-binary-for-test")
		},
	})
	if err == nil {
		t.Fatal("expected pre-start failure, got nil")
	}
	if observedSessionID == "" {
		t.Fatal("Muse command did not pass provisional --session-id")
	}
	if !strings.Contains(grantDecisionAtLaunch, `"allow"`) {
		t.Fatalf("expected private Muse session MCP grant for %s to allow mcp__wf_muse__ping at launch, got %q", observedSessionID, grantDecisionAtLaunch)
	}
	if afterOut, hErr := evalMuseMCP(observedSessionID); hErr != nil || strings.Contains(afterOut, `"allow"`) {
		t.Fatalf("expected Muse session MCP grant for %s to be removed after pre-start failure, got out=%q err=%v", observedSessionID, afterOut, hErr)
	}
	// Verify session was downgraded back to pending with empty ProviderSessionID.
	reloaded, loadErr := loadSession(root, session.Name)
	if loadErr != nil {
		t.Fatalf("loadSession after pre-start failure: %v", loadErr)
	}
	if reloaded.State != sessionStatePending || reloaded.ProviderSessionID != "" {
		t.Fatalf("session not downgraded: %#v", reloaded)
	}
	// Private out-of-workspace runtime directory must be cleaned up after confirmed failure.
	if rtDir, pErr := sessionMCPRuntimeDirPath(run.Dir); pErr == nil {
		if _, statErr := os.Stat(rtDir); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("private session MCP runtime dir was not cleaned up after failure: %v", statErr)
		}
	}
	// Muse grants must live outside the workspace checkout.
	workspaceGrantDir := filepath.Join(root, ".agent-layer", "state", dispatchStateDir, "muse-mcp")
	if _, statErr := os.Stat(workspaceGrantDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Muse session MCP grants must not be stored inside workspace: %v", statErr)
	}
	// Ensure PruneSessionMCPGrants runs cleanly during pruneDispatchEvidence.
	orphanID := "99999999-9999-4999-8999-999999999999"
	if err := musepolicy.WriteSessionMCPGrant(root, orphanID, []string{"wf-muse"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = musepolicy.RemoveSessionMCPGrant(root, orphanID) })
	if err := pruneDispatchEvidence(root, time.Now(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestSessionMCPReviewHardeningRegressions(t *testing.T) {
	// 1. TOML basic string escaping must never emit Go-only \xNN escapes.
	got := encodeTOMLBasicString("/tmp/a\"b\\c\x01\n日本語")
	if strings.Contains(got, `\x`) {
		t.Fatalf("encodeTOMLBasicString emitted invalid TOML escape: %s", got)
	}
	if got != `"/tmp/a\"b\\c\u0001\n日本語"` {
		t.Fatalf("encodeTOMLBasicString = %s", got)
	}

	// 2. Wrapper must reject path-traversal --server before creating or writing handshake files.
	traversalRunDir := t.TempDir()
	if err := RunSessionMCPServerWithOptions(traversalRunDir, "../../escape", "/nonexistent", "deadbeef", true); err == nil || !strings.Contains(err.Error(), "must contain only ASCII") {
		t.Fatalf("expected invalid server name error for traversal, got %v", err)
	}
	if rtDir, err := sessionMCPRuntimeDirPath(traversalRunDir); err == nil {
		if _, statErr := os.Stat(rtDir); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("runtime dir should not be created for invalid server name: %v", statErr)
		}
	}

	// 3. Claude permissions.ask blocks session MCP in non-YOLO modes, allowed in YOLO.
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = config.ApprovalModeAll
	project.Config.Agents.Claude.AgentSpecific = config.ProviderPassthrough{
		"permissions": map[string]any{"ask": []any{"mcp__wf__*"}},
	}
	claudeTarget, _ := lookupTarget(AgentClaude)
	if err := validateSessionMCPServersForProject(project, claudeTarget, []SessionMCPServer{{Name: "wf", Command: "/bin/wf"}}); err == nil || !strings.Contains(err.Error(), "permissions.ask") {
		t.Fatalf("expected Claude permissions.ask error in non-YOLO mode, got %v", err)
	}
	project.Config.Approvals.Mode = config.ApprovalModeYOLO
	if err := validateSessionMCPServersForProject(project, claudeTarget, []SessionMCPServer{{Name: "wf", Command: "/bin/wf"}}); err != nil {
		t.Fatalf("unexpected Claude permissions.ask error in YOLO mode: %v", err)
	}

	// 4. Invalid UTF-8 and unpaired UTF-16 surrogates rejected in --mcp-config.
	if _, err := parseSessionMCPConfigJSON([]byte("{\"mcpServers\":{\"wf\":{\"command\":\"/bin/\xff\"}}}")); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected invalid UTF-8 rejection, got %v", err)
	}
	if _, err := parseSessionMCPConfigJSON([]byte(`{"mcpServers":{"wf":{"command":"/bin/wf","args":["\uD800"]}}}`)); err == nil || !strings.Contains(err.Error(), "surrogate") {
		t.Fatalf("expected unpaired surrogate rejection, got %v", err)
	}
	// Valid surrogate pair must still be accepted.
	if _, err := parseSessionMCPConfigJSON([]byte(`{"mcpServers":{"wf":{"command":"/bin/wf","args":["\uD83C\uDFF0"]}}}`)); err != nil {
		t.Fatalf("valid surrogate pair rejected: %v", err)
	}

	// 5. Symlinked existing ancestor pointing inside workspace is caught for non-existent child.
	ws := t.TempDir()
	outside := t.TempDir()
	linkIntoWS := filepath.Join(outside, "link-to-ws")
	if err := os.Symlink(ws, linkIntoWS); err != nil {
		t.Fatal(err)
	}
	if !pathWithinDir(filepath.Join(linkIntoWS, "nonexistent", "child"), ws) {
		t.Fatal("pathWithinDir failed to catch non-existent path under symlink pointing into workspace")
	}

	// 6. Grok DisableMemory restores --no-memory on ACP launch args.
	disableMem := true
	project.Config.Agents.Grok.DisableMemory = &disableMem
	grokTarget, _ := lookupTarget(AgentGrok)
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := buildProviderCommandForSession(grokTarget, project, root, []string{}, []byte("hi"), "", "", false, dispatchModeFresh, "", nil, run, io.Discard)
	if err != nil {
		t.Fatalf("buildProviderCommandForSession grok: %v", err)
	}
	if !slices.Contains(cmd.Args, "--no-memory") {
		t.Fatalf("grok ACP command missing --no-memory when disable_memory=true: %v", cmd.Args)
	}

	// 7. Server-to-client JSON-RPC request carrying method must not spoof handshake response.
	var mu sync.Mutex
	pending := map[string]string{"1": acpMethodInitialize}
	var hsStatus sessionMCPHandshakeStatus
	observeMCPHandshakeResponse(
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{}}`),
		&mu, pending, &hsStatus, func() {},
	)
	if hsStatus.Initialized || hsStatus.Failed || pending["1"] != acpMethodInitialize {
		t.Fatalf("server-to-client request mutated pending handshake: status=%#v pending=%v", hsStatus, pending)
	}

	// 8. Antigravity hyphen/underscore prefix AND normalized equality collisions rejected at preflight.
	agyTarget, _ := lookupTarget(AgentAntigravity)
	project.Config.MCP.Servers = []config.MCPServer{{ID: "foo-bar", Enabled: boolPtr(true), Transport: config.TransportStdio, Command: "/bin/true"}}
	if err := validateSessionMCPServersForProject(project, agyTarget, []SessionMCPServer{{Name: "foo", Command: "/bin/wf"}}); err == nil || !strings.Contains(err.Error(), "ambiguous Antigravity") {
		t.Fatalf("expected Antigravity hyphen/underscore prefix rejection, got %v", err)
	}
	if err := validateSessionMCPServersForProject(project, agyTarget, []SessionMCPServer{{Name: "foo_bar", Command: "/bin/wf"}}); err == nil || !strings.Contains(err.Error(), "ambiguous Antigravity") {
		t.Fatalf("expected Antigravity normalized equality rejection for foo_bar vs foo-bar, got %v", err)
	}

	// 9. Dangling symlink pointing into workspace fails closed in resolvePathThroughExistingAncestors.
	danglingLink := filepath.Join(outside, "dangling-into-ws")
	if err := os.Symlink(filepath.Join(ws, "not-yet-created"), danglingLink); err != nil {
		t.Fatal(err)
	}
	if !pathWithinDir(filepath.Join(danglingLink, "leaf"), ws) {
		t.Fatal("pathWithinDir failed to reject path through dangling symlink")
	}

	// 10. Zombie child process is rejected by checkSessionMCPHandshakesReady.
	zombieRunDir := t.TempDir()
	zombieCmd := exec.Command("true")
	if err := zombieCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = zombieCmd.Wait()
		_ = removeSessionMCPRuntimeDir(zombieRunDir)
	})
	// Poll briefly until the unreaped child enters zombie state.
	zombieDeadline := time.Now().Add(2 * time.Second)
	for !processIsZombie(zombieCmd.Process.Pid) && time.Now().Before(zombieDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !processIsZombie(zombieCmd.Process.Pid) {
		t.Fatalf("unreaped child pid %d did not enter zombie state within deadline", zombieCmd.Process.Pid)
	}
	zombieRT, err := ensureSessionMCPRuntimeDir(zombieRunDir)
	if err != nil {
		t.Fatal(err)
	}
	wrapperPID := os.Getpid()
	wrapperStart := processStartIdentity(wrapperPID)
	if err := writeJSONAtomic(filepath.Join(zombieRT, sessionMCPProcsFile), sessionMCPProcsDocument{
		Processes: []sessionMCPProcess{{Server: "z", PID: wrapperPID, ProcessGroupID: wrapperPID, ProcessStartIdentity: wrapperStart}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionMCPHandshakeStatus(zombieRT, sessionMCPHandshakeStatus{
		Server:               "z",
		Nonce:                "zn",
		PID:                  wrapperPID,
		ProcessStartIdentity: wrapperStart,
		ChildPID:             zombieCmd.Process.Pid,
		ChildStartIdentity:   processStartIdentity(zombieCmd.Process.Pid),
		Initialized:          true,
		ToolsListed:          true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := checkSessionMCPHandshakesReady(zombieRunDir, []string{"z"}, "zn"); err == nil || !strings.Contains(err.Error(), "child process exited") {
		t.Fatalf("expected zombie child rejection, got %v", err)
	}
}

func TestCancelTerminatesSurvivingSessionMCPFromRunningState(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Spawn a short-lived process to represent a dead provider leader.
	deadCmd := exec.Command("true")
	deadCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := deadCmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadPID := deadCmd.Process.Pid
	deadStart := processStartIdentity(deadPID)
	_ = deadCmd.Wait()

	// Spawn a detached live MCP server process group and reap asynchronously
	// once killed (matching orphan reparenting semantics).
	sleeper := exec.Command("sleep", "60")
	sleeper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	sleeperPID := sleeper.Process.Pid
	sleeperStart := processStartIdentity(sleeperPID)
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		_ = sleeper.Wait()
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-sleeperPID, syscall.SIGKILL)
		<-reaped
	})
	procsDoc := sessionMCPProcsDocument{
		Processes: []sessionMCPProcess{{
			Server:               "wf",
			PID:                  sleeperPID,
			ProcessGroupID:       sleeperPID,
			ProcessStartIdentity: sleeperStart,
		}},
	}
	rtDir, err := ensureSessionMCPRuntimeDir(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeSessionMCPRuntimeDir(run.Dir) })
	if err := writeJSONAtomic(filepath.Join(rtDir, sessionMCPProcsFile), procsDoc); err != nil {
		t.Fatal(err)
	}
	// Leave invocation in running state with dead worker & dead provider leader.
	run.Record.State = dispatchStateRunning
	run.Record.PID = deadPID
	run.Record.ProcessGroupID = deadPID
	run.Record.ProcessStartIdentity = deadStart
	run.Record.SupervisorPID = deadPID
	run.Record.SupervisorStartIdentity = deadStart
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Cancel(CancelRequest{Root: root, ID: session.Name, Stdout: &out}); err != nil {
		t.Fatalf("Cancel from running with dead provider + live MCP: %v", err)
	}
	var res Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.State != dispatchStateCancelled || !res.TerminationConfirmed {
		t.Fatalf("expected cancelled + termination_confirmed=true, got %+v", res)
	}
}

func TestSessionMCPTestsIsolateRuntimeAndCacheRoots(t *testing.T) {
	home := isolateSessionMCPCacheDir(t)
	// Production runtime root must remain anchored under canonical /tmp independent of TMPDIR.
	t.Setenv("TMPDIR", t.TempDir())
	canonicalTmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil || canonicalTmp == "" {
		canonicalTmp = "/tmp"
	}
	defaultRoot := filepath.Join(canonicalTmp, fmt.Sprintf("agent-layer-dispatch-%d", os.Getuid()))
	if got := defaultSessionMCPRuntimeRoot(); got != defaultRoot {
		t.Fatalf("defaultSessionMCPRuntimeRoot() with TMPDIR set = %q, want %q", got, defaultRoot)
	}
	if got := sessionMCPRuntimeRoot(); got == defaultRoot {
		t.Fatalf("TestMain did not isolate sessionMCPRuntimeRoot() away from %q", defaultRoot)
	}

	root := writeDispatchRepo(t, dispatchRepoConfig{})
	storeDir, err := sessionMCPDurableStoreDir(root)
	if err != nil {
		t.Fatalf("sessionMCPDurableStoreDir: %v", err)
	}
	if !sessionMCPTestPathWithinDir(t, storeDir, home) {
		t.Fatalf("durable store dir %q was not isolated inside test home %q", storeDir, home)
	}

	origExec := sessionMCPExecutable
	sessionMCPExecutable = func() (string, error) { return "/opt/al-test", nil }
	t.Cleanup(func() { sessionMCPExecutable = origExec })

	project := loadApprovalsTestProject(t, root)
	codexTarget, _ := lookupTarget(AgentCodex)
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	rtDir, err := sessionMCPRuntimeDirPath(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if sessionMCPTestPathWithinDir(t, rtDir, defaultRoot) {
		t.Fatalf("test runtime dir %q resolved inside production root %q", rtDir, defaultRoot)
	}
	if _, err := buildProviderCommandForSession(codexTarget, project, root, nil, []byte("hi"), "", "", false, dispatchModeFresh, runtimeSessionID, []SessionMCPServer{{Name: "wf", Command: "/bin/wf"}}, run, io.Discard); err != nil {
		t.Fatalf("buildProviderCommandForSession: %v", err)
	}
	if _, err := os.Stat(rtDir); err != nil {
		t.Fatalf("expected staged runtime dir %q to exist: %v", rtDir, err)
	}
	if err := removeSessionMCPRuntimeDir(run.Dir); err != nil {
		t.Fatalf("removeSessionMCPRuntimeDir: %v", err)
	}
	if _, err := os.Stat(rtDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime dir %q still exists after cleanup: %v", rtDir, err)
	}
}

func sessionMCPTestPathWithinDir(t *testing.T, path, dir string) bool {
	t.Helper()
	resolvedPath, err := resolvePathThroughExistingAncestors(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	resolvedDir, err := resolvePathThroughExistingAncestors(dir)
	if err != nil {
		t.Fatalf("resolve %q: %v", dir, err)
	}
	rel, err := filepath.Rel(resolvedDir, resolvedPath)
	if err != nil {
		t.Fatalf("relate %q to %q: %v", resolvedPath, resolvedDir, err)
	}
	return filepath.IsLocal(rel)
}
