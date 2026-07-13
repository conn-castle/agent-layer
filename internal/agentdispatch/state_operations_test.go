package agentdispatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateRejectsMalformedMappingsAndRecords(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "Upper", "two_words", "../escape"} {
		if _, err := sessionPath(root, name); err == nil {
			t.Fatalf("sessionPath accepted %q", name)
		}
	}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	name := "tiny-round-capacitor"
	if err := os.WriteFile(filepath.Join(stateDir, name+".json"), []byte(`{"name":"tiny-round-capacitor","agent":"codex","extra":true}`), 0o600); err != nil {
		t.Fatalf("write malformed mapping: %v", err)
	}
	if _, err := loadSession(root, name); err == nil {
		t.Fatal("loadSession accepted unknown JSON fields")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if err := os.Remove(filepath.Join(stateDir, name+".json")); err != nil {
		t.Fatalf("remove malformed mapping: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "INVALID.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write invalid filename: %v", err)
	}
	if _, err := listSessions(root); err == nil {
		t.Fatal("listSessions accepted an invalid state filename")
	}

	runID := "11111111-1111-4111-8111-111111111111"
	runDir := filepath.Join(dispatchRunPath(root), runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create run directory: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(runDir, dispatchRunFile), RunRecord{ID: "22222222-2222-4222-8222-222222222222"}); err != nil {
		t.Fatalf("write mismatched record: %v", err)
	}
	if _, err := loadRunRecord(root, runID); err == nil {
		t.Fatal("loadRunRecord accepted a mismatched ID")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
}

func TestReservationDoesNotOverwriteCollidingName(t *testing.T) {
	root := t.TempDir()
	originalSizes, originalShapes, originalElectrical := nameSizes, nameShapes, nameElectrical
	t.Cleanup(func() { nameSizes, nameShapes, nameElectrical = originalSizes, originalShapes, originalElectrical })
	nameSizes, nameShapes, nameElectrical = []string{"x"}, []string{"y"}, []string{"z"}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	collision := filepath.Join(stateDir, "x-y-z.json")
	const original = `{"name":"x-y-z","agent":"codex"}`
	if err := os.WriteFile(collision, []byte(original), 0o600); err != nil {
		t.Fatalf("write collision: %v", err)
	}
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	if _, err := reserveSession(root, run); err == nil {
		t.Fatal("reserveSession overwrote an existing mapping")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	data, err := os.ReadFile(collision) // #nosec G304 -- collision is a test-controlled path inside t.TempDir.
	if err != nil || string(data) != original {
		t.Fatalf("collision changed: %q, %v", data, err)
	}
}

func TestDispatchSessionRetentionPrunesOnlyExpiredInactiveMappings(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	old := now.Add(-dispatchSessionRetention - time.Hour)

	expired := Session{Name: "tiny-round-capacitor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: old}
	current := Session{Name: "small-bright-resistor", Agent: AgentClaude, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: now.Add(-time.Hour)}
	active := Session{Name: "large-steady-relay", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: old, RunID: runtimeSessionID}
	for _, session := range []Session{expired, current, active} {
		if err := persistSession(root, session); err != nil {
			t.Fatalf("persist %s: %v", session.Name, err)
		}
	}
	runDir := filepath.Join(dispatchRunPath(root), runtimeSessionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create active run: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(runDir, dispatchRunFile), RunRecord{ID: runtimeSessionID, State: dispatchStateRunning, RecoveryState: recoveryAcceptanceUnknown, PID: os.Getpid(), ProcessStartIdentity: processStartIdentity(os.Getpid())}); err != nil {
		t.Fatalf("write active run: %v", err)
	}
	if err := pruneExpiredSessions(root, now); err != nil {
		t.Fatalf("pruneExpiredSessions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dispatchStatePath(root), expired.Name+".json")); !os.IsNotExist(err) {
		t.Fatalf("expired inactive mapping remains: %v", err)
	}
	for _, path := range []string{
		filepath.Join(dispatchStatePath(root), current.Name+".json"),
		filepath.Join(dispatchStatePath(root), active.Name+".json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retention removed preserved mapping %s: %v", path, err)
		}
	}

	corruptPath := filepath.Join(dispatchStatePath(root), "short-curved-diode.json")
	if err := os.WriteFile(corruptPath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt mapping: %v", err)
	}
	if err := pruneExpiredSessions(root, now); err == nil {
		t.Fatal("retention hid a corrupt mapping")
	}
	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("retention removed corrupt mapping: %v", err)
	}
}

func TestListAndInspectExposeCurrentStateWithoutMutation(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	session.State = "durable"
	session.ProviderSessionID = runtimeSessionID
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist: %v", err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryNotResumable
	run.Record.CompletedAt = &now
	run.Record.NotResumable = true
	run.Record.TerminalReason = "provider failure"
	run.Record.ProviderSessionID = runtimeSessionID
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatalf("write record: %v", err)
	}

	var listed bytes.Buffer
	if err := List(ListRequest{Root: root, Stdout: &listed}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(listed.String(), session.Name+"\tclaude\tdurable") {
		t.Fatalf("list = %q", listed.String())
	}
	var inspection bytes.Buffer
	if err := Inspect(InspectionRequest{Root: root, ID: run.Record.ID, Stdout: &inspection}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	for _, want := range []string{"Mode: fresh", "State: failed", "Provider status: not resumable", "Terminal reason: provider failure"} {
		if !strings.Contains(inspection.String(), want) {
			t.Fatalf("inspection omitted %q: %q", want, inspection.String())
		}
	}
}

func TestDeleteRejectsCorruptRunRecordButAllowsMissingRecord(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, dispatchRunFile), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("corrupt run record: %v", err)
	}
	if err := Delete(root, session.Name); err == nil {
		t.Fatal("Delete accepted corrupt run record")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if err := os.RemoveAll(run.Dir); err != nil {
		t.Fatalf("remove run: %v", err)
	}
	if err := Delete(root, session.Name); err != nil {
		t.Fatalf("Delete with missing run record: %v", err)
	}
}
