package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
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
	var got publicResult
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
	var got publicResult
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
	var got publicResult
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if got.State != dispatchStateFailed || got.Error != "dispatch was interrupted before launching its worker" {
		t.Fatalf("wait result = %#v", got)
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
