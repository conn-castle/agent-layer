package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartAndContinueExposeImmutableInvocationIDs(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	first, session := terminalConversationForAsyncTest(t, root)
	if first.Record.ID == "" {
		t.Fatal("first invocation ID is empty")
	}
	var stdout bytes.Buffer
	err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: session.Name, Prompt: "next",
		Stdout: &stdout, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker: func(string, string, string) (launchedWorker, error) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = read.Close() })
			return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var started Result
	if err := json.Unmarshal(stdout.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Handle != session.Name || started.InvocationID == "" || started.InvocationID == first.Record.ID {
		t.Fatalf("continue result = %#v, first = %s", started, first.Record.ID)
	}
	old, err := loadRunRecord(root, first.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.ID != first.Record.ID || old.State != dispatchStateCompleted {
		t.Fatalf("old invocation mutated: %#v", old)
	}
	current, err := loadRunRecord(root, started.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if current.PreviousRunID != first.Record.ID {
		t.Fatalf("continued invocation = %#v", current)
	}
}

func TestExactInvocationSelectorDoesNotFollowContinuation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	first, session := terminalConversationForAsyncTest(t, root)
	second, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	second.Record.Name = session.Name
	second.Record.PreviousRunID = first.Record.ID
	second.Record.State = dispatchStateRunning
	second.Record.RecoveryState = recoveryAcceptanceUnknown
	if err := writeRunRecord(second.Dir, &second.Record); err != nil {
		t.Fatal(err)
	}
	session.RunID = second.Record.ID
	session.ActiveRunID = second.Record.ID
	session.ActiveClaimKnown = true
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	var byOldID bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, ID: first.Record.ID, Stdout: &byOldID}); err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(byOldID.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.InvocationID != first.Record.ID || inspected.State != dispatchStateCompleted {
		t.Fatalf("old invocation inspect = %#v", inspected)
	}

	var byHandle bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, Handle: session.Name, Stdout: &byHandle}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(byHandle.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.InvocationID != second.Record.ID {
		t.Fatalf("handle inspect followed the wrong invocation: %#v", inspected)
	}

	err = Inspect(InspectRequest{Root: root, Handle: session.Name, InvocationID: first.Record.ID, Stdout: bytes.NewBuffer(nil)})
	requireDispatchExitCode(t, err, ExitUsage)
}

func TestInvocationSelectorErrorIsSharedAcrossIDHandleAndInvocationID(t *testing.T) {
	cases := []InspectRequest{
		{},
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Handle: "tiny-round-capacitor"},
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", InvocationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"},
		{Handle: "tiny-round-capacitor", InvocationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{ID: "id", Handle: "handle", InvocationID: "invocation"},
	}
	for _, request := range cases {
		err := Inspect(request)
		exitErr := requireDispatchExitError(t, err, ExitUsage)
		if !strings.Contains(exitErr.Error(), "exactly one invocation selector") {
			t.Fatalf("selector %#v error %q does not match every call site", request, exitErr)
		}
	}
}

func TestCancelledUnconfirmedDoesNotReleaseClaim(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateStarting
	run.Record.ProviderLaunchIntent = true
	run.Record.LauncherPID = 1
	run.Record.LauncherStartIdentity = "stale-launcher"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Cancel(CancelRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != dispatchStateCancelled || result.TerminationConfirmed || result.InvocationID != run.Record.ID || result.Error != "" {
		t.Fatalf("cancel result = %#v", result)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.TerminationConfirmed || record.TerminationObservation != terminationObservationUncertainLaunch {
		t.Fatalf("uncertain launch was confirmed: %#v", record)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != run.Record.ID {
		t.Fatalf("unconfirmed cancel released the claim: %#v", retained)
	}
}

func TestLegacyRecordWithoutLaunchProtocolStaysUnconfirmed(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.LaunchProtocol = ""
	run.Record.State = dispatchStateCancelled
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	run.Record.TerminalExitCode = ExitTargetFailure
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	updated, err := persistTerminationEvidence(run.Dir, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TerminationConfirmed {
		t.Fatalf("legacy record gained prelaunch proof: %#v", updated)
	}
}

func TestInspectOmitsErrorForCancelledInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCancelled
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result InspectResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != dispatchStateCancelled || result.Error != "" {
		t.Fatalf("cancelled inspect = %#v", result)
	}
}

func TestInspectReportsErrorForFailedInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "provider failed"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result InspectResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != dispatchStateFailed || result.Error != "provider failed" {
		t.Fatalf("failed inspect = %#v", result)
	}
}

func TestInspectReturnsOnlyPublicInvocationState(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	run.Record.LaunchFenced = true
	run.Record.TerminationObservation = terminationObservationGroupLive
	run.Record.TerminationAttemptError = "private failure"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result InspectResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.InvocationID != run.Record.ID || !result.TerminationConfirmed || result.TerminationConfirmedAt == nil {
		t.Fatalf("inspect state = %#v", result)
	}
	if bytes.Contains(stdout.Bytes(), []byte("termination_observation")) ||
		bytes.Contains(stdout.Bytes(), []byte("termination_attempt_error")) ||
		bytes.Contains(stdout.Bytes(), []byte("evidence")) {
		t.Fatalf("inspect exposed private evidence: %s", stdout.Bytes())
	}
}

func TestOutputReturnsPartialEventsOnFailedRun(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "provider failed"
	run.Record.TerminalExitCode = ExitTargetFailure
	run.Record.LaunchFenced = true
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.Record.EventsPath, []byte(`{"kind":"answer","answer":"partial"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result OutputResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "partial") {
		t.Fatalf("failed-run events = %#v", result)
	}
	stdout.Reset()
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactFinalAnswer, Stdout: &stdout}); err == nil {
		t.Fatal("failed run returned a final answer")
	}
}

func TestOutputReadsLegacyEventsWithoutLaunchIntent(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	if run.Record.ProviderLaunchIntent {
		t.Fatal("legacy fixture already had launch intent")
	}
	if err := os.WriteFile(run.Record.EventsPath, []byte(`{"kind":"answer","answer":"legacy"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result OutputResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "legacy") {
		t.Fatalf("legacy events = %#v", result)
	}
}

func TestOutputRejectsMissingEventsWithoutLaunchIntent(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: bytes.NewBuffer(nil)})
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestOutputRejectsInvalidArtifactBeforeSelectorLookup(t *testing.T) {
	for _, artifact := range []string{"", "stdout", "FINAL_ANSWER"} {
		err := Output(OutputRequest{ID: "missing-handle", Artifact: artifact, Stdout: bytes.NewBuffer(nil)})
		exitErr := requireDispatchExitError(t, err, ExitUsage)
		if !strings.Contains(exitErr.Error(), "final_answer or events") {
			t.Fatalf("artifact %q error = %v", artifact, err)
		}
		if strings.Contains(exitErr.Error(), "was not found") {
			t.Fatalf("artifact %q looked up the selector: %v", artifact, err)
		}
	}
}

func TestOutputBoundsTextWithoutPagination(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	payload := append(bytes.Repeat([]byte("x"), maxOutputReadBytes-1), []byte("é")...)
	if err := os.WriteFile(run.Record.AnswerPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	run.Record.LaunchFenced = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactFinalAnswer, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result OutputResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Content) != maxOutputReadBytes-1 {
		t.Fatalf("bounded output = %#v", result)
	}
}

func TestWaitConditionTimeoutReportsConditionMetFalse(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Wait(WaitRequest{
		Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
		Timeout: time.Millisecond, PollInterval: time.Millisecond, Stdout: &stdout,
	}); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ConditionMet == nil || *result.ConditionMet || result.TerminationConfirmed {
		t.Fatalf("timeout wait = %#v", result)
	}
}

func TestDefaultExpiryRemovesOnlyConfirmedEligibleRecords(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	now := time.Now().UTC()
	old := now.Add(-dispatchSessionRetention - time.Hour)
	expired, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	expired.Record.State = dispatchStateCompleted
	expired.Record.RecoveryState = recoveryResumeRequired
	expired.Record.CompletedAt = &old
	expired.Record.LaunchFenced = true
	expired.Record.TerminationConfirmed = true
	expired.Record.TerminationConfirmedAt = &old
	if err := writeJSONAtomic(filepath.Join(expired.Dir, dispatchRunFile), expired.Record); err != nil {
		t.Fatal(err)
	}
	uncertain, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	uncertain.Record.State = dispatchStateCancelled
	uncertain.Record.RecoveryState = recoveryAcceptanceUnknown
	uncertain.Record.CompletedAt = &old
	uncertain.Record.ProviderLaunchIntent = true
	if err := writeJSONAtomic(filepath.Join(uncertain.Dir, dispatchRunFile), uncertain.Record); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired.Dir); !os.IsNotExist(err) {
		t.Fatalf("confirmed expired evidence remains: %v", err)
	}
	if _, err := os.Stat(uncertain.Dir); err != nil {
		t.Fatalf("uncertain evidence was pruned: %v", err)
	}
}

func TestFailedKillPersistsAttemptAndAllowsRetry(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	proc := execSleepingProcessGroup(t)
	run.Record.State = dispatchStateRunning
	run.Record.PID = proc.Pid
	run.Record.ProcessGroupID = proc.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(proc.Pid)
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(proc.Pid, 0); err != nil {
		t.Fatalf("test process is not alive: %v", err)
	}
	record, err := persistTerminationEvidence(run.Dir, syscall.EPERM, false)
	if err != nil {
		t.Fatal(err)
	}
	if record.TerminationConfirmed || record.TerminationAttemptError == "" {
		t.Fatalf("failed kill evidence = %#v", record)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != run.Record.ID {
		t.Fatalf("failed kill released the claim: %#v", retained)
	}
}

func TestDeadSupervisorWithLiveProcessGroupStaysUnconfirmed(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	proc := execSleepingProcessGroup(t)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = 1
	run.Record.SupervisorStartIdentity = "dead-supervisor"
	run.Record.PID = 1
	run.Record.ProcessGroupID = proc.Pid
	run.Record.ProcessStartIdentity = "dead-provider"
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	record, err := reconcileOrphan(root, run.Record)
	if err != nil {
		t.Fatal(err)
	}
	if record.TerminationConfirmed || record.State != dispatchStateRunning {
		t.Fatalf("live process group was confirmed or terminalized: %#v", record)
	}
	if record.TerminationObservation != terminationObservationGroupLive || record.TerminationAttemptError == "" {
		t.Fatalf("live group observation was not persisted: %#v", record)
	}
}

func TestOutputPreservesUTF8Text(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	payload := []byte("éx")
	if err := os.WriteFile(run.Record.EventsPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result OutputResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != string(payload) || result.Truncated {
		t.Fatalf("UTF-8 output = %#v", result)
	}
}

func TestOutputResolvesRelativeArtifactPathInsideRun(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	const content = "relative output"
	if err := os.WriteFile(run.Record.EventsPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	run.Record.EventsPath = filepath.Base(run.Record.EventsPath)
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result OutputResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != content {
		t.Fatalf("relative output = %#v", result)
	}
}

func TestOutputRejectsSymlinkArtifacts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(run.Dir, "events.link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	run.Record.EventsPath = link
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Stdout: &stdout})
	requireDispatchExitCode(t, err, ExitConfig)
	if stdout.Len() != 0 {
		t.Fatalf("failed output wrote a result: %s", stdout.Bytes())
	}
}

func TestCompletedEmptyAnswerPathIsRetrievalFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	run.Record.LaunchFenced = true
	run.Record.AnswerPath = ""
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactFinalAnswer, Stdout: &stdout})
	requireDispatchExitCode(t, err, ExitConfig)
	if stdout.Len() != 0 {
		t.Fatalf("failed output wrote a result: %s", stdout.Bytes())
	}
}

func TestWaitRespectsTimeoutWhileRunLockHeld(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withRunLock(run.Dir, func() error {
			close(started)
			time.Sleep(2 * time.Second)
			return nil
		})
	}()
	<-started
	var stdout bytes.Buffer
	began := time.Now()
	if err := Wait(WaitRequest{
		Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
		Timeout: 150 * time.Millisecond, PollInterval: 20 * time.Millisecond, Stdout: &stdout,
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("wait blocked on run lock for %s", elapsed)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ConditionMet == nil || *result.ConditionMet {
		t.Fatalf("busy-lock wait = %#v", result)
	}
	<-done
}

func TestWaitRespectsTimeoutAndConfirmsWithoutSessionLock(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCancelled
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	run.Record.TerminalExitCode = ExitTargetFailure
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withSessionLock(root, session.Name, func() error {
			close(started)
			time.Sleep(2 * time.Second)
			return nil
		})
	}()
	<-started
	var stdout bytes.Buffer
	began := time.Now()
	if err := Wait(WaitRequest{
		Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
		Timeout: 150 * time.Millisecond, PollInterval: 20 * time.Millisecond, Stdout: &stdout,
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("wait blocked on session lock for %s", elapsed)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ConditionMet == nil || !*result.ConditionMet || !result.TerminationConfirmed {
		t.Fatalf("session-lock wait = %#v", result)
	}
	<-done
}

func TestInspectSurfacesRunLockOpenError(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	lockPath := filepath.Join(run.Dir, ".record.lock")
	_ = os.Remove(lockPath)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: bytes.NewBuffer(nil)})
	if err == nil {
		t.Fatal("inspect ignored a run-lock open error")
	}
}

func TestInspectReturnsPromptlyWhenRunLockHeld(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withRunLock(run.Dir, func() error {
			close(started)
			time.Sleep(500 * time.Millisecond)
			return nil
		})
	}()
	<-started
	var stdout bytes.Buffer
	began := time.Now()
	if err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > 200*time.Millisecond {
		t.Fatalf("inspect blocked on run lock for %s", elapsed)
	}
	<-done
}

func TestWaitRespectsCancellationWhileRunLockHeld(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withRunLock(run.Dir, func() error {
			close(started)
			time.Sleep(2 * time.Second)
			return nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- Wait(WaitRequest{
			Context: ctx, Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
			Timeout: 8 * time.Second, PollInterval: 20 * time.Millisecond, Stdout: bytes.NewBuffer(nil),
		})
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-waitDone:
		if !errorsIsContext(err) {
			t.Fatalf("busy-lock wait cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait ignored context cancellation while the run lock was held")
	}
	<-done
}

func TestLegacyFenceDoesNotConfirmWhileSubmitterLives(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	reaped := exec.Command("true")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.LaunchProtocol = ""
	run.Record.LaunchFenced = true
	run.Record.State = dispatchStateCancelled
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	run.Record.TerminalExitCode = ExitTargetFailure
	run.Record.PID = reaped.Process.Pid
	run.Record.ProcessGroupID = reaped.Process.Pid
	run.Record.ProcessStartIdentity = "gone-provider"
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeJSONAtomic(filepath.Join(run.Dir, dispatchRunFile), run.Record); err != nil {
		t.Fatal(err)
	}
	updated, err := persistTerminationEvidence(run.Dir, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TerminationConfirmed {
		t.Fatalf("legacy LaunchFenced confirmed while a live submitter remains: %#v", updated)
	}
}

func TestLaunchCrashWindowWithLiveProcessDoesNotConfirm(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	proc := execSleepingProcessGroup(t)
	run.Record.State = dispatchStateStarting
	run.Record.ProviderLaunchIntent = true
	run.Record.LauncherPID = 1
	run.Record.LauncherStartIdentity = "dead-launcher"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Wait(WaitRequest{
		Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
		Timeout: 80 * time.Millisecond, PollInterval: 10 * time.Millisecond, Stdout: &stdout,
	}); err != nil {
		t.Fatal(err)
	}
	var waited Result
	if err := json.Unmarshal(stdout.Bytes(), &waited); err != nil {
		t.Fatal(err)
	}
	if waited.TerminationConfirmed || (waited.ConditionMet != nil && *waited.ConditionMet) {
		t.Fatalf("wait confirmed a crash-window launch: %#v", waited)
	}
	stdout.Reset()
	if err := Inspect(InspectRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(stdout.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.TerminationConfirmed {
		t.Fatalf("inspect confirmed a crash-window launch: %#v", inspected)
	}
	if bytes.Contains(stdout.Bytes(), []byte("termination_observation")) || bytes.Contains(stdout.Bytes(), []byte("termination_proof")) {
		t.Fatalf("inspect exposed private termination evidence: %s", stdout.Bytes())
	}
	if err := syscall.Kill(proc.Pid, 0); err != nil {
		t.Fatalf("unrecorded provider process exited during wait/inspect: %v", err)
	}
}

func TestCancelRetriesFailedUnconfirmedOwnedExecution(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	cmd := exec.Command("/bin/sleep", "60")
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
		}
	})
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "provider process group death was not proven"
	run.Record.TerminalExitCode = ExitTargetFailure
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	run.Record.TerminationAttemptError = "terminate dispatch provider process group"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Cancel(CancelRequest{Root: root, ID: run.Record.ID, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != dispatchStateFailed || !result.TerminationConfirmed {
		t.Fatalf("failed-unconfirmed cancel = %#v", result)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateFailed || record.TerminationProof == "" {
		t.Fatalf("retry did not preserve failed proof: %#v", record)
	}
	released, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if released.ActiveRunID == run.Record.ID && !record.TerminationConfirmed {
		t.Fatalf("unconfirmed failed retry released nothing: %#v", released)
	}
	err = Cancel(CancelRequest{Root: root, ID: run.Record.ID, Stdout: bytes.NewBuffer(nil)})
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestTerminationProofSurvivesReload(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCancelled
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	run.Record.TerminalExitCode = ExitTargetFailure
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if _, err := persistConfirmedTermination(run.Dir); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.TerminationConfirmed || reloaded.TerminationProof != terminationProofPrelaunchNoIntent {
		t.Fatalf("reloaded proof = %#v", reloaded)
	}
}

func TestLaunchIntentWithoutIdentityStaysUncertainAcrossWait(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateStarting
	run.Record.ProviderLaunchIntent = true
	run.Record.LauncherPID = 1
	run.Record.LauncherStartIdentity = "dead-launcher"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var stdout bytes.Buffer
	err := Wait(WaitRequest{
		Context: ctx, Root: root, ID: run.Record.ID, Condition: waitConditionTerminationConfirmed,
		Timeout: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond, Stdout: &stdout,
	})
	if err != nil && !errorsIsContext(err) {
		var result Result
		if json.Unmarshal(stdout.Bytes(), &result) == nil && result.ConditionMet != nil && !*result.ConditionMet {
			return
		}
		t.Fatalf("uncertain launch wait err=%v out=%s", err, stdout.Bytes())
	}
	if stdout.Len() > 0 {
		var result Result
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr == nil && result.TerminationConfirmed {
			t.Fatalf("wait confirmed an uncertain launch: %#v", result)
		}
	}
}

func TestMCPInspectAndOutputUseInvocationID(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	if err := os.WriteFile(run.Record.EventsPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, newMCPTestTools(root))
	inspectCall := callMCPTool(t, session, ToolInspect, SelectorInput{InvocationID: run.Record.ID})
	if inspectCall.IsError {
		t.Fatalf("dispatch_inspect failed: %s", toolResultText(inspectCall))
	}
	inspectJSON, err := json.Marshal(inspectCall.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(inspectJSON, &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.InvocationID != run.Record.ID {
		t.Fatalf("mcp inspect = %#v", inspected)
	}
	if bytes.Contains(inspectJSON, []byte("launch_protocol")) {
		t.Fatalf("mcp inspect exposed launch protocol: %s", inspectJSON)
	}
	outputCall := callMCPTool(t, session, ToolOutput, OutputInput{InvocationID: run.Record.ID, Artifact: artifactEvents})
	if outputCall.IsError {
		t.Fatalf("dispatch_output failed: %s", toolResultText(outputCall))
	}
	outputJSON, err := json.Marshal(outputCall.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output OutputResult
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		t.Fatal(err)
	}
	if output.InvocationID != run.Record.ID || output.Content != "hello" {
		t.Fatalf("mcp output = %#v", output)
	}
	both := callMCPTool(t, session, ToolInspect, SelectorInput{Handle: "tiny-round-capacitor", InvocationID: run.Record.ID})
	if !both.IsError {
		t.Fatal("mcp inspect accepted an ambiguous selector")
	}
}

func TestMCPCancelRetriesFailedUnconfirmedOwnedExecution(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	cmd := exec.Command("/bin/sleep", "60")
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
		}
	})
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "provider process group death was not proven"
	run.Record.TerminalExitCode = ExitTargetFailure
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	run.Record.TerminationAttemptError = "terminate dispatch provider process group"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, newMCPTestTools(root))
	call := callMCPTool(t, session, ToolCancel, SelectorInput{InvocationID: run.Record.ID})
	if call.IsError {
		t.Fatalf("dispatch_cancel retry failed: %s", toolResultText(call))
	}
	result := decodeToolResult(t, call)
	if result.State != dispatchStateFailed || !result.TerminationConfirmed {
		t.Fatalf("mcp failed-unconfirmed cancel = %#v", result)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateFailed || record.TerminationProof == "" {
		t.Fatalf("mcp retry did not preserve failed proof: %#v", record)
	}
}

func errorsIsContext(err error) bool {
	return err == context.DeadlineExceeded || err == context.Canceled
}

func execSleepingProcessGroup(t *testing.T) *os.Process {
	t.Helper()
	proc, err := os.StartProcess("/bin/sleep", []string{"sleep", "60"}, &os.ProcAttr{
		Dir:   t.TempDir(),
		Files: []*os.File{nil, nil, nil},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
		_, _ = proc.Wait()
	})
	return proc
}
