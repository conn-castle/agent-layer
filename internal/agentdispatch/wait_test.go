package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWaitReturnsDurableCompletedResult(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	if err := os.WriteFile(run.Record.AnswerPath, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, session.Name, run.Record.ID); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Wait(WaitRequest{Root: root, ID: session.Name, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Handle != session.Name || got.State != dispatchStateCompleted || got.ResultPath != run.Record.AnswerPath {
		t.Fatalf("wait result = %#v", got)
	}
}

func TestCompletedResultPathRejectsEmptyAndNonFilePaths(t *testing.T) {
	for _, answerPath := range []string{"", t.TempDir()} {
		_, err := completedResultPath(RunRecord{AnswerPath: answerPath})
		requireDispatchExitCode(t, err, ExitConfig)
	}
}

func TestWaitReturnsFailedJSONAndExitCategory(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "authentication failed"
	run.Record.TerminalExitCode = ExitUnavailable
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := Wait(WaitRequest{Root: root, ID: session.Name, Stdout: &stdout})
	requireDispatchExitCode(t, err, ExitUnavailable)
	var got Result
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if got.State != dispatchStateFailed || got.Error != "authentication failed" {
		t.Fatalf("wait result = %#v", got)
	}
}

func TestWaitBlocksUntilTerminal(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Wait(WaitRequest{Root: root, ID: session.Name, Stdout: io.Discard}) }()
	select {
	case err := <-done:
		t.Fatalf("wait returned early: %v", err)
	case <-time.After(2 * dispatchWaitInterval):
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	current.State = dispatchStateCancelled
	current.CompletedAt = &now
	current.TerminalReason = "cancelled"
	current.TerminalExitCode = ExitTargetFailure
	if err := writeRunRecord(run.Dir, &current); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWaitYieldsRunningWithoutChangingInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	activity := time.Now().UTC().Add(-time.Minute)
	output := activity.Add(-time.Minute)
	run.Record.LastActivityAt = &activity
	run.Record.LastOutputAt = &output
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := Wait(WaitRequest{
		Root: root, ID: session.Name, Stdout: &stdout,
		Timeout: 2 * dispatchWaitInterval,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Handle != session.Name || got.State != dispatchStateRunning {
		t.Fatalf("wait result = %#v", got)
	}
	if got.LastActivityAt == nil || !got.LastActivityAt.Equal(activity) || got.LastOutputAt == nil || !got.LastOutputAt.Equal(output) {
		t.Fatalf("wait lost recorded activity timestamps: %#v", got)
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("invocation state = %q, want running", current.State)
	}
}

// TestWaitReturnsPromptlyWhenContextIsCancelled ensures that a CLI interrupted
// with Ctrl-C stops polling without changing the provider invocation state.
func TestWaitReturnsPromptlyWhenContextIsCancelled(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Wait(WaitRequest{Context: ctx, Root: root, ID: session.Name, Stdout: io.Discard})
	}()
	select {
	case err := <-done:
		t.Fatalf("wait returned before cancellation: %v", err)
	case <-time.After(2 * dispatchWaitInterval):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context cancellation", err)
		}
	case <-time.After(2 * dispatchWaitInterval):
		t.Fatal("wait did not return after cancellation")
	}
}

// TestReconcileOrphanKeepsReapedProviderWithLiveWorker guards the completion
// window: after the worker reaps the provider process, the run record still
// says running with a provably dead provider PID until the terminal record is
// written. A concurrent waiter must not terminalize the run while the worker
// is alive, or successful runs intermittently fail and lose their answers.
func TestReconcileOrphanKeepsReapedProviderWithLiveWorker(t *testing.T) {
	root := t.TempDir()
	run, _ := newWaitTestRun(t, root)
	reaped := exec.Command("true")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.PID = reaped.Process.Pid
	run.Record.ProcessStartIdentity = "reaped-provider"
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	record, err := reconcileOrphan(root, run.Record)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateRunning {
		t.Fatalf("live worker with reaped provider was reconciled to %q", record.State)
	}
}

// TestWaitFailsInvocationAbandonedBeforeWorkerLaunch covers the only
// crash window without a self-healing process: the launching CLI died after
// claiming the run but before publishing any worker identity. Without
// launcher-identity reconciliation, wait would poll such a run forever.
func TestWaitFailsInvocationAbandonedBeforeWorkerLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	record, err := reconcileOrphan(root, run.Record)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStatePending {
		t.Fatalf("pending run with live launcher was reconciled to %q", record.State)
	}
	run.Record.LauncherPID = 99999999
	run.Record.LauncherStartIdentity = "gone"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = Wait(WaitRequest{Root: root, ID: session.Name, Stdout: &stdout})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	var got Result
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if got.State != dispatchStateFailed || got.Error != "dispatch was interrupted before launching its worker" {
		t.Fatalf("wait result = %#v", got)
	}
}

func TestWaitRetainsOrphanClaimWhileDescendantsSurvive(t *testing.T) {
	root := t.TempDir()
	run, session := newWaitTestRun(t, root)
	childPath := filepath.Join(root, "child.pid")
	cmd := exec.Command("/bin/sh", "-c", `sleep 60 & echo $! > "$1"`, "sh", childPath) // #nosec G204 -- fixed test command and test-owned path.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	waitForProviderChildPID(t, childPath)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = "reaped-leader"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	err := Wait(WaitRequest{Root: root, ID: session.Name, Timeout: time.Millisecond})
	requireDispatchExitCode(t, err, ExitUnavailable)
	current, err := loadSession(root, session.Name)
	if err != nil || current.ActiveRunID != run.Record.ID {
		t.Fatalf("orphan recovery released a live descendant claim: %#v, %v", current, err)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitForProviderProcessGroupExit(t, cmd.Process.Pid)
	requireDispatchExitCode(t, Wait(WaitRequest{Root: root, ID: session.Name}), ExitTargetFailure)
	current, err = loadSession(root, session.Name)
	if err != nil || current.ActiveRunID != "" {
		t.Fatalf("dead orphan claim was not released: %#v, %v", current, err)
	}
}

func TestWaitRecoversReusedProcessGroupWithoutSignallingIt(t *testing.T) {
	for _, state := range []string{dispatchStateRunning, dispatchStateCancelled} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			run, session := newWaitTestRun(t, root)
			unrelated := exec.Command("sleep", "60")
			prepareProviderProcessGroup(unrelated)
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
			run.Record.State = state
			run.Record.PID = unrelated.Process.Pid
			run.Record.ProcessGroupID = unrelated.Process.Pid
			run.Record.ProcessStartIdentity = "previous-owner"
			if state == dispatchStateCancelled {
				now := time.Now().UTC()
				run.Record.CompletedAt = &now
			}
			if err := writeRunRecord(run.Dir, &run.Record); err != nil {
				t.Fatal(err)
			}
			err := Wait(WaitRequest{Root: root, ID: session.Name, Timeout: time.Millisecond})
			if state == dispatchStateRunning {
				requireDispatchExitCode(t, err, ExitTargetFailure)
			} else if err != nil {
				t.Fatal(err)
			}
			wantActive := ""
			if state == dispatchStateCancelled {
				// Cancelled waits are idempotent reads. A replacement claim is
				// the boundary that checks whether cancelled work still lives.
				replacement, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := claimConversation(root, session.Name, replacement.Record.ID); err != nil {
					t.Fatal(err)
				}
				wantActive = replacement.Record.ID
			}
			current, err := loadSession(root, session.Name)
			if err != nil || current.ActiveRunID != wantActive {
				t.Fatalf("reused group prevented claim recovery: %#v, %v", current, err)
			}
			if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("recovery signalled an unrelated process: %v", err)
			}
		})
	}
}

func TestWaitRejectsUnknownHandle(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Wait(WaitRequest{Root: root, ID: "missing-handle", Stdout: io.Discard})
	var dispatchErr *ExitError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ExitUsage {
		t.Fatalf("error = %v", err)
	}
}

func newWaitTestRun(t *testing.T, root string) (*dispatchRun, Session) {
	t.Helper()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	return run, session
}
