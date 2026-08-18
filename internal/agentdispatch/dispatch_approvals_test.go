package agentdispatch

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
)

// loadApprovalsTestProject loads a dispatch project and asserts the dispatch
// context is the inert one these tests assume, so an unexpected inherited
// environment or depth cannot silently change what is being measured.
func loadApprovalsTestProject(t *testing.T, root string) *config.ProjectConfig {
	t.Helper()
	project, stderr, env, depth, err := loadDispatchProject(root, io.Discard, []string{})
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if stderr != io.Discard || len(env) != 0 || depth != 0 {
		t.Fatalf("unexpected dispatch context: stderr=%T env=%v depth=%d", stderr, env, depth)
	}
	return project
}

// dispatchArgsForMode builds one provider command for the given approvals mode
// and returns its argv joined for substring assertions.
func dispatchArgsForMode(t *testing.T, agent string, mode string, dispatchMode string) string {
	t.Helper()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = mode
	target, ok := lookupTarget(agent)
	if !ok {
		t.Fatalf("%s target missing from registry", agent)
	}
	run, err := newDispatchRun(root, agent, supportedProviderVersions[agent], dispatchMode)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	command, err := buildProviderCommand(target, project, []string{}, []byte("prompt"), "", "", false, dispatchMode, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build %s command: %v", agent, err)
	}
	return strings.Join(command.Args, " ")
}

// TestDispatchGrantsWriteCapabilityOnlyWhenCommandsAreApproved pins the headless
// capability contract for every approvals mode. Both providers deny writes by
// default in their headless entry points (`codex exec` starts read-only, and
// `claude -p` denies Edit/Write), so without an explicit setting an approved
// command runs and then fails on its first write.
func TestDispatchGrantsWriteCapabilityOnlyWhenCommandsAreApproved(t *testing.T) {
	cases := []struct {
		mode           string
		codexSandbox   string
		claudePermMode string
		grokSandbox    string
		grokPermMode   string
	}{
		{config.ApprovalModeNone, config.CodexSandboxReadOnly, claudePermissionModeDontAsk, grok.SandboxReadOnly, "dontAsk"},
		{config.ApprovalModeMCP, config.CodexSandboxReadOnly, claudePermissionModeDontAsk, grok.SandboxReadOnly, "dontAsk"},
		{config.ApprovalModeCommands, config.CodexSandboxWorkspaceWrite, claudePermissionModeAcceptEdits, grok.SandboxWorkspace, "acceptEdits"},
		{config.ApprovalModeAll, config.CodexSandboxWorkspaceWrite, claudePermissionModeAcceptEdits, grok.SandboxWorkspace, "acceptEdits"},
	}
	for _, testCase := range cases {
		for _, dispatchMode := range []string{dispatchModeFresh, dispatchModeResume} {
			codexArgs := dispatchArgsForMode(t, AgentCodex, testCase.mode, dispatchMode)
			wantCodex := "-c " + config.CodexSandboxModeKey + "=" + testCase.codexSandbox
			if !strings.Contains(codexArgs, wantCodex) {
				t.Errorf("Codex %s/%s args %q omitted %q", testCase.mode, dispatchMode, codexArgs, wantCodex)
			}
			if strings.Contains(codexArgs, config.CodexSandboxDangerFullAccess) {
				t.Errorf("Codex %s/%s args %q escalated to full access", testCase.mode, dispatchMode, codexArgs)
			}

			claudeArgs := dispatchArgsForMode(t, AgentClaude, testCase.mode, dispatchMode)
			wantClaude := "--permission-mode " + testCase.claudePermMode
			if !strings.Contains(claudeArgs, wantClaude) {
				t.Errorf("Claude %s/%s args %q omitted %q", testCase.mode, dispatchMode, claudeArgs, wantClaude)
			}
			if strings.Contains(claudeArgs, "--dangerously-skip-permissions") {
				t.Errorf("Claude %s/%s args %q skipped permissions outside yolo", testCase.mode, dispatchMode, claudeArgs)
			}

			grokArgs := dispatchArgsForMode(t, AgentGrok, testCase.mode, dispatchMode)
			wantGrokSandbox := "--sandbox " + testCase.grokSandbox
			if !strings.Contains(grokArgs, wantGrokSandbox) {
				t.Errorf("Grok %s/%s args %q omitted %q", testCase.mode, dispatchMode, grokArgs, wantGrokSandbox)
			}
			wantGrokMode := "--permission-mode " + testCase.grokPermMode
			if !strings.Contains(grokArgs, wantGrokMode) {
				t.Errorf("Grok %s/%s args %q omitted %q", testCase.mode, dispatchMode, grokArgs, wantGrokMode)
			}
		}
	}
}

// TestYOLODispatchKeepsFullBypassWithoutSandboxDowngrade guards the one mode
// that already worked, so filling in the other modes cannot narrow it.
func TestYOLODispatchKeepsFullBypassWithoutSandboxDowngrade(t *testing.T) {
	for _, dispatchMode := range []string{dispatchModeFresh, dispatchModeResume} {
		codexArgs := dispatchArgsForMode(t, AgentCodex, config.ApprovalModeYOLO, dispatchMode)
		for _, want := range []string{"approval_policy=never", config.CodexSandboxModeKey + "=" + config.CodexSandboxDangerFullAccess, "web_search=live"} {
			if !strings.Contains(codexArgs, want) {
				t.Errorf("Codex yolo/%s args %q omitted %q", dispatchMode, codexArgs, want)
			}
		}
		if strings.Contains(codexArgs, config.CodexSandboxWorkspaceWrite) {
			t.Errorf("Codex yolo/%s args %q downgraded the sandbox", dispatchMode, codexArgs)
		}

		claudeArgs := dispatchArgsForMode(t, AgentClaude, config.ApprovalModeYOLO, dispatchMode)
		if !strings.Contains(claudeArgs, "--dangerously-skip-permissions") {
			t.Errorf("Claude yolo/%s args %q dropped the bypass flag", dispatchMode, claudeArgs)
		}
		// bypassPermissions already allows everything; a narrower mode or an
		// allowlist would only re-introduce prompts yolo exists to remove.
		if strings.Contains(claudeArgs, "--permission-mode") || strings.Contains(claudeArgs, "--allowedTools") {
			t.Errorf("Claude yolo/%s args %q constrained a full bypass", dispatchMode, claudeArgs)
		}

		grokArgs := dispatchArgsForMode(t, AgentGrok, config.ApprovalModeYOLO, dispatchMode)
		if !strings.Contains(grokArgs, "--permission-mode bypassPermissions --always-approve") {
			t.Errorf("Grok yolo/%s args %q dropped the bypass flags", dispatchMode, grokArgs)
		}
		if strings.Contains(grokArgs, "--sandbox") {
			t.Errorf("Grok yolo/%s args %q set a sandbox", dispatchMode, grokArgs)
		}
	}
}

// TestClaudeDispatchDeliversApprovalsOnCommandLine covers the reason dispatch
// cannot rely on the generated settings file: Claude applies a project's
// permissions.allow rules only after the workspace trust dialog is accepted,
// and that dialog never appears under --print.
func TestClaudeDispatchDeliversApprovalsOnCommandLine(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	enabled := true
	project.Config.Approvals.Mode = config.ApprovalModeAll
	project.CommandsAllow = []string{"git status", "npm test"}
	project.Config.MCP.Servers = append(project.Config.MCP.Servers, config.MCPServer{
		ID:      "custom-server",
		Enabled: &enabled,
		Command: "custom-mcp",
	})

	target, ok := lookupTarget(AgentClaude)
	if !ok {
		t.Fatal("Claude target missing from registry")
	}
	run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	command, err := buildProviderCommand(target, project, []string{}, []byte("prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Claude command: %v", err)
	}

	for _, want := range []string{"Bash(git status:*)", "Bash(npm test:*)", "mcp__custom-server__*"} {
		if !argPairPresent(command.Args, "--allowedTools", want) {
			t.Errorf("Claude args %v omitted --allowedTools %q", command.Args, want)
		}
	}

	// A pattern containing a comma must survive intact; a comma-joined list
	// would split it into two unusable rules.
	project.CommandsAllow = []string{"awk -F, {print}"}
	project.Config.MCP.Servers = nil
	command, err = buildProviderCommand(target, project, []string{}, []byte("prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Claude command with comma pattern: %v", err)
	}
	if !argPairPresent(command.Args, "--allowedTools", "Bash(awk -F, {print}:*)") {
		t.Errorf("Claude args %v split a comma-bearing command pattern", command.Args)
	}
}

// TestDispatchRespectsExplicitProviderPermissionPins keeps provider-native
// passthrough authoritative so a project can still choose its own policy.
func TestDispatchRespectsExplicitProviderPermissionPins(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = config.ApprovalModeAll
	project.CommandsAllow = []string{"git status"}
	project.Config.Agents.Codex.AgentSpecific = config.ProviderPassthrough{
		config.CodexSandboxModeKey: config.CodexSandboxReadOnly,
	}
	project.Config.Agents.Claude.AgentSpecific = config.ProviderPassthrough{
		claudePermissionsPassthroughKey: map[string]any{claudeDefaultModePassthroughKey: "plan"},
	}
	run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}

	codexTarget, _ := lookupTarget(AgentCodex)
	codexCommand, err := buildProviderCommand(codexTarget, project, []string{}, []byte("prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Codex command: %v", err)
	}
	if strings.Contains(strings.Join(codexCommand.Args, " "), config.CodexSandboxModeKey+"=") {
		t.Errorf("Codex args %v overrode a pinned sandbox_mode", codexCommand.Args)
	}

	claudeTarget, _ := lookupTarget(AgentClaude)
	claudeCommand, err := buildProviderCommand(claudeTarget, project, []string{}, []byte("prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Claude command: %v", err)
	}
	claudeArgs := strings.Join(claudeCommand.Args, " ")
	if strings.Contains(claudeArgs, "--permission-mode") {
		t.Errorf("Claude args %q overrode a pinned permissions.defaultMode", claudeArgs)
	}
	// The pinned mode replaces only the mode. The allowlist still has to reach
	// the CLI, because the settings file it lives in stays untrusted under -p.
	if !argPairPresent(claudeCommand.Args, "--allowedTools", "Bash(git status:*)") {
		t.Errorf("Claude args %v dropped the allowlist alongside a pinned mode", claudeCommand.Args)
	}
}

// claudeStreamEvents runs one Claude JSONL record through the production path.
// It goes through reduceStructuredTestEvent, which parses with the selective
// parser rather than encoding/json; the two disagree on shape for nested
// fields, so a reducer change has to be proven against the parser that
// actually feeds it.
func claudeStreamEvents(t *testing.T, record string) []providerEvent {
	t.Helper()
	events, err := reduceStructuredTestEvent(AgentClaude, runtimeSessionID, []byte(record))
	if err != nil {
		t.Fatalf("read structured events: %v", err)
	}
	return events
}

// TestClaudeDeniedToolCallsFailDispatch covers the silent-failure case: Claude
// reports a run whose tool calls were denied as a success, with is_error false
// and a fluent final answer, so accepting that answer would report work that
// never happened as complete.
func TestClaudeDeniedToolCallsFailDispatch(t *testing.T) {
	denied := claudeStreamEvents(t, `{"type":"result","session_id":"`+runtimeSessionID+`","is_error":false,`+
		`"result":"I attempted to create hello.txt, but the write needs your permission.",`+
		`"permission_denials":[{"tool_name":"Write","tool_use_id":"toolu_01"}]}`)
	var failure *providerEvent
	for index, event := range denied {
		if event.Kind == eventFailure {
			failure = &denied[index]
		}
		if event.Kind == eventComplete {
			t.Fatalf("denied result completed the dispatch: %#v", denied)
		}
	}
	if failure == nil {
		t.Fatalf("denied result events = %#v, want a failure", denied)
	}
	if !strings.Contains(failure.Reason, "Write") {
		t.Errorf("failure reason %q did not name the denied tool", failure.Reason)
	}

	// The denied tool name is an example, not the signal. A denial reported
	// without one still has to fail, or a provider change that drops the field
	// would silently restore the success this test exists to prevent.
	unnamed := claudeStreamEvents(t, `{"type":"result","session_id":"`+runtimeSessionID+`","is_error":false,`+
		`"result":"I could not create hello.txt.","permission_denials":[{"tool_use_id":"toolu_01"}]}`)
	for _, event := range unnamed {
		if event.Kind == eventComplete {
			t.Fatalf("denial without a tool name completed the dispatch: %#v", unnamed)
		}
	}
	if !slices.ContainsFunc(unnamed, func(event providerEvent) bool { return event.Kind == eventFailure }) {
		t.Fatalf("denial without a tool name events = %#v, want a failure", unnamed)
	}

	// Claude reports an empty list on a clean run, which must stay a success or
	// every dispatch would fail.
	clean := claudeStreamEvents(t, `{"type":"result","session_id":"`+runtimeSessionID+`","is_error":false,`+
		`"result":"Created hello.txt.","permission_denials":[]}`)
	var answer string
	for _, event := range clean {
		if event.Kind == eventFailure {
			t.Fatalf("clean result produced a failure: %#v", clean)
		}
		if event.Kind == eventAnswer {
			answer = event.Answer
		}
	}
	if answer != "Created hello.txt." {
		t.Errorf("clean result answer = %q, want the final answer", answer)
	}
}

// argPairPresent reports whether args contains flag immediately followed by value.
func argPairPresent(args []string, flag string, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag && args[index+1] == value {
			return true
		}
	}
	return false
}
