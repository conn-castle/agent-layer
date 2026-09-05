package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the published worker and public wait/continue boundary, including
// failure before a final result. A reducer-only test cannot prove resumability.
func TestAntigravityHandoffRetainsConversation(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	for _, tc := range []struct {
		name, script, reason string
	}{
		{"init then exit", `printf '{"event":"init","conversation_id":"` + id + `"}\n'; exit 1`, "exited"},
		{"failed result identity", `printf '{"event":"result","result":{"status":"ERROR","conversation_id":"` + id + `","error":"handoff failed"}}\n'; exit 1`, "handoff failed"},
		{"conflicting result", `printf '{"event":"init","conversation_id":"` + id + `"}\n{"event":"result","result":{"status":"SUCCESS","conversation_id":"wrong","response":"wrong answer","usage":{}}}\n'`, "conflicting session IDs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			binDir := t.TempDir()
			writeDispatchStub(t, binDir, "agy", tc.script)
			t.Setenv("PATH", testPath(binDir))
			t.Setenv("AL_TEST_LOG", filepath.Join(root, "provider-args"))
			var gate *os.File
			launcher := func(string, string, string) (launchedWorker, error) {
				read, write, err := os.Pipe()
				if err != nil {
					return launchedWorker{}, err
				}
				gate = read
				t.Cleanup(func() { _ = read.Close() })
				return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
			}
			var out bytes.Buffer
			if err := Start(StartOptions{
				Root: root, WorkDir: root, Agent: AgentAntigravity, Prompt: "handoff", Stdout: &out,
				Env: os.Environ(), LookPath: mockLookPath(binDir), launchWorker: launcher,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentAntigravity], nil },
			}); err != nil {
				t.Fatal(err)
			}
			var response Result
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			session, err := loadSession(root, response.Handle)
			if err != nil {
				t.Fatal(err)
			}
			if err := RunWorker(root, session.RunID, gate); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("worker failure = %v, want %q", err, tc.reason)
			}
			out.Reset()
			requireDispatchExitCode(t, Wait(WaitRequest{Root: root, ID: session.Name, Stdout: &out}), ExitTargetFailure)
			if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.State != dispatchStateFailed || response.ResultPath != "" {
				t.Fatalf("failed handoff published a result: %s, %v", out.String(), err)
			}
			session, err = loadSession(root, session.Name)
			if err != nil || session.ProviderSessionID != id || session.ActiveRunID != "" {
				t.Fatalf("lost conversation after failure: %#v, %v", session, err)
			}
			// Continuation must send the captured identity to the provider, not
			// merely keep the same Agent Layer handle around a fresh conversation.
			writeDispatchStub(t, binDir, "agy", `printf '{"event":"init","conversation_id":"`+id+`"}\n{"event":"result","result":{"status":"SUCCESS","conversation_id":"`+id+`","response":"continued","usage":{"input_tokens":1}}}\n'`)
			if err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Handle: session.Name, Prompt: "continue", Env: os.Environ(),
				LookPath: mockLookPath(binDir), launchWorker: launcher,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentAntigravity], nil },
			}); err != nil {
				t.Fatal(err)
			}
			session, err = loadSession(root, session.Name)
			if err != nil {
				t.Fatal(err)
			}
			if err := RunWorker(root, session.RunID, gate); err != nil {
				t.Fatal(err)
			}
			assertFileContains(t, filepath.Join(root, "provider-args"), "=--conversation\n")
			assertFileContains(t, filepath.Join(root, "provider-args"), "="+id+"\n")
			out.Reset()
			if err := Wait(WaitRequest{Root: root, ID: session.Name, Stdout: &out}); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.State != dispatchStateCompleted {
				t.Fatalf("continued handoff = %s, %v", out.String(), err)
			}
			answer, err := os.ReadFile(response.ResultPath) // #nosec G304 -- test-owned dispatch result.
			if err != nil || string(answer) != "continued" {
				t.Fatalf("continued answer = %q, %v", answer, err)
			}
		})
	}
}

func TestContinueWithoutIdentityRequiresProvenSafeRetry(t *testing.T) {
	for _, recovery := range []string{recoveryAcceptanceUnknown, recoveryResumeRequired, recoveryRetrySafe} {
		t.Run(recovery, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			run, session := newWaitTestRun(t, root)
			session.ProviderSessionID = ""
			session.State = sessionStatePending
			if err := persistSession(root, session); err != nil {
				t.Fatal(err)
			}
			run.Record.State = dispatchStateFailed
			run.Record.RecoveryState = recovery
			now := time.Now().UTC()
			run.Record.CompletedAt = &now
			if err := writeRunRecord(run.Dir, &run.Record); err != nil {
				t.Fatal(err)
			}
			if err := releaseConversation(root, session.Name, run.Record.ID); err != nil {
				t.Fatal(err)
			}
			launched := false
			err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Handle: session.Name, Prompt: "retry", Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker: func(string, string, string) (launchedWorker, error) {
					launched = true
					return launchedWorker{}, fmt.Errorf("test launcher reached")
				},
			})
			if recovery == recoveryRetrySafe {
				if !launched {
					t.Fatalf("safe retry rejected: %v", err)
				}
			} else {
				requireDispatchExitCode(t, err, ExitUnavailable)
				if launched || !strings.Contains(err.Error(), "no provider session ID") {
					t.Fatalf("unsafe continuation: launched=%t, err=%v", launched, err)
				}
				current, err := loadSession(root, session.Name)
				if err != nil || current.RunID != run.Record.ID {
					t.Fatalf("rejected continuation changed current invocation: %#v, %v", current, err)
				}
			}
		})
	}
}

func TestRunnerBoundsHandoffAndCleansDescendants(t *testing.T) {
	const answer = `printf '{"type":"thread.started","thread_id":"` + runtimeSessionID + `"}\n{"type":"agent_message","message":"final"}\n{"type":"turn.completed"}\n'; `
	for _, tc := range []struct {
		name, script string
		wantFailure  bool
	}{
		{"terminal but live", answer + `trap '' TERM; while :; do sleep 1; done`, true},
		{"failure after terminal", answer + `sleep 0.05; printf '{"type":"turn.failed","message":"late failure"}\n'`, true},
		{"stdout closed but live", `exec 1>&-; trap '' TERM; while :; do sleep 1; done`, true},
		{"inherited output", answer + `sleep 60 &`, false},
		{"detached output", answer + `sleep 60 >/dev/null 2>&1 &`, false},
		{"exit near deadline with slow descendant cleanup", answer + `(trap '' TERM; exec sleep 60) >/dev/null 2>&1 & sleep 4.2`, false},
		{"exit without terminal with inherited output", `sleep 60 & exit 1`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			result, err := executeProvider(providerCommand{
				Path: "/bin/sh", Args: []string{"-c", tc.script}, Env: os.Environ(), Provider: AgentCodex,
			}, nil, run, root, nil, func(string) error { return nil })
			if time.Since(started) > providerShutdownGrace+4*providerTerminationGrace {
				t.Fatalf("handoff did not finish within shutdown bounds: %v", err)
			}
			if tc.wantFailure {
				requireDispatchExitCode(t, err, ExitTargetFailure)
				var unproven *unprovenProviderTerminationError
				if errors.As(err, &unproven) {
					t.Fatalf("test provider termination was unproven: %v", err)
				}
				if result.Answer != "" {
					t.Fatalf("failed shutdown returned final answer: %#v", result)
				}
			} else if err != nil || result.Answer != "final" {
				t.Fatalf("clean handoff = %#v, %v", result, err)
			}
			if !providerProcessGroupDead(run.Record.ProcessGroupID) {
				t.Fatalf("handoff left live descendants in group %d (recorded identity %q, current identity %q, reused %t): %v", run.Record.ProcessGroupID, run.Record.ProcessStartIdentity, processStartIdentity(run.Record.PID), providerProcessGroupReused(run.Record), err)
			}
		})
	}
}

func TestRunnerLargePromptWithInheritedStdin(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	prompt := bytes.Repeat([]byte("prompt bytes\n"), 32*1024)
	// The child deliberately holds stdin without reading it. The leader
	// reads only a prefix; a pipe-backed prompt would leave exec's copy
	// goroutine blocked after its successful exit.
	script := `read -r first; [ "$first" = "prompt bytes" ] || exit 2
exec 3<&0
sleep 60 <&3 >/dev/null 2>&1 &
printf '{"type":"thread.started","thread_id":"` + runtimeSessionID + `"}\n{"type":"agent_message","message":"final"}\n{"type":"turn.completed"}\n'
`
	result, err := executeProvider(providerCommand{
		Path: "/bin/sh", Args: []string{"-c", script}, Env: os.Environ(), Provider: AgentCodex,
	}, prompt, run, root, nil, func(string) error { return nil })
	if err != nil || result.Answer != "final" {
		t.Fatalf("completed provider with inherited stdin = %#v, %v", result, err)
	}
	if !providerProcessGroupDead(run.Record.ProcessGroupID) {
		t.Fatal("provider left an stdin-holding descendant alive")
	}
	files, err := filepath.Glob(filepath.Join(run.Dir, "provider-stdin-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("private prompt was left on disk: %v, %v", files, err)
	}
}

func TestAntigravityIdentityEvidenceSurvivesPersistenceFailure(t *testing.T) {
	for _, expected := range []string{"", runtimeSessionID} {
		t.Run("expected="+expected, func(t *testing.T) {
			root := t.TempDir()
			run, err := newDispatchRun(root, AgentAntigravity, supportedProviderVersions[AgentAntigravity], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			session, err := reserveSession(root, run)
			if err != nil {
				t.Fatal(err)
			}
			const observed = "22222222-2222-4222-8222-222222222222"
			persistCalled := false
			_, err = executeProvider(providerCommand{
				Path: "/bin/sh", Args: []string{"-c", `printf '{"event":"init","conversation_id":"` + observed + `"}\n'`},
				Env: os.Environ(), Provider: AgentAntigravity, SessionID: expected,
			}, nil, run, root, nil, func(string) error {
				persistCalled = true
				return errors.New("session store unavailable")
			})
			requireDispatchExitCode(t, err, ExitTargetFailure)
			_ = finishDispatchFailure(dispatchExecution{Root: root, Run: run, Session: session, Mode: dispatchModeFresh}, err)
			record, loadErr := loadRunRecord(root, run.Record.ID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if expected == "" {
				if !persistCalled || record.ProviderSessionID != observed || !strings.Contains(record.TerminalReason, "session store unavailable") {
					t.Fatalf("lost observed identity on persistence failure: %#v", record)
				}
			} else if persistCalled || record.ProviderSessionID != "" || !strings.Contains(record.TerminalReason, "requested conversation") {
				t.Fatalf("accepted mismatched resume identity: %#v", record)
			}
		})
	}
}
