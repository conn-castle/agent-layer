package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartPublishesHandleBeforeAuthorizingWorker(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var stdout bytes.Buffer
	var gateRead *os.File
	launcher := func(string, string, string) (launchedWorker, error) {
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		gateRead = read
		return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
	}
	err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Model: "gpt-test",
		ReasoningEffort: "high", Role: "plan-reviewer", Prompt: "Review this", Stdout: &stdout,
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateRead.Close() }()
	var token [1]byte
	if _, err := gateRead.Read(token[:]); err != nil || token[0] != 1 {
		t.Fatalf("worker authorization = %v, %v", token, err)
	}
	var response Result
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Handle == "" || response.State != dispatchStateRunning {
		t.Fatalf("start response = %#v", response)
	}
	session, err := loadSession(root, response.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if session.Model != "gpt-test" || session.ReasoningEffort != "high" {
		t.Fatalf("conversation settings were not durable: %#v", session)
	}
	record, err := loadRunRecord(root, session.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Role != "plan-reviewer" {
		t.Fatalf("dispatch role = %q", record.Role)
	}
}

func TestStartUsesOneLockedSkillSnapshotForRootAndTargetProjection(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	workDir := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "snapshot")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	oldManifest := []byte("---\nname: snapshot\ndescription: Original snapshot.\n---\nold bytes")
	newManifest := []byte("---\nname: snapshot\ndescription: Mutated source.\n---\nnew bytes")
	source := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(source, oldManifest, 0o600); err != nil {
		t.Fatal(err)
	}

	launcher := func(string, string, string) (launchedWorker, error) {
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
	var lookups atomic.Int32
	err := Start(StartOptions{
		Root: root, WorkDir: workDir, Agent: AgentCodex, Skill: "snapshot", Prompt: "Use the skill",
		Env: []string{}, LookPath: alwaysFound, launchWorker: launcher,
		VersionLookup: func(string, string) (string, error) {
			lookups.Add(1)
			if err := os.WriteFile(source, newManifest, 0o600); err != nil {
				return "", err
			}
			return supportedProviderVersions[AgentCodex], nil
		},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if lookups.Load() != 1 {
		t.Fatalf("provider version lookups = %d, want 1", lookups.Load())
	}
	for _, projectionRoot := range []string{root, workDir} {
		projected, readErr := os.ReadFile(filepath.Join(projectionRoot, ".agents", "skills", "snapshot", "SKILL.md")) // #nosec G304 -- test-controlled path.
		if readErr != nil || string(projected) != string(oldManifest) {
			t.Fatalf("projection at %s did not use the original locked snapshot: %q, %v", projectionRoot, projected, readErr)
		}
	}
	mutated, err := os.ReadFile(source) // #nosec G304 -- test-controlled path.
	if err != nil || string(mutated) != string(newManifest) {
		t.Fatalf("test mutation did not occur: %q, %v", mutated, err)
	}
}

func TestStartRequiresExplicitAgentAndOnePromptSource(t *testing.T) {
	tests := []StartOptions{
		{Model: "model", ReasoningEffort: "high", Prompt: "p"},
		{Agent: AgentCodex, Model: "model", ReasoningEffort: "high"},
		{Agent: AgentCodex, Model: "model", ReasoningEffort: "high", Prompt: "p", PromptFile: "p.md"},
	}
	for _, opts := range tests {
		err := Start(opts)
		requireDispatchExitCode(t, err, ExitUsage)
	}
}

func TestResolvePromptSourceNormalizesPathButRejectsWhitespaceOnlySources(t *testing.T) {
	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Review this."), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err := resolvePromptSource("", "  "+promptPath+"  ")
	if err != nil {
		t.Fatalf("resolve trimmed prompt file path: %v", err)
	}
	if prompt != "Review this." {
		t.Fatalf("prompt = %q", prompt)
	}
	for _, source := range []struct {
		prompt string
		file   string
	}{
		{prompt: " \t\n "},
		{file: " \t\n "},
	} {
		_, err := resolvePromptSource(source.prompt, source.file)
		requireDispatchExitCode(t, err, ExitUsage)
	}
	if err := os.WriteFile(promptPath, []byte(" \t\n "), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = resolvePromptSource("", promptPath)
	requireDispatchExitCode(t, err, ExitUsage)
}

func TestStartAllowsOmittedOverrides(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	var stdout bytes.Buffer
	launcher := func(string, string, string) (launchedWorker, error) {
		read, write, err := os.Pipe()
		if err != nil {
			return launchedWorker{}, err
		}
		go func() { defer func() { _ = read.Close() }(); var token [1]byte; _, _ = read.Read(token[:]) }()
		return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
	}
	err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentAntigravity, Prompt: "Review this", Stdout: &stdout,
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentAntigravity], nil },
		launchWorker:  launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response Result
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	session, err := loadSession(root, response.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if session.Model != "" || session.ReasoningEffort != "" {
		t.Fatalf("optional overrides unexpectedly set: %#v", session)
	}
}

func TestConcurrentContinueStartsOnlyOneInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	_, session := terminalConversationForAsyncTest(t, root)
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
	options := ContinueOptions{
		Root: root, WorkDir: root, Handle: session.Name, Prompt: "Continue",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  launcher,
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			opts := options
			opts.Stdout = &out
			errs <- Continue(opts)
		}()
	}
	wg.Wait()
	close(errs)
	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 || launches.Load() != 1 {
		t.Fatalf("successful continues=%d worker launches=%d", succeeded, launches.Load())
	}
}

func TestContinueFailureBeforeResponsePreservesCurrentInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	current, session := terminalConversationForAsyncTest(t, root)
	err := Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: session.Name, Prompt: "Continue",
		Env: []string{}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker: func(string, string, string) (launchedWorker, error) {
			return launchedWorker{}, errors.New("worker unavailable")
		},
	})
	if err == nil {
		t.Fatal("continue unexpectedly succeeded")
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retained.RunID != current.Record.ID || retained.ActiveRunID != "" {
		t.Fatalf("failed continue replaced current invocation: %#v", retained)
	}
}

func TestCancelIsIdempotentOnlyForCancelledInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var stdout bytes.Buffer
		if err := Cancel(CancelRequest{Root: root, ID: session.Name, Stdout: &stdout}); err != nil {
			t.Fatal(err)
		}
		var result Result
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.State != dispatchStateCancelled {
			t.Fatalf("cancel result = %#v", result)
		}
	}
}

// TestRunWorkerUnauthorizedOnlySupervisorTerminalizes proves that an
// unauthorized worker invocation (for example a manual `__dispatch-worker`
// call against a live run) cannot terminalize an invocation it does not own,
// while the recorded worker still fails its own run when its gate is closed.
func TestRunWorkerUnauthorizedOnlySupervisorTerminalizes(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = "another-process"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(root, run.Record.ID, bytes.NewReader(nil)); err == nil {
		t.Fatal("impostor worker reported success")
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("impostor worker terminalized a live run: %q", current.State)
	}
	current.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(filepathForRun(root, current.ID), &current); err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(root, current.ID, bytes.NewReader(nil)); err == nil {
		t.Fatal("unauthorized recorded worker reported success")
	}
	current, err = loadRunRecord(root, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateFailed {
		t.Fatalf("recorded worker could not terminalize its own run: %q", current.State)
	}
}

func TestLegacyActivityFieldDoesNotBreakCurrentConversation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	data, err := os.ReadFile(filepath.Join(run.Dir, dispatchRunFile))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["last_activity_kind"] = "answer_candidate"
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, dispatchRunFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load legacy run record: %v", err)
	}
	if loaded.ID != run.Record.ID {
		t.Fatalf("loaded wrong invocation: %s", loaded.ID)
	}
}

func terminalConversationForAsyncTest(t *testing.T, root string) (*dispatchRun, Session) {
	t.Helper()
	run, session := newWaitTestRun(t, root)
	session.Model = "gpt-test"
	session.ReasoningEffort = "high"
	session.ProviderSessionID = "provider-session"
	session.State = sessionStateDurable
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	if err := os.WriteFile(run.Record.AnswerPath, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, session.Name, run.Record.ID); err != nil {
		t.Fatal(err)
	}
	return run, session
}
