package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Reserved work", Reservation: &reservation.Handle,
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
	err := start(&repeated, func(string, string, string) (launchedWorker, error) {
		t.Fatal("repeated start launched a second worker")
		return launchedWorker{}, nil
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
		t.Fatalf("repeated start error = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}
	if repeated.Len() != 0 {
		t.Fatalf("repeated start returned success output: %s", repeated.String())
	}
	var waited bytes.Buffer
	err = Wait(WaitRequest{Root: root, InvocationID: reservation.InvocationID, Stdout: &waited, Timeout: 2 * time.Second})
	if !errors.As(err, &exitErr) || exitErr.Code != ExitTargetFailure {
		t.Fatalf("wait after interrupted launch = %v, want exit %d", err, ExitTargetFailure)
	}
	var result Result
	if err := json.Unmarshal(waited.Bytes(), &result); err != nil || result.State != dispatchStateFailed {
		t.Fatalf("wait after interrupted launch = %q, %v", waited.String(), err)
	}
	record, err := loadRunRecord(root, reservation.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RecoveryState != recoveryAcceptanceUnknown || !record.LaunchFenced {
		t.Fatalf("interrupted reservation recovery = %q fenced=%t", record.RecoveryState, record.LaunchFenced)
	}
}

func reserveInTest(t *testing.T, root string) Result {
	t.Helper()
	var reserved bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, Stdout: &reserved}); err != nil {
		t.Fatal(err)
	}
	var reservation Result
	if err := json.Unmarshal(reserved.Bytes(), &reservation); err != nil {
		t.Fatal(err)
	}
	return reservation
}

// TestReservedStartLaunchesOnce proves that concurrent starts claim the
// reservation once and every later start fails without launching.
func TestReservedStartLaunchesOnce(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	reservation := reserveInTest(t, root)
	var launches atomic.Int32
	launcher := func(string, string, string) (launchedWorker, error) {
		launches.Add(1)
		read, write, err := os.Pipe()
		if err != nil {
			return launchedWorker{}, err
		}
		go func() { defer func() { _ = read.Close() }(); var token [1]byte; _, _ = read.Read(token[:]) }()
		return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
	}
	start := func(agent string, prompt string) (Result, error) {
		var stdout bytes.Buffer
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: agent, Prompt: prompt, Reservation: &reservation.Handle,
			Stdout: &stdout, Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker:  launcher,
		})
		var result Result
		if err == nil {
			if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
				t.Errorf("start wrote invalid JSON %q: %v", stdout.String(), decodeErr)
			}
		}
		return result, err
	}

	const concurrent = 8
	results := make([]Result, concurrent)
	errs := make([]error, concurrent)
	var group sync.WaitGroup
	for index := range concurrent {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = start(AgentCodex, "Reserved work")
		}()
	}
	group.Wait()
	already := 0
	started := 0
	for index := range concurrent {
		if errs[index] == nil {
			started++
			if results[index].InvocationID != reservation.InvocationID || results[index].Handle != reservation.Handle {
				t.Fatalf("concurrent start %d returned %#v, want reservation %#v", index, results[index], reservation)
			}
			continue
		}
		var exitErr *ExitError
		if !errors.As(errs[index], &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
			t.Fatalf("concurrent start %d: %v, want exit %d", index, errs[index], ExitReservationAlreadyStarted)
		}
		already++
	}
	if got := launches.Load(); got != 1 || started != 1 || already != concurrent-1 {
		t.Fatalf("concurrent starts launched %d workers with %d successes and %d already started errors, want 1, 1, and %d", got, started, already, concurrent-1)
	}

	// A second start is rejected regardless of whether its arguments match.
	for _, repeat := range []struct{ agent, prompt string }{{"Codex", "Reserved work"}, {AgentCodex, "Reserved work\n"}} {
		result, err := start(repeat.agent, repeat.prompt)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted || result.InvocationID != "" {
			t.Fatalf("repeated start %#v = %#v, %v", repeat, result, err)
		}
	}
	_, err := start(AgentCodex, "Different work")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
		t.Fatalf("start with different arguments error = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}
	if got := launches.Load(); got != 1 {
		t.Fatalf("repeated starts launched %d workers, want 1", got)
	}
}

// TestContinueExplainsInterruptedReservedStart covers a start that claimed its
// reservation but died before recording the conversation: continue must not
// claim the reservation never started.
func TestContinueExplainsInterruptedReservedStart(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	reservation := reserveInTest(t, root)
	if _, claimed, err := claimReservation(root, reservation.InvocationID, func(current *RunRecord) {
		current.Agent = AgentCodex
	}); err != nil || !claimed {
		t.Fatalf("claim reservation = %t, %v", claimed, err)
	}
	err := Continue(ContinueOptions{Root: root, Handle: reservation.Handle, Prompt: "More", Env: []string{}, LookPath: alwaysFound})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable || strings.Contains(err.Error(), "never started") || !strings.Contains(err.Error(), reservation.InvocationID) {
		t.Fatalf("continue after an interrupted reserved start = %v", err)
	}
}
