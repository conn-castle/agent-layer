package agentdispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateAPIsRejectMissingAndInvalidInputs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "UPPER", "under_score", "../escape"} {
		if err := persistSession(root, Session{Name: name}); err == nil {
			t.Fatalf("persistSession accepted %q", name)
		}
	}
	if _, err := loadSession(root, "tiny-round-capacitor"); err == nil {
		t.Fatal("loadSession accepted a missing mapping")
	} else {
		requireDispatchExitCode(t, err, ExitUsage)
	}
	for _, id := range []string{"not-a-uuid", "11111111x1111-4111-8111-111111111111", "g111111-1111-4111-8111-111111111111"} {
		if err := parseUUID(id); err == nil {
			t.Fatalf("parseUUID accepted %q", id)
		}
	}
	if _, err := loadRunRecord(root, "11111111-1111-4111-8111-111111111111"); err == nil {
		t.Fatal("loadRunRecord accepted a missing record")
	} else {
		requireDispatchExitCode(t, err, ExitUsage)
	}
}

func TestStateStorageReportsCorruptionAndFilesystemFailures(t *testing.T) {
	root := t.TempDir()
	if err := writeRunRecord(root, nil); err == nil {
		t.Fatal("writeRunRecord accepted nil")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if err := writeRunRecord("/dev/null", &RunRecord{}); err == nil {
		t.Fatal("writeRunRecord accepted a non-directory")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if _, err := newDispatchRun("/dev/null", AgentCodex, "version", "fresh"); err == nil {
		t.Fatal("newDispatchRun accepted a non-directory root")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if err := writeJSONAtomic(filepath.Join("/dev/null", "state.json"), map[string]string{"x": "y"}); err == nil {
		t.Fatal("writeJSONAtomic accepted a non-directory parent")
	}
	if err := writeJSONAtomic(filepath.Join(root, "unsupported.json"), make(chan int)); err == nil {
		t.Fatal("writeJSONAtomic accepted unsupported JSON")
	}

	jsonPath := filepath.Join(root, "multiple.json")
	if err := os.WriteFile(jsonPath, []byte(`{} {}`), 0o600); err != nil {
		t.Fatalf("write multiple values: %v", err)
	}
	var decoded map[string]any
	if err := readJSON(jsonPath, &decoded); err == nil {
		t.Fatal("readJSON accepted multiple JSON values")
	}
	if _, err := listSessions("/dev/null"); err == nil {
		t.Fatal("listSessions accepted invalid root")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
}

func TestStateHelpersDescribeSafeProcessAndNameFacts(t *testing.T) {
	if got := processAlive(0); got != statusUnknown {
		t.Fatalf("processAlive(0) = %q", got)
	}
	if got := processAlive(os.Getpid()); got != processStatusAlive {
		t.Fatalf("processAlive(self) = %q", got)
	}
	if id, err := newUUID(); err != nil {
		t.Fatalf("newUUID: %v", err)
	} else if err := parseUUID(id); err != nil {
		t.Fatalf("generated UUID invalid: %v", err)
	}
	if name, err := randomDispatchName(); err != nil || !validDispatchName(name) {
		t.Fatalf("random name = %q, %v", name, err)
	}
}
