package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestParseFanoutTargetRequiresSelfContainedUniqueFields(t *testing.T) {
	target, err := ParseFanoutTarget("agent=codex,model=gpt-5,reasoning=high")
	if err != nil || target.Agent != AgentCodex || target.Model != "gpt-5" || target.ReasoningEffort != "high" {
		t.Fatalf("target = %#v, %v", target, err)
	}
	for _, value := range []string{"model=x", "agent=unknown", "agent=codex,agent=claude", "agent=codex,prompt=x"} {
		if _, err := ParseFanoutTarget(value); err == nil {
			t.Fatalf("accepted invalid target %q", value)
		}
	}
}

func TestCapabilityCacheInvalidatesWhenBinaryIdentityChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "codex")
	logPath := filepath.Join(t.TempDir(), "calls")
	write := func(marker string) {
		script := "#!/bin/sh\nprintf '%s\\n' " + supportedProviderVersions[AgentCodex] + "\nprintf '" + marker + "\\n' >> " + logPath + "\n"
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test provider must be executable.
			t.Fatal(err)
		}
	}
	target, _ := lookupTarget(AgentCodex)
	write("first")
	if _, _, err := compatibleTargetVersionCached(root, path, target, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := compatibleTargetVersionCached(root, path, target, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath) // #nosec G304 -- test-owned path.
	if err != nil || strings.Count(string(data), "first") != 1 {
		t.Fatalf("cache did not reuse identity: %q, %v", data, err)
	}
	time.Sleep(time.Millisecond)
	write("second-marker")
	if _, _, err := compatibleTargetVersionCached(root, path, target, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(logPath) // #nosec G304 -- test-owned path.
	if err != nil || !strings.Contains(string(data), "second-marker") {
		t.Fatalf("cache did not invalidate changed binary: %q, %v", data, err)
	}
}

func TestCapabilityCacheToleratesNullEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nprintf '%s\\n' " + supportedProviderVersions[AgentCodex] + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test provider must be executable.
		t.Fatal(err)
	}
	cachePath := capabilityCachePath(root)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(`{"entries":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentCodex)
	if _, version, err := compatibleTargetVersionCached(root, path, target, nil); err != nil || version != supportedProviderVersions[AgentCodex] {
		t.Fatalf("null-entries cache broke resolution: %q, %v", version, err)
	}
}

func TestFanoutPrepFailureFinalizesPreparedChildren(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	prepared := []preparedFanoutChild{{request: dispatchExecution{Root: root, Run: run, Session: session}}}

	var warnings bytes.Buffer
	failPreparedFanoutChildren(root, prepared, &warnings, errDispatchRunNotFound, writeRunRecord)
	if warnings.Len() != 0 {
		t.Fatalf("finalization warned unexpectedly: %q", warnings.String())
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateFailed || record.RecoveryState != recoveryRetrySafe || record.CompletedAt == nil {
		t.Fatalf("prepared child not finalized: %#v", record)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ActiveRunID != "" {
		t.Fatalf("prep failure retained active claim: %#v", retained)
	}
	if err := Delete(root, session.Name); err != nil {
		t.Fatalf("Delete after prep failure: %v", err)
	}
}

func TestFanoutPrepPublicationFailureStillReleasesPreparedClaim(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	preparationErr := errors.New("injected fanout preparation failure")
	publicationErr := errors.New("injected prepared-child terminal publication failure")
	state := defaultFanoutCoordinatorState()
	var pendingWrites atomic.Int32
	state.writePreparedRunRecord = func(dir string, record *RunRecord) error {
		if record.State == dispatchStateFailed {
			return publicationErr
		}
		if pendingWrites.Add(1) == 2 {
			return preparationErr
		}
		return writeRunRecord(dir, record)
	}
	var launches atomic.Int32
	var warnings bytes.Buffer

	err := fanout(FanoutOptions{
		Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}},
		PromptArgs: []string{"shared"}, Stderr: &warnings, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
		NewCommand: func(string, ...string) *exec.Cmd {
			launches.Add(1)
			return exec.Command("/bin/sh", "-c", "exit 0") // #nosec G204 -- fixed test command must remain unlaunched.
		},
	}, state)
	if !errors.Is(err, preparationErr) {
		t.Fatalf("Fanout error = %v, want preparation failure", err)
	}
	if launches.Load() != 0 {
		t.Fatalf("provider launches = %d, want 0", launches.Load())
	}
	if !strings.Contains(warnings.String(), publicationErr.Error()) {
		t.Fatalf("rollback warnings = %q, want publication failure", warnings.String())
	}

	runEntries, err := os.ReadDir(dispatchRunPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(runEntries) != 2 {
		t.Fatalf("run count = %d, want 2", len(runEntries))
	}
	var preparedRecord RunRecord
	for _, entry := range runEntries {
		record, loadErr := loadRunRecord(root, entry.Name())
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if record.Name != "" {
			preparedRecord = record
		}
	}
	if preparedRecord.ID == "" {
		t.Fatalf("prepared child record not found in %#v", runEntries)
	}
	if preparedRecord.State != dispatchStatePending {
		t.Fatalf("failed terminal publication changed durable record: %#v", preparedRecord)
	}
	session, err := loadSession(root, preparedRecord.Name)
	if err != nil {
		t.Fatal(err)
	}
	if session.ActiveRunID != "" {
		t.Fatalf("terminal publication failure retained prepared claim: %#v", session)
	}
}

func TestRetentionPrunesOnlyExpiredTerminalFanoutManifests(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	old := now.Add(-dispatchSessionRetention - time.Hour)
	makeManifest := func(state string, completedAt *time.Time) string {
		id, err := newUUID()
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFanoutManifest(root, FanoutManifest{ID: id, State: state, CreatedAt: old, CompletedAt: completedAt, Children: []FanoutChild{}}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	expired := makeManifest(dispatchStateCompleted, &old)
	recent := makeManifest(dispatchStateFailed, &now)
	running := makeManifest(dispatchStateRunning, nil)
	corruptID := "44444444-4444-4444-8444-444444444444"
	corruptPath := fanoutPath(root, corruptID)
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corruptPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := pruneDispatchEvidence(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fanoutPath(root, expired)); !os.IsNotExist(err) {
		t.Fatalf("expired terminal fanout manifest remains: %v", err)
	}
	for _, id := range []string{recent, running, corruptID} {
		if _, err := os.Stat(fanoutPath(root, id)); err != nil {
			t.Fatalf("retention removed preserved fanout manifest %s: %v", id, err)
		}
	}
}

func TestFanoutWaitsForEveryChildAndEmitsIndependentResults(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var calls atomic.Int32
	command := func(string, ...string) *exec.Cmd {
		call := calls.Add(1)
		if call == 1 {
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"first"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; sleep 0.05; printf '{"type":"thread.started","thread_id":"22222222-2222-4222-8222-222222222222"}\n{"type":"agent_message","message":"second"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}
	var stdout bytes.Buffer
	err := Fanout(FanoutOptions{
		Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}},
		PromptArgs: []string{"shared"}, Stdout: &stdout, Env: []string{},
		LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command,
	})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	var manifest FanoutManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if manifest.State != dispatchStateCompleted || len(manifest.Children) != 2 || manifest.Children[0].RunID == manifest.Children[1].RunID {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, child := range manifest.Children {
		data, err := os.ReadFile(child.ResultPath)
		if err != nil || (string(data) != "first" && string(data) != "second") {
			t.Fatalf("child result = %q, %v", data, err)
		}
	}
}

func TestFanoutPartialFailurePreservesCompleteTerminalEvidence(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var calls atomic.Int32
	command := func(string, ...string) *exec.Cmd {
		if calls.Add(1) == 1 {
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; exit 2`) // #nosec G204 -- fixed test command.
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; sleep 0.05; printf '{"type":"thread.started","thread_id":"22222222-2222-4222-8222-222222222222"}\n{"type":"agent_message","message":"survived"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}
	var stdout bytes.Buffer
	err := Fanout(FanoutOptions{Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}}, PromptArgs: []string{"shared"}, Stdout: &stdout, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	var manifest FanoutManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.State != dispatchStateFailed || len(manifest.Children) != 2 {
		t.Fatalf("partial failure manifest = %#v", manifest)
	}
	states := map[string]bool{}
	for _, child := range manifest.Children {
		states[child.Status] = true
	}
	if !states[dispatchStateFailed] || !states[dispatchStateCompleted] {
		t.Fatalf("fanout did not preserve all terminal states: %#v", manifest.Children)
	}
}

func TestFanoutCancellationReleasesEachClaimAfterGroupDeath(t *testing.T) {
	root := t.TempDir()
	manifestID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	manifest := FanoutManifest{ID: manifestID, State: dispatchStateRunning, CreatedAt: time.Now().UTC()}
	type liveChild struct {
		run     *dispatchRun
		session Session
		cmd     *exec.Cmd
		wait    chan error
	}
	children := make([]liveChild, 0, 2)
	defer func() {
		for _, child := range children {
			if processAlive(child.cmd.Process.Pid) == processStatusAlive {
				_ = syscall.Kill(-child.cmd.Process.Pid, syscall.SIGKILL)
			}
			select {
			case <-child.wait:
			default:
			}
		}
	}()
	for index := 0; index < 2; index++ {
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		session, err := reserveSession(root, run)
		if err != nil {
			t.Fatal(err)
		}
		readyPath := filepath.Join(t.TempDir(), "ready")
		cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; touch "$1"; while :; do sleep 1; done`, "sh", readyPath) // #nosec G204 -- test-owned path passed as a positional argument.
		prepareProviderProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		wait := make(chan error, 1)
		go func() { wait <- cmd.Wait() }()
		children = append(children, liveChild{run: run, session: session, cmd: cmd, wait: wait})
		waitForFanoutTestPath(t, readyPath)
		run.Record.State = dispatchStateRunning
		run.Record.RecoveryState = recoveryAcceptanceUnknown
		run.Record.PID = cmd.Process.Pid
		run.Record.ProcessGroupID = cmd.Process.Pid
		run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatal(err)
		}
		manifest.Children = append(manifest.Children, FanoutChild{RunID: run.Record.ID, Name: session.Name, Status: dispatchStateRunning})
	}
	if err := writeFanoutManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	if err := cancelFanout(root, manifest.ID); err != nil {
		t.Fatalf("cancelFanout: %v", err)
	}
	terminalManifest, err := loadFanoutManifest(root, manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if terminalManifest.State != dispatchStateCancelled || terminalManifest.CompletedAt == nil {
		t.Fatalf("cancelled fanout manifest = %#v", terminalManifest)
	}
	for _, child := range children {
		if err := <-child.wait; err == nil {
			t.Fatal("automatically SIGKILLed fanout child exited successfully")
		}
		record, err := loadRunRecord(root, child.run.Record.ID)
		if err != nil {
			t.Fatal(err)
		}
		retained, err := loadSession(root, child.session.Name)
		if err != nil {
			t.Fatal(err)
		}
		if record.State != dispatchStateCancelled || processOwnership(record) != ownershipDead || retained.ActiveRunID != "" {
			t.Fatalf("fanout cancellation did not release a dead child owner: record = %#v, session = %#v", record, retained)
		}
	}
}

func TestFanoutManifestWritePreservesCancelledChildEvidence(t *testing.T) {
	root := t.TempDir()
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	children := []FanoutChild{
		{RunID: "11111111-1111-4111-8111-111111111111", Name: "small-bright-resistor", Status: dispatchStateCancelled},
		{RunID: "22222222-2222-4222-8222-222222222222", Name: "large-steady-relay", Status: dispatchStateCancelled},
	}
	if err := writeFanoutManifest(root, FanoutManifest{ID: id, State: dispatchStateCancelled, CreatedAt: now, CompletedAt: &now, Children: children}); err != nil {
		t.Fatal(err)
	}
	stale := FanoutManifest{ID: id, State: dispatchStateCancelled, CreatedAt: now, CompletedAt: &now, Children: append([]FanoutChild(nil), children...)}
	stale.Children[0].Status = dispatchStateFailed
	stale.Children[0].Error = "owning execution returned"
	stale.Children[1].Status = dispatchStatePending
	if err := writeFanoutManifest(root, stale); err != nil {
		t.Fatal(err)
	}
	published, err := loadFanoutManifest(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if published.Children[0].Status != dispatchStateFailed || published.Children[1].Status != dispatchStateCancelled {
		t.Fatalf("stale coordinator write regressed terminal child evidence: %#v", published.Children)
	}
}

func TestFanoutDrainsChildrenAfterPostLaunchCoordinatorFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	gatePath := filepath.Join(t.TempDir(), "coordinator-failed")
	siblingDonePath := filepath.Join(t.TempDir(), "sibling-done")
	var calls atomic.Int32
	command := func(string, ...string) *exec.Cmd {
		if calls.Add(1) == 1 {
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"first"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; while [ ! -f "$1" ]; do sleep 0.01; done; touch "$2"; printf '{"type":"thread.started","thread_id":"22222222-2222-4222-8222-222222222222"}\n{"type":"agent_message","message":"second"}\n{"type":"turn.completed"}\n'`, "sh", gatePath, siblingDonePath) // #nosec G204 -- test-owned paths passed as positional arguments.
	}

	injectedErr := errors.New("injected post-launch coordinator load failure")
	coordinatorState := defaultFanoutCoordinatorState()
	var coordinatorLoads atomic.Int32
	coordinatorState.loadRunRecord = func(root string, id string) (RunRecord, error) {
		if coordinatorLoads.Add(1) == 1 {
			if _, err := os.Stat(siblingDonePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("delayed sibling completed before injected coordinator failure: %v", err)
			}
			if err := os.WriteFile(gatePath, []byte("failed"), 0o600); err != nil {
				t.Fatalf("release delayed sibling: %v", err)
			}
			return RunRecord{}, injectedErr
		}
		return loadRunRecord(root, id)
	}

	err := fanout(FanoutOptions{
		Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}},
		PromptArgs: []string{"shared"}, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
		NewCommand:    command,
	}, coordinatorState)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("Fanout error = %v, want original coordinator failure", err)
	}
	if _, err := os.Stat(siblingDonePath); err != nil {
		t.Fatalf("Fanout returned before delayed sibling completed: %v", err)
	}

	runEntries, err := os.ReadDir(dispatchRunPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(runEntries) != 2 {
		t.Fatalf("run count = %d, want 2", len(runEntries))
	}
	for _, entry := range runEntries {
		record, err := loadRunRecord(root, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !terminalDispatchState(record.State) || record.CompletedAt == nil {
			t.Fatalf("child did not terminalize: %#v", record)
		}
		if processOwned(record) {
			t.Fatalf("provider remains active after Fanout returned: %#v", record)
		}
		session, err := loadSession(root, record.Name)
		if err != nil {
			t.Fatal(err)
		}
		if session.ActiveRunID != "" {
			t.Fatalf("child claim remains active: %#v", session)
		}
	}

	manifestEntries, err := os.ReadDir(fanoutStateRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifestEntries) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(manifestEntries))
	}
	manifest, err := loadFanoutManifest(root, manifestEntries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State != dispatchStateFailed || manifest.Error != injectedErr.Error() || manifest.CompletedAt == nil {
		t.Fatalf("aggregate coordinator failure evidence = %#v", manifest)
	}
	for _, child := range manifest.Children {
		if child.Status != dispatchStateCompleted {
			t.Fatalf("aggregate omitted terminal child evidence: %#v", manifest.Children)
		}
	}
}

func TestFanoutRecoversDurableEvidenceAfterFinalManifestWriteFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var calls atomic.Int32
	command := func(string, ...string) *exec.Cmd {
		if calls.Add(1) == 1 {
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"first"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"22222222-2222-4222-8222-222222222222"}\n{"type":"agent_message","message":"second"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}

	injectedErr := errors.New("injected final manifest publication failure")
	coordinatorState := defaultFanoutCoordinatorState()
	var failedTerminalWrite atomic.Bool
	coordinatorState.writeManifest = func(root string, manifest FanoutManifest) error {
		if manifest.CompletedAt != nil && manifest.State == dispatchStateCompleted && failedTerminalWrite.CompareAndSwap(false, true) {
			return injectedErr
		}
		return writeFanoutManifest(root, manifest)
	}

	err := fanout(FanoutOptions{
		Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}},
		PromptArgs: []string{"shared"}, Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
		NewCommand:    command,
	}, coordinatorState)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("Fanout error = %v, want original final publication failure", err)
	}
	if !failedTerminalWrite.Load() {
		t.Fatal("terminal manifest write was not injected")
	}

	runEntries, err := os.ReadDir(dispatchRunPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(runEntries) != 2 {
		t.Fatalf("run count = %d, want 2", len(runEntries))
	}
	for _, entry := range runEntries {
		record, err := loadRunRecord(root, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if record.State != dispatchStateCompleted || record.CompletedAt == nil || processOwned(record) {
			t.Fatalf("child lifecycle evidence = %#v", record)
		}
		session, err := loadSession(root, record.Name)
		if err != nil {
			t.Fatal(err)
		}
		if session.ActiveRunID != "" {
			t.Fatalf("child claim remains active: %#v", session)
		}
	}

	manifestEntries, err := os.ReadDir(fanoutStateRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifestEntries) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(manifestEntries))
	}
	manifest, err := loadFanoutManifest(root, manifestEntries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State != dispatchStateFailed || manifest.Error != injectedErr.Error() || manifest.CompletedAt == nil {
		t.Fatalf("recovered aggregate evidence = %#v", manifest)
	}
	for _, child := range manifest.Children {
		if child.Status != dispatchStateCompleted {
			t.Fatalf("recovered aggregate omitted terminal child evidence: %#v", manifest.Children)
		}
	}
}

func TestFanoutPreservesCompletedManifestAfterReadBackFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	command := func(string, ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"done"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}

	injectedErr := errors.New("injected terminal manifest read-back failure")
	coordinatorState := defaultFanoutCoordinatorState()
	coordinatorState.loadManifest = func(root string, id string) (FanoutManifest, error) {
		manifest, err := loadFanoutManifest(root, id)
		if err == nil && manifest.State == dispatchStateCompleted {
			return FanoutManifest{}, injectedErr
		}
		return manifest, err
	}

	err := fanout(FanoutOptions{
		Root: root, Targets: []FanoutTarget{{Agent: AgentCodex}, {Agent: AgentCodex}}, PromptArgs: []string{"shared"}, Env: []string{},
		LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
		NewCommand: command,
	}, coordinatorState)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("Fanout error = %v, want terminal read-back failure", err)
	}

	manifestEntries, err := os.ReadDir(fanoutStateRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifestEntries) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(manifestEntries))
	}
	manifest, err := loadFanoutManifest(root, manifestEntries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State != dispatchStateCompleted || manifest.CompletedAt == nil || manifest.Error != "" {
		t.Fatalf("read-back failure mutated durable completed evidence: %#v", manifest)
	}
}

func TestConcurrentResumeRejectsSecondOwnerWithActiveRun(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	session.State = "durable"
	session.ProviderSessionID = runtimeSessionID
	session.ActiveRunID = ""
	session.ActiveClaimKnown = false
	completed := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &completed
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	startedPath := filepath.Join(t.TempDir(), "provider-started")
	var launches atomic.Int32
	var firstCommand atomic.Pointer[exec.Cmd]
	var firstFinished atomic.Bool
	firstDone := make(chan error, 1)
	t.Cleanup(func() {
		if firstFinished.Load() {
			return
		}
		if cmd := firstCommand.Load(); cmd != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-firstDone:
		case <-time.After(2 * time.Second):
		}
	})
	command := func(string, ...string) *exec.Cmd {
		if launches.Add(1) == 1 {
			cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; touch "$1"; while :; do sleep 1; done`, "sh", startedPath) // #nosec G204 -- test-owned path passed as a positional argument.
			firstCommand.Store(cmd)
			return cmd
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"resumed"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}
	go func() {
		firstDone <- Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"one"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command})
	}()
	waitForFanoutTestPath(t, startedPath)
	active, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	waitForFanoutRunState(t, root, active.ActiveRunID, dispatchStateRunning)
	activeClaim, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if activeClaim.ActiveRunID != active.ActiveRunID {
		t.Fatalf("running provider did not retain its active claim: %#v", activeClaim)
	}
	err = Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"two"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command})
	if err == nil || !strings.Contains(err.Error(), "already active in run") {
		t.Fatalf("second resume error = %v", err)
	}
	records, warnings, listErr := listRunRecords(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(warnings) != 0 {
		t.Fatalf("list run warnings = %#v", warnings)
	}
	var rejected *RunRecord
	for index := range records {
		if records[index].Mode == dispatchModeResume && records[index].ID != active.ActiveRunID && records[index].State == dispatchStateFailed {
			rejected = &records[index]
			break
		}
	}
	if rejected == nil {
		t.Fatalf("rejected resume did not publish terminal attempted-run evidence: %#v", records)
	}
	if rejected.RecoveryState != recoveryRetrySafe || rejected.CompletedAt == nil || rejected.TerminalReason != err.Error() {
		t.Fatalf("rejected resume evidence = %#v, error = %v", rejected, err)
	}
	blocked, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != activeClaim {
		t.Fatalf("blocked resume mutated conversation mapping: before = %#v, after = %#v", activeClaim, blocked)
	}
	expired := time.Now().UTC().Add(-dispatchSessionRetention - time.Hour)
	rejected.CompletedAt = &expired
	if err := writeRunRecord(filepath.Join(dispatchRunPath(root), rejected.ID), rejected); err != nil {
		t.Fatalf("age rejected resume evidence: %v", err)
	}
	if err := pruneDispatchEvidence(root, time.Now().UTC()); err != nil {
		t.Fatalf("prune rejected resume evidence: %v", err)
	}
	if _, err := loadRunRecord(root, rejected.ID); !errors.Is(err, errDispatchRunNotFound) {
		t.Fatalf("expired rejected resume evidence remains: %v", err)
	}
	if launches.Load() != 1 {
		t.Fatalf("provider launches = %d, want 1", launches.Load())
	}
	if err := Cancel(CancelRequest{Root: root, ID: active.ActiveRunID}); err != nil {
		t.Fatalf("Cancel first resume: %v", err)
	}
	requireDispatchExitCode(t, <-firstDone, ExitTargetFailure)
	firstFinished.Store(true)
	finalized, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ActiveRunID != "" {
		t.Fatalf("owning execution did not release after provider termination: %#v", finalized)
	}
	if err := Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"three"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command}); err != nil {
		t.Fatalf("resume after owning finalization: %v", err)
	}
	if launches.Load() != 2 {
		t.Fatalf("provider launches after finalization = %d, want 2", launches.Load())
	}
}

func TestWriteFanoutManifestPreservesUnreadableEvidence(t *testing.T) {
	root := t.TempDir()
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	path := fanoutPath(root, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("{not-json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	err = writeFanoutManifest(root, FanoutManifest{ID: id, State: dispatchStateRunning, CreatedAt: time.Now().UTC()})
	requireDispatchExitCode(t, err, ExitConfig)
	retained, err := os.ReadFile(path) // #nosec G304 -- path is test-owned fanout evidence.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, corrupt) {
		t.Fatalf("unreadable manifest was overwritten: got %q, want %q", retained, corrupt)
	}
}

func waitForFanoutTestPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect test path %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for test path %s", path)
}

func waitForFanoutRunState(t *testing.T, root string, runID string, state string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := loadRunRecord(root, runID)
		if err != nil {
			t.Fatalf("load run %s while waiting for %s: %v", runID, state, err)
		}
		if record.State == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run %s to reach %s", runID, state)
}
