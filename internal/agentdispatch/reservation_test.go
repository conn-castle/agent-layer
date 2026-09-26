package agentdispatch

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestReservedStartInterruptedMidLaunchIsNeverRelaunched covers the crash
// window the CLI cannot reproduce: a start that claimed its reservation dies
// before its worker exists. The repeat start must resolve that invocation
// through launch recovery and return it rather than launching again.
func TestReservedStartInterruptedMidLaunchIsNeverRelaunched(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := filepath.Join(t.TempDir(), "bin")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}\n'`)
	var reserved bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, Stdout: &reserved}); err != nil {
		t.Fatal(err)
	}
	var reservation Result
	if err := json.Unmarshal(reserved.Bytes(), &reservation); err != nil {
		t.Fatal(err)
	}
	start := func(stdout *bytes.Buffer, launcher workerLauncher) error {
		return Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Reserved work", Reservation: reservation.Handle,
			Stdout: stdout, Env: []string{}, LookPath: mockLookPath(binDir),
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker:  launcher,
		})
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("interrupted start did not reach its worker launch")
			}
		}()
		_ = start(&bytes.Buffer{}, func(_ string, runID string, _ string) (launchedWorker, error) {
			record, err := loadRunRecord(root, runID)
			if err != nil {
				t.Fatal(err)
			}
			record.LauncherPID = 99999999
			record.LauncherStartIdentity = "gone"
			if err := writeRunRecord(filepathForRun(root, runID), &record); err != nil {
				t.Fatal(err)
			}
			panic("start process died before launching its worker")
		})
	}()

	var repeated bytes.Buffer
	if err := start(&repeated, func(string, string, string) (launchedWorker, error) {
		t.Fatal("repeated start launched a second worker")
		return launchedWorker{}, nil
	}); err != nil {
		t.Fatalf("repeated start: %v", err)
	}
	var result Result
	if err := json.Unmarshal(repeated.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.InvocationID != reservation.InvocationID || !result.AlreadyStarted || result.State != dispatchStateFailed {
		t.Fatalf("repeated start result = %#v", result)
	}
	record, err := loadRunRecord(root, reservation.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RecoveryState != recoveryAcceptanceUnknown || !record.LaunchFenced {
		t.Fatalf("interrupted reservation recovery = %q fenced=%t", record.RecoveryState, record.LaunchFenced)
	}
}
