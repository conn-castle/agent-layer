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
	_, err := executeProvider(providerCommand{Path: filepath.Join(root, "missing-provider"), Provider: AgentCodex, SessionID: runtimeSessionID}, nil, preStart, root, nil, func(string) error { return nil })
	var start *preStartFailure
	if !errors.As(err, &start) {
		t.Fatalf("start error = %T: %v", err, err)
	}

	failedRun := newRun(t)
	terminationTestDeadline := 3 * providerTerminationGrace
	failedStarted := time.Now()
	_, err = executeProvider(providerCommand{
		Path:      "/bin/sh",
		Args:      []string{"-c", `trap '' TERM; printf '{"type":"turn.failed","message":"provider refused"}\n'; while :; do sleep 1; done`},
		Env:       os.Environ(),
		Provider:  AgentCodex,
		SessionID: runtimeSessionID,
	}, nil, failedRun, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if elapsed := time.Since(failedStarted); elapsed > terminationTestDeadline {
		t.Fatalf("reducer failure waited %s for a SIGTERM-ignoring provider", elapsed)
	}

	publicationRun := newRun(t)
	publicationStarted := time.Now()
	_, err = executeProvider(providerCommand{
		Path: "/bin/sh", Env: os.Environ(), Provider: AgentCodex, SessionID: runtimeSessionID,
	}, nil, publicationRun, root, func(string, ...string) *exec.Cmd {
		current, loadErr := loadRunRecord(root, publicationRun.Record.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if writeErr := writeRunRecord(publicationRun.Dir, &current); writeErr != nil {
			t.Fatal(writeErr)
		}
		return exec.Command("/bin/sh", "-c", `printf '{"type":"error","message":"provider refused"}\n'; exit 1`) // #nosec G204 -- fixed test-only shell command.
	}, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if elapsed := time.Since(publicationStarted); elapsed > terminationTestDeadline {
		t.Fatalf("fenced launch after a concurrent record write waited %s", elapsed)
	}

	timeoutRun := newRun(t)
	timeoutLog := filepath.Join(timeoutRun.Dir, "antigravity.log")
	if err := os.WriteFile(timeoutLog, []byte("Error: timeout waiting for response\n"), 0o600); err != nil {
		t.Fatalf("write timeout log: %v", err)
	}
	_, err = executeProvider(providerCommand{Path: "/bin/sh", Args: []string{"-c", `printf '{"event":"result","result":{"status":"SUCCESS","conversation_id":"conversation","response":"answer","usage":{"input_tokens":1}}}\n'`}, Env: os.Environ(), Provider: AgentAntigravity, LogPath: timeoutLog}, nil, timeoutRun, root, nil, func(string) error { return nil })
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

// TestRunnerTreatsCodexErrorEventsAsDiagnostics proves a Codex error event,
// which Codex also emits for stream retries it recovers from, does not end the
// dispatch; the turn outcome or exit decides, and the error explains failures.
func TestRunnerTreatsCodexErrorEventsAsDiagnostics(t *testing.T) {
	const prefix = `printf '{"type":"thread.started","thread_id":"` + runtimeSessionID + `"}\n{"type":"turn.started"}\n{"type":"error","message":"Reconnecting... 2/5 (request timed out)"}\n'; `
	for _, tc := range []struct {
		name, script, wantErr string
	}{
		{"recovered retry", prefix + `printf '{"type":"agent_message","message":"final"}\n{"type":"turn.completed"}\n'`, ""},
		{"aborted turn without reason", prefix + `printf '{"type":"turn.aborted"}\n'`, "codex dispatch did not complete: Codex reported a terminal failure"},
		{"latest diagnostic survives empty progress", prefix + `printf '{"type":"error","message":"Reconnecting... 3/5 (connection reset)"}\n{"type":"error"}\n{"type":"turn.started"}\n'; exit 1`, "codex exited with code 1; `al dispatch` exiting 70; last provider error: Reconnecting... 3/5 (connection reset)"},
		{"turn failure", prefix + `printf '{"type":"turn.failed","error":{"message":"quota exhausted"}}\n'`, "codex dispatch did not complete: quota exhausted"},
		{"nonzero exit after completed turn", prefix + `printf '{"type":"agent_message","message":"final"}\n{"type":"turn.completed"}\n'; exit 1`, "codex exited with code 1; `al dispatch` exiting 70; last provider error: Reconnecting... 2/5 (request timed out)"},
		{"exit without terminal event", prefix + `exit 1`, "codex exited with code 1; `al dispatch` exiting 70; last provider error: Reconnecting... 2/5 (request timed out)"},
		{"clean exit without terminal event", prefix, "codex dispatch completed without required terminal result, session ID, and final answer; last provider error: Reconnecting... 2/5 (request timed out)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			result, err := executeProvider(providerCommand{
				Path: "/bin/sh", Args: []string{"-c", tc.script}, Env: os.Environ(), Provider: AgentCodex,
			}, nil, run, root, nil, func(string) error { return nil })
			if tc.wantErr == "" {
				if err != nil || result.Answer != "final" {
					t.Fatalf("recovered Codex retry = %#v, %v", result, err)
				}
				return
			}
			requireDispatchExitCode(t, err, ExitTargetFailure)
			if err.Error() != tc.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestProviderDiagnosticPreservesWrappedFailure(t *testing.T) {
	cause := errors.New("provider wait failed")
	err := withProviderDiagnostic(providerWaitError(AgentCodex, cause), "retry notice")
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped wait cause was not preserved: %v", err)
	}
	if want := "wait for codex: provider wait failed; last provider error: retry notice"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
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
