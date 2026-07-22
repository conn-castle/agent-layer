package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContinueRejectsUnclaimableDisabledAndUnavailableConversations(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	pending := Session{Name: "calm-steady-rectifier", Agent: AgentCodex, State: "pending"}
	if err := persistSession(root, pending); err != nil {
		t.Fatalf("persist pending: %v", err)
	}
	err := Continue(ContinueOptions{Root: root, Handle: pending.Name, Prompt: "Continue", Env: []string{}})
	requireDispatchExitCode(t, err, ExitConfig)

	_, disabled := terminalConversationForAsyncTest(t, root)
	disableAgentInDispatchConfig(t, root, AgentCodex)
	err = Continue(ContinueOptions{Root: root, Handle: disabled.Name, Prompt: "Continue", Env: []string{}})
	requireDispatchExitCode(t, err, ExitConfig)

	root = writeDispatchRepo(t, dispatchRepoConfig{})
	_, unavailable := terminalConversationForAsyncTest(t, root)
	err = Continue(ContinueOptions{Root: root, Handle: unavailable.Name, Prompt: "Continue", Env: []string{}, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestWriteOptionsRendersJSONAndPropagatesWriterFailure(t *testing.T) {
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
	var response OptionsResponse
	if err := json.Unmarshal(text.Bytes(), &response); err != nil {
		t.Fatalf("options output is not JSON: %v: %q", err, text.String())
	}
	if len(response.Agents) != len(targetRegistry()) || response.Agents[0].Available {
		t.Fatalf("options response = %#v", response)
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
	terminationTestDeadline := 3 * providerTerminationGrace
	failedStarted := time.Now()
	_, err = executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `trap '' TERM; printf '{"type":"turn.failed","message":"provider refused"}\n'; while :; do sleep 1; done`},
		Env:        os.Environ(),
		Provider:   AgentCodex,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, nil, failedRun, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if elapsed := time.Since(failedStarted); elapsed > terminationTestDeadline {
		t.Fatalf("reducer failure waited %s for a SIGTERM-ignoring provider", elapsed)
	}

	publicationRun := newRun(t)
	publicationStarted := time.Now()
	_, err = executeProvider(providerCommand{
		Path: "/bin/sh", Env: os.Environ(), Provider: AgentAntigravity, Plain: true,
	}, nil, publicationRun, root, func(string, ...string) *exec.Cmd {
		current, loadErr := loadRunRecord(root, publicationRun.Record.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if writeErr := writeRunRecord(publicationRun.Dir, &current); writeErr != nil {
			t.Fatal(writeErr)
		}
		return exec.Command("/bin/sh", "-c", `trap '' TERM; while :; do sleep 1; done`) // #nosec G204 -- fixed test-only shell command.
	}, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitUnavailable)
	if elapsed := time.Since(publicationStarted); elapsed > terminationTestDeadline {
		t.Fatalf("running-state publication failure waited %s for a SIGTERM-ignoring provider", elapsed)
	}

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
	boundaryLog := filepath.Join(root, "boundary-timeout.log")
	boundaryStderr := filepath.Join(root, "boundary-stderr.log")
	boundaryDiagnostic := strings.Repeat("x", 64*1024-10) + "Error: timeout waiting for response"
	if err := os.WriteFile(boundaryStderr, nil, 0o600); err != nil {
		t.Fatalf("write boundary stderr: %v", err)
	}
	if err := os.WriteFile(boundaryLog, []byte(boundaryDiagnostic), 0o600); err != nil {
		t.Fatalf("write boundary timeout log: %v", err)
	}
	if timedOut, readErr := antigravityTimeoutReported(boundaryStderr, boundaryLog); readErr != nil || !timedOut {
		t.Fatalf("boundary-spanning Antigravity timeout = timedOut %t, error %v", timedOut, readErr)
	}

	if err := replayAnswer(filepath.Join(root, "missing-answer"), io.Discard); err == nil {
		t.Fatal("replayAnswer accepted a missing capture")
	} else {
		requireDispatchExitCode(t, err, ExitTargetFailure)
	}
}

func TestCaptureWriterPreservesProviderOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answer")
	writer, err := newCaptureWriter(path)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, err := writer.Write([]byte("provider output")); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil || string(data) != "provider output" {
		t.Fatalf("capture = %q, %v", data, err)
	}
}
