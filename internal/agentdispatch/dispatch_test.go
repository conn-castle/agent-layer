package agentdispatch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
)

const antigravityStructuredOK = `while IFS= read -r line; do
  if [ -n "${AL_TEST_LOG:-}" ]; then
    printf 'ACP=%s\n' "$line" >> "$AL_TEST_LOG"
  fi
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"22222222-2222-4222-8222-222222222222"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*|*'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"22222222-2222-4222-8222-222222222222","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"%s"}}}}\n' "${AL_TEST_AGY_ANSWER:-agy ok}"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2,"thoughtTokens":1,"cachedReadTokens":0}}}\n' "$id"
      ;;
  esac
done`

func TestProviderAssignedSessionPreservesResolvedSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		agent, executable, response, model, effort string
		repoConfig                                 dispatchRepoConfig
	}{
		{
			agent: AgentCodex, executable: "codex",
			response: `printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'`,
			model:    "gpt-5.4", effort: "high",
			repoConfig: dispatchRepoConfig{CodexModel: "gpt-5.4", CodexReasoningEffort: "high"},
		},
		{
			agent: AgentAntigravity, executable: "agy", response: antigravityStructuredOK,
			model: "gemini-4-neon-medium", effort: "medium",
			repoConfig: dispatchRepoConfig{AntigravityModel: "gemini-4-neon-medium"},
		},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			root := writeDispatchRepo(t, tc.repoConfig)
			binDir := t.TempDir()
			logPath := filepath.Join(t.TempDir(), "provider.log")
			writeDispatchStub(t, binDir, tc.executable, tc.response)
			if err := executeFreshDispatch(dispatchExecRequest{
				Root: root, Agent: tc.agent, Prompt: "First turn",
				Env:      []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
				LookPath: mockLookPath(binDir),
			}); err != nil {
				t.Fatal(err)
			}
			sessions, err := listSessions(root)
			if err != nil || len(sessions) != 1 {
				t.Fatalf("fresh dispatch sessions = %#v, %v", sessions, err)
			}
			session := sessions[0]
			if session.ProviderSessionID == "" || session.Model != tc.model || session.ReasoningEffort != tc.effort || !session.TargetPinned {
				t.Fatalf("fresh dispatch lost resolved settings: %#v", session)
			}
			reserved := reserveContinuationInTest(t, root, session.Name)
			if err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Second turn",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[tc.agent], nil },
				launchWorker:  fakeWorkerLauncher(nil),
			}); err != nil {
				t.Fatal(err)
			}
			var request workerRequest
			if err := readJSON(filepath.Join(filepathForRun(root, reserved.InvocationID), workerRequestFile), &request); err != nil {
				t.Fatal(err)
			}
			if request.Model != tc.model || request.Effort != tc.effort || !request.TargetPinned {
				t.Fatalf("continuation did not inherit resolved settings: %#v", request)
			}
		})
	}
}

func TestRunUsesSuppliedRepositoryRootWithoutCreatingWorktree(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- fixed test command with a test-owned path.
		// Resolve the repository from the path above, never from an inherited
		// GIT_DIR: git exports it to hooks, so under pre-commit this fixture would
		// otherwise operate on the developer's own checkout.
		cmd.Env = gitenv.WithoutDiscovery()
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return string(output)
	}
	git("init")
	before := git("worktree", "list", "--porcelain")

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf 'PWD=%s\n' "$PWD" >> "$AL_TEST_LOG"
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'`)
	if err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentCodex,
		Prompt:   "work here",
		Env:      []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
		LookPath: mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	assertFileContains(t, logPath, "PWD="+resolvedRoot)
	if after := git("worktree", "list", "--porcelain"); after != before {
		t.Fatalf("dispatch changed git worktrees:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunLaunchesProviderWithSkillsInSuppliedWorkingDirectory(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	workingDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf 'PWD=%s\n' "$PWD" >> "$AL_TEST_LOG"
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'`)

	if err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		WorkDir:  workingDir,
		Agent:    AgentCodex,
		Skill:    "review-plan",
		Prompt:   "work in caller checkout",
		Env:      []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
		LookPath: mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	resolvedWorkingDir, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	assertFileContains(t, logPath, "PWD="+resolvedWorkingDir)
	if _, err := os.Stat(filepath.Join(workingDir, ".agents", "skills", "review-plan", "SKILL.md")); err != nil {
		t.Fatalf("stat working-directory skill projection: %v", err)
	}
}

func TestBuildOptionsJSONShape(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{},
		LookPath: func(string) (string, error) {
			return "/bin/mock", nil
		},
		VersionLookup: func(_ string, agent string) (string, error) {
			return supportedProviderVersions[agent], nil
		},
	})
	if err != nil {
		t.Fatalf("BuildOptions error: %v", err)
	}
	var claude AgentOption
	for _, target := range options.Agents {
		if target.Agent == AgentClaude {
			claude = target
		}
	}
	if !claude.Model.OverrideSupported || len(claude.Model.Suggestions) != 0 || !claude.Model.AllowCustom || claude.Model.DiscoveryError == "" || claude.Model.Source != "unavailable" {
		t.Fatalf("unexpected claude model metadata: %#v", claude.Model)
	}
	var codex AgentOption
	for _, target := range options.Agents {
		if target.Agent == AgentCodex {
			codex = target
		}
	}
	if !codex.Model.OverrideSupported || len(codex.Model.Suggestions) != 0 || !codex.Model.AllowCustom || codex.Model.DiscoveryError == "" || codex.Model.Source != "unavailable" {
		t.Fatalf("unexpected codex model metadata: %#v", codex.Model)
	}
	if !codex.ReasoningEffort.OverrideSupported || !codex.ReasoningEffort.AllowCustom {
		t.Fatalf("unexpected codex reasoning effort metadata: %#v", codex.ReasoningEffort)
	}
	if want := config.FieldOptionValues(config.CodexReasoningEffortFieldKey); !slices.Equal(codex.ReasoningEffort.Suggestions, want) {
		t.Fatalf("codex reasoning effort suggestions = %v, want shared catalog %v", codex.ReasoningEffort.Suggestions, want)
	}
	var agy AgentOption
	for _, target := range options.Agents {
		if target.Agent == AgentAntigravity {
			agy = target
		}
	}
	if !agy.Model.OverrideSupported || agy.Model.Configured != "Gemini 3.1 Pro (High)" || !agy.Model.AllowCustom {
		t.Fatalf("unexpected antigravity model metadata: %#v", agy.Model)
	}
	if len(agy.Model.Suggestions) != 0 || agy.Model.DiscoveryError == "" {
		t.Fatalf("unexpected antigravity model suggestions: %#v", agy.Model.Suggestions)
	}
	if agy.ReasoningEffort.OverrideSupported {
		t.Fatalf("antigravity reasoning_effort should remain unsupported: %#v", agy.ReasoningEffort)
	}
}

func TestBuildOptionsUsesTargetModelSuggestionProvider(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `if [ "$2" = "models" ]; then
  printf 'live\tLive Antigravity Model\nbackup\tBackup Antigravity Model\n'
fi`)

	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env: []string{
			"PATH=" + testPath(binDir),
			"AL_TEST_LOG=" + logPath,
		},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("BuildOptions error: %v", err)
	}
	var agy AgentOption
	for _, target := range options.Agents {
		if target.Agent == AgentAntigravity {
			agy = target
		}
	}
	got := strings.Join(agy.Model.Suggestions, ",")
	if got != "live,backup" {
		t.Fatalf("antigravity suggestions = %q", got)
	}
	assertFileContains(t, logPath, "ARG_0=--gemini_dir="+filepath.Join(root, ".agy"))
	assertFileContains(t, logPath, "ARG_1=models")
	assertFileContains(t, logPath, "AGY_CLI_DISABLE_AUTO_UPDATE=1")
}

func TestStartBlocksNestedDispatchAtDepthOne(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 1})
	err := Start(StartOptions{
		Root: root, Agent: AgentCodex, Prompt: "Review",
		Env: []string{clients.EnvDispatchActive + "=3"},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
		t.Fatalf("expected nested exit, got %T: %v", err, err)
	}
	if !strings.Contains(exitErr.Error(), "built-in subagent tool") {
		t.Fatalf("expected nested-dispatch error to mention the built-in subagent tool, got %q", exitErr.Error())
	}
}

func TestDispatchAllowsNestedDispatchWithinDefaultDepth(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"codex ok"}\n'`)
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:   root,
		Agent:  AgentCodex,
		Prompt: "Review",
		Env: []string{
			"PATH=" + testPath(binDir),
			clients.EnvDispatchActive + "=2",
			"AL_TEST_LOG=" + logPath,
		},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "codex ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, clients.EnvDispatchActive+"=3")
	// Positive env-passthrough contract: dispatch creates a per-run directory
	// via run.Create and exports AL_RUN_DIR / AL_RUN_ID via BuildEnv.
	assertFileContains(t, logPath, "AL_RUN_DIR=")
	assertFileContains(t, logPath, "AL_RUN_ID=")
	assertFileContains(t, logPath, updatewarn.EnvSuppress+"=1")
	// Negative env-passthrough contract: BuildEnv strips AL_SHIM_ACTIVE so the
	// shim marker does not leak into the dispatched child env.
	assertFileDoesNotContain(t, logPath, "AL_SHIM_ACTIVE=")
}

func TestDispatchAllowsNestedDispatchWithinConfiguredDepth(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"codex ok"}\n'`)
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:   root,
		Agent:  AgentCodex,
		Prompt: "Review",
		Env: []string{
			"PATH=" + testPath(binDir),
			clients.EnvDispatchActive + "=1",
			"AL_TEST_LOG=" + logPath,
		},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "codex ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, clients.EnvDispatchActive+"=2")
}

func TestStartBlocksNestedDispatchAtConfiguredDepth(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
	err := Start(StartOptions{
		Root: root, Agent: AgentCodex, Prompt: "Review",
		Env: []string{clients.EnvDispatchActive + "=2"},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
		t.Fatalf("expected nested exit, got %T: %v", err, err)
	}
}

func TestStartRejectsInvalidDispatchDepthEnv(t *testing.T) {
	t.Parallel()
	// A present-but-non-parseable AL_DISPATCH_ACTIVE fails loud rather than
	// silently defaulting to depth 0. Empty/whitespace counts as malformed: the
	// variable is only ever set by dispatch itself to a positive integer.
	for _, value := range []string{"bogus", "", "-1"} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
			err := Start(StartOptions{
				Root: root, Agent: AgentCodex, Prompt: "Review",
				Env: []string{clients.EnvDispatchActive + "=" + value},
			})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
				t.Fatalf("expected nested exit, got %T: %v", err, err)
			}
			if !strings.Contains(exitErr.Error(), clients.EnvDispatchActive) {
				t.Fatalf("expected %s in error, got %q", clients.EnvDispatchActive, exitErr.Error())
			}
		})
	}
}

func TestRunAntigravityUsesConfiguredModel(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", antigravityStructuredOK)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentAntigravity,
		Prompt:   "Review",
		Env:      env,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=acp")
	assertFileContains(t, logPath, "ARG_1=--stdio")
	assertFileContains(t, logPath, `"method":"session/set_model"`)
	assertFileContains(t, logPath, `"modelId":"Gemini 3.1 Pro (High)"`)
}

func TestRunAntigravityUsesModelOverride(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", antigravityStructuredOK)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentAntigravity,
		Model:    "Gemini 3.5 Flash (High)",
		Prompt:   "Review",
		Env:      env,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, `"method":"session/set_model"`)
	assertFileContains(t, logPath, `"modelId":"Gemini 3.5 Flash (High)"`)
	assertFileDoesNotContain(t, logPath, "Gemini 3.1 Pro (High)")
}

func TestRunAntigravityExactSlugCapturesStructuredEvents(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", antigravityStructuredOK)
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:            root,
		Agent:           AgentAntigravity,
		Model:           "gemini-3.5-flash-low",
		ReasoningEffort: "low",
		Prompt:          "Review",
		Env:             []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath, "AL_TEST_AGY_ANSWER=agy structured ok"},
		Stdout:          &stdout,
		Stderr:          &bytes.Buffer{},
		LookPath:        mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("exact-slug dispatch error: %v", err)
	}
	if stdout.String() != "agy structured ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=acp")
	assertFileContains(t, logPath, "ARG_1=--stdio")
	assertFileContains(t, logPath, `"method":"session/set_model"`)
	assertFileContains(t, logPath, `"modelId":"gemini-3.5-flash-low"`)
}

func TestRunAntigravityRejectsEmptySessionID(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":""}}\n' "$id"
      ;;
  esac
done`)
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:   root,
		Agent:  AgentAntigravity,
		Prompt: "Review",
		Env: []string{
			"PATH=" + testPath(binDir),
			"AL_TEST_LOG=" + logPath,
		},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if stdout.Len() != 0 {
		t.Fatalf("invalid Antigravity answer leaked to caller: %q", stdout.String())
	}
	if !strings.Contains(err.Error(), "empty sessionId") {
		t.Fatalf("empty session error = %q", err)
	}
}

func TestClaudeSkillPromptAndCommandConstruction(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{ClaudeModel: "opus", ClaudeReasoningEffort: "high", ClaudeLocalConfigDir: true})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "claude.log")
	promptPath := filepath.Join(t.TempDir(), "claude.prompt")
	writeDispatchStub(t, binDir, "claude", `printf '{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"claude ok"}}}\n'`)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
		"AL_TEST_PROMPT=" + promptPath,
	}
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentClaude,
		Skill:    "review-plan",
		Prompt:   "Review",
		Env:      env,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=--print")
	assertFileContains(t, logPath, "ARG_7=--model")
	assertFileContains(t, logPath, "ARG_8=opus")
	assertFileContains(t, logPath, "ARG_9=--effort")
	assertFileContains(t, logPath, "ARG_10=high")
	assertFileContains(t, logPath, "CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude-config"))
	assertFileContains(t, logPath, "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=0")
	assertFileContains(t, promptPath, "/review-plan\nReview")
	sessions, err := listSessions(root)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list sessions = %#v, %v", sessions, err)
	}
	record, err := loadRunRecord(root, sessions[0].RunID)
	if err != nil {
		t.Fatalf("load run record: %v", err)
	}
	if record.Model != "opus" || record.ReasoningEffort != "high" || record.Skill != "review-plan" {
		t.Fatalf("run execution configuration = %#v", record)
	}
}

func TestGrokFreshAndResumeDispatchPreserveProviderSession(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true")
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "grok.log")
	rpcLog := filepath.Join(t.TempDir(), "grok-rpc.log")
	writeDispatchStub(t, binDir, "grok", `
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$AL_TEST_RPC_LOG"
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token"}]}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"11111111-1111-4111-8111-111111111111"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"11111111-1111-4111-8111-111111111111"}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"11111111-1111-4111-8111-111111111111","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"grok ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","_meta":{"sessionId":"11111111-1111-4111-8111-111111111111","usage":{"inputTokens":10,"outputTokens":5}}}}\n' "$id"
      ;;
  esac
done
`)

	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root: root, Agent: AgentGrok, Prompt: "Review",
		Env:    []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath, "AL_TEST_RPC_LOG=" + rpcLog},
		Stdout: &stdout, Stderr: &bytes.Buffer{}, LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "grok ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	sessions, err := listSessions(root)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list sessions = %#v, %v", sessions, err)
	}
	session := sessions[0]
	if session.Agent != AgentGrok || session.State != sessionStateDurable {
		t.Fatalf("persisted Grok session = %#v", session)
	}
	if err := parseUUID(session.ProviderSessionID); err != nil {
		t.Fatalf("provider session ID %q: %v", session.ProviderSessionID, err)
	}
	assertFileContains(t, logPath, "ARG_0=--no-auto-update")
	assertFileContains(t, logPath, "ARG_1=--permission-mode")
	assertFileContains(t, logPath, "ARG_2=default")
	assertFileContains(t, logPath, "ARG_3=agent")
	assertFileContains(t, logPath, "ARG_4=--no-leader")
	assertFileContains(t, rpcLog, `"method":"session/new"`)

	stdout.Reset()
	err = executeContinueDispatch(dispatchExecRequest{
		Root: root, Prompt: "Continue",
		Env:    []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath, "AL_TEST_RPC_LOG=" + rpcLog},
		Stdout: &stdout, Stderr: &bytes.Buffer{}, LookPath: mockLookPath(binDir),
	}, session.Name)
	if err != nil {
		t.Fatalf("resume dispatch error: %v", err)
	}
	if stdout.String() != "grok ok" {
		t.Fatalf("resume stdout = %q", stdout.String())
	}
	resumed, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("load resumed session: %v", err)
	}
	if resumed.ProviderSessionID != session.ProviderSessionID {
		t.Fatalf("resume changed provider session ID from %q to %q", session.ProviderSessionID, resumed.ProviderSessionID)
	}
	assertFileContains(t, rpcLog, `"method":"session/resume"`)
	assertFileContains(t, rpcLog, session.ProviderSessionID)
}

func TestAntigravityCommandConstruction(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", antigravityStructuredOK)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentAntigravity,
		Prompt:   "Review",
		Env:      env,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=acp")
	assertFileContains(t, logPath, "ARG_1=--stdio")
	assertFileContains(t, logPath, "ARG_2=--gemini_dir="+filepath.Join(root, ".agy"))
	assertFileContains(t, logPath, "AGY_CLI_DISABLE_AUTO_UPDATE=1")
}

// TestCodexDownstreamRejectsCustomOverrideSuppressesProviderStderr proves
// that provider diagnostics remain private when a turn fails.
func TestCodexDownstreamRejectsCustomOverrideSuppressesProviderStderr(t *testing.T) {
	t.Parallel()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	// The stub looks for --model bogus-rejected-model on argv and exits
	// non-zero with a recognizable stderr message; any other invocation
	// succeeds. This mimics the contract documented in spec § CLI:
	// "If the downstream CLI rejects a custom override value, dispatch
	// exits 70 and preserves the target error text on stderr."
	stub := `
if printf '%s\n' "$@" | grep -qx "bogus-rejected-model"; then
  printf 'codex: model bogus-rejected-model is not recognized\n' >&2
  exit 2
fi
printf '{"type":"agent_message","message":"ok"}\n'
`
	writeDispatchStub(t, binDir, "codex", stub)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentCodex,
		Model:    "bogus-rejected-model",
		Prompt:   "Review",
		Env:      env,
		Stdout:   &stdout,
		Stderr:   &stderr,
		LookPath: mockLookPath(binDir),
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitTargetFailure {
		t.Fatalf("expected ExitTargetFailure (70), got %T: %v", err, err)
	}
	if strings.Contains(stderr.String(), "bogus-rejected-model is not recognized") {
		t.Fatalf("provider stderr leaked to caller: %q", stderr.String())
	}
}

func TestSyncRunExitErrorAttributesPostWriteCleanupAfterSuccessfulSync(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("%w: failed to unlock sync lock", sync.ErrPostWriteLockCleanup)
	exitErr := syncRunExitError(err)
	if exitErr.Code != ExitConfig {
		t.Fatalf("exit code = %d, want %d", exitErr.Code, ExitConfig)
	}
	if !strings.Contains(exitErr.Error(), "generated sync outputs succeeded") {
		t.Fatalf("dispatch cleanup error = %q, want successful-output attribution", exitErr.Error())
	}
	if strings.Contains(exitErr.Error(), "sync failed") {
		t.Fatalf("dispatch cleanup error = %q, must not misattribute successful sync as failed", exitErr.Error())
	}
}

type dispatchRepoConfig struct {
	AntigravityModel             string
	ClaudeModel                  string
	ClaudeReasoningEffort        string
	ClaudeLocalConfigDir         bool
	CodexModel                   string
	CodexReasoningEffort         string
	CodexLocalConfigDir          bool
	DispatchMaxDepth             int
	DispatchSessionRetentionDays int
}

func writeDispatchRepo(t *testing.T, repoConfig dispatchRepoConfig) string {
	t.Helper()
	root := t.TempDir()
	paths := config.DefaultPaths(root)
	for _, dir := range []string{paths.InstructionsDir, paths.SkillsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	localConfigLine := ""
	if repoConfig.ClaudeLocalConfigDir {
		localConfigLine = "local_config_dir = true\n"
	}
	codexLocalConfigLine := ""
	if repoConfig.CodexLocalConfigDir {
		codexLocalConfigLine = "local_config_dir = true\n"
	}
	antigravityModelLine := ""
	if repoConfig.AntigravityModel != "" {
		antigravityModelLine = fmt.Sprintf("model = %q\n", repoConfig.AntigravityModel)
	}
	dispatchBlock := ""
	if repoConfig.DispatchMaxDepth != 0 {
		dispatchBlock = fmt.Sprintf("max_depth = %d\n", repoConfig.DispatchMaxDepth)
	}
	if repoConfig.DispatchSessionRetentionDays != 0 {
		dispatchBlock += fmt.Sprintf("session_retention_days = %d\n", repoConfig.DispatchSessionRetentionDays)
	}
	configToml := fmt.Sprintf(`
[[instructions.local]]
selectors = ["00_rules.md"]
order = 0

[dispatch]
%s

[approvals]
mode = "all"

[agents.antigravity]
enabled = true
%s

[agents.claude]
enabled = true
model = %q
reasoning_effort = %q
%s
[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = true
model = %q
reasoning_effort = %q
%s

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false

[agents.grok]
enabled = false

[agents.muse]
enabled = false

[warnings]
instruction_token_threshold = 50000
mcp_server_threshold = 50
`, dispatchBlock, antigravityModelLine, repoConfig.ClaudeModel, repoConfig.ClaudeReasoningEffort, localConfigLine,
		repoConfig.CodexModel, repoConfig.CodexReasoningEffort, codexLocalConfigLine)
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	skillDir := filepath.Join(paths.SkillsDir, "review-plan")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skill := "---\nname: review-plan\ndescription: Review a plan.\n---\n\nReview it.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte(""), 0o600); err != nil {
		t.Fatalf("write commands.allow: %v", err)
	}
	block, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read gitignore block: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "gitignore.block"), block, 0o600); err != nil {
		t.Fatalf("write gitignore block: %v", err)
	}
	return root
}

func writeDispatchStub(t *testing.T, binDir string, name string, outputScript string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	path := filepath.Join(binDir, name)
	version := map[string]string{
		"claude": supportedProviderVersions[AgentClaude],
		"codex":  supportedProviderVersions[AgentCodex],
		"agy":    supportedProviderVersions[AgentAntigravity],
		"grok":   supportedProviderVersions[AgentGrok],
		"muse":   supportedProviderVersions[AgentMuse],
	}[name]
	drainStdin := `if [ -n "${AL_TEST_PROMPT:-}" ]; then
  cat > "$AL_TEST_PROMPT"
else
  cat >/dev/null
fi`
	if name == "agy" || name == "grok" {
		drainStdin = ""
	}
	content := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '%%s\n' %q
  exit 0
fi
{
  i=0
  for arg in "$@"; do
    echo "ARG_${i}=${arg}"
    i=$((i + 1))
  done
  env | grep -E '^(AL_|CODEX_HOME|CLAUDE_CONFIG_DIR|CLAUDE_CODE_|AGY_CLI|GROK_)' | sort || true
} >> "$AL_TEST_LOG"
%s
if [ %q = "codex" ]; then
  printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n'
fi
%s
if [ %q = "claude" ]; then
  session=""
  previous=""
  for arg in "$@"; do
    if [ "$previous" = "session" ] || [ "$previous" = "resume" ]; then
      session="$arg"
      previous=""
      continue
    fi
    case "$arg" in
      --session-id) previous="session" ;;
      --resume) previous="resume" ;;
    esac
  done
  printf '{"type":"result","session_id":"%%s","result":"ok","is_error":false}\n' "$session"
fi
if [ %q = "codex" ]; then
  printf '{"type":"turn.completed"}\n'
fi
`, version, drainStdin, name, outputScript, name, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test writes an executable shell stub in a test-owned bin directory.
		t.Fatalf("write stub: %v", err)
	}
}

func mockLookPath(binDir string) func(string) (string, error) {
	return func(name string) (string, error) {
		path := filepath.Join(binDir, name)
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
}

func testPath(binDir string) string {
	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %q in %s:\n%s", want, path, string(data))
	}
}

func assertFileDoesNotContain(t *testing.T, path string, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Fatalf("did not expect %q in %s:\n%s", unwanted, path, string(data))
	}
}
