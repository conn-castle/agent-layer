package agentdispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	failPreparedFanoutChildren(root, prepared, &warnings, errDispatchRunNotFound)
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
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	var launches atomic.Int32
	command := func(string, ...string) *exec.Cmd {
		if launches.Add(1) == 1 {
			close(started)
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; sleep 2; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"done"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
		}
		return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"unexpected"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test command.
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"one"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first resume did not launch")
	}
	err = Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"two"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }, NewCommand: command})
	if err == nil || !strings.Contains(err.Error(), "already active in run") {
		t.Fatalf("second resume error = %v", err)
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first resume: %v", err)
	}
	if launches.Load() != 1 {
		t.Fatalf("provider launches = %d, want 1", launches.Load())
	}
}
