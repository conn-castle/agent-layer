package agentdispatch

import (
	"bufio"
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
	"sync/atomic"
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
	var hsStatus sessionMCPHandshakeStatus
	tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "wf"}, func(s sessionMCPHandshakeStatus) {
		hsStatus = s
	})
	tracker.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	decision := tracker.prepareResponse([]byte(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{}}`))
	tracker.commitResponse(decision)
	if decision.outcome != sessionMCPResponseIgnore || hsStatus.Ready || hsStatus.Failed || tracker.isPassthrough() {
		t.Fatalf("server-to-client request mutated pending handshake: decision=%#v status=%#v", decision, hsStatus)
	}
	idKey, ok := canonicalJSONRPCIDKey(json.RawMessage(`1`))
	if !ok {
		t.Fatal("canonicalJSONRPCIDKey(1) failed")
	}
	if _, stillPending := tracker.pending[idKey]; !stillPending {
		t.Fatalf("server-to-client request evicted pending id 1: pending=%d", len(tracker.pending))
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
		Ready:                true,
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

func TestSessionMCPHandshakeTrackerProtocolAgnostic(t *testing.T) {
	observe := func(tr *sessionMCPHandshakeTracker, line string) sessionMCPResponseDecision {
		d := tr.prepareResponse([]byte(line))
		tr.commitResponse(d)
		return d
	}

	t.Run("mcp_2025_11_25_and_2026_07_28_two_phase_ready", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			req  string
			resp string
		}{
			{
				"mcp_2025_11_25_initialize",
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
				`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"srv","version":"1.0"}}}`,
			},
			{
				"mcp_2026_07_28_server_discover",
				`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`,
				`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2026-07-28","capabilities":{}}}`,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var published []sessionMCPHandshakeStatus
				tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
					published = append(published, s)
				})
				tr.trackRequest([]byte(tc.req))
				// Phase 1 arms passthrough BEFORE Ready is published.
				d := tr.prepareResponse([]byte(tc.resp))
				if d.outcome != sessionMCPResponseReady || !tr.isPassthrough() || len(published) != 0 || tr.status.Ready {
					t.Fatalf("prepareResponse should arm passthrough without publishing ready: d=%#v passthrough=%t published=%#v", d, tr.isPassthrough(), published)
				}
				// Phase 2 publishes Ready after stdout write succeeds.
				tr.commitResponse(d)
				if len(published) != 1 || !published[0].Ready || published[0].Failed || len(tr.pending) != 0 {
					t.Fatalf("commitResponse failed: published=%#v pending=%d", published, len(tr.pending))
				}
			})
		}
	})

	t.Run("version_negotiation_32601_fallback_and_strict_error_validation", func(t *testing.T) {
		var published []sessionMCPHandshakeStatus
		tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			published = append(published, s)
		})
		tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`))
		observe(tr, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`)
		if tr.isPassthrough() || len(published) != 0 || tr.status.Failed || len(tr.pending) != 0 {
			t.Fatalf("-32601 fallback should neither fail nor mark ready: passthrough=%t status=%#v pending=%d", tr.isPassthrough(), tr.status, len(tr.pending))
		}
		// Serializers that include explicit `"result": null` alongside `-32601` error are also tolerated.
		tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{}}`))
		observe(tr, `{"jsonrpc":"2.0","id":2,"result":null,"error":{"code":-32601,"message":"Method not found"}}`)
		if tr.isPassthrough() || len(published) != 0 || tr.status.Failed || len(tr.pending) != 0 {
			t.Fatalf("-32601 with explicit null result should neither fail nor mark ready: passthrough=%t status=%#v", tr.isPassthrough(), tr.status)
		}
		tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{}}`))
		observe(tr, `{"jsonrpc":"2.0","id":3,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"srv"}}}`)
		if !tr.isPassthrough() || len(published) != 1 || !published[0].Ready || published[0].Failed {
			t.Fatalf("fallback initialize did not confirm ready: passthrough=%t published=%#v", tr.isPassthrough(), published)
		}
	})

	t.Run("jsonrpc_2_0_error_and_envelope_validation", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			respLine  string
			wantReady bool
			wantFail  bool
			wantErr   string
		}{
			{"internal_error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"boom"}}`, false, true, "startup request returned JSON-RPC error -32603"},
			{"server_error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"auth"}}`, false, true, "startup request returned JSON-RPC error -32000"},
			{"32601_missing_message", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601}}`, false, true, "startup request returned malformed JSON-RPC error"},
			{"32601_null_message", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":null}}`, false, true, "startup request returned malformed JSON-RPC error"},
			{"32601_float_code", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601.0,"message":"Method not found"}}`, false, true, "startup request returned malformed JSON-RPC error"},
			{"empty_error_object", `{"jsonrpc":"2.0","id":1,"error":{}}`, false, true, "startup request returned malformed JSON-RPC error"},
			{"malformed_error_string", `{"jsonrpc":"2.0","id":1,"error":"not-an-object"}`, false, true, "startup request returned malformed JSON-RPC error"},
			{"both_result_and_error", `{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-32601,"message":"Method not found"}}`, false, true, "startup request returned invalid JSON-RPC response"},
			{"wrong_case_result_rejected", `{"jsonrpc":"2.0","id":1,"Result":{}}`, false, true, "startup request returned invalid JSON-RPC response"},
			{"neither_result_nor_error", `{"jsonrpc":"2.0","id":1}`, false, true, "startup request returned invalid JSON-RPC response"},
			{"explicit_null_error_without_result", `{"jsonrpc":"2.0","id":1,"error":null}`, false, true, "startup request returned invalid JSON-RPC response"},
			{"explicit_null_result_accepted", `{"jsonrpc":"2.0","id":1,"result":null}`, true, false, ""},
			{"explicit_null_error_with_null_result_accepted", `{"jsonrpc":"2.0","id":1,"error":null,"result":null}`, true, false, ""},
			{"empty_object_result_accepted", `{"jsonrpc":"2.0","id":1,"result":{}}`, true, false, ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var published []sessionMCPHandshakeStatus
				tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
					published = append(published, s)
				})
				tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"secret-token-method"}`))
				tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":2,"method":"initialize"}`))
				observe(tr, tc.respLine)
				if len(published) != 1 || published[0].Ready != tc.wantReady || published[0].Failed != tc.wantFail || tr.isPassthrough() != tc.wantReady {
					t.Fatalf("got passthrough=%t published=%#v, want ready=%t failed=%t", tr.isPassthrough(), published, tc.wantReady, tc.wantFail)
				}
				if tc.wantErr != "" && published[0].Error != tc.wantErr {
					t.Fatalf("error = %q, want %q", published[0].Error, tc.wantErr)
				}
				if strings.Contains(published[0].Error, "secret-token-method") {
					t.Fatalf("status error leaked untrusted method name: %q", published[0].Error)
				}
				if tc.wantFail {
					// Once failed, a subsequent valid response must not flip ready.
					observe(tr, `{"jsonrpc":"2.0","id":2,"result":{}}`)
					if tr.isPassthrough() || len(published) != 1 || !tr.status.Failed {
						t.Fatalf("subsequent response mutated failed state: passthrough=%t status=%#v", tr.isPassthrough(), tr.status)
					}
				}
			})
		}
	})

	t.Run("canonical_ids_jsonrpc_version_and_invalid_ids", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			req  string
			resp string
		}{
			{"escaped_string_id", `{"jsonrpc":"2.0","id":"a","method":"initialize"}`, `{"jsonrpc":"2.0","id":"\u0061","result":{}}`},
			{"numeric_int_and_float_equal", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, `{"jsonrpc":"2.0","id":1.0,"result":{}}`},
			{"numeric_scientific_equal", `{"jsonrpc":"2.0","id":1e0,"method":"initialize"}`, `{"jsonrpc":"2.0","id": 1 ,"result":{}}`},
			{"numeric_zero", `{"jsonrpc":"2.0","id":0,"method":"ping"}`, `{"jsonrpc":"2.0","id":-0,"result":{}}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var last sessionMCPHandshakeStatus
				tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) { last = s })
				tr.trackRequest([]byte(tc.req))
				observe(tr, tc.resp)
				if !tr.isPassthrough() || !last.Ready || last.Failed {
					t.Fatalf("expected canonical id match to mark ready, got status=%#v", last)
				}
			})
		}

		// Non-2.0 requests, notifications, and invalid JSON-RPC ID types are ignored.
		tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, nil)
		for _, ignored := range []string{
			`{"id":1,"method":"initialize"}`,
			`{"jsonrpc":"1.0","id":1,"method":"initialize"}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":null,"method":"initialize"}`,
			`{"jsonrpc":"2.0","id":true,"method":"initialize"}`,
			`{"jsonrpc":"2.0","id":{},"method":"initialize"}`,
			`{"jsonrpc":"2.0","id":[1],"method":"initialize"}`,
			`{"jsonrpc":"2.0","id":1e99999999,"method":"initialize"}`,
		} {
			tr.trackRequest([]byte(ignored))
		}
		if len(tr.pending) != 0 {
			t.Fatalf("invalid/non-2.0/notification requests should not populate pending: %d", len(tr.pending))
		}
		// String "1" must not match numeric 1, and non-2.0 response must be ignored.
		tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":"1","method":"initialize"}`))
		observe(tr, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		observe(tr, `{"jsonrpc":"1.0","id":"1","result":{}}`)
		observe(tr, `{"id":"1","result":{}}`)
		if tr.isPassthrough() || tr.status.Failed || len(tr.pending) != 1 {
			t.Fatalf("mismatched/non-2.0 responses mutated tracker: passthrough=%t status=%#v pending=%d", tr.isPassthrough(), tr.status, len(tr.pending))
		}
	})

	t.Run("bounded_pending_requests_evicts_oldest", func(t *testing.T) {
		var boundedStatus sessionMCPHandshakeStatus
		bounded := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			boundedStatus = s
		})
		total := sessionMCPMaxPendingRequests + 32
		for i := range total {
			bounded.trackRequest([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"probe/%d"}`, i, i)))
		}
		if len(bounded.pending) != sessionMCPMaxPendingRequests || bounded.order.Len() != sessionMCPMaxPendingRequests {
			t.Fatalf("pending size=(%d,%d), want %d", len(bounded.pending), bounded.order.Len(), sessionMCPMaxPendingRequests)
		}
		observe(bounded, `{"jsonrpc":"2.0","id":0,"result":{}}`)
		if bounded.isPassthrough() {
			t.Fatal("evicted request id 0 should not confirm readiness")
		}
		observe(bounded, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, total-1))
		if !bounded.isPassthrough() || !boundedStatus.Ready {
			t.Fatalf("latest request id %d did not confirm readiness: %#v", total-1, boundedStatus)
		}
	})
}

func waitSignal(t *testing.T, ch <-chan struct{}, desc string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", desc)
	}
}

func waitErrChan(t *testing.T, ch <-chan error, desc string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", desc)
		return nil
	}
}

func readStringBounded(t *testing.T, r *bufio.Reader, closer io.Closer, desc string) string {
	t.Helper()
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := r.ReadString('\n')
		ch <- res{s, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("%s: %v", desc, got.err)
		}
		return got.s
	case <-time.After(10 * time.Second):
		_ = closer.Close()
		t.Fatalf("timed out waiting for %s", desc)
		return ""
	}
}

func readAllBounded(t *testing.T, r io.Reader, closer io.Closer, desc string) []byte {
	t.Helper()
	type res struct {
		data []byte
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		data, err := io.ReadAll(r)
		ch <- res{data, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("%s: %v", desc, got.err)
		}
		return got.data
	case <-time.After(10 * time.Second):
		_ = closer.Close()
		t.Fatalf("timed out waiting for %s", desc)
		return nil
	}
}

func TestSessionMCPRelayRawPassthroughAndRaceSafety(t *testing.T) {
	t.Run("fast_client_writes_above_acpMaxFrameBytes_immediately_before_publish_completes", func(t *testing.T) {
		clientR, clientW := io.Pipe()
		childInR, childInW := io.Pipe()
		childOutR, childOutW := io.Pipe()
		stdoutR, stdoutW := io.Pipe()
		t.Cleanup(func() {
			_ = clientR.Close()
			_ = clientW.Close()
			_ = childInR.Close()
			_ = childInW.Close()
			_ = childOutR.Close()
			_ = childOutW.Close()
			_ = stdoutR.Close()
			_ = stdoutW.Close()
		})

		// Block publish until the client has ALREADY sent >16 MiB on stdin after
		// reading the startup response, proving passthrough was armed BEFORE stdout.Write.
		publishStarted := make(chan struct{}, 1)
		releasePublish := make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-releasePublish:
			default:
				close(releasePublish)
			}
		})
		var lastStatus sessionMCPHandshakeStatus
		var statusMu sync.Mutex
		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			if s.Ready {
				select {
				case publishStarted <- struct{}{}:
				default:
				}
				<-releasePublish
			}
			statusMu.Lock()
			lastStatus = s
			statusMu.Unlock()
		})
		var terminateCalls atomic.Int32
		terminate := func() { terminateCalls.Add(1) }

		stdinDone := make(chan struct{})
		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdinDone)
			relaySessionMCPStdin(clientR, childInW, acpMaxFrameBytes, tracker, terminate)
		}()
		go func() {
			defer close(stdoutDone)
			relaySessionMCPStdout(childOutR, stdoutW, acpMaxFrameBytes, tracker, terminate)
			_ = stdoutW.Close()
		}()

		const largeSize = acpMaxFrameBytes + 1024*1024 // 17 MiB > 16 MiB acpMaxFrameBytes
		largeReq := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"blob":"` + strings.Repeat("A", largeSize) + `"}}` + "\n"
		largeResp := `{"jsonrpc":"2.0","id":2,"result":{"blob":"` + strings.Repeat("B", largeSize) + `"}}` + "\n"

		serverReceivedLarge := make(chan struct{})
		serverErr := make(chan error, 1)
		go func() {
			defer func() { _ = childOutW.Close() }()
			srvReader := bufio.NewReaderSize(childInR, 64*1024)
			firstLine, err := srvReader.ReadString('\n')
			if err != nil {
				serverErr <- fmt.Errorf("server read startup request: %w", err)
				return
			}
			if firstLine != "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\",\"params\":{}}\r\n" {
				serverErr <- fmt.Errorf("startup line altered: %q", firstLine)
				return
			}
			if _, err := io.WriteString(childOutW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2026-07-28\"}}\n"); err != nil {
				serverErr <- fmt.Errorf("server write startup response: %w", err)
				return
			}
			gotLargeReq, err := io.ReadAll(srvReader)
			if err != nil {
				serverErr <- fmt.Errorf("server read large request: %w", err)
				return
			}
			if string(gotLargeReq) != largeReq {
				serverErr <- fmt.Errorf("large stdin request mismatch: got %d bytes, want %d bytes", len(gotLargeReq), len(largeReq))
				return
			}
			close(serverReceivedLarge)
			if _, err := io.WriteString(childOutW, largeResp); err != nil {
				serverErr <- fmt.Errorf("server write large response: %w", err)
				return
			}
			serverErr <- nil
		}()

		if _, err := io.WriteString(clientW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\",\"params\":{}}\r\n"); err != nil {
			t.Fatalf("client write startup: %v", err)
		}
		clientReader := bufio.NewReaderSize(stdoutR, 64*1024)
		gotInitResp := readStringBounded(t, clientReader, stdoutR, "client read startup response")
		if gotInitResp != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2026-07-28\"}}\n" {
			t.Fatalf("unexpected startup response: %q", gotInitResp)
		}

		// Immediately blast >16 MiB request BEFORE status publish completes!
		clientWriteErr := make(chan error, 1)
		go func() {
			_, wErr := io.WriteString(clientW, largeReq)
			_ = clientW.Close()
			clientWriteErr <- wErr
		}()
		waitSignal(t, serverReceivedLarge, "server receiving >16 MiB stdin payload while publish is blocked")
		waitSignal(t, publishStarted, "stdout publish starting")
		close(releasePublish)

		gotLargeResp := readAllBounded(t, clientReader, stdoutR, "client read large response")
		if err := waitErrChan(t, clientWriteErr, "client large write"); err != nil {
			t.Fatalf("client write large request: %v", err)
		}
		if err := waitErrChan(t, serverErr, "server goroutine"); err != nil {
			t.Fatalf("server error: %v", err)
		}
		waitSignal(t, stdinDone, "stdin relay exit")
		waitSignal(t, stdoutDone, "stdout relay exit")

		if string(gotLargeResp) != largeResp {
			t.Fatalf("large stdout response mismatch: got %d bytes, want %d bytes", len(gotLargeResp), len(largeResp))
		}
		if terminateCalls.Load() != 0 {
			t.Fatalf("unexpected terminateChild calls: %d", terminateCalls.Load())
		}
		statusMu.Lock()
		defer statusMu.Unlock()
		if !lastStatus.Ready || lastStatus.Failed {
			t.Fatalf("unexpected final status: %#v", lastStatus)
		}
	})

	t.Run("short_post_readiness_stdin_payload_without_newline_forwarded_promptly", func(t *testing.T) {
		clientR, clientW := io.Pipe()
		childInR, childInW := io.Pipe()
		childOutR, childOutW := io.Pipe()
		stdoutR, stdoutW := io.Pipe()
		t.Cleanup(func() {
			_ = clientR.Close()
			_ = clientW.Close()
			_ = childInR.Close()
			_ = childInW.Close()
			_ = childOutR.Close()
			_ = childOutW.Close()
			_ = stdoutR.Close()
			_ = stdoutW.Close()
		})

		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, nil)
		stdinDone := make(chan struct{})
		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdinDone)
			relaySessionMCPStdin(clientR, childInW, acpMaxFrameBytes, tracker, func() {})
		}()
		go func() {
			defer close(stdoutDone)
			relaySessionMCPStdout(childOutR, stdoutW, acpMaxFrameBytes, tracker, func() {})
			_ = stdoutW.Close()
		}()

		const wantRaw = "raw-no-nl"
		gotRawCh := make(chan string, 1)
		go func() {
			defer func() { _ = childOutW.Close() }()
			r := bufio.NewReader(childInR)
			_, _ = r.ReadString('\n')
			_, _ = io.WriteString(childOutW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
			buf := make([]byte, len(wantRaw))
			if _, err := io.ReadFull(r, buf); err == nil {
				gotRawCh <- string(buf)
			}
		}()

		if _, err := io.WriteString(clientW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n"); err != nil {
			t.Fatal(err)
		}
		readStringBounded(t, bufio.NewReader(stdoutR), stdoutR, "short-payload startup response")
		// Write a short chunk WITHOUT a trailing newline and keep stdin open.
		if _, err := io.WriteString(clientW, wantRaw); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-gotRawCh:
			if got != wantRaw {
				t.Fatalf("got %q, want %q", got, wantRaw)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("short post-readiness non-newline stdin payload stalled")
		}
		_ = clientW.Close()
		waitSignal(t, stdinDone, "stdin relay exit")
		waitSignal(t, stdoutDone, "stdout relay exit")
	})

	t.Run("mid_frame_stdin_passthrough_when_ready_flips_after_buffered_prefix", func(t *testing.T) {
		const smallLimit = 128 * 1024 // 128 KiB (> 64 KiB read chunk)
		clientR, clientW := io.Pipe()
		childInR, childInW := io.Pipe()
		childOutR, childOutW := io.Pipe()
		stdoutR, stdoutW := io.Pipe()
		t.Cleanup(func() {
			_ = clientR.Close()
			_ = clientW.Close()
			_ = childInR.Close()
			_ = childInW.Close()
			_ = childOutR.Close()
			_ = childOutW.Close()
			_ = stdoutR.Close()
			_ = stdoutW.Close()
		})

		readyCh := make(chan struct{}, 1)
		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			if s.Ready {
				select {
				case readyCh <- struct{}{}:
				default:
				}
			}
		})
		var terminateCalls atomic.Int32
		terminate := func() { terminateCalls.Add(1) }

		stdinDone := make(chan struct{})
		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdinDone)
			relaySessionMCPStdin(clientR, childInW, smallLimit, tracker, terminate)
		}()
		go func() {
			defer close(stdoutDone)
			relaySessionMCPStdout(childOutR, stdoutW, smallLimit, tracker, terminate)
			_ = stdoutW.Close()
		}()

		prefix := bytes.Repeat([]byte("P"), 80*1024)
		suffix := append(bytes.Repeat([]byte("S"), 120*1024), '\n')
		wantSecondLine := append(slices.Clone(prefix), suffix...) // 200 KiB > 128 KiB smallLimit

		serverReadFirst := make(chan struct{})
		serverGot := make(chan []byte, 1)
		go func() {
			defer func() { _ = childOutW.Close() }()
			r := bufio.NewReaderSize(childInR, 64*1024)
			_, _ = r.ReadString('\n')
			close(serverReadFirst)
			rest, _ := io.ReadAll(r)
			serverGot <- rest
		}()

		if _, err := io.WriteString(clientW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n"); err != nil {
			t.Fatal(err)
		}
		waitSignal(t, serverReadFirst, "server reading first line")

		// Write 80 KiB (no newline) BEFORE readiness flips. Because io.Pipe is unbuffered,
		// Write returns only after stdin relay has buffered all 80 KiB in lineBuf while !passthrough.
		if _, err := clientW.Write(prefix); err != nil {
			t.Fatal(err)
		}

		stdoutDiscard := make(chan struct{})
		go func() {
			defer close(stdoutDiscard)
			_, _ = io.Copy(io.Discard, stdoutR)
		}()
		if _, err := io.WriteString(childOutW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n"); err != nil {
			t.Fatal(err)
		}
		waitSignal(t, readyCh, "ready publish")

		if _, err := clientW.Write(suffix); err != nil {
			t.Fatal(err)
		}
		_ = clientW.Close()

		var gotSecondLine []byte
		select {
		case gotSecondLine = <-serverGot:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for server to receive mid-frame passthrough payload")
		}
		waitSignal(t, stdinDone, "stdin relay exit")
		waitSignal(t, stdoutDone, "stdout relay exit")
		waitSignal(t, stdoutDiscard, "stdout drain")

		if !bytes.Equal(gotSecondLine, wantSecondLine) {
			t.Fatalf("mid-frame passthrough corrupted payload: got %d bytes, want %d bytes", len(gotSecondLine), len(wantSecondLine))
		}
		if terminateCalls.Load() != 0 {
			t.Fatalf("unexpected terminateChild calls: %d", terminateCalls.Load())
		}
	})

	t.Run("epipe_on_ready_response_does_not_publish_ready", func(t *testing.T) {
		var published []sessionMCPHandshakeStatus
		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			published = append(published, s)
		})
		tracker.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		stdoutR, stdoutW := io.Pipe()
		_ = stdoutR.Close() // Force write failure on the ready response line itself.
		var termCalls atomic.Int32
		relaySessionMCPStdout(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n"), stdoutW, acpMaxFrameBytes, tracker, func() {
			termCalls.Add(1)
		})
		if termCalls.Load() != 1 || len(published) != 0 || tracker.status.Ready {
			t.Fatalf("EPIPE on ready response should terminate without publishing ready: term=%d published=%#v status=%#v", termCalls.Load(), published, tracker.status)
		}
		// Exit-path markFailedUnlessReady must still mark failure because Ready was never committed.
		if tracker.markFailedUnlessReady("server exited before completing startup handshake") {
			t.Fatal("markFailedUnlessReady treated uncommitted passthrough as Ready")
		}
		if len(published) != 1 || !published[0].Failed {
			t.Fatalf("expected startup exit failure published after EPIPE, got %#v", published)
		}
	})

	t.Run("pre_readiness_oversized_frames_fail_and_terminate", func(t *testing.T) {
		const smallLimit = 1024

		var outStatus sessionMCPHandshakeStatus
		outTracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "out"}, func(s sessionMCPHandshakeStatus) {
			outStatus = s
		})
		var outTerm atomic.Int32
		childOutR, childOutW := io.Pipe()
		go func() {
			_, _ = childOutW.Write(bytes.Repeat([]byte("x"), smallLimit+512))
			_ = childOutW.Close()
		}()
		var outBuf bytes.Buffer
		relaySessionMCPStdout(childOutR, &outBuf, smallLimit, outTracker, func() { outTerm.Add(1) })
		if !outStatus.Failed || outStatus.Ready || outTerm.Load() != 1 {
			t.Fatalf("pre-readiness oversized stdout: status=%#v term=%d", outStatus, outTerm.Load())
		}

		var inStatus sessionMCPHandshakeStatus
		inTracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "in"}, func(s sessionMCPHandshakeStatus) {
			inStatus = s
		})
		var inTerm atomic.Int32
		childInR, childInW := io.Pipe()
		drainDone := make(chan struct{})
		go func() {
			defer close(drainDone)
			_, _ = io.Copy(io.Discard, childInR)
		}()
		relaySessionMCPStdin(bytes.NewReader(bytes.Repeat([]byte("y"), smallLimit+512)), childInW, smallLimit, inTracker, func() { inTerm.Add(1) })
		waitSignal(t, drainDone, "childIn drain")
		if !inStatus.Failed || inStatus.Ready || inTerm.Load() != 1 {
			t.Fatalf("pre-readiness oversized stdin: status=%#v term=%d", inStatus, inTerm.Load())
		}
	})

	t.Run("post_readiness_stdout_epipe_terminates_and_drains", func(t *testing.T) {
		childOutR, childOutW := io.Pipe()
		stdoutR, stdoutW := io.Pipe()
		t.Cleanup(func() {
			_ = childOutR.Close()
			_ = childOutW.Close()
			_ = stdoutR.Close()
			_ = stdoutW.Close()
		})
		readyCh := make(chan struct{}, 1)
		var lastStatus sessionMCPHandshakeStatus
		var statusMu sync.Mutex
		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			statusMu.Lock()
			lastStatus = s
			statusMu.Unlock()
			if s.Ready {
				select {
				case readyCh <- struct{}{}:
				default:
				}
			}
		})
		tracker.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))

		var termCalls atomic.Int32
		termCalled := make(chan struct{}, 1)
		terminate := func() {
			termCalls.Add(1)
			select {
			case termCalled <- struct{}{}:
			default:
			}
		}

		relayDone := make(chan struct{})
		go func() {
			defer close(relayDone)
			relaySessionMCPStdout(childOutR, stdoutW, acpMaxFrameBytes, tracker, terminate)
		}()

		go func() {
			defer func() { _ = childOutW.Close() }()
			_, _ = io.WriteString(childOutW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
			payload := bytes.Repeat([]byte("noise\n"), 1024)
			for {
				select {
				case <-termCalled:
					_, _ = childOutW.Write(payload)
					return
				default:
					if _, err := childOutW.Write(payload); err != nil {
						return
					}
				}
			}
		}()

		readStringBounded(t, bufio.NewReader(stdoutR), stdoutR, "read ready response")
		waitSignal(t, readyCh, "ready publish")
		_ = stdoutR.Close()

		waitSignal(t, relayDone, "stdout relay exit after post-readiness EPIPE")
		if termCalls.Load() != 1 {
			t.Fatalf("expected terminateChild called once on post-readiness EPIPE, got %d", termCalls.Load())
		}
		statusMu.Lock()
		defer statusMu.Unlock()
		if !lastStatus.Ready || lastStatus.Failed {
			t.Fatalf("post-readiness EPIPE should preserve ready=true without startup failure: %#v", lastStatus)
		}
	})

	t.Run("pipelined_large_stdout_frame_buffered_with_ready_response", func(t *testing.T) {
		const smallLimit = 1024
		tracker := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, nil)
		tracker.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`))

		readyLine := "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n"
		largeLine := "{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"data\":\"" + strings.Repeat("Z", smallLimit*4) + "\"}}\n"
		var outBuf bytes.Buffer
		var termCalls atomic.Int32
		relaySessionMCPStdout(strings.NewReader(readyLine+largeLine), &outBuf, smallLimit, tracker, func() { termCalls.Add(1) })
		if termCalls.Load() != 0 || !tracker.isPassthrough() {
			t.Fatalf("unexpected failure: passthrough=%t term=%d", tracker.isPassthrough(), termCalls.Load())
		}
		if outBuf.String() != readyLine+largeLine {
			t.Fatalf("pipelined large stdout mismatch: got %d bytes, want %d bytes", outBuf.Len(), len(readyLine)+len(largeLine))
		}
	})

	t.Run("unterminated_eof_stdout_response_does_not_publish_ready", func(t *testing.T) {
		var published []sessionMCPHandshakeStatus
		tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			published = append(published, s)
		})
		tr.trackRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		var outBuf bytes.Buffer
		relaySessionMCPStdout(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`), &outBuf, acpMaxFrameBytes, tr, func() {})
		if tr.isPassthrough() || len(published) != 0 || tr.status.Ready {
			t.Fatalf("unterminated EOF response should not publish Ready: passthrough=%t published=%#v status=%#v", tr.isPassthrough(), published, tr.status)
		}
		if outBuf.String() != `{"jsonrpc":"2.0","id":1,"result":{}}` {
			t.Fatalf("expected raw bytes still forwarded, got %q", outBuf.String())
		}
	})

	t.Run("split_crlf_at_exact_startup_limit_and_trailing_cr_at_eof", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
		limit := len(req) // exact content length without CRLF

		// Split CRLF across two Pipe writes: "<req>\r" then "\n" must succeed.
		clientR, clientW := io.Pipe()
		childInR, childInW := io.Pipe()
		t.Cleanup(func() {
			_ = clientR.Close()
			_ = clientW.Close()
			_ = childInR.Close()
			_ = childInW.Close()
		})
		tr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, nil)
		var termCalls atomic.Int32
		stdinDone := make(chan struct{})
		go func() {
			defer close(stdinDone)
			relaySessionMCPStdin(clientR, childInW, limit, tr, func() { termCalls.Add(1) })
		}()
		gotCh := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(childInR)
			gotCh <- string(data)
		}()
		if _, err := io.WriteString(clientW, req+"\r"); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(clientW, "\n"); err != nil {
			t.Fatal(err)
		}
		_ = clientW.Close()
		waitSignal(t, stdinDone, "split-CRLF stdin relay exit")
		if termCalls.Load() != 0 || tr.status.Failed {
			t.Fatalf("exact-limit split CRLF was rejected: term=%d status=%#v", termCalls.Load(), tr.status)
		}
		if got := <-gotCh; got != req+"\r\n" {
			t.Fatalf("got %q, want %q", got, req+"\r\n")
		}

		// Trailing CR at EOF without LF exceeds limit and must fail.
		eofR, eofW := io.Pipe()
		eofChildR, eofChildW := io.Pipe()
		t.Cleanup(func() {
			_ = eofR.Close()
			_ = eofW.Close()
			_ = eofChildR.Close()
			_ = eofChildW.Close()
		})
		var eofStatus sessionMCPHandshakeStatus
		eofTr := newSessionMCPHandshakeTracker(sessionMCPHandshakeStatus{Server: "srv"}, func(s sessionMCPHandshakeStatus) {
			eofStatus = s
		})
		var eofTerm atomic.Int32
		eofDone := make(chan struct{})
		go func() {
			defer close(eofDone)
			relaySessionMCPStdin(eofR, eofChildW, limit, eofTr, func() { eofTerm.Add(1) })
		}()
		go func() { _, _ = io.Copy(io.Discard, eofChildR) }()
		if _, err := io.WriteString(eofW, req+"\r"); err != nil {
			t.Fatal(err)
		}
		_ = eofW.Close()
		waitSignal(t, eofDone, "EOF trailing-CR stdin relay exit")
		if !eofStatus.Failed || eofTerm.Load() != 1 {
			t.Fatalf("trailing CR exceeding limit at EOF should fail: status=%#v term=%d", eofStatus, eofTerm.Load())
		}
	})
}
