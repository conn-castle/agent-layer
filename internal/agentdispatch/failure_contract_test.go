package agentdispatch

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeRejectsPendingDisabledAndUnavailableMappings(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	pending := Session{Name: "calm-steady-rectifier", Agent: AgentCodex, State: "pending"}
	if err := persistSession(root, pending); err != nil {
		t.Fatalf("persist pending: %v", err)
	}
	err := Resume(ResumeOptions{Root: root, Name: pending.Name, Env: []string{}})
	requireDispatchExitCode(t, err, ExitUnavailable)

	durable := pending
	durable.Name = "calm-steady-capacitor"
	durable.Agent = AgentClaude
	durable.State = "durable"
	durable.ProviderSessionID = runtimeSessionID
	if err := persistSession(root, durable); err != nil {
		t.Fatalf("persist durable: %v", err)
	}
	disableAgentInDispatchConfig(t, root, AgentClaude)
	err = Resume(ResumeOptions{Root: root, Name: durable.Name, Env: []string{}})
	requireDispatchExitCode(t, err, ExitConfig)

	root = writeDispatchRepo(t, dispatchRepoConfig{})
	if err := persistSession(root, durable); err != nil {
		t.Fatalf("persist durable in enabled repo: %v", err)
	}
	err = Resume(ResumeOptions{Root: root, Name: durable.Name, Env: []string{}, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestPublicOperationsWriteStableEmptyAndJSONForms(t *testing.T) {
	root := t.TempDir()
	var text bytes.Buffer
	if err := List(ListRequest{Root: root, Stdout: &text}); err != nil {
		t.Fatalf("empty List: %v", err)
	}
	if text.String() != "No dispatch sessions.\n" {
		t.Fatalf("empty list = %q", text.String())
	}
	var jsonOut bytes.Buffer
	if err := List(ListRequest{Root: root, Stdout: &jsonOut, JSON: true}); err != nil {
		t.Fatalf("JSON List: %v", err)
	}
	if strings.TrimSpace(jsonOut.String()) != "[]" {
		t.Fatalf("empty JSON list = %q", jsonOut.String())
	}
	if err := Inspect(InspectionRequest{Root: root, ID: ""}); err == nil {
		t.Fatal("Inspect accepted missing ID")
	} else {
		requireDispatchExitCode(t, err, ExitUsage)
	}
	if err := List(ListRequest{Root: root, Stdout: failingWriter{}}); err == nil {
		t.Fatal("List hid a caller writer error")
	}
}

func TestWriteOptionsRendersCurrentContractAndPropagatesWriterFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var text bytes.Buffer
	err := WriteOptions(OptionsRequest{
		Root:     root,
		Env:      []string{},
		Stdout:   &text,
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	})
	if err != nil {
		t.Fatalf("WriteOptions text: %v", err)
	}
	if !strings.Contains(text.String(), "fresh=false resume=false inspect=true random_eligible=false") ||
		!strings.Contains(text.String(), "random: excluded (provider binary not found)") ||
		!strings.Contains(text.String(), "model: override_supported=true") ||
		!strings.Contains(text.String(), "reasoning_effort: override_supported=") ||
		!strings.Contains(text.String(), "Random pool:  (excludes_caller=false empty=true)") {
		t.Fatalf("options text = %q", text.String())
	}
	err = WriteOptions(OptionsRequest{Root: root, Env: []string{}, Stdout: failingWriter{}, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	if err == nil {
		t.Fatal("WriteOptions hid a writer error")
	}
}

func TestRunnerFailsLoudlyForProviderAndCaptureFailures(t *testing.T) {
	root := t.TempDir()
	newRun := func(t *testing.T) *dispatchRun {
		t.Helper()
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
		if err != nil {
			t.Fatalf("new run: %v", err)
		}
		return run
	}

	preStart := newRun(t)
	_, err := executeProvider(providerCommand{Path: filepath.Join(root, "missing-provider"), Provider: AgentCodex, Structured: true, SessionID: runtimeSessionID}, nil, preStart, root, nil, func(string) error { return nil })
	var start *preStartFailure
	if !errors.As(err, &start) {
		t.Fatalf("start error = %T: %v", err, err)
	}

	failedRun := newRun(t)
	_, err = executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '{"type":"turn.failed","message":"provider refused"}\n'`},
		Env:        os.Environ(),
		Provider:   AgentCodex,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, nil, failedRun, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)

	timeoutRun := newRun(t)
	timeoutLog := filepath.Join(timeoutRun.Dir, "antigravity.log")
	if err := os.WriteFile(timeoutLog, []byte("Error: timeout waiting for response\n"), 0o600); err != nil {
		t.Fatalf("write timeout log: %v", err)
	}
	_, err = executeProvider(providerCommand{Path: "/bin/sh", Args: []string{"-c", `printf answer`}, Env: os.Environ(), Provider: AgentAntigravity, Plain: true, LogPath: timeoutLog}, nil, timeoutRun, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if timedOut, readErr := antigravityTimeoutReported(timeoutRun.Record.StderrPath, filepath.Join(root, "missing-log")); readErr == nil || timedOut {
		t.Fatalf("missing Antigravity diagnostics = timedOut %t, error %v", timedOut, readErr)
	}
	if err := os.WriteFile(timeoutRun.Record.StderrPath, []byte("Error: timeout waiting for response\n"), 0o600); err != nil {
		t.Fatalf("write timeout stderr: %v", err)
	}
	if timedOut, readErr := antigravityTimeoutReported(timeoutRun.Record.StderrPath, filepath.Join(root, "missing-log")); readErr == nil || timedOut {
		t.Fatalf("timeout stderr with missing Antigravity log = timedOut %t, error %v", timedOut, readErr)
	}

	if err := replayAnswer(filepath.Join(root, "missing-answer"), io.Discard); err == nil {
		t.Fatal("replayAnswer accepted a missing capture")
	} else {
		requireDispatchExitCode(t, err, ExitTargetFailure)
	}
}

func TestCaptureLimitsPreventPartialPublication(t *testing.T) {
	budget := &captureBudget{max: 3}
	if err := budget.reserve(2); err != nil {
		t.Fatalf("reserve within budget: %v", err)
	}
	if err := budget.reserve(2); err == nil {
		t.Fatal("aggregate capture budget accepted overflow")
	}

	path := filepath.Join(t.TempDir(), "answer")
	writer, err := newLimitedWriter(path, 3, nil)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatalf("write within limit: %v", err)
	}
	if _, err := writer.Write([]byte("x")); err == nil {
		t.Fatal("answer capture accepted overflow")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
}
