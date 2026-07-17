package agentdispatch

import (
	"bytes"
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

func TestResumeValidatesVersionPromptAndSkillBeforeLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session := Session{Name: "short-bright-transistor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID}
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	err := Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"resume"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(string, string) (string, error) { return "0.1.0", nil }})
	requireDispatchExitCode(t, err, ExitUnavailable)
	err = Resume(ResumeOptions{Root: root, Name: session.Name, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }})
	requireDispatchExitCode(t, err, ExitUsage)
	err = Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"resume"}, Skill: "missing", Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }})
	requireDispatchExitCode(t, err, ExitConfig)
}

func TestRunUsesConfiguredDefaultAndHonorsQuietOutput(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, "instruction_token_threshold = 50000", "instruction_token_threshold = 1")
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"answer"}\n'`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
		Stdout:     &stdout,
		Stderr:     &stderr,
		Quiet:      true,
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("quiet Run: %v", err)
	}
	if stdout.String() != "answer" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "instructions") {
		t.Fatalf("quiet stderr leaked warnings: %q", stderr.String())
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

func TestPreLaunchCancellationIsReleasedOnlyByOwningExecution(t *testing.T) {
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

	var launches atomic.Int32
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = launchExecution(dispatchExecution{
		Root: root, Project: project, Target: targetMeta{Name: AgentCodex},
		Run: run, Session: session, Mode: dispatchModeFresh,
		NewCommand: func(string, ...string) *exec.Cmd {
			launches.Add(1)
			return exec.Command("/bin/sh", "-c", "exit 0") // #nosec G204 -- fixed test command must remain unlaunched.
		},
	}).await()
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if launches.Load() != 0 {
		t.Fatalf("provider launches after pre-launch cancellation = %d, want 0", launches.Load())
	}
	finalized, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ActiveRunID != "" {
		t.Fatalf("owning execution did not release pre-launch cancellation: %#v", finalized)
	}
}

func TestDeleteProtectsRunningRecordWithUnprovableOwnership(t *testing.T) {
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
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, session.Name); err == nil {
		t.Fatal("Delete removed a mapping whose run may still be live")
	} else {
		requireDispatchExitCode(t, err, ExitUnavailable)
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
