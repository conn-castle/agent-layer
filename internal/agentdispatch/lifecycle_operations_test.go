package agentdispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReconcileDoesNotTerminalizeRunWithUnprovableOwnership(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveSession(root, run); err != nil {
		t.Fatal(err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.PID = os.Getpid()
	run.Record.ProcessStartIdentity = ""
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	record, err := reconcileOrphan(root, run.Record)
	if err != nil {
		t.Fatalf("reconcileOrphan: %v", err)
	}
	if record.State != dispatchStateRunning {
		t.Fatalf("unprovable ownership was terminalized to %q", record.State)
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("unprovable ownership was terminalized on disk to %q", current.State)
	}
}

func TestRetentionRemovesOnlyExpiredUnreferencedTerminalEvidence(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	old := now.Add(-dispatchSessionRetention - time.Hour)
	makeRecord := func(state string, completed *time.Time) *dispatchRun {
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		run.Record.State = state
		if state == dispatchStateCompleted {
			run.Record.RecoveryState = recoveryResumeRequired
		} else {
			run.Record.RecoveryState = recoveryAcceptanceUnknown
		}
		run.Record.CompletedAt = completed
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatal(err)
		}
		return run
	}
	expired := makeRecord(dispatchStateCompleted, &old)
	active := makeRecord(dispatchStateRunning, nil)
	current := makeRecord(dispatchStateCompleted, &old)
	session := Session{Name: "tiny-round-capacitor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, RunID: current.Record.ID, CreatedAt: now, LastUsedAt: now}
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired.Dir); !os.IsNotExist(err) {
		t.Fatalf("expired terminal evidence remains: %v", err)
	}
	for _, dir := range []string{active.Dir, current.Dir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("preserved evidence removed: %v", err)
		}
	}
}

func TestCancelEscalatesButRetainsClaimUntilOwnedProcessIsReaped(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(t.TempDir(), "sigterm-handler-ready")
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; touch "$1"; while :; do sleep 1; done`, "sh", readyPath) // #nosec G204 -- test-owned path passed as a positional argument.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	waitStarted := false
	waitDone := make(chan error, 1)
	defer func() {
		if !stopped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if waitStarted {
				<-waitDone
			} else {
				_ = cmd.Wait()
			}
		}
	}()
	waitForTestPath(t, readyPath)
	run.Record.State = dispatchStateRunning
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	waitStarted = true
	go func() { waitDone <- cmd.Wait() }()
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- Cancel(CancelRequest{Root: root, ID: run.Record.ID}) }()
	waitForRunState(t, root, run.Record.ID, dispatchStateCancelled)
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateCancelled || record.RecoveryState != recoveryAcceptanceUnknown {
		t.Fatalf("cancelled record = %#v", record)
	}
	retainedDuringGrace, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retainedDuringGrace.ActiveRunID != run.Record.ID || processOwnership(record) != ownershipOwned {
		t.Fatalf("cancel released a claim during the graceful shutdown window: session = %#v, record = %#v", retainedDuringGrace, record)
	}
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel did not finish after bounded forced escalation")
	}
	if err := <-waitDone; err == nil {
		t.Fatal("automatically SIGKILLed test process exited successfully")
	}
	stopped = true
	released, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if released.ActiveRunID != "" {
		t.Fatalf("cancel retained the claim after proving process-group death: %#v", released)
	}
	replacement, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claimConversation(root, session.Name, replacement.Record.ID); err != nil {
		t.Fatalf("replace cancelled claim after process-group death proof: %v", err)
	}
	recovered, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ActiveRunID != replacement.Record.ID || recovered.RunID != replacement.Record.ID {
		t.Fatalf("dead-owner recovery did not publish replacement claim: %#v", recovered)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, dispatchRunFile)); err != nil {
		t.Fatalf("cancel removed evidence: %v", err)
	}
}

// TestRetentionRemovesRetiredFanoutState proves the retired synchronous
// fanout state root cannot survive as on-disk garbage after the asynchronous
// cutover: no code can create, read, or finish those manifests again.
func TestRetentionRemovesRetiredFanoutState(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".agent-layer", "tmp", "fanouts", "old-fanout")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "manifest.json"), []byte(`{"id":"old-fanout"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "tmp", "fanouts")); !os.IsNotExist(err) {
		t.Fatalf("retired fanout state remains: %v", err)
	}
}
