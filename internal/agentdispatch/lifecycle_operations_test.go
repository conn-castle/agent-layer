package agentdispatch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryUsesImmutableRunsAfterFriendlyMappingAdvances(t *testing.T) {
	root := t.TempDir()
	first, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, first)
	if err != nil {
		t.Fatal(err)
	}
	first.Record.State = dispatchStateCompleted
	first.Record.RecoveryState = recoveryResumeRequired
	completed := time.Now().UTC()
	first.Record.CompletedAt = &completed
	if err := writeRunRecord(first.Dir, &first.Record); err != nil {
		t.Fatal(err)
	}
	second, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	second.Record.Name = session.Name
	second.Record.PreviousRunID = first.Record.ID
	second.Record.State = dispatchStateCompleted
	second.Record.RecoveryState = recoveryResumeRequired
	second.Record.CompletedAt = &completed
	if err := writeRunRecord(second.Dir, &second.Record); err != nil {
		t.Fatal(err)
	}
	session.RunID = second.Record.ID
	session.ActiveRunID = ""
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if !strings.Contains(output.String(), first.Record.ID) || !strings.Contains(output.String(), second.Record.ID) {
		t.Fatalf("history omitted a retained turn: %q", output.String())
	}
}

func TestHistorySkipsUnreadableRunRecordsAndWarns(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &completed
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	corruptID := "33333333-3333-4333-8333-333333333333"
	corruptDir := filepath.Join(dispatchRunPath(root), corruptID)
	if err := os.MkdirAll(corruptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, dispatchRunFile), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	var output, warnings bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, Stderr: &warnings}); err != nil {
		t.Fatalf("History with a corrupt unrelated record: %v", err)
	}
	if !strings.Contains(output.String(), run.Record.ID) {
		t.Fatalf("history omitted the valid turn: %q", output.String())
	}
	if !strings.Contains(warnings.String(), corruptID) {
		t.Fatalf("history hid the skipped corrupt record: %q", warnings.String())
	}
	if _, err := os.Stat(filepath.Join(corruptDir, dispatchRunFile)); err != nil {
		t.Fatalf("history mutated corrupt evidence: %v", err)
	}
}

func TestInspectDoesNotTerminalizeRunWithUnprovableOwnership(t *testing.T) {
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

	var output bytes.Buffer
	if err := Inspect(InspectionRequest{Root: root, ID: run.Record.ID, Stdout: &output}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateRunning {
		t.Fatalf("unprovable ownership was terminalized to %q", record.State)
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

func TestCancelSignalsExactOwnedProcessGroup(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "sleep 30") // #nosec G204 -- fixed test process.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	run.Record.State = dispatchStateRunning
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := Cancel(CancelRequest{Root: root, ID: run.Record.ID}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateCancelled || record.RecoveryState != recoveryAcceptanceUnknown {
		t.Fatalf("cancelled record = %#v", record)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != "" {
		t.Fatalf("cancel retained active claim: %#v", retained)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, dispatchRunFile)); err != nil {
		t.Fatalf("cancel removed evidence: %v", err)
	}
}
