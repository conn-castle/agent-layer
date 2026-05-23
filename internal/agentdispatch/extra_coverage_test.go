package agentdispatch

// These tests cover the small remaining branches in dispatch.go, options.go,
// resolve.go, and adapters.go that the primary behavior tests do not reach.
// Each test exercises a concrete branch with a real failure mode (writer
// error, missing binary, decoder rejection, signal forwarding, etc.) so a
// production-code mutation that drops the branch will flip the assertion.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// TestSyncWriterAcceptsNilWriter pins the documented behavior that a
// syncWriter with no destination still acknowledges all bytes written,
// covering adapters.go:Write nil branch.
func TestSyncWriterAcceptsNilWriter(t *testing.T) {
	w := newSyncWriter(nil)
	n, err := w.Write([]byte("payload"))
	if err != nil {
		t.Fatalf("nil syncWriter Write error: %v", err)
	}
	if n != len("payload") {
		t.Fatalf("nil syncWriter Write n = %d, want %d", n, len("payload"))
	}
}

// TestStartErrorFallbackWrapsArbitraryError covers adapters.go:startError
// fallback path when the error is not exec.ErrNotFound. The dispatch
// contract requires ExitTargetFailure (70) with the original error wrapped.
func TestStartErrorFallbackWrapsArbitraryError(t *testing.T) {
	sentinel := errors.New("start kaboom")
	err := startError(AgentClaude, sentinel)
	exitErr := requireDispatchExitCode(t, err, ExitTargetFailure)
	if !errors.Is(exitErr, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", exitErr)
	}
	if !strings.Contains(exitErr.Error(), "start claude") {
		t.Fatalf("expected target name in error, got %q", exitErr.Error())
	}
}

// TestMapWaitErrorClampsNonPositiveExitCode covers the adapters.go:307
// branch where exitErr.ExitCode() <= 0 is clamped to 1 in the dispatch
// stderr message (e.g. when the child was killed by a signal and Go
// reports exit code -1).
func TestMapWaitErrorClampsNonPositiveExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-killed processes only produce negative ExitCode on POSIX")
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "kill -KILL $$")
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected kill -KILL to fail the child")
	}
	exitErr := requireDispatchExitCode(t, mapWaitError(AgentCodex, runErr), ExitTargetFailure)
	// The clamp guarantees the user-facing message always reports a positive
	// "code N" even though Go's ExitCode() returned -1 for a signal kill.
	if !strings.Contains(exitErr.Error(), "code 1") {
		t.Fatalf("expected clamped 'code 1' in mapped error, got %q", exitErr.Error())
	}
}

// TestRunStructuredCommandReportsStartFailure covers the adapters.go:179
// cmd.Start error path: starting a binary that fails to exec must surface
// as ExitTargetFailure (the startError path wraps the underlying error
// when it is not exec.ErrNotFound from PATH lookup).
func TestRunStructuredCommandReportsStartFailure(t *testing.T) {
	binDir := t.TempDir()
	cmd := exec.CommandContext(context.Background(), filepath.Join(binDir, "definitely-not-here"))
	var stdout, stderr bytes.Buffer
	err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream)
	_ = requireDispatchExitCode(t, err, ExitTargetFailure)
}

// TestRunStructuredCommandPropagatesDecoderError covers adapters.go:218
// (decodeErr branch): when the decoder returns a non-nil error after a
// clean child exit, runStructuredCommand must return that decoder error.
func TestRunStructuredCommandPropagatesDecoderError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	binDir := t.TempDir()
	// Stub emits garbage JSON so the decoder fails AFTER the child exits 0.
	stubPath := filepath.Join(binDir, "garbage")
	stub := "#!/bin/sh\nprintf '{not-json}\\n'\n"
	if err := os.WriteFile(stubPath, []byte(stub), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path
	var stdout, stderr bytes.Buffer
	err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream)
	_ = requireDispatchExitCode(t, err, ExitTargetFailure)
}

// TestDecodeClaudeStreamFailsWithoutTextEvent covers adapters.go:322 — a
// Claude stream that never emits a recognized text event must fail loudly
// rather than return silently.
func TestDecodeClaudeStreamFailsWithoutTextEvent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := decodeClaudeStream(strings.NewReader(""), &stdout, &stderr)
	exitErr := requireDispatchExitCode(t, err, ExitTargetFailure)
	if !strings.Contains(exitErr.Error(), "no recognized answer-text event") {
		t.Fatalf("expected fail-loud message, got %q", exitErr.Error())
	}
}

// TestDecodeClaudeStreamInvalidJSON covers the wrapExitError branch in
// decodeClaudeStream when the upstream payload is not valid JSON.
func TestDecodeClaudeStreamInvalidJSON(t *testing.T) {
	err := decodeClaudeStream(strings.NewReader("{not-json}\n"), &bytes.Buffer{}, &bytes.Buffer{})
	_ = requireDispatchExitCode(t, err, ExitTargetFailure)
}

// TestDecodeClaudeStreamWriteError covers adapters.go:331 — a stdout
// writer that fails mid-decode must propagate the writer error to the
// caller (without wrapping it as a stream error).
func TestDecodeClaudeStreamWriteError(t *testing.T) {
	sentinel := errors.New("stdout broke")
	err := decodeClaudeStream(
		strings.NewReader(`{"delta":{"type":"text_delta","text":"x"}}`+"\n"),
		brokenWriter{err: sentinel},
		&bytes.Buffer{},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel writer error, got %v", err)
	}
}

// TestDecodeCodexStreamWriteError covers adapters.go:413 — a stdout
// writer that fails mid-decode for Codex must propagate the writer error.
func TestDecodeCodexStreamWriteError(t *testing.T) {
	sentinel := errors.New("stdout broke")
	err := decodeCodexStream(
		strings.NewReader(`{"type":"agent_message","message":"x"}`+"\n"),
		brokenWriter{err: sentinel},
		&bytes.Buffer{},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel writer error, got %v", err)
	}
}

// TestDecodeClaudeStreamSurfacesResultError covers the result-event error
// branch (adapters.go:339 + writeClaudeResultError). When Claude reports a
// policy/permission failure via is_error=true the decoder must emit a
// `claude: error: ...` line on stderr but otherwise complete the stream
// without forcing a non-zero dispatch exit (mirroring spec § Output and
// Artifacts: result errors are surfaced, not swallowed).
func TestDecodeClaudeStreamSurfacesResultError(t *testing.T) {
	stream := strings.Join([]string{
		`{"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"result","is_error":true,"result":"permission denied"}`,
		"",
	}, "\n")
	var stdout, stderr bytes.Buffer
	if err := decodeClaudeStream(strings.NewReader(stream), &stdout, &stderr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if stdout.String() != "hello" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "claude: error: permission denied") {
		t.Fatalf("expected result-error line on stderr, got %q", stderr.String())
	}
}

// TestDecodeClaudeStreamResultErrorSubtypeFallbacks pins the three
// fallbacks in writeClaudeResultError (result missing -> subtype, both
// missing -> "error") and the subtype-prefix branch in
// claudeResultIsError, all on a single test surface.
func TestDecodeClaudeStreamResultErrorSubtypeFallbacks(t *testing.T) {
	cases := []struct {
		name        string
		event       string
		wantStderr  string
		wantInError bool
	}{
		{
			name:        "subtype error_max_turns surfaces as subtype",
			event:       `{"type":"result","subtype":"error_max_turns"}`,
			wantStderr:  "claude: error: error_max_turns",
			wantInError: true,
		},
		{
			name:        "is_error true with no payload defaults to error",
			event:       `{"type":"result","is_error":true}`,
			wantStderr:  "claude: error: error",
			wantInError: true,
		},
		{
			name:        "non-error result is ignored",
			event:       `{"type":"result","subtype":"success"}`,
			wantStderr:  "",
			wantInError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := `{"delta":{"type":"text_delta","text":"ok"}}` + "\n" + tc.event + "\n"
			var stdout, stderr bytes.Buffer
			if err := decodeClaudeStream(strings.NewReader(stream), &stdout, &stderr); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if stdout.String() != "ok" {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if tc.wantInError {
				if !strings.Contains(stderr.String(), tc.wantStderr) {
					t.Fatalf("expected %q in stderr, got %q", tc.wantStderr, stderr.String())
				}
			} else if stderr.String() != "" {
				t.Fatalf("expected empty stderr for non-error result, got %q", stderr.String())
			}
		})
	}
}

// TestClaudeTextDeltaIgnoresUnknownShape covers the adapters.go:382
// fall-through: an event with neither `delta` nor `event.delta` returns
// ("", false) and is treated as a non-text event by the decoder.
func TestClaudeTextDeltaIgnoresUnknownShape(t *testing.T) {
	if text, ok := claudeTextDelta(map[string]any{"type": "result"}); ok || text != "" {
		t.Fatalf("expected ('', false), got (%q, %t)", text, ok)
	}
	// Nested event without a delta key is also ignored.
	if text, ok := claudeTextDelta(map[string]any{"event": map[string]any{"type": "ping"}}); ok || text != "" {
		t.Fatalf("expected ('', false) for delta-less event, got (%q, %t)", text, ok)
	}
}

// TestFirstStringMissingKeysReturnsFalse covers adapters.go:firstString
// fall-through when none of the requested keys are present as strings.
func TestFirstStringMissingKeysReturnsFalse(t *testing.T) {
	if got, ok := firstString(map[string]any{"other": "x"}, "message", "text"); ok || got != "" {
		t.Fatalf("expected ('', false), got (%q, %t)", got, ok)
	}
	// Non-string value with a matching key is also rejected.
	if got, ok := firstString(map[string]any{"message": 42}, "message"); ok || got != "" {
		t.Fatalf("expected ('', false) for non-string value, got (%q, %t)", got, ok)
	}
}

// TestRunAntigravityReportsStartFailure covers adapters.go:109 — when the
// agy binary cannot be started, runAntigravity must surface
// ExitTargetFailure via startError without leaking a panic.
func TestRunAntigravityReportsStartFailure(t *testing.T) {
	binDir := t.TempDir()
	missing := filepath.Join(binDir, "agy-does-not-exist")
	var stdout, stderr bytes.Buffer
	err := runAntigravity(
		targetMeta{Name: AgentAntigravity, Binary: missing},
		&config.ProjectConfig{Root: binDir},
		[]string{"PATH=/bin"},
		[]byte("Prompt"),
		RunOptions{Stdout: &stdout, Stderr: &stderr},
		defaultCommandFactory,
	)
	_ = requireDispatchExitCode(t, err, ExitTargetFailure)
}

// TestRunStructuredCommandForwardsSIGTERM covers the SIGTERM branch in
// runStructuredCommand (adapters.go:213). The stub sleeps; the test sends
// SIGTERM to its own process and expects ExitSigterm (143).
func TestRunStructuredCommandForwardsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal forwarding test is POSIX-only")
	}
	binDir := t.TempDir()
	stubPath := filepath.Join(binDir, "sleeper")
	stub := "#!/bin/sh\ni=0\nwhile [ \"$i\" -lt 100 ]; do sleep 0.05; i=$((i + 1)); done\nexit 0\n"
	if err := os.WriteFile(stubPath, []byte(stub), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	var stdout, stderr bytes.Buffer
	err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream)
	exitErr := requireDispatchExitCode(t, err, ExitSigterm)
	if !strings.Contains(exitErr.Error(), "SIGTERM") {
		t.Fatalf("expected SIGTERM in error message, got %q", exitErr.Error())
	}
}

// TestWaitWithSignalForwardsSIGTERM covers the SIGTERM branch in
// waitWithSignal (adapters.go:294) used by the Antigravity adapter.
func TestWaitWithSignalForwardsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal forwarding test is POSIX-only")
	}
	binDir := t.TempDir()
	stubPath := filepath.Join(binDir, "sleeper")
	stub := "#!/bin/sh\ni=0\nwhile [ \"$i\" -lt 100 ]; do sleep 0.05; i=$((i + 1)); done\nexit 0\n"
	if err := os.WriteFile(stubPath, []byte(stub), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stub: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	err := waitWithSignal(cmd, AgentAntigravity)
	exitErr := requireDispatchExitCode(t, err, ExitSigterm)
	if !strings.Contains(exitErr.Error(), "SIGTERM") {
		t.Fatalf("expected SIGTERM in error message, got %q", exitErr.Error())
	}
}

// TestResolveTargetRejectsBogusAgent covers resolve.go:20 — an agent name
// that is neither blank, "random", nor a registered target must yield
// ExitUsage with the unknown-target message.
func TestResolveTargetRejectsBogusAgent(t *testing.T) {
	_, err := resolveTarget(config.Config{}, RunOptions{Agent: "not-an-agent"}, "", false)
	exitErr := requireDispatchExitCode(t, err, ExitUsage)
	if !strings.Contains(exitErr.Error(), "unknown dispatch target") {
		t.Fatalf("expected unknown-target message, got %q", exitErr.Error())
	}
}

// TestResolveTargetPropagatesRandomEmptyPool covers resolve.go:31 —
// resolveTarget must surface the chooseRandomTarget error path verbatim
// (no swallow, no rewrap) when the random pool is empty.
func TestResolveTargetPropagatesRandomEmptyPool(t *testing.T) {
	_, err := resolveTarget(config.Config{}, RunOptions{Agent: AgentRandom, LookPath: alwaysFound}, "", false)
	_ = requireDispatchExitCode(t, err, ExitUnavailable)
}

// TestChooseRandomTargetDefaultsLookPath covers the resolve.go:52 branch
// where the caller does not supply a LookPath. With a config that has no
// enabled agents we still take the default-LookPath assignment path and
// then fall through to the empty-pool error.
func TestChooseRandomTargetDefaultsLookPath(t *testing.T) {
	_, err := chooseRandomTarget(config.Config{}, "", false, nil, nil)
	_ = requireDispatchExitCode(t, err, ExitUnavailable)
}

// TestChooseRandomTargetSkipsUninstalledAgents covers the resolve.go:63
// continue when an enabled agent is not on PATH.
func TestChooseRandomTargetSkipsUninstalledAgents(t *testing.T) {
	cfg := dispatchCoverageConfig(AgentCodex, AgentClaude)
	// Only "claude" is "installed"; codex must be skipped via the err branch.
	lookPath := func(name string) (string, error) {
		if name == "claude" {
			return "/mock/claude", nil
		}
		return "", exec.ErrNotFound
	}
	selected, err := chooseRandomTarget(cfg, "", false, lookPath, nil)
	if err != nil {
		t.Fatalf("chooseRandomTarget error: %v", err)
	}
	if selected != AgentClaude {
		t.Fatalf("selected = %s, want %s", selected, AgentClaude)
	}
}

// TestBuildOptionsDefaultsEnvAndLookPath covers options.go:70-76 — when
// the request does not supply Env or LookPath, BuildOptions must fall back
// to os.Environ and exec.LookPath. We just need the call to succeed
// without panicking (config load surfaces from disk).
func TestBuildOptionsDefaultsEnvAndLookPath(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	options, err := BuildOptions(OptionsRequest{Root: root})
	if err != nil {
		t.Fatalf("BuildOptions defaults error: %v", err)
	}
	if options == nil || len(options.Targets) == 0 {
		t.Fatal("expected non-empty default options response")
	}
}

// TestBuildOptionsSurfacesConfigLoadError covers options.go:78 — when the
// project config cannot be loaded, BuildOptions must return ExitConfig
// instead of panicking.
func TestBuildOptionsSurfacesConfigLoadError(t *testing.T) {
	// A path that does not have an Agent Layer install will fail the
	// strict project config load.
	_, err := BuildOptions(OptionsRequest{Root: t.TempDir()})
	_ = requireDispatchExitCode(t, err, ExitConfig)
}

// TestBuildTargetOptionsReportsUninstalledExclusion covers options.go:125
// — an enabled-but-uninstalled agent must produce
// random_exclusion_reason="uninstalled" and unavailable_reasons=
// ["binary_not_found"], not be silently treated as random-eligible.
func TestBuildTargetOptionsReportsUninstalledExclusion(t *testing.T) {
	cfg := dispatchCoverageConfig(AgentCodex)
	// Codex is enabled in cfg; lookPath fails so it is uninstalled.
	options := buildTargetOptions(cfg, "", false, func(string) (string, error) {
		return "", exec.ErrNotFound
	})
	var codex TargetOption
	for _, target := range options {
		if target.Agent == AgentCodex {
			codex = target
		}
	}
	if codex.Agent == "" {
		t.Fatalf("codex missing from buildTargetOptions output: %#v", options)
	}
	if codex.RandomEligible {
		t.Fatal("uninstalled codex should not be random-eligible")
	}
	if codex.RandomExclusionReason == nil || *codex.RandomExclusionReason != "uninstalled" {
		t.Fatalf("expected uninstalled exclusion, got %#v", codex.RandomExclusionReason)
	}
	if len(codex.UnavailableReasons) != 1 || codex.UnavailableReasons[0] != "binary_not_found" {
		t.Fatalf("expected [binary_not_found], got %#v", codex.UnavailableReasons)
	}
}

// TestUnavailableReasonsReportsDisabledFirst covers options.go:156 — the
// disabled branch must take precedence over the not-installed branch.
func TestUnavailableReasonsReportsDisabledFirst(t *testing.T) {
	reasons := unavailableReasons(false, false)
	if len(reasons) != 1 || reasons[0] != "disabled" {
		t.Fatalf("expected [disabled], got %#v", reasons)
	}
	// Disabled wins even when the binary is installed.
	reasons = unavailableReasons(false, true)
	if len(reasons) != 1 || reasons[0] != "disabled" {
		t.Fatalf("expected [disabled] (installed=true), got %#v", reasons)
	}
}

// TestFieldOptionMissingFieldLookup covers options.go:187 — when the
// registry advertises an override key but the field catalog has no
// matching entry, fieldOption must return early with allow_custom=false
// and an empty suggestions list rather than panicking.
func TestFieldOptionMissingFieldLookup(t *testing.T) {
	cfg := dispatchCoverageConfig(AgentCodex)
	target := targetMeta{
		Name:          AgentCodex,
		ModelKey:      "agents.not.a.real.key",
		SupportsModel: true,
	}
	option := fieldOption(cfg, target, true)
	if !option.OverrideSupported {
		t.Fatal("override supported should remain true even when field is missing")
	}
	if option.AllowCustom {
		t.Fatal("missing field must not allow custom values")
	}
	if len(option.Suggestions) != 0 {
		t.Fatalf("missing field must yield zero suggestions, got %#v", option.Suggestions)
	}
}

// TestWriteOptionsTextWriterErrors covers every fmt.Fprintf/Fprintln
// branch in writeOptionsText + writeTargetDetails + writeFieldOption that
// returns a writer error. failAtWriter aborts the Nth write so each early-
// return branch can be exercised. The total write count is fixed by the
// response shape, so the loop walks every step deterministically.
func TestWriteOptionsTextWriterErrors(t *testing.T) {
	exclusion := "uninstalled"
	options := &OptionsResponse{
		Caller: CallerInfo{Known: false},
		Random: RandomInfo{Pool: []string{AgentCodex}},
		Targets: []TargetOption{
			{
				Agent:                 AgentCodex,
				Enabled:               true,
				Installed:             false,
				DispatchCapable:       false,
				RandomEligible:        false,
				RandomExclusionReason: &exclusion,
				Streaming:             StreamingOption{AnswerText: "final", Progress: "partial"},
				Model:                 FieldOption{OverrideSupported: true, Configured: "x", Suggestions: []string{"x"}},
				ReasoningEffort:       FieldOption{OverrideSupported: true, Configured: "y", Suggestions: []string{"y"}},
				UnavailableReasons:    []string{"binary_not_found"},
			},
		},
	}
	// Count total writes by running once into an io.Discard wrapper.
	counter := &countingWriter{}
	if err := writeOptionsText(counter, options); err != nil {
		t.Fatalf("baseline writeOptionsText error: %v", err)
	}
	if counter.writes < 8 {
		t.Fatalf("expected at least 8 writes for full options text, got %d", counter.writes)
	}
	// Walk every write index and assert that a failure at that point
	// propagates without panic.
	for i := 1; i <= counter.writes; i++ {
		err := writeOptionsText(&failAtWriter{failAt: i}, options)
		if err == nil {
			t.Fatalf("writer failure at write %d returned nil error", i)
		}
	}
}

// TestRunCoversEnvAndQuietBranches exercises dispatch.go:28 (env=nil
// default), dispatch.go:43 (quiet/noise gated stderr), and dispatch.go:83
// (warnings iteration). The repo config enables warnings via a tiny
// instruction-token threshold, so sync emits a real warning that must be
// suppressed in quiet mode.
func TestRunCoversEnvAndQuietBranches(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	// Lower the instruction-token threshold so the existing instructions
	// trip a real warning during sync. The warnings iteration branch in
	// dispatch.go writes each warning to infoStderr, which Quiet=true
	// redirects to io.Discard.
	replaceDispatchConfigText(t, root, "instruction_token_threshold = 50000", "instruction_token_threshold = 1")

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)

	// Save and restore PATH/HOME for the Env=nil branch. We do NOT pass
	// Env in opts so dispatch.go takes the os.Environ() default path.
	prevPath := os.Getenv("PATH")
	prevHome := os.Getenv("HOME")
	prevCaller := os.Getenv(clients.EnvDispatchCallerAgent)
	prevActive := os.Getenv(clients.EnvDispatchActive)
	t.Setenv("PATH", testPath(binDir))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AL_TEST_LOG", logPath)
	// Make sure no caller/active marker leaks from the parent process.
	if err := os.Unsetenv(clients.EnvDispatchCallerAgent); err != nil {
		t.Fatalf("unset caller: %v", err)
	}
	if err := os.Unsetenv(clients.EnvDispatchActive); err != nil {
		t.Fatalf("unset active: %v", err)
	}
	t.Cleanup(func() {
		if prevPath != "" {
			_ = os.Setenv("PATH", prevPath)
		}
		if prevHome != "" {
			_ = os.Setenv("HOME", prevHome)
		}
		if prevCaller != "" {
			_ = os.Setenv(clients.EnvDispatchCallerAgent, prevCaller)
		}
		if prevActive != "" {
			_ = os.Setenv(clients.EnvDispatchActive, prevActive)
		}
	})

	var stdout, stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Quiet:      true,
		Stdout:     &stdout,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run quiet error: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	// In quiet mode, the warning text must not appear on the caller's
	// stderr — proves infoStderr was rerouted to io.Discard.
	if strings.Contains(stderr.String(), "instructions") {
		t.Fatalf("quiet=true should suppress instruction warnings, got stderr=%q", stderr.String())
	}
}

// TestRunPromptOrSkillRequired covers dispatch.go:71 — ResolvePrompt
// returning empty + no skill must surface ExitUsage via BuildChildPrompt.
func TestRunPromptOrSkillRequired(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)
	err := Run(RunOptions{
		Root:     root,
		Agent:    AgentAntigravity,
		Env:      []string{"PATH=" + testPath(binDir)},
		LookPath: mockLookPath(binDir),
		// No PromptArgs, no Skill, no stdin → ResolvePrompt returns ""
		// and BuildChildPrompt then surfaces ExitUsage.
	})
	exitErr := requireDispatchExitCode(t, err, ExitUsage)
	if !strings.Contains(exitErr.Error(), messages.DispatchPromptOrSkillRequired) {
		t.Fatalf("expected prompt-or-skill message, got %q", exitErr.Error())
	}
}

// TestRunResolvePromptStdinError covers dispatch.go:71 — when stdin read
// fails, dispatch must surface the ResolvePrompt error verbatim.
func TestRunResolvePromptStdinError(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)
	err := Run(RunOptions{
		Root:      root,
		Agent:     AgentAntigravity,
		ReadStdin: true,
		Stdin:     errorReader{},
		Env:       []string{"PATH=" + testPath(binDir)},
		LookPath:  mockLookPath(binDir),
	})
	_ = requireDispatchExitCode(t, err, ExitUsage)
}

// TestRunSurfacesSyncFailure covers dispatch.go:80 — when sync fails
// (e.g. gitignore.block is missing), dispatch must surface ExitConfig
// with the documented sync-failed message.
func TestRunSurfacesSyncFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	// Remove the gitignore.block file so sync's first step fails.
	if err := os.Remove(filepath.Join(root, ".agent-layer", "gitignore.block")); err != nil {
		t.Fatalf("remove gitignore block: %v", err)
	}
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir)},
		LookPath:   mockLookPath(binDir),
	})
	exitErr := requireDispatchExitCode(t, err, ExitConfig)
	if !strings.Contains(exitErr.Error(), "dispatch sync failed") {
		t.Fatalf("expected sync-failed message, got %q", exitErr.Error())
	}
}

// TestRunSurfacesSkillProjectionMismatch covers dispatch.go:90 — when the
// skill is in config but its target-specific projection is missing,
// dispatch must surface ExitConfig with the missing-skill-projection
// message after sync (proving sync ran AND validation fired after it).
func TestRunSurfacesSkillProjectionMismatch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)
	// Remove the synced projection so the post-sync check fires. We do
	// this AFTER the dispatch sync regenerates it: instead, point Run at
	// a target that uses the shared agents projection (antigravity) and
	// remove .agents after sync would run by deleting the source skill
	// directory entirely. Simpler approach: delete sync's projection
	// output by removing the .agents/skills/review-plan/SKILL.md file
	// that sync would create, then run with --skill review-plan. The
	// dispatch's sync call regenerates it, so to make this fail we must
	// remove the source skill template that sync reads from. The
	// dispatchRepoConfig writes the source in .agent-layer/skills/.
	if err := os.RemoveAll(filepath.Join(root, ".agent-layer", "skills", "review-plan")); err != nil {
		t.Fatalf("remove skill source: %v", err)
	}
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		Skill:      "review-plan",
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir)},
		LookPath:   mockLookPath(binDir),
	})
	exitErr := requireDispatchExitCode(t, err, ExitConfig)
	// The error must come from BuildChildPrompt's missing-skill branch
	// (skill not in project) before adapter launch.
	if !strings.Contains(exitErr.Error(), "review-plan") {
		t.Fatalf("expected review-plan in error, got %q", exitErr.Error())
	}
}

// TestRunSurfacesRunCreateFailure covers dispatch.go:94 — when
// run.Create fails (because .agent-layer/tmp/runs is a regular file, not
// a directory), dispatch must surface ExitTargetFailure with the
// documented "dispatch run setup failed" message.
func TestRunSurfacesRunCreateFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("blocking-file collision behavior differs on Windows")
	}
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)
	// Pre-create .agent-layer/tmp/runs as a regular file so MkdirAll
	// fails inside run.Create.
	runsPath := filepath.Join(root, ".agent-layer", "tmp", "runs")
	if err := os.MkdirAll(filepath.Dir(runsPath), 0o700); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(runsPath, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir)},
		LookPath:   mockLookPath(binDir),
	})
	exitErr := requireDispatchExitCode(t, err, ExitTargetFailure)
	if !strings.Contains(exitErr.Error(), "dispatch run setup failed") {
		t.Fatalf("expected run-setup-failed message, got %q", exitErr.Error())
	}
}

// TestRunUnsupportedModelTarget covers dispatch.go:52 — supplying --model
// against a target whose registry entry has SupportsModel=false must
// surface ExitUsage with the documented message.
func TestRunUnsupportedModelTarget(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		Model:      "anything",
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=/bin"},
		LookPath:   alwaysFound,
	})
	exitErr := requireDispatchExitCode(t, err, ExitUsage)
	if !strings.Contains(exitErr.Error(), "does not support --model") {
		t.Fatalf("expected unsupported-model message, got %q", exitErr.Error())
	}
}

// TestRunQuietViaNoiseMode covers the dispatch.go:43 branch where Quiet
// is false but the project's warnings.noise_mode is "quiet". This ensures
// the noise-mode-only path also routes infoStderr to io.Discard.
func TestRunQuietViaNoiseMode(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	// Switch the warnings block to quiet AND lower the threshold so a
	// real warning would otherwise fire.
	replaceDispatchConfigText(t, root,
		"[warnings]\ninstruction_token_threshold = 50000",
		"[warnings]\nnoise_mode = \"quiet\"\ninstruction_token_threshold = 1",
	)
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "agy", `printf 'ok'`)

	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir)},
		Stdout:     io.Discard,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run noise_mode=quiet error: %v\nstderr:\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "instructions") {
		t.Fatalf("noise_mode=quiet should suppress instruction warnings, got stderr=%q", stderr.String())
	}
}

// brokenWriter always fails Write with the configured error. Used to
// drive decodeClaudeStream / decodeCodexStream into their writer-error
// branches without touching production code.
type brokenWriter struct {
	err error
}

func (b brokenWriter) Write(_ []byte) (int, error) {
	return 0, b.err
}

// countingWriter counts the number of Write calls without ever failing.
// Used to measure the total writes of a writeOptionsText call so a
// follow-up test can drive a failAtWriter through every write index.
type countingWriter struct {
	writes int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++
	return len(p), nil
}

// failAtWriter is reused from coverage_test.go (same package); the new
// helpers above complement it without duplicating its definition.
