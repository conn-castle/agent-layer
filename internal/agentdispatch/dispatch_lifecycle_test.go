package agentdispatch

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
)

func TestContinueValidatesPromptAndVersionBeforeLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	_, session := terminalConversationForAsyncTest(t, root)
	err := Continue(ContinueOptions{Root: root, Handle: session.Name, Env: []string{}, LookPath: alwaysFound})
	requireDispatchExitCode(t, err, ExitUsage)
	err = Continue(ContinueOptions{Root: root, Handle: session.Name, Prompt: "Continue", Env: []string{}, LookPath: alwaysFound, VersionLookup: func(string, string) (string, error) { return "0.1.0", nil }})
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestRejectedResumeExposesTerminalPublicationFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	active, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	active.Record.Name = "short-bright-transistor"
	if err := writeRunRecord(active.Dir, &active.Record); err != nil {
		t.Fatal(err)
	}
	session := Session{
		Name: active.Record.Name, Agent: AgentCodex, State: sessionStateDurable,
		ProviderSessionID: runtimeSessionID, RunID: active.Record.ID,
		ActiveRunID: active.Record.ID, ActiveClaimKnown: true,
	}
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	attempted, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	attempted.Record.Name = session.Name
	attempted.Record.PreviousRunID = active.Record.ID
	if err := writeRunRecord(attempted.Dir, &attempted.Record); err != nil {
		t.Fatal(err)
	}
	_, claimErr := claimConversation(root, session.Name, attempted.Record.ID)
	if claimErr == nil {
		t.Fatal("claim against an active run unexpectedly succeeded")
	}
	publicationErr := errors.New("injected rejected-resume publication failure")
	err = finishRejectedResume(attempted, claimErr, func(string, *RunRecord) error {
		return publicationErr
	})
	requireDispatchExitCode(t, err, ExitConfig)
	if !errors.Is(err, publicationErr) || !strings.Contains(err.Error(), "already active in run "+active.Record.ID) {
		t.Fatalf("rejected resume error = %v, want claim rejection and publication failure", err)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained != session {
		t.Fatalf("publication failure mutated active mapping: before = %#v, after = %#v", session, retained)
	}
	durable, err := loadRunRecord(root, attempted.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durable.State != dispatchStatePending {
		t.Fatalf("failed terminal publication changed attempted record: %#v", durable)
	}
}

func TestExecuteDispatchPreservesFailedFreshRunForRecoveryHistory(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = finishDispatchFailure(dispatchExecution{Root: root, Project: project, Run: run, Session: session, Mode: "fresh"}, exitError(ExitTargetFailure, "failed"))
	requireDispatchExitCode(t, err, ExitTargetFailure)
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("failed fresh run lost its history mapping: %v", err)
	}
	if retained.ActiveRunID != "" || retained.RunID != run.Record.ID {
		t.Fatalf("failed fresh mapping = %#v", retained)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load failed record: %v", err)
	}
	if record.State != dispatchStateFailed || record.RecoveryState != recoveryAcceptanceUnknown || record.TerminalReason != "failed" {
		t.Fatalf("failed record = %#v", record)
	}
}

func TestUnprovenProviderTerminationRetainsRunAndActiveClaim(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	cause := &unprovenProviderTerminationError{err: exitError(ExitTargetFailure, "provider process group death was not proven")}
	if err := finishDispatchFailure(dispatchExecution{Root: root, Run: run, Session: session, Mode: dispatchModeFresh}, cause); !errors.Is(err, cause) {
		t.Fatalf("finishDispatchFailure error = %v, want unproven termination failure", err)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != run.Record.ID {
		t.Fatalf("unproven termination released active claim: %#v", retained)
	}
	durable, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durable.State != dispatchStateRunning || durable.CompletedAt != nil || durable.TerminalReason != "" {
		t.Fatalf("unproven termination terminalized run evidence: %#v", durable)
	}
}

func TestFailureFinalizationReleasesClaimWhenTerminalWriteFails(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	conflicting := run.Record
	conflicting.Revision += 5
	if err := writeJSONAtomic(filepath.Join(run.Dir, dispatchRunFile), conflicting); err != nil {
		t.Fatalf("force revision conflict: %v", err)
	}
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = finishDispatchFailure(dispatchExecution{Root: root, Project: project, Run: run, Session: session, Mode: dispatchModeFresh}, exitError(ExitTargetFailure, "provider failed"))
	requireDispatchExitCode(t, err, ExitUnavailable)
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != "" {
		t.Fatalf("terminal-write failure left the claim stuck: %#v", retained)
	}
}

func TestFailedRunRecordPublicationPreservesCallerRevisionForFailureFinalization(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	durableBefore, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load durable record before injected failure: %v", err)
	}

	run.Record.State = dispatchStateStarting
	publicationErr := errors.New("injected run-record publication failure")
	err = writeRunRecordWithPublisher(run.Dir, &run.Record, func(string, any) error {
		return publicationErr
	})
	if !errors.Is(err, publicationErr) {
		t.Fatalf("writeRunRecordWithPublisher error = %v, want injected failure", err)
	}
	if run.Record.Revision != durableBefore.Revision || !run.Record.UpdatedAt.Equal(durableBefore.UpdatedAt) {
		t.Fatalf("failed publication advanced caller state: caller revision/time = %d/%s, durable = %d/%s", run.Record.Revision, run.Record.UpdatedAt, durableBefore.Revision, durableBefore.UpdatedAt)
	}
	durableAfter, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load durable record after injected failure: %v", err)
	}
	if durableAfter.Revision != run.Record.Revision || !durableAfter.UpdatedAt.Equal(run.Record.UpdatedAt) {
		t.Fatalf("caller and durable state diverged after failed publication: caller = %#v, durable = %#v", run.Record, durableAfter)
	}

	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	cause := exitError(ExitTargetFailure, "provider failed after publication error")
	err = finishDispatchFailure(dispatchExecution{Root: root, Project: project, Run: run, Session: session, Mode: dispatchModeFresh}, cause)
	requireDispatchExitCode(t, err, ExitTargetFailure)
	terminal, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load terminal record: %v", err)
	}
	if terminal.State != dispatchStateFailed || terminal.CompletedAt == nil || terminal.TerminalReason != cause.Error() {
		t.Fatalf("failure finalization did not persist canonical terminal history: %#v", terminal)
	}
	if terminal.Revision != durableBefore.Revision+1 || run.Record.Revision != terminal.Revision || !run.Record.UpdatedAt.Equal(terminal.UpdatedAt) {
		t.Fatalf("terminal caller/durable state mismatch: caller = %#v, durable = %#v", run.Record, terminal)
	}
}

func TestPreLaunchCancellationAllowsSafeReplacementWithoutProcessIdentity(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	if err := Cancel(CancelRequest{Root: root, ID: run.Record.ID}); err != nil {
		t.Fatalf("Cancel pending run: %v", err)
	}
	claimed, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ActiveRunID != run.Record.ID {
		t.Fatalf("pre-launch cancellation released before its owner ran: %#v", claimed)
	}
	replacement, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := claimConversation(root, session.Name, replacement.Record.ID)
	if err != nil {
		t.Fatalf("replace never-launched cancelled claim: %v", err)
	}
	if replaced.ActiveRunID != replacement.Record.ID || replaced.RunID != replacement.Record.ID {
		t.Fatalf("replacement claim was not published: %#v", replaced)
	}

	var launches atomic.Int32
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = executeDispatch(dispatchExecution{
		Root: root, Project: project, Target: targetMeta{Name: AgentCodex},
		Run: run, Session: session, Mode: dispatchModeFresh,
		NewCommand: func(string, ...string) *exec.Cmd {
			launches.Add(1)
			return exec.Command("/bin/sh", "-c", "exit 0") // #nosec G204 -- fixed test command must remain unlaunched.
		},
	})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if launches.Load() != 0 {
		t.Fatalf("provider launches after pre-launch cancellation = %d, want 0", launches.Load())
	}
	finalized, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ActiveRunID != replacement.Record.ID || finalized.RunID != replacement.Record.ID {
		t.Fatalf("old owner finalization released replacement claim: %#v", finalized)
	}
}

func TestNeverLaunchedCancelledClaimRecoveryOperations(t *testing.T) {
	setup := func(t *testing.T) (string, *dispatchRun, Session) {
		t.Helper()
		root := t.TempDir()
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatalf("new run: %v", err)
		}
		session, err := reserveSession(root, run)
		if err != nil {
			t.Fatalf("reserve session: %v", err)
		}
		if err := Cancel(CancelRequest{Root: root, ID: run.Record.ID}); err != nil {
			t.Fatalf("cancel pending run: %v", err)
		}
		return root, run, session
	}

	t.Run("orphan reconciliation releases stale claim", func(t *testing.T) {
		root, run, session := setup(t)
		record, err := loadRunRecord(root, run.Record.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileOrphan(root, record); err != nil {
			t.Fatalf("reconcile cancelled run: %v", err)
		}
		reconciled, err := loadSession(root, session.Name)
		if err != nil {
			t.Fatal(err)
		}
		if reconciled.ActiveRunID != "" {
			t.Fatalf("reconciliation retained never-launched claim: %#v", reconciled)
		}
	})

}

func TestCancellationRevisionRaceIsFinalizedByOwningExecution(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}

	var cancelErr error
	var cancelled atomic.Bool
	var launches atomic.Int32
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = executeDispatch(dispatchExecution{
		Root: root, Project: project, Target: targetMeta{Name: AgentCodex},
		Version: supportedProviderVersions[AgentCodex], Run: run, Session: session, Mode: dispatchModeFresh,
		Stderr: dispatchTestWriter(func(data []byte) (int, error) {
			if cancelled.CompareAndSwap(false, true) {
				cancelErr = Cancel(CancelRequest{Root: root, ID: run.Record.ID})
			}
			return len(data), nil
		}),
		NewCommand: func(string, ...string) *exec.Cmd {
			launches.Add(1)
			return exec.Command("/bin/sh", "-c", "exit 0") // #nosec G204 -- fixed test command must remain unlaunched.
		},
	})
	if cancelErr != nil {
		t.Fatalf("Cancel during identity publication: %v", cancelErr)
	}
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if launches.Load() != 0 {
		t.Fatalf("provider launches after cancellation revision race = %d, want 0", launches.Load())
	}
	finalized, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ActiveRunID != "" {
		t.Fatalf("cancellation revision race left the owner claim stuck: %#v", finalized)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateCancelled {
		t.Fatalf("cancellation revision race lost terminal evidence: %#v", record)
	}
}

type dispatchTestWriter func([]byte) (int, error)

func (write dispatchTestWriter) Write(data []byte) (int, error) { return write(data) }

func TestClaimReplacementBlockedByCompatibilityOwnerWithUnprovableOwnership(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.PID = os.Getpid()
	run.Record.ProcessStartIdentity = ""
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	// Compatibility mappings created before explicit active claims use RunID
	// as their only owner reference.
	session.ActiveRunID = ""
	session.ActiveClaimKnown = false
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	beforeClaim, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claimConversation(root, session.Name, replacement.Record.ID); err == nil {
		t.Fatal("replacement bypassed compatibility owner evidence")
	} else {
		requireDispatchExitCode(t, err, ExitUnavailable)
	}
	afterClaim, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if afterClaim != beforeClaim {
		t.Fatalf("blocked compatibility claim mutated mapping: before = %#v, after = %#v", beforeClaim, afterClaim)
	}
}

func TestPreStartFailureDowngradesUnstartedDurableMapping(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	session.ProviderSessionID = runtimeSessionID
	session.State = sessionStateDurable
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist durable mapping: %v", err)
	}
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentClaude)}
	cause := &preStartFailure{err: exitError(ExitTargetFailure, "provider never started")}
	if err := finishDispatchFailure(dispatchExecution{Root: root, Project: project, Run: run, Session: session, Mode: dispatchModeFresh}, cause); err == nil {
		t.Fatal("finishDispatchFailure hid the cause")
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("pre-start failure lost its history mapping: %v", err)
	}
	if retained.State == sessionStateDurable || retained.ProviderSessionID != "" {
		t.Fatalf("mapping still advertises an uncreated provider session: %#v", retained)
	}
	if retained.RunID != run.Record.ID {
		t.Fatalf("mapping lost run history: %#v", retained)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateFailed || record.RecoveryState != recoveryRetrySafe {
		t.Fatalf("pre-start failure record = %#v", record)
	}
}

func TestDispatchInputAndEnvironmentContracts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project, stderr, env, depth, err := loadDispatchProject(root, nil, []string{clients.EnvDispatchActive + "=2"})
	if err != nil || stderr != io.Discard || depth != 2 || len(env) != 1 {
		t.Fatalf("loadDispatchProject = %#v, %v, %#v, %d, %v", project, stderr, env, depth, err)
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		t.Fatalf("depth two should be allowed: %v", err)
	}
	if err := checkDispatchDepth(project.Config, 3); err == nil {
		t.Fatal("max depth was accepted")
	} else {
		requireDispatchExitCode(t, err, ExitNested)
	}
	if _, _, _, _, err := loadDispatchProject(root, nil, []string{clients.EnvDispatchActive + "=invalid"}); err == nil {
		t.Fatal("invalid depth was accepted")
	} else {
		requireDispatchExitCode(t, err, ExitNested)
	}
	if err := writeIdentity(failingWriter{}, "tiny-round-capacitor", AgentCodex, "fresh", false); err == nil {
		t.Fatal("writeIdentity hid a writer failure")
	}
}
