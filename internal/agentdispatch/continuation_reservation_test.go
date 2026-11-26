package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// completedConversationForTest creates a durable conversation whose initial
// invocation is completed with confirmed termination and a readable result.
func completedConversationForTest(t *testing.T, root string, agent string, providerSessionID string, reservedStart bool) (Session, *dispatchRun) {
	t.Helper()
	var run *dispatchRun
	var err error
	if reservedStart {
		expires := time.Now().UTC().Add(24 * time.Hour)
		run, err = newReservedDispatchRun(root, expires)
		if err != nil {
			t.Fatal(err)
		}
		run.Record.Agent = agent
		run.Record.ProviderVersion = supportedProviderVersions[agent]
		run.Record.LaunchDigest = reservationLaunchClaim
	} else {
		run, err = newDispatchRun(root, agent, supportedProviderVersions[agent], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
	}
	session, err := reserveSession(root, run, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.Record.AnswerPath, []byte("initial answer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.Record.EventsPath, []byte(`{"type":"complete"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	run.Record.ProviderSessionID = providerSessionID
	run.Record.LaunchFenced = true
	run.Record.TerminationConfirmed = true
	run.Record.TerminationConfirmedAt = &now
	run.Record.TerminationProof = terminationProofPrelaunchNoIntent
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session.Agent = agent
	session.Model = "test-model"
	session.ReasoningEffort = "high"
	session.TargetPinned = true
	session.ProviderSessionID = providerSessionID
	session.State = sessionStateDurable
	session.RunID = run.Record.ID
	session.ActiveRunID = ""
	session.ActiveClaimKnown = true
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func reserveContinuationInTest(t *testing.T, root string, handle string) Result {
	t.Helper()
	var buf bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, ContinueHandle: &handle, Stdout: &buf}); err != nil {
		t.Fatalf("Reserve(--continue %q): %v", handle, err)
	}
	var res Result
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decode reserve output %q: %v", buf.String(), err)
	}
	return res
}

func fakeWorkerLauncher(launches *atomic.Int32) workerLauncher {
	return func(string, string, string) (launchedWorker, error) {
		if launches != nil {
			launches.Add(1)
		}
		read, write, err := os.Pipe()
		if err != nil {
			return launchedWorker{}, err
		}
		go func() {
			defer func() { _ = read.Close() }()
			var token [1]byte
			_, _ = read.Read(token[:])
		}()
		return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
	}
}

func TestContinuationReservationIdempotentAndConcurrent(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-1", false)

	const concurrent = 8
	results := make([]Result, concurrent)
	errs := make([]error, concurrent)
	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			handle := session.Name
			errs[i] = Reserve(ReserveOptions{Root: root, ContinueHandle: &handle, Stdout: &buf})
			if errs[i] == nil {
				if err := json.Unmarshal(buf.Bytes(), &results[i]); err != nil {
					errs[i] = err
				}
			}
		}()
	}
	wg.Wait()

	first := results[0]
	if errs[0] != nil || first.Handle != session.Name || first.InvocationID == "" || first.InvocationID == firstRun.Record.ID || first.State != dispatchStateReserved || first.TerminationConfirmed || first.ReservationExpiresAt == nil {
		t.Fatalf("initial continuation reservation = %#v, err=%v", first, errs[0])
	}
	for i := 1; i < concurrent; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent reserve %d failed: %v", i, errs[i])
		}
		if results[i].InvocationID != first.InvocationID || results[i].Handle != first.Handle || results[i].State != dispatchStateReserved {
			t.Fatalf("concurrent reserve %d = %#v, want same reservation %#v", i, results[i], first)
		}
	}

	// Only two run directories exist: the initial run and the one pending reservation.
	entries, err := os.ReadDir(dispatchRunPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("run directory count = %d, want 2", len(entries))
	}

	// Sequential repeat also returns the exact same reservation.
	repeat := reserveContinuationInTest(t, root, session.Name)
	if repeat.InvocationID != first.InvocationID || !repeat.ReservationExpiresAt.Equal(*first.ReservationExpiresAt) {
		t.Fatalf("repeat reserve = %#v, want %#v", repeat, first)
	}
}

func TestContinuationReservationNoDisturbanceAndExclusivity(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-2", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	// While pending, inspect/wait/output by conversation handle report the latest launched invocation.
	var inspectBuf bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, Handle: session.Name, Stdout: &inspectBuf}); err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(inspectBuf.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.InvocationID != firstRun.Record.ID || inspected.State != dispatchStateCompleted {
		t.Fatalf("inspect by handle while continuation reserved = %#v, want completed %s", inspected, firstRun.Record.ID)
	}

	var waitBuf bytes.Buffer
	if err := Wait(WaitRequest{Root: root, Handle: session.Name, Stdout: &waitBuf, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	var waited Result
	if err := json.Unmarshal(waitBuf.Bytes(), &waited); err != nil {
		t.Fatal(err)
	}
	if waited.InvocationID != firstRun.Record.ID || waited.State != dispatchStateCompleted || waited.ResultPath == "" {
		t.Fatalf("wait by handle while continuation reserved = %#v, want completed %s", waited, firstRun.Record.ID)
	}

	var outBuf bytes.Buffer
	if err := Output(OutputRequest{Root: root, Handle: session.Name, Artifact: artifactFinalAnswer, Stdout: &outBuf}); err != nil {
		t.Fatal(err)
	}
	var out OutputResult
	if err := json.Unmarshal(outBuf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.InvocationID != firstRun.Record.ID || out.Content != "initial answer\n" {
		t.Fatalf("output by handle while continuation reserved = %#v", out)
	}

	// Addressing the reserved continuation directly by invocation ID observes reserved state.
	var resInspectBuf bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, InvocationID: reserved.InvocationID, Stdout: &resInspectBuf}); err != nil {
		t.Fatal(err)
	}
	var resInspected InspectResult
	if err := json.Unmarshal(resInspectBuf.Bytes(), &resInspected); err != nil {
		t.Fatal(err)
	}
	if resInspected.InvocationID != reserved.InvocationID || resInspected.State != dispatchStateReserved || resInspected.ReservationExpiresAt == nil {
		t.Fatalf("inspect reserved continuation = %#v", resInspected)
	}

	var resWaitBuf bytes.Buffer
	if err := Wait(WaitRequest{Root: root, InvocationID: reserved.InvocationID, Stdout: &resWaitBuf, Timeout: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	var resWaited Result
	if err := json.Unmarshal(resWaitBuf.Bytes(), &resWaited); err != nil {
		t.Fatal(err)
	}
	if resWaited.State != dispatchStateReserved || resWaited.ConditionMet == nil || *resWaited.ConditionMet {
		t.Fatalf("bounded wait on reserved continuation = %#v", resWaited)
	}

	// Competing ordinary continue is refused without launching anything.
	var launches atomic.Int32
	err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: session.Name, Prompt: "competing",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(&launches),
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable || !strings.Contains(err.Error(), reserved.InvocationID) {
		t.Fatalf("competing continue = %v, want ExitUnavailable mentioning %s", err, reserved.InvocationID)
	}
	if launches.Load() != 0 {
		t.Fatalf("competing continue launched %d workers", launches.Load())
	}
	entries, err := os.ReadDir(dispatchRunPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("competing continue created run dirs: got %d, want 2", len(entries))
	}
}

func TestContinuationReservationLaunchesAtMostOnceAndSurvivesMoveOn(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-3", true)
	reserved := reserveContinuationInTest(t, root, session.Name)

	var launches atomic.Int32
	launcher := fakeWorkerLauncher(&launches)
	const concurrent = 8
	var arrived atomic.Int32
	ready := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	launch := func(handle string, selector string, prompt string) (Result, error) {
		var stdout bytes.Buffer
		err := Continue(ContinueOptions{
			Context: ctx, Root: root, WorkDir: root, Handle: handle, Reservation: &selector, Prompt: prompt,
			Stdout: &stdout, Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			beforeProvider: func(ctx context.Context, _ string) (func(), error) {
				if arrived.Add(1) == concurrent {
					close(ready)
				}
				select {
				case <-ready:
					return func() {}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
			launchWorker: launcher,
		})
		var res Result
		if err == nil {
			if decErr := json.Unmarshal(stdout.Bytes(), &res); decErr != nil {
				t.Errorf("invalid continue JSON %q: %v", stdout.String(), decErr)
			}
		}
		return res, err
	}

	// Concurrent launches of the same continuation reservation: exactly one wins.
	results := make([]Result, concurrent)
	errs := make([]error, concurrent)
	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = launch("", reserved.InvocationID, "Follow-up prompt")
		}()
	}
	wg.Wait()

	started, already := 0, 0
	for i := range concurrent {
		if errs[i] == nil {
			started++
			if results[i].InvocationID != reserved.InvocationID || results[i].Handle != session.Name || results[i].State != dispatchStateRunning {
				t.Fatalf("winner %d = %#v", i, results[i])
			}
			continue
		}
		var exitErr *ExitError
		if !errors.As(errs[i], &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
			t.Fatalf("loser %d error = %v, want exit %d", i, errs[i], ExitReservationAlreadyStarted)
		}
		already++
	}
	if started != 1 || already != concurrent-1 || launches.Load() != 1 {
		t.Fatalf("concurrent continuation launches = %d started, %d already, %d workers; want 1, %d, 1", started, already, launches.Load(), concurrent-1)
	}

	// Verify the worker request preserved session metadata.
	var req workerRequest
	if err := readJSON(filepath.Join(filepathForRun(root, reserved.InvocationID), workerRequestFile), &req); err != nil {
		t.Fatal(err)
	}
	if req.Mode != dispatchModeResume || req.Model != "test-model" || req.Effort != "high" || !req.TargetPinned {
		t.Fatalf("worker request did not preserve session configuration: %#v", req)
	}

	// Complete the reserved continuation, then move the conversation on to a third invocation.
	now := time.Now().UTC()
	if err := os.WriteFile(filepath.Join(filepathForRun(root, reserved.InvocationID), "result.md"), []byte("second answer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	completed, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
		r.State = dispatchStateCompleted
		r.RecoveryState = recoveryResumeRequired
		r.CompletedAt = &now
		r.LaunchFenced = true
		r.TerminationConfirmed = true
		r.TerminationConfirmedAt = &now
		r.TerminationProof = terminationProofPrelaunchNoIntent
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, completed); err != nil {
		t.Fatal(err)
	}

	// Start a third invocation via ordinary continue so the conversation has moved on.
	var thirdBuf bytes.Buffer
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: session.Name, Prompt: "Third turn",
		Stdout: &thirdBuf, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  launcher,
	}); err != nil {
		t.Fatalf("third continue failed: %v", err)
	}
	if launches.Load() != 2 {
		t.Fatalf("launches after third turn = %d, want 2", launches.Load())
	}

	// Replaying the earlier continuation reservation after move-on still reports already started.
	_, err = launch("", reserved.InvocationID, "Replay after move-on")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("replay after move-on = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}

	// Replaying the original start reservation still reports already started.
	startHandle := session.Name
	err = Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Replay start", Reservation: &startHandle,
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  launcher,
	})
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
		t.Fatalf("start reservation replay after reserved continuation = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}
	if launches.Load() != 2 {
		t.Fatalf("replays launched extra workers: got %d, want 2", launches.Load())
	}
}

func TestPlainStartConversationUnaffectedByContinuationReservation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-plain", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	var launches atomic.Int32
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Follow up",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(&launches),
	}); err != nil {
		t.Fatal(err)
	}

	// start --reservation on a plain-started conversation must still report NotFound (80),
	// never mistaking the continuation reservation for a start reservation.
	handle := session.Name
	err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Replay start", Reservation: &handle,
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(&launches),
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationNotFound {
		t.Fatalf("start --reservation on plain-started conversation = %v, want exit %d", err, ExitReservationNotFound)
	}
	if launches.Load() != 1 {
		t.Fatalf("launches = %d, want 1", launches.Load())
	}
}

func TestContinuationReservationCancelAndExpiryReturnToContinuable(t *testing.T) {
	for _, mode := range []string{"cancel", "expire_via_launch", "expire_via_reserve", "expire_via_cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-"+mode, true)
			reserved := reserveContinuationInTest(t, root, session.Name)

			var launches atomic.Int32
			launcher := fakeWorkerLauncher(&launches)

			wantCode := ExitReservationExpired
			switch mode {
			case "cancel":
				wantCode = ExitReservationCancelled
				var cancelBuf bytes.Buffer
				if err := Cancel(CancelRequest{Root: root, InvocationID: reserved.InvocationID, Stdout: &cancelBuf}); err != nil {
					t.Fatal(err)
				}
				var cancelled Result
				if err := json.Unmarshal(cancelBuf.Bytes(), &cancelled); err != nil {
					t.Fatal(err)
				}
				if cancelled.State != dispatchStateCancelled || !cancelled.TerminationConfirmed || cancelled.InvocationID != reserved.InvocationID {
					t.Fatalf("cancel result = %#v", cancelled)
				}
			default:
				if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
					past := time.Now().UTC().Add(-time.Minute)
					r.ReservationExpiresAt = &past
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if mode == "expire_via_cancel" {
					if err := Cancel(CancelRequest{Root: root, InvocationID: reserved.InvocationID}); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "expire_via_reserve" {
					fresh := reserveContinuationInTest(t, root, session.Name)
					if fresh.InvocationID == reserved.InvocationID {
						t.Fatalf("reserve after expiry reused expired reservation %s", reserved.InvocationID)
					}
					if err := Cancel(CancelRequest{Root: root, InvocationID: fresh.InvocationID}); err != nil {
						t.Fatal(err)
					}
				}
			}

			// Launching the retired continuation reservation fails with the exact expected code.
			err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Should not launch",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker:  launcher,
			})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != wantCode {
				t.Fatalf("continue retired reservation = %v, want exit %d", err, wantCode)
			}
			if launches.Load() != 0 {
				t.Fatalf("retired reservation launched %d workers", launches.Load())
			}

			// Start reservation replay still reports already started.
			startHandle := session.Name
			err = Start(StartOptions{
				Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Replay start", Reservation: &startHandle,
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker:  launcher,
			})
			if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
				t.Fatalf("start reservation replay = %v, want exit %d", err, ExitReservationAlreadyStarted)
			}

			// Conversation is continuable again: both fresh reserve and ordinary continue work.
			fresh := reserveContinuationInTest(t, root, session.Name)
			if fresh.InvocationID == reserved.InvocationID || fresh.State != dispatchStateReserved {
				t.Fatalf("fresh continuation reservation = %#v, want new ID distinct from %s", fresh, reserved.InvocationID)
			}
			if err := Cancel(CancelRequest{Root: root, InvocationID: fresh.InvocationID}); err != nil {
				t.Fatal(err)
			}
			if err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Handle: session.Name, Prompt: "Ordinary continue works",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker:  launcher,
			}); err != nil {
				t.Fatalf("ordinary continue after retirement failed: %v", err)
			}
			if launches.Load() != 1 {
				t.Fatalf("launches = %d, want 1", launches.Load())
			}
		})
	}
}

func TestContinuationReservationInterruptedMidLaunchResolvesFailedAndStaysContinuable(t *testing.T) {
	t.Run("crash_after_session_claim_before_worker", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-crash2", false)
		reserved := reserveContinuationInTest(t, root, session.Name)

		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("interrupted continue did not reach worker launch")
				}
			}()
			_ = Continue(ContinueOptions{
				Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Crash mid-launch",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker: func(_ string, runID string, _ string) (launchedWorker, error) {
					rec, err := loadRunRecord(root, runID)
					if err != nil {
						t.Fatal(err)
					}
					rec.LauncherPID = 99999999
					rec.LauncherStartIdentity = "gone"
					if err := writeRunRecord(filepathForRun(root, runID), &rec); err != nil {
						t.Fatal(err)
					}
					panic("continue process died before spawning worker")
				},
			})
		}()

		// Replaying the same reservation never relaunches.
		err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Retry",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("replayed interrupted continuation reservation launched a worker")
				return launchedWorker{}, nil
			},
		})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
			t.Fatalf("replay after crash = %v, want exit %d", err, ExitReservationAlreadyStarted)
		}

		// Wait reconciles the interrupted invocation to failed and releases the active claim.
		var waited bytes.Buffer
		err = Wait(WaitRequest{Root: root, InvocationID: reserved.InvocationID, Stdout: &waited, Timeout: 2 * time.Second})
		if !errors.As(err, &exitErr) || exitErr.Code != ExitTargetFailure {
			t.Fatalf("wait after crash = %v, want exit %d", err, ExitTargetFailure)
		}
		var result Result
		if err := json.Unmarshal(waited.Bytes(), &result); err != nil || result.State != dispatchStateFailed || !result.TerminationConfirmed {
			t.Fatalf("wait output after crash = %q, err=%v", waited.String(), err)
		}

		// Conversation stays continuable.
		next := reserveContinuationInTest(t, root, session.Name)
		if next.InvocationID == reserved.InvocationID {
			t.Fatalf("next reservation reused failed ID %s", reserved.InvocationID)
		}
		var launches atomic.Int32
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Reservation: &next.InvocationID, Prompt: "Next turn",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker:  fakeWorkerLauncher(&launches),
		}); err != nil {
			t.Fatalf("continue after crash recovery failed: %v", err)
		}
		if launches.Load() != 1 {
			t.Fatalf("launches = %d, want 1", launches.Load())
		}
	})

	t.Run("crash_after_run_claim_before_session_update", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-crash1", false)
		reserved := reserveContinuationInTest(t, root, session.Name)

		// Simulate crash after updating the run record to pending/claimed, before updating session.json.
		if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
			r.LaunchDigest = reservationLaunchClaim
			r.State = dispatchStatePending
			r.LauncherPID = 99999999
			r.LauncherStartIdentity = "gone"
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		// Replay never relaunches.
		err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Retry",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("replayed interrupted continuation reservation launched a worker")
				return launchedWorker{}, nil
			},
		})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
			t.Fatalf("replay after crash window 1 = %v, want exit %d", err, ExitReservationAlreadyStarted)
		}

		// Even without calling wait first, reserving a new continuation reconciles the orphan to failed
		// and lets the conversation continue cleanly.
		next := reserveContinuationInTest(t, root, session.Name)
		if next.InvocationID == reserved.InvocationID {
			t.Fatalf("reserve after crash window 1 reused %s", reserved.InvocationID)
		}
		rec, err := loadRunRecord(root, reserved.InvocationID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != dispatchStateFailed || !rec.TerminationConfirmed {
			t.Fatalf("reconciled crash window 1 record = %#v", rec)
		}
	})
}

func TestContinuationReservationEligibilityCheckedAtReserveAndLaunch(t *testing.T) {
	t.Run("running_and_unconfirmed_rejected", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		run, session := newWaitTestRun(t, root)
		handle := session.Name
		err := Reserve(ReserveOptions{Root: root, ContinueHandle: &handle})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable {
			t.Fatalf("reserve on running conversation = %v, want ExitUnavailable", err)
		}

		// Terminal but unconfirmed is also rejected.
		now := time.Now().UTC()
		if _, err := updateRunEvidence(run.Dir, func(r *RunRecord) error {
			r.State = dispatchStateCancelled
			r.CompletedAt = &now
			r.TerminalReason = terminalReasonCancelledByCaller
			r.TerminalExitCode = ExitTargetFailure
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		err = Reserve(ReserveOptions{Root: root, ContinueHandle: &handle})
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable {
			t.Fatalf("reserve on unconfirmed cancelled conversation = %v, want ExitUnavailable", err)
		}
		record, err := loadRunRecord(root, run.Record.ID)
		if err != nil || record.TerminationConfirmed || record.LaunchFenced {
			t.Fatalf("continue implicitly fenced unconfirmed cancellation: %#v, %v", record, err)
		}
	})

	t.Run("unstarted_start_reservation_rejected", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		startRes := reserveInTest(t, root)
		handle := startRes.Handle
		err := Reserve(ReserveOptions{Root: root, ContinueHandle: &handle})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable {
			t.Fatalf("reserve --continue on unstarted start reservation = %v, want ExitUnavailable", err)
		}
	})

	t.Run("rechecked_at_launch_without_claiming", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-elig", false)
		reserved := reserveContinuationInTest(t, root, session.Name)

		// Remove the completed answer so the conversation is no longer eligible at launch.
		if err := os.Remove(firstRun.Record.AnswerPath); err != nil {
			t.Fatal(err)
		}
		err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Should fail eligibility",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("ineligible continuation launched a worker")
				return launchedWorker{}, nil
			},
		})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitConfig {
			t.Fatalf("launch after missing result = %v, want ExitConfig", err)
		}
		// Reservation must remain unstarted/reserved because eligibility failed before claim.
		rec, err := loadRunRecord(root, reserved.InvocationID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != dispatchStateReserved || rec.LaunchDigest != "" {
			t.Fatalf("ineligible launch mutated reservation: %#v", rec)
		}
	})

	t.Run("claude_conversation_eligible", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, _ := completedConversationForTest(t, root, AgentClaude, "claude-sess-1", false)
		reserved := reserveContinuationInTest(t, root, session.Name)
		if reserved.State != dispatchStateReserved || reserved.Handle != session.Name {
			t.Fatalf("claude continuation reservation = %#v", reserved)
		}
	})

	t.Run("failed_without_provider_session_and_unknown_acceptance_rejected", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		session, err := reserveSession(root, run, 30*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		run.Record.State = dispatchStateFailed
		run.Record.RecoveryState = recoveryAcceptanceUnknown
		run.Record.CompletedAt = &now
		run.Record.TerminalReason = "network failure"
		run.Record.TerminalExitCode = ExitTargetFailure
		run.Record.LaunchFenced = true
		run.Record.TerminationConfirmed = true
		run.Record.TerminationConfirmedAt = &now
		run.Record.TerminationProof = terminationProofPrelaunchNoIntent
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatal(err)
		}
		session.State = sessionStatePending
		session.ProviderSessionID = ""
		session.ActiveRunID = ""
		session.ActiveClaimKnown = true
		if err := persistSession(root, session); err != nil {
			t.Fatal(err)
		}
		handle := session.Name
		err = Reserve(ReserveOptions{Root: root, ContinueHandle: &handle})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable {
			t.Fatalf("reserve on unknown-acceptance conversation = %v, want ExitUnavailable", err)
		}
	})

	t.Run("failed_with_provider_session_eligible", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		session, err := reserveSession(root, run, 30*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		run.Record.State = dispatchStateFailed
		run.Record.RecoveryState = recoveryAcceptanceUnknown
		run.Record.CompletedAt = &now
		run.Record.ProviderSessionID = "provider-sess-failed"
		run.Record.TerminalReason = "provider error"
		run.Record.TerminalExitCode = ExitTargetFailure
		run.Record.LaunchFenced = true
		run.Record.TerminationConfirmed = true
		run.Record.TerminationConfirmedAt = &now
		run.Record.TerminationProof = terminationProofPrelaunchNoIntent
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatal(err)
		}
		session.State = sessionStateDurable
		session.ProviderSessionID = "provider-sess-failed"
		session.ActiveRunID = ""
		session.ActiveClaimKnown = true
		if err := persistSession(root, session); err != nil {
			t.Fatal(err)
		}
		reserved := reserveContinuationInTest(t, root, session.Name)
		if reserved.State != dispatchStateReserved || reserved.Handle != session.Name {
			t.Fatalf("reserve on failed conversation with provider session = %#v", reserved)
		}
	})
}

func TestContinuationReservationRetrySafeFreshMode(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryRetrySafe
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "pre-start failure"
	run.Record.TerminalExitCode = ExitUnavailable
	run.Record.LaunchFenced = true
	run.Record.TerminationConfirmed = true
	run.Record.TerminationConfirmedAt = &now
	run.Record.TerminationProof = terminationProofPrelaunchNoIntent
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session.State = sessionStatePending
	session.ProviderSessionID = ""
	session.ActiveRunID = ""
	session.ActiveClaimKnown = true
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	reserved := reserveContinuationInTest(t, root, session.Name)
	var launches atomic.Int32
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Retry fresh",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(&launches),
	}); err != nil {
		t.Fatalf("continue retry-safe reservation failed: %v", err)
	}
	var req workerRequest
	if err := readJSON(filepath.Join(filepathForRun(root, reserved.InvocationID), workerRequestFile), &req); err != nil {
		t.Fatal(err)
	}
	if req.Mode != dispatchModeFresh || launches.Load() != 1 {
		t.Fatalf("retry-safe continuation request = %#v, launches=%d, want fresh mode", req, launches.Load())
	}
}

func TestContinuationReservationRejectsInvalidSelectorsAndMismatchedHandle(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session1, firstRun1 := completedConversationForTest(t, root, AgentCodex, "provider-sess-a", false)
	session2, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-b", false)
	startRes := reserveInTest(t, root)
	contRes := reserveContinuationInTest(t, root, session1.Name)

	cases := []struct {
		name     string
		handle   string
		selector string
	}{
		{"empty", "", ""},
		{"whitespace", "", "   "},
		{"non_uuid", "", "not-a-uuid"},
		{"handle_instead_of_id", "", session1.Name},
		{"unknown_uuid", "", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{"start_reservation_id", "", startRes.InvocationID},
		{"plain_run_id", "", firstRun1.Record.ID},
		{"mismatched_handle", session2.Name, contRes.InvocationID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selector := tc.selector
			err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Handle: tc.handle, Reservation: &selector, Prompt: "Work",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker: func(string, string, string) (launchedWorker, error) {
					t.Fatal("invalid selector launched a worker")
					return launchedWorker{}, nil
				},
			})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationNotFound {
				t.Fatalf("continue --reservation %q (handle=%q) = %v, want exit %d", tc.selector, tc.handle, err, ExitReservationNotFound)
			}
		})
	}

	// Matching handle + reservation ID succeeds.
	var launches atomic.Int32
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: session1.Name, Reservation: &contRes.InvocationID, Prompt: "Matching handle",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(&launches),
	}); err != nil {
		t.Fatalf("continue with matching handle + reservation failed: %v", err)
	}
	if launches.Load() != 1 {
		t.Fatalf("launches = %d, want 1", launches.Load())
	}
}

func TestPendingContinuationReservationProtectedFromRetentionPruning(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-retain", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	// Age session past retention while continuation reservation is still unexpired.
	old := time.Now().UTC().Add(-72 * time.Hour)
	session, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	session.CreatedAt = old
	session.LastUsedAt = old
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	if err := pruneDispatchEvidence(root, time.Now().UTC(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSession(root, session.Name); err != nil {
		t.Fatalf("retention pruned session with unexpired continuation reservation: %v", err)
	}
	if _, err := loadRunRecord(root, reserved.InvocationID); err != nil {
		t.Fatalf("retention pruned unexpired continuation reservation: %v", err)
	}
}

func TestOptionsAdvertiseContinuationReservationsOnCLIOnly(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	options, err := BuildOptions(OptionsRequest{
		Root: root, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.Reservations == nil || !options.Reservations.Start || !options.Reservations.Continue {
		t.Fatalf("CLI options.Reservations = %#v, want start=true continue=true", options.Reservations)
	}

	// MCP dispatch_options omits CLI-only reservations.
	session := newMCPTestSession(t, newMCPTestTools(root))
	result := callMCPTool(t, session, ToolOptions, map[string]any{})
	if result.IsError {
		t.Fatalf("dispatch_options failed: %s", toolResultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var mcpOptions OptionsResponse
	if err := json.Unmarshal(data, &mcpOptions); err != nil {
		t.Fatal(err)
	}
	if mcpOptions.Reservations != nil {
		t.Fatalf("MCP dispatch_options unexpectedly included CLI-only reservations: %#v", mcpOptions.Reservations)
	}
}

func TestContinuationReservationAllFiveHarnessesAndMuseCrashRecovery(t *testing.T) {
	harnesses := []struct {
		agent   string
		version string
	}{
		{AgentCodex, supportedProviderVersions[AgentCodex]},
		{AgentClaude, claudeLineageMinimumVersion},
		{AgentAntigravity, supportedProviderVersions[AgentAntigravity]},
		{AgentGrok, supportedProviderVersions[AgentGrok]},
		{AgentMuse, supportedProviderVersions[AgentMuse]},
	}
	for _, tc := range harnesses {
		t.Run(tc.agent, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
			rawCfg, err := os.ReadFile(cfgPath) // #nosec G304 -- test config path.
			if err != nil {
				t.Fatal(err)
			}
			enabledCfg := strings.Replace(
				strings.Replace(string(rawCfg), "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true", 1),
				"[agents.muse]\nenabled = false", "[agents.muse]\nenabled = true", 1,
			)
			if err := os.WriteFile(cfgPath, []byte(enabledCfg), 0o600); err != nil { // #nosec G703 -- test config path.
				t.Fatal(err)
			}
			providerSessionID := "sess-" + tc.agent + "-1"
			session, firstRun := completedConversationForTest(t, root, tc.agent, providerSessionID, true)
			reserved := reserveContinuationInTest(t, root, session.Name)
			if reserved.Handle != session.Name || reserved.State != dispatchStateReserved || reserved.InvocationID == firstRun.Record.ID {
				t.Fatalf("%s reserve --continue = %#v", tc.agent, reserved)
			}

			var launches atomic.Int32
			var stdout bytes.Buffer
			if err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Continue " + tc.agent,
				Stdout: &stdout, Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) {
					return tc.version, nil
				},
				launchWorker: fakeWorkerLauncher(&launches),
			}); err != nil {
				t.Fatalf("%s continue --reservation failed: %v", tc.agent, err)
			}
			if launches.Load() != 1 {
				t.Fatalf("%s launches = %d, want 1", tc.agent, launches.Load())
			}

			rec, err := loadRunRecord(root, reserved.InvocationID)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Agent != tc.agent || rec.ProviderVersion != tc.version || rec.ProviderSessionID != providerSessionID || rec.PreviousRunID != firstRun.Record.ID {
				t.Fatalf("%s launched record = %#v", tc.agent, rec)
			}
			if tc.agent == AgentClaude && rec.LineagePath == "" {
				t.Fatalf("claude continuation reservation did not populate LineagePath: %#v", rec)
			}

			var req workerRequest
			if err := readJSON(filepath.Join(filepathForRun(root, reserved.InvocationID), workerRequestFile), &req); err != nil {
				t.Fatal(err)
			}
			if req.Mode != dispatchModeResume || req.Model != "test-model" || req.Effort != "high" || !req.TargetPinned {
				t.Fatalf("%s worker request = %#v", tc.agent, req)
			}

			// Verify Muse recovery baseline accepts an interrupted reserved continuation
			// reconciled through the real orphan reconciliation path.
			if tc.agent == AgentMuse {
				if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
					r.State = dispatchStatePending
					r.SupervisorPID = 0
					r.SupervisorStartIdentity = ""
					r.LauncherPID = 99999999
					r.LauncherStartIdentity = "gone"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				rec, err := loadRunRecord(root, reserved.InvocationID)
				if err != nil {
					t.Fatal(err)
				}
				reconciled, err := tryReconcileOrphan(root, rec)
				if err != nil {
					t.Fatal(err)
				}
				if reconciled.State != dispatchStateFailed || !reconciled.TerminationConfirmed {
					t.Fatalf("reconciled muse continuation = %#v", reconciled)
				}
				baseline, err := resolveMuseRecoveryBaseline(root, reserved.InvocationID, &museRecoveryBaseline{
					SessionID:  providerSessionID,
					ViewCursor: "cursor-1",
				})
				if err != nil || baseline.ViewCursor != "cursor-1" {
					t.Fatalf("resolveMuseRecoveryBaseline after interrupted reserved continuation = %#v, %v", baseline, err)
				}
			}
		})
	}
}

func TestCancelledWorkerCannotClobberNewerContinuationReservation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-clobber", false)
	reserved := reserveContinuationInTest(t, root, session.Name)
	session, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a stale claim in the mapping so only the reservation guard
	// blocks a write from the termination-confirmed predecessor.
	session.ActiveRunID = firstRun.Record.ID
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	_, err = persistOwnedSession(root, session.Name, firstRun.Record.ID, func(s *Session) {
		s.Model = "stale-worker-model"
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable {
		t.Fatalf("stale worker persistOwnedSession = %v, want ExitUnavailable", err)
	}
	reloaded, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ContinuationReservationID != reserved.InvocationID || reloaded.Model != "test-model" {
		t.Fatalf("stale worker clobbered session: %#v", reloaded)
	}
}

func TestFreshModeWorkerCrashBeforeProviderIntentStaysContinuable(t *testing.T) {
	for _, agent := range []string{AgentCodex, AgentClaude} {
		t.Run(agent, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			firstRun, err := newDispatchRun(root, agent, supportedProviderVersions[agent], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			session, err := reserveSession(root, firstRun, 30*24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			firstRun.Record.State = dispatchStateFailed
			firstRun.Record.RecoveryState = recoveryRetrySafe
			firstRun.Record.CompletedAt = &now
			firstRun.Record.TerminalReason = "pre-start error"
			firstRun.Record.TerminalExitCode = ExitUnavailable
			firstRun.Record.LaunchFenced = true
			firstRun.Record.TerminationConfirmed = true
			firstRun.Record.TerminationConfirmedAt = &now
			firstRun.Record.TerminationProof = terminationProofPrelaunchNoIntent
			if err := writeRunRecord(firstRun.Dir, &firstRun.Record); err != nil {
				t.Fatal(err)
			}
			session.State = sessionStatePending
			session.ProviderSessionID = ""
			session.ActiveRunID = ""
			session.ActiveClaimKnown = true
			if err := persistSession(root, session); err != nil {
				t.Fatal(err)
			}

			reserved := reserveContinuationInTest(t, root, session.Name)
			if err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Retry fresh",
				Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[agent], nil },
				launchWorker:  fakeWorkerLauncher(nil),
			}); err != nil {
				t.Fatal(err)
			}

			// Simulate a worker that had already published SupervisorPID (and for
			// Claude, a provisional caller-assigned ProviderSessionID) and died
			// before writing ProviderLaunchIntent.
			if agent == AgentClaude {
				if _, err := persistOwnedSession(root, session.Name, reserved.InvocationID, func(s *Session) {
					s.ProviderSessionID = "provisional-claude-id"
					s.State = sessionStateDurable
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
				r.SupervisorPID = 99999999
				r.SupervisorStartIdentity = "gone"
				r.LauncherPID = 99999998
				r.LauncherStartIdentity = "gone"
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			// Even without calling wait first, reserve --continue reconciles the
			// dead worker, clears any provisional ID, and reserves a fresh turn.
			next := reserveContinuationInTest(t, root, session.Name)
			if next.InvocationID == reserved.InvocationID {
				t.Fatalf("reserve after dead fresh worker reused %s", reserved.InvocationID)
			}
			reconciled, err := loadRunRecord(root, reserved.InvocationID)
			if err != nil {
				t.Fatal(err)
			}
			if reconciled.State != dispatchStateFailed || reconciled.RecoveryState != recoveryRetrySafe || !reconciled.TerminationConfirmed {
				t.Fatalf("reconciled fresh worker record = %#v", reconciled)
			}
			reloadedSession, err := loadSession(root, session.Name)
			if err != nil {
				t.Fatal(err)
			}
			if reloadedSession.ProviderSessionID != "" || reloadedSession.State != sessionStatePending {
				t.Fatalf("provisional provider session ID was not downgraded: %#v", reloadedSession)
			}
		})
	}
}

func TestContinuationReservationIdempotentRetrySurvivesIneligibility(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-idem", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	// Remove the prior answer file after reserving; a retry of reserve --continue
	// must still return the existing pending reservation ID instead of stranding
	// the conversation.
	if err := os.Remove(firstRun.Record.AnswerPath); err != nil {
		t.Fatal(err)
	}
	retry := reserveContinuationInTest(t, root, session.Name)
	if retry.InvocationID != reserved.InvocationID {
		t.Fatalf("retry reserve --continue = %#v, want existing %#v", retry, reserved)
	}
}

func TestContinuationReservationPostSessionClaimCrashContinuableWithoutWait(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-nowait", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	func() {
		defer func() { _ = recover() }()
		_ = Continue(ContinueOptions{
			Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Crash",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			launchWorker: func(_ string, runID string, _ string) (launchedWorker, error) {
				rec, err := loadRunRecord(root, runID)
				if err != nil {
					t.Fatal(err)
				}
				rec.LauncherPID = 99999999
				rec.LauncherStartIdentity = "gone"
				if err := writeRunRecord(filepathForRun(root, runID), &rec); err != nil {
					t.Fatal(err)
				}
				panic("crash before worker launch")
			},
		})
	}()

	// Immediately calling reserve --continue without calling wait first reconciles
	// the dead post-session-claim run and returns a fresh reservation.
	next := reserveContinuationInTest(t, root, session.Name)
	if next.InvocationID == reserved.InvocationID {
		t.Fatalf("reserve after post-session-claim crash reused %s", reserved.InvocationID)
	}
	rec, err := loadRunRecord(root, reserved.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != dispatchStateFailed || !rec.TerminationConfirmed {
		t.Fatalf("reconciled post-session-claim crash record = %#v", rec)
	}
}

func TestContinuationReservationMissingRunRecordSelfHeals(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-missing", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	runDir := filepathForRun(root, reserved.InvocationID)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	next := reserveContinuationInTest(t, root, session.Name)
	if next.InvocationID == reserved.InvocationID || next.State != dispatchStateReserved {
		t.Fatalf("reserve after deleted pending run dir = %#v", next)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settling an absent run recreated %s: %v", runDir, err)
	}
}

func TestContinuationReservationKeepsSelectedAgedConversation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchSessionRetentionDays: 1})
	selected, _ := completedConversationForTest(t, root, AgentCodex, "selected", false)
	other, _ := completedConversationForTest(t, root, AgentCodex, "other", false)
	for _, session := range []Session{selected, other} {
		session.LastUsedAt = time.Now().UTC().Add(-48 * time.Hour)
		if err := persistSession(root, session); err != nil {
			t.Fatal(err)
		}
	}
	if reserved := reserveContinuationInTest(t, root, selected.Name); reserved.Handle != selected.Name {
		t.Fatalf("reserve returned another conversation: %#v", reserved)
	}
	if _, err := loadSession(root, other.Name); !errors.Is(err, errDispatchSessionNotFound) {
		t.Fatalf("unrelated aged conversation survived pruning: %v", err)
	}
}

func TestMissingContinuationReservationDoesNotPreventPruning(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "dangling", false)
	now := time.Now().UTC()
	session.LastUsedAt = now.Add(-48 * time.Hour)
	session.ContinuationReservationID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, now, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSession(root, session.Name); !errors.Is(err, errDispatchSessionNotFound) {
		t.Fatalf("missing reservation prevented pruning: %v", err)
	}
}

func TestUnreadableContinuationReservationPreventsPruningAndSettlement(t *testing.T) {
	for _, corrupt := range []string{"malformed ID", "dangling symlink"} {
		t.Run(corrupt, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			session, _ := completedConversationForTest(t, root, AgentCodex, "unreadable", false)
			reserved := reserveContinuationInTest(t, root, session.Name)
			session.ContinuationReservationID = reserved.InvocationID
			if corrupt == "malformed ID" {
				session.ContinuationReservationID = "../invalid"
			} else {
				path := filepath.Join(filepathForRun(root, reserved.InvocationID), dispatchRunFile)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing-dispatch.json", path); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Now().UTC()
			session.LastUsedAt = now.Add(-48 * time.Hour)
			if err := persistSession(root, session); err != nil {
				t.Fatal(err)
			}
			if err := pruneDispatchEvidence(root, now, 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			retained, active, unreadable, err := classifyDispatchPoolOccupancy(root)
			if err != nil || retained != 0 || active != 0 || unreadable != 1 {
				t.Fatalf("corrupt reservation occupancy = %d retained, %d active, %d unreadable, %v; want 0, 0, 1, nil", retained, active, unreadable, err)
			}
			err = Reserve(ReserveOptions{Root: root, ContinueHandle: &session.Name})
			requireDispatchExitCode(t, err, ExitConfig)
			if corrupt == "malformed ID" {
				path, pathErr := sessionPath(root, session.Name)
				if pathErr != nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("malformed reservation error %q does not name the mapping %q: %v", err, path, pathErr)
				}
			}
			requireDispatchExitCode(t, Continue(ContinueOptions{
				Root: root, Handle: session.Name, Prompt: "second turn", Env: []string{},
			}), ExitConfig)
			mapping, err := loadSession(root, session.Name)
			if err != nil || mapping.ContinuationReservationID != session.ContinuationReservationID {
				t.Fatalf("unreadable reservation mapping was lost: %#v, %v", mapping, err)
			}
		})
	}
}

func TestContinuationReservationRetirementSurvivesPruningBeforeSlotCleanup(t *testing.T) {
	for _, outcome := range []string{"cancelled", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			session, _ := completedConversationForTest(t, root, AgentCodex, "retired", false)
			reserved := reserveContinuationInTest(t, root, session.Name)
			retiredAt := time.Now().UTC().Add(time.Hour)
			session, err := loadSession(root, session.Name)
			if err != nil {
				t.Fatal(err)
			}
			session.LastUsedAt = retiredAt.Add(-48 * time.Hour)
			if err := persistSession(root, session); err != nil {
				t.Fatal(err)
			}
			dir := filepathForRun(root, reserved.InvocationID)
			var record RunRecord
			if outcome == "cancelled" {
				if _, _, _, err := beginCancellation(root, reserved.InvocationID); err != nil {
					t.Fatal(err)
				}
				record, err = persistConfirmedTermination(dir)
			} else {
				record, err = updateRunEvidence(dir, func(r *RunRecord) error {
					r.ReservationExpiresAt = &retiredAt
					expireReservation(r, retiredAt)
					return nil
				})
			}
			if err != nil || !record.TerminationConfirmed {
				t.Fatalf("retire reservation: %#v, %v", record, err)
			}
			if err := pruneDispatchEvidence(root, retiredAt, 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			stale, err := loadSession(root, session.Name)
			if err != nil || stale.ContinuationReservationID != reserved.InvocationID {
				t.Fatalf("pruned conversation with outstanding retirement: %#v, %v", stale, err)
			}
			if err := releaseIfConfirmed(root, record); err != nil {
				t.Fatal(err)
			}
			released, err := loadSession(root, session.Name)
			if err != nil || released.ContinuationReservationID != "" || !released.LastUsedAt.Equal(*record.TerminationConfirmedAt) {
				t.Fatalf("release did not use recorded retirement time: %#v, %v", released, err)
			}
			next := reserveContinuationInTest(t, root, session.Name)
			if next.InvocationID == reserved.InvocationID {
				t.Fatalf("reused retired reservation: %#v", next)
			}
		})
	}
}

func TestContinuationRepairsProvisionalSessionAfterTerminalWrite(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		mode := "ordinary"
		if reserved {
			mode = "reserved"
		}
		t.Run(mode, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			session, err := reserveSession(root, run, testDispatchSessionRetention)
			if err != nil {
				t.Fatal(err)
			}
			session.State = sessionStateDurable
			session.ProviderSessionID = "provisional-id"
			if err := persistSession(root, session); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			run.Record.State = dispatchStateFailed
			run.Record.RecoveryState = recoveryRetrySafe
			run.Record.CompletedAt = &now
			run.Record.LaunchFenced = true
			if reserved {
				// Simulate both writes finishing before mapping cleanup.
				applyTerminationConfirmation(&run.Record, now)
			}
			if err := writeRunRecord(run.Dir, &run.Record); err != nil {
				t.Fatal(err)
			}
			// For ordinary continue, simulate a crash after the terminal
			// write but before recording confirmation of the fenced run.
			var stdout bytes.Buffer
			opts := ContinueOptions{
				Root: root, WorkDir: root, Handle: session.Name, Prompt: "Retry without provider ID",
				Stdout: &stdout, Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentClaude], nil },
				launchWorker:  fakeWorkerLauncher(nil),
			}
			if reserved {
				reservation := reserveContinuationInTest(t, root, session.Name)
				opts.Reservation = &reservation.InvocationID
			}
			if err := Continue(opts); err != nil {
				t.Fatal(err)
			}
			var result Result
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			var request workerRequest
			if err := readJSON(filepath.Join(filepathForRun(root, result.InvocationID), workerRequestFile), &request); err != nil {
				t.Fatal(err)
			}
			mapping, err := loadSession(root, session.Name)
			if err != nil || mapping.ProviderSessionID != "" || mapping.State != sessionStatePending || request.Mode != dispatchModeFresh {
				t.Fatalf("continued with a provisional provider ID: %#v, mode %s, err %v", mapping, request.Mode, err)
			}
			previous, err := loadRunRecord(root, run.Record.ID)
			if err != nil || !previous.TerminationConfirmed || previous.State != dispatchStateFailed {
				t.Fatalf("predecessor remains unreconciled: %#v, %v", previous, err)
			}
		})
	}
}

func TestContinuationReservationExpiresDuringClaimPreparation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, predecessor := completedConversationForTest(t, root, AgentCodex, "expiring", false)
	reserved := reserveContinuationInTest(t, root, session.Name)
	synctest.Test(t, func(t *testing.T) {
		deadline := time.Now().UTC().Add(time.Second)
		if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
			r.ReservationExpiresAt = &deadline
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		record, _, claimed, err := claimContinuationReservation(root, session.Name, reserved.InvocationID, func(r *RunRecord, _ Session, _ RunRecord) {
			time.Sleep(time.Until(deadline))
			r.ProviderVersion = "launch metadata"
		}, writeJSONAtomic)
		if err != nil || claimed || record.LaunchDigest != "" || record.ProviderVersion != "" {
			t.Fatalf("expired reservation recorded a claim: %#v, claimed %v, err %v", record, claimed, err)
		}
		requireDispatchExitCode(t, reportClaimedContinuationReservation(record), ExitReservationExpired)
		mapping, err := loadSession(root, session.Name)
		if err != nil || mapping.ContinuationReservationID != "" || mapping.RunID != predecessor.Record.ID {
			t.Fatalf("expiry changed the current invocation: %#v, %v", mapping, err)
		}
	})
}

func TestContinuationReservationMappingWriteFailureIsRetrySafe(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, predecessor := completedConversationForTest(t, root, AgentCodex, "inherited", false)
	reserved := reserveContinuationInTest(t, root, session.Name)
	competing := make(chan error, 1)
	result, _, claimed, err := claimContinuationReservation(root, session.Name, reserved.InvocationID, func(r *RunRecord, mapping Session, _ RunRecord) {
		r.ProviderSessionID = mapping.ProviderSessionID
	}, func(string, any) error {
		go func() {
			competing <- Reserve(ReserveOptions{Root: root, ContinueHandle: &session.Name})
		}()
		return syscall.ENOSPC
	})
	if !errors.Is(err, syscall.ENOSPC) || !claimed || result.State != dispatchStateFailed || !result.TerminationConfirmed {
		t.Fatalf("expected a terminal claim with the mapping save error: %#v, %v, %v", result, claimed, err)
	}
	if err := <-competing; err != nil {
		t.Fatalf("competing reserve could not recover the failed claim: %v", err)
	}
	record, err := loadRunRecord(root, reserved.InvocationID)
	if err != nil || record.State != dispatchStateFailed || record.RecoveryState != recoveryRetrySafe ||
		!record.TerminationConfirmed || record.ProviderSessionID != session.ProviderSessionID ||
		!strings.Contains(record.TerminalReason, "claim reserved continuation mapping") {
		t.Fatalf("mapping save failure recorded incorrect evidence: %#v, %v", record, err)
	}
	requireDispatchExitCode(t, reportClaimedContinuationReservation(record), ExitReservationAlreadyStarted)
	next := reserveContinuationInTest(t, root, session.Name)
	record, err = loadRunRecord(root, next.InvocationID)
	if err != nil || next.InvocationID == reserved.InvocationID || record.PreviousRunID != predecessor.Record.ID {
		t.Fatalf("could not reserve again from the predecessor: %#v, %v", record, err)
	}
}

func TestContinuationReservationReplayWithDeletedPromptFileReportsStatus(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-pfile", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	missingPromptFile := filepath.Join(t.TempDir(), "deleted-prompt.txt")
	if err := Cancel(CancelRequest{Root: root, InvocationID: reserved.InvocationID}); err != nil {
		t.Fatal(err)
	}
	err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved.InvocationID, PromptFile: missingPromptFile,
		Env: []string{}, LookPath: alwaysFound,
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationCancelled {
		t.Fatalf("cancelled replay with missing prompt file = %v, want exit %d", err, ExitReservationCancelled)
	}

	// Same for already started.
	reserved2 := reserveContinuationInTest(t, root, session.Name)
	if err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved2.InvocationID, Prompt: "Launch once",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  fakeWorkerLauncher(nil),
	}); err != nil {
		t.Fatal(err)
	}
	err = Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved2.InvocationID, PromptFile: missingPromptFile,
		Env: []string{}, LookPath: alwaysFound,
	})
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
		t.Fatalf("already-started replay with missing prompt file = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}
}

func TestCancelContinuationReservationPreservesAgedConversation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchSessionRetentionDays: 1})
	session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-aged", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	// Age session past 1-day retention while 7-day reservation is pending.
	old := time.Now().UTC().Add(-48 * time.Hour)
	loaded, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	loaded.CreatedAt = old
	loaded.LastUsedAt = old
	if err := persistSession(root, loaded); err != nil {
		t.Fatal(err)
	}

	if err := Cancel(CancelRequest{Root: root, InvocationID: reserved.InvocationID}); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, time.Now(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	fresh := reserveContinuationInTest(t, root, session.Name)
	if fresh.InvocationID == reserved.InvocationID || fresh.Handle != session.Name {
		t.Fatalf("reserve after cancelling aged conversation = %#v", fresh)
	}
}

func TestContinuationReservationFailBeforePublicationRollsBackAndBlocksReplay(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-failpub", false)
	reserved := reserveContinuationInTest(t, root, session.Name)

	err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Fail launcher",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker: func(string, string, string) (launchedWorker, error) {
			return launchedWorker{}, errors.New("boom launcher")
		},
	})
	if err == nil {
		t.Fatal("expected launcher failure")
	}

	// Session rolled back to firstRun and released active claim; replay still exits 82.
	reloaded, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RunID != firstRun.Record.ID || reloaded.ActiveRunID != "" {
		t.Fatalf("session after failBeforePublication = %#v", reloaded)
	}
	err = Continue(ContinueOptions{
		Root: root, WorkDir: root, Reservation: &reserved.InvocationID, Prompt: "Replay",
		Env: []string{}, LookPath: alwaysFound,
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationAlreadyStarted {
		t.Fatalf("replay after failBeforePublication = %v, want exit %d", err, ExitReservationAlreadyStarted)
	}
	// Conversation stays continuable.
	next := reserveContinuationInTest(t, root, session.Name)
	if next.InvocationID == reserved.InvocationID {
		t.Fatalf("next reserve reused failed ID %s", reserved.InvocationID)
	}
}

func TestOrdinaryContinueRejectsStalePredecessorAndMidPrepReservation(t *testing.T) {
	t.Run("mid_prep_reservation", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, firstRun := completedConversationForTest(t, root, AgentCodex, "provider-sess-race1", false)
		var reserved Result
		err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: session.Name, Prompt: "Ordinary",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			beforeProvider: func(context.Context, string) (func(), error) {
				// Simulate a continuation reservation landing after pre-check and before claimConversation.
				reserved = reserveContinuationInTest(t, root, session.Name)
				return func() {}, nil
			},
			launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("ordinary continue launched despite mid-prep reservation")
				return launchedWorker{}, nil
			},
		})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable || !strings.Contains(err.Error(), reserved.InvocationID) {
			t.Fatalf("ordinary continue racing reserve = %v, want ExitUnavailable mentioning %s", err, reserved.InvocationID)
		}
		reloaded, err := loadSession(root, session.Name)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.ContinuationReservationID != reserved.InvocationID || reloaded.RunID != firstRun.Record.ID {
			t.Fatalf("session after racing ordinary continue = %#v", reloaded)
		}
	})

	t.Run("stale_predecessor", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		session, _ := completedConversationForTest(t, root, AgentCodex, "provider-sess-race2", false)
		err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: session.Name, Prompt: "Stale ordinary",
			Env: []string{}, LookPath: alwaysFound,
			VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
			beforeProvider: func(context.Context, string) (func(), error) {
				// Complete a reserved continuation after pre-check and before claimConversation.
				res := reserveContinuationInTest(t, root, session.Name)
				if err := Continue(ContinueOptions{
					Root: root, WorkDir: root, Reservation: &res.InvocationID, Prompt: "Reserved turn",
					Env: []string{}, LookPath: alwaysFound,
					VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
					launchWorker:  fakeWorkerLauncher(nil),
				}); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				if err := os.WriteFile(filepath.Join(filepathForRun(root, res.InvocationID), "result.md"), []byte("turn 2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				completed, err := updateRunEvidence(filepathForRun(root, res.InvocationID), func(r *RunRecord) error {
					r.State = dispatchStateCompleted
					r.RecoveryState = recoveryResumeRequired
					r.CompletedAt = &now
					r.LaunchFenced = true
					r.TerminationConfirmed = true
					r.TerminationConfirmedAt = &now
					r.TerminationProof = terminationProofPrelaunchNoIntent
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := releaseConversation(root, completed); err != nil {
					t.Fatal(err)
				}
				return func() {}, nil
			},
			launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("stale ordinary continue launched")
				return launchedWorker{}, nil
			},
		})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable || !strings.Contains(err.Error(), "advanced to invocation") {
			t.Fatalf("stale predecessor continue = %v, want ExitUnavailable about advanced invocation", err)
		}
	})
}
