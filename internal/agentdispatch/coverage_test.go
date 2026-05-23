package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestWriteOptionsRendersTextAndJSON(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})

	var text bytes.Buffer
	err := WriteOptions(OptionsRequest{
		Root: root,
		// Pass an explicit empty Env so the parent process's
		// AL_DISPATCH_CALLER_AGENT (if any) cannot flip the "caller: unknown"
		// assertion below.
		Env:    []string{},
		Stdout: &text,
		LookPath: func(string) (string, error) {
			return "", exec.ErrNotFound
		},
	})
	if err != nil {
		t.Fatalf("WriteOptions text error: %v", err)
	}
	gotText := text.String()
	for _, want := range []string{
		"Agent Dispatch options",
		"caller: unknown",
		"random pool: (none)",
		"- codex enabled=true installed=false dispatch_capable=false",
	} {
		if !strings.Contains(gotText, want) {
			t.Fatalf("expected %q in options text:\n%s", want, gotText)
		}
	}

	var jsonOut bytes.Buffer
	err = WriteOptions(OptionsRequest{
		Root:   root,
		JSON:   true,
		Env:    []string{clients.EnvDispatchCallerAgent + "=" + AgentCodex},
		Stdout: &jsonOut,
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/mock/claude", nil
			}
			return "", exec.ErrNotFound
		},
	})
	if err != nil {
		t.Fatalf("WriteOptions JSON error: %v", err)
	}
	var decoded OptionsResponse
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatalf("decode options JSON: %v\n%s", err, jsonOut.String())
	}
	if !decoded.Caller.Known || decoded.Caller.Agent != AgentCodex {
		t.Fatalf("unexpected caller in JSON: %#v", decoded.Caller)
	}
	if strings.Join(decoded.Random.Pool, ",") != AgentClaude {
		t.Fatalf("unexpected random pool in JSON: %#v", decoded.Random.Pool)
	}
}

func TestBuildOptionsRequiresRoot(t *testing.T) {
	_, err := BuildOptions(OptionsRequest{})
	_ = requireDispatchExitCode(t, err, ExitConfig)
}

func TestWriteOptionsPropagatesWriterError(t *testing.T) {
	err := writeOptionsText(failingWriter{}, &OptionsResponse{})
	if err == nil {
		t.Fatal("expected writer error")
	}

	err = WriteOptions(OptionsRequest{Root: " "})
	_ = requireDispatchExitCode(t, err, ExitConfig)
}

func TestWriteOptionsTextPropagatesLateWriterErrors(t *testing.T) {
	options := &OptionsResponse{
		Caller: CallerInfo{Known: true, Agent: AgentClaude},
		Random: RandomInfo{Pool: []string{AgentCodex}},
		Targets: []TargetOption{{
			Agent:           AgentCodex,
			Enabled:         true,
			Installed:       true,
			DispatchCapable: true,
		}},
	}
	for _, failAt := range []int{2, 3, 4} {
		writer := &failAtWriter{failAt: failAt}
		err := writeOptionsText(writer, options)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("failAt=%d expected write failure, got %v", failAt, err)
		}
	}
}

func TestResolveTargetUsesConfiguredDefaults(t *testing.T) {
	cfg := dispatchCoverageConfig(AgentCodex, AgentClaude, AgentAntigravity)
	cfg.Agents.Codex.Dispatch.DefaultAgent = AgentClaude

	resolved, err := resolveTarget(cfg, RunOptions{}, AgentCodex, true)
	if err != nil {
		t.Fatalf("resolveTarget explicit default error: %v", err)
	}
	if resolved.Target.Name != AgentClaude {
		t.Fatalf("target = %s, want %s", resolved.Target.Name, AgentClaude)
	}
	if !strings.Contains(resolved.Notice, "agents.codex.dispatch.default_agent") {
		t.Fatalf("expected implicit default notice, got %q", resolved.Notice)
	}

	resolved, err = resolveTarget(cfg, RunOptions{
		LookPath:     alwaysFound,
		ChooseRandom: chooseOnly(AgentAntigravity),
	}, AgentClaude, true)
	if err != nil {
		t.Fatalf("resolveTarget random default error: %v", err)
	}
	if resolved.Target.Name != AgentAntigravity {
		t.Fatalf("target = %s, want %s", resolved.Target.Name, AgentAntigravity)
	}
	if !strings.Contains(resolved.Notice, "random selection") {
		t.Fatalf("expected random notice, got %q", resolved.Notice)
	}
}

func TestChooseRandomTargetErrorBranches(t *testing.T) {
	_, err := chooseRandomTarget(config.Config{}, "", false, alwaysFound, nil)
	_ = requireDispatchExitCode(t, err, ExitUnavailable)

	cfg := dispatchCoverageConfig(AgentCodex)
	selected, err := chooseRandomTarget(cfg, "", false, alwaysFound, nil)
	if err != nil {
		t.Fatalf("chooseRandomTarget single target error: %v", err)
	}
	if selected != AgentCodex {
		t.Fatalf("selected = %s, want %s", selected, AgentCodex)
	}

	sentinel := errors.New("chooser failed")
	_, err = chooseRandomTarget(cfg, "", false, alwaysFound, func([]string) (string, error) {
		return "", sentinel
	})
	exitErr := requireDispatchExitCode(t, err, ExitTargetFailure)
	if !errors.Is(exitErr, sentinel) {
		t.Fatalf("expected wrapped chooser error, got %v", err)
	}

	_, err = chooseRandomTarget(cfg, "", false, alwaysFound, chooseOnly("invalid"))
	_ = requireDispatchExitCode(t, err, ExitTargetFailure)
}

func TestConfigureTargetEnvironment(t *testing.T) {
	root := t.TempDir()
	claudeExpected := root + "/.claude-config"
	localClaude := true

	env := configureClaudeEnv(root, nil, config.ClaudeConfig{LocalConfigDir: &localClaude}, nil)
	if got, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR"); !ok || got != claudeExpected {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, %t; want %q", got, ok, claudeExpected)
	}

	var stderr bytes.Buffer
	env = configureClaudeEnv(root, []string{"CLAUDE_CONFIG_DIR=/custom"}, config.ClaudeConfig{LocalConfigDir: &localClaude}, &stderr)
	if got, _ := clients.GetEnv(env, "CLAUDE_CONFIG_DIR"); got != "/custom" {
		t.Fatalf("expected custom Claude config dir to be preserved, got %q", got)
	}
	if !strings.Contains(stderr.String(), "/custom") || !strings.Contains(stderr.String(), claudeExpected) {
		t.Fatalf("expected Claude config warning, got %q", stderr.String())
	}

	env = configureClaudeEnv(root, []string{"CLAUDE_CONFIG_DIR=" + claudeExpected, "KEEP=1"}, config.ClaudeConfig{}, nil)
	if _, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatalf("expected default Claude config dir to be unset when local_config_dir is false: %#v", env)
	}

	codexExpected := root + "/.codex"
	env = configureCodexEnv(root, nil, nil)
	if got, ok := clients.GetEnv(env, "CODEX_HOME"); !ok || got != codexExpected {
		t.Fatalf("CODEX_HOME = %q, %t; want %q", got, ok, codexExpected)
	}

	stderr.Reset()
	env = configureCodexEnv(root, []string{"CODEX_HOME=/custom-codex"}, &stderr)
	if got, _ := clients.GetEnv(env, "CODEX_HOME"); got != "/custom-codex" {
		t.Fatalf("expected custom CODEX_HOME to be preserved, got %q", got)
	}
	if !strings.Contains(stderr.String(), "/custom-codex") || !strings.Contains(stderr.String(), codexExpected) {
		t.Fatalf("expected Codex home warning, got %q", stderr.String())
	}
}

func TestAdapterErrorMapping(t *testing.T) {
	_ = requireDispatchExitCode(t, startError(AgentCodex, exec.ErrNotFound), ExitUnavailable)
	_ = requireDispatchExitCode(t, runTarget(targetMeta{Name: "unknown"}, &config.ProjectConfig{}, nil, nil, RunOptions{}), ExitUsage)

	if err := mapWaitError(AgentCodex, nil); err != nil {
		t.Fatalf("mapWaitError nil = %v", err)
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exit 7")
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected command to fail")
	}
	exitErr := requireDispatchExitCode(t, mapWaitError(AgentCodex, runErr), ExitTargetFailure)
	if !strings.Contains(exitErr.Error(), "code 7") {
		t.Fatalf("expected exit code in mapped error, got %q", exitErr.Error())
	}

	sentinel := errors.New("wait failed")
	exitErr = requireDispatchExitCode(t, mapWaitError(AgentCodex, sentinel), ExitTargetFailure)
	if !errors.Is(exitErr, sentinel) {
		t.Fatalf("expected wrapped wait error, got %v", exitErr)
	}
}

func TestRunCodexUsesModelAndReasoningOverrides(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	promptPath := filepath.Join(t.TempDir(), "codex.prompt")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"ok"}\n'`)

	var stdout bytes.Buffer
	err := runCodex(
		targetMeta{Name: AgentCodex, Binary: filepath.Join(binDir, "codex")},
		&config.ProjectConfig{Root: root},
		[]string{"AL_TEST_LOG=" + logPath, "AL_TEST_PROMPT=" + promptPath},
		[]byte("Prompt"),
		RunOptions{Model: "gpt-5", ReasoningEffort: "high", Stdout: &stdout, Stderr: &bytes.Buffer{}},
		defaultCommandFactory,
	)
	if err != nil {
		t.Fatalf("runCodex error: %v", err)
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=exec")
	assertFileContains(t, logPath, "ARG_2=--model")
	assertFileContains(t, logPath, "ARG_3=gpt-5")
	assertFileContains(t, logPath, "ARG_4=-c")
	assertFileContains(t, logPath, "ARG_5=model_reasoning_effort=high")
	assertFileContains(t, logPath, "CODEX_HOME="+filepath.Join(root, ".codex"))
	assertFileContains(t, promptPath, "Prompt")
}

func TestRunClaudeUsesOverridesAndYoloFlag(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "claude.log")
	writeDispatchStub(t, binDir, "claude", `printf '{"delta":{"type":"text_delta","text":"ok"}}\n'`)

	var stdout bytes.Buffer
	err := runClaude(
		targetMeta{Name: AgentClaude, Binary: filepath.Join(binDir, "claude")},
		&config.ProjectConfig{
			Root: root,
			Config: config.Config{
				Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
				Agents: config.AgentsConfig{
					Claude: config.ClaudeConfig{Model: "config-model", ReasoningEffort: "config-effort"},
				},
			},
		},
		[]string{"AL_TEST_LOG=" + logPath},
		[]byte("Prompt"),
		RunOptions{Model: "override-model", ReasoningEffort: "override-effort", Stdout: &stdout, Stderr: &bytes.Buffer{}},
		defaultCommandFactory,
	)
	if err != nil {
		t.Fatalf("runClaude error: %v", err)
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_5=--model")
	assertFileContains(t, logPath, "ARG_6=override-model")
	assertFileContains(t, logPath, "ARG_7=--effort")
	assertFileContains(t, logPath, "ARG_8=override-effort")
	assertFileContains(t, logPath, "ARG_9=--dangerously-skip-permissions")
}

func TestRunAntigravityUsesYoloFlag(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)

	var stdout bytes.Buffer
	err := runAntigravity(
		targetMeta{Name: AgentAntigravity, Binary: filepath.Join(binDir, "agy")},
		&config.ProjectConfig{
			Root:   root,
			Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO}},
		},
		[]string{"AL_TEST_LOG=" + logPath},
		[]byte("Prompt"),
		RunOptions{Stdout: &stdout, Stderr: &bytes.Buffer{}},
		defaultCommandFactory,
	)
	if err != nil {
		t.Fatalf("runAntigravity error: %v", err)
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=--gemini_dir="+filepath.Join(root, ".agy"))
	assertFileContains(t, logPath, "ARG_1=--dangerously-skip-permissions")
	assertFileContains(t, logPath, "ARG_2=--print-timeout")
	assertFileContains(t, logPath, "ARG_4=--print")
}

func TestDecodeStructuredOutputAlternateShapes(t *testing.T) {
	var stdout bytes.Buffer
	err := decodeClaudeStream(strings.NewReader(
		`{"delta":{"type":"text_delta","text":"top-level"}}`+"\n"+
			`{"delta":{"type":"thinking","text":"ignored"}}`+"\n",
	), &stdout, nil)
	if err != nil {
		t.Fatalf("decodeClaudeStream error: %v", err)
	}
	if stdout.String() != "top-level" {
		t.Fatalf("Claude stdout = %q", stdout.String())
	}

	stdout.Reset()
	var stderr bytes.Buffer
	err = decodeCodexStream(strings.NewReader(
		"\n"+
			`{"item":{"type":"agent_message","text":"nested"}}`+"\n"+
			`{"type":"turn.started"}`+"\n",
	), &stdout, &stderr)
	if err != nil {
		t.Fatalf("decodeCodexStream error: %v", err)
	}
	if stdout.String() != "nested" {
		t.Fatalf("Codex stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "codex: turn.started") {
		t.Fatalf("expected Codex progress event, got %q", stderr.String())
	}
}

func TestExitErrorErrorAndUnwrapContracts(t *testing.T) {
	var nilExit *ExitError
	if nilExit.Error() != "" || nilExit.Unwrap() != nil {
		t.Fatalf("nil ExitError methods returned Error=%q Unwrap=%v", nilExit.Error(), nilExit.Unwrap())
	}
	if got := (&ExitError{Code: 42}).Error(); got != "dispatch exit 42" {
		t.Fatalf("fallback error text = %q", got)
	}

	sentinel := errors.New("root cause")
	wrapped := wrapExitError(ExitTargetFailure, "", sentinel)
	if wrapped.Error() != sentinel.Error() {
		t.Fatalf("wrapped error text = %q, want %q", wrapped.Error(), sentinel.Error())
	}
	if !errors.Is(wrapped, sentinel) {
		t.Fatalf("expected errors.Is to match wrapped cause")
	}
}

func TestResolvePromptAdditionalBranches(t *testing.T) {
	got, err := ResolvePrompt(nil, strings.NewReader("ignored"), false)
	if err != nil {
		t.Fatalf("ResolvePrompt no read error: %v", err)
	}
	if got != "" {
		t.Fatalf("prompt = %q, want empty when stdin read is disabled", got)
	}

	_, err = ResolvePrompt(nil, errorReader{}, true)
	_ = requireDispatchExitCode(t, err, ExitUsage)

	_, err = BuildChildPrompt(&config.ProjectConfig{
		Skills: []config.Skill{{Name: "review-plan"}},
	}, "unknown", "Review", "review-plan")
	_ = requireDispatchExitCode(t, err, ExitUsage)

	err = validateSkillProjection(t.TempDir(), targetMeta{Name: AgentCodex, SharedSkillProject: true}, "missing")
	exitErr := requireDispatchExitCode(t, err, ExitConfig)
	if !strings.Contains(exitErr.Error(), filepath.Join(".agents", "skills", "missing", "SKILL.md")) {
		t.Fatalf("expected shared skill projection path in error, got %q", exitErr.Error())
	}
}

func TestRunReportsMissingBinaryBeforeSync(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=/missing"},
		LookPath: func(string) (string, error) {
			return "", exec.ErrNotFound
		},
	})
	_ = requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestRunAdditionalPreflightBranches(t *testing.T) {
	// Pass an explicit Env so a parent process with AL_DISPATCH_ACTIVE=1
	// cannot short-circuit Run() with ExitNested before it reaches the
	// missing-config branch under test.
	_ = requireDispatchExitCode(t, Run(RunOptions{Root: t.TempDir(), Env: []string{"PATH=/missing"}}), ExitConfig)

	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root:            root,
		Agent:           AgentAntigravity,
		ReasoningEffort: "high",
		PromptArgs:      []string{"Review"},
		Env:             []string{"PATH=/bin"},
		LookPath:        alwaysFound,
	})
	_ = requireDispatchExitCode(t, err, ExitUsage)

	disableAgentInDispatchConfig(t, root, AgentCodex)
	err = Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=/bin"},
		LookPath:   alwaysFound,
	})
	_ = requireDispatchExitCode(t, err, ExitConfig)
}

func TestRunYoloModeWarnsAndPassesPermissionFlag(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, `mode = "all"`, `mode = "yolo"`)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
		Stdout:     &stdout,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run yolo error: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	// Pin the exact canonical YOLO acknowledgement (not just any "yolo"
	// substring, which a stray debug line or stack trace could satisfy).
	if !strings.Contains(stderr.String(), messages.WarningsPolicyYOLOAck) {
		t.Fatalf("expected YOLO acknowledgement %q in stderr, got %q", messages.WarningsPolicyYOLOAck, stderr.String())
	}
	assertFileContains(t, logPath, "ARG_1=--dangerously-skip-permissions")
}

// TestWriteOptionsTextIncludesPerTargetContract exercises F7: the
// human-readable text mode must emit the full per-target contract required
// by spec § CLI (random eligibility plus exclusion reason, streaming caps,
// configured model/reasoning_effort with override+allow_custom+suggestions,
// and the unavailable_reasons list).
func TestWriteOptionsTextIncludesPerTargetContract(t *testing.T) {
	exclusion := "caller"
	var text bytes.Buffer
	err := writeOptionsText(&text, &OptionsResponse{
		Caller: CallerInfo{Known: true, Agent: AgentCodex},
		Random: RandomInfo{Pool: []string{AgentAntigravity}},
		Targets: []TargetOption{
			{
				Agent:                 AgentCodex,
				Enabled:               true,
				Installed:             true,
				DispatchCapable:       true,
				RandomEligible:        false,
				RandomExclusionReason: &exclusion,
				Streaming:             StreamingOption{AnswerText: "final", Progress: "partial"},
				Model: FieldOption{
					OverrideSupported: true,
					Configured:        "gpt-5",
					Suggestions:       []string{"gpt-5", "gpt-5-mini"},
					AllowCustom:       true,
				},
				ReasoningEffort: FieldOption{
					OverrideSupported: true,
					Configured:        "high",
					Suggestions:       []string{"low", "medium", "high"},
					AllowCustom:       true,
				},
				UnavailableReasons: []string{},
			},
			{
				Agent:              AgentAntigravity,
				Enabled:            true,
				Installed:          false,
				DispatchCapable:    false,
				RandomEligible:     false,
				Streaming:          StreamingOption{AnswerText: "partial", Progress: "none"},
				Model:              FieldOption{OverrideSupported: false},
				ReasoningEffort:    FieldOption{OverrideSupported: false},
				UnavailableReasons: []string{"binary_not_found"},
			},
		},
	})
	if err != nil {
		t.Fatalf("writeOptionsText error: %v", err)
	}
	got := text.String()
	for _, want := range []string{
		// Codex block (random-eligible false with caller exclusion reason).
		"- codex enabled=true installed=true dispatch_capable=true",
		"random_eligible: false, reason: caller",
		"streaming: answer_text=final progress=partial",
		"model: configured=gpt-5 override=true allow_custom=true suggestions=[gpt-5, gpt-5-mini]",
		"reasoning_effort: configured=high override=true allow_custom=true suggestions=[low, medium, high]",
		"unavailable_reasons: [none]",
		// Antigravity block (not dispatch-capable, no override support).
		"- antigravity enabled=true installed=false dispatch_capable=false",
		"random_eligible: false\n",
		"streaming: answer_text=partial progress=none",
		"model: configured=none override=false allow_custom=false suggestions=[]",
		"unavailable_reasons: [binary_not_found]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in options text:\n%s", want, got)
		}
	}
}

func TestWriteOptionsTextKnownCallerAndRegistryFallbacks(t *testing.T) {
	var text bytes.Buffer
	err := writeOptionsText(&text, &OptionsResponse{
		Caller: CallerInfo{Known: true, Agent: AgentClaude},
		Random: RandomInfo{Pool: []string{AgentCodex}},
		Targets: []TargetOption{{
			Agent:           AgentCodex,
			Enabled:         true,
			Installed:       true,
			DispatchCapable: true,
		}},
	})
	if err != nil {
		t.Fatalf("writeOptionsText known caller error: %v", err)
	}
	got := text.String()
	for _, want := range []string{"caller: claude", "random pool: codex", "- codex enabled=true installed=true dispatch_capable=true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in known-caller options text:\n%s", want, got)
		}
	}

	if validTargetOrRandom("bogus") {
		t.Fatal("bogus target should not be valid")
	}
	if caller, ok := knownCallerFromEnv([]string{clients.EnvDispatchCallerAgent + "=bogus"}); ok || caller != "" {
		t.Fatalf("unknown caller marker = %q, %t", caller, ok)
	}
	if targetEnabled(config.Config{}, "bogus") {
		t.Fatal("unknown target should not be enabled")
	}
	cfg := dispatchCoverageConfig(AgentAntigravity)
	cfg.Agents.Antigravity.Dispatch.DefaultAgent = AgentCodex
	if got := dispatchDefaultForCaller(cfg, AgentAntigravity); got != AgentCodex {
		t.Fatalf("antigravity default = %s, want %s", got, AgentCodex)
	}
}

func requireDispatchExitCode(t *testing.T, err error, code int) *ExitError {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != code {
		t.Fatalf("expected ExitError code %d, got %T: %v", code, err, err)
	}
	return exitErr
}

func dispatchCoverageConfig(enabledAgents ...string) config.Config {
	cfg := config.Config{}
	for _, agent := range enabledAgents {
		switch agent {
		case AgentCodex:
			cfg.Agents.Codex.Enabled = boolPtr(true)
		case AgentClaude:
			cfg.Agents.Claude.Enabled = boolPtr(true)
		case AgentAntigravity:
			cfg.Agents.Antigravity.Enabled = boolPtr(true)
		}
	}
	return cfg
}

func boolPtr(value bool) *bool {
	return &value
}

func alwaysFound(name string) (string, error) {
	return "/mock/" + name, nil
}

func chooseOnly(agent string) RandomChooser {
	return func([]string) (string, error) {
		return agent, nil
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type failAtWriter struct {
	writes int
	failAt int
}

func (w *failAtWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func disableAgentInDispatchConfig(t *testing.T, root string, agent string) {
	t.Helper()
	replaceDispatchConfigText(t, root, "[agents."+agent+"]\nenabled = true", "[agents."+agent+"]\nenabled = false")
}

func replaceDispatchConfigText(t *testing.T, root string, old string, replacement string) {
	t.Helper()
	configPath := config.DefaultPaths(root).ConfigPath
	data, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if updated == string(data) {
		t.Fatalf("config did not contain %q", old)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
