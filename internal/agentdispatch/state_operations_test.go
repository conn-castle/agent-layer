package agentdispatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	run.Record.State = "failed"
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
	for _, want := range []string{"State: failed", "Provider status: not resumable", "Terminal reason: provider failure"} {
		if !strings.Contains(inspection.String(), want) {
			t.Fatalf("inspection omitted %q: %q", want, inspection.String())
		}
	}
}
