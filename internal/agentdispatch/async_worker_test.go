package agentdispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPublishedInvocationIsExecutedExactlyOnceByItsWorker covers the async
// handoff that every background dispatch depends on: Start publishes a prepared
// invocation and hands the detached worker the authorization to run it, and the
// worker replays that published request — not a re-derived one — to completion.
// The request is consumed in the process, so a worker that runs again cannot
// bill the provider a second time for the same invocation.
func TestPublishedInvocationIsExecutedExactlyOnceByItsWorker(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := filepath.Join(t.TempDir(), "bin")
	providerLog := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"item.completed","item":{"type":"agent_message","text":"worker answer"}}\n'`)
	t.Setenv("PATH", testPath(binDir))
	t.Setenv("AL_TEST_LOG", providerLog)

	var gate *os.File
	launcher := func(string, string, string) (launchedWorker, error) {
		read, write, err := os.Pipe()
		if err != nil {
			return launchedWorker{}, err
		}
		gate = read
		return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, nil
	}
	var stdout bytes.Buffer
	if err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review this",
		Stdout: &stdout, Env: []string{"PATH=" + testPath(binDir)}, LookPath: alwaysFound,
		VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
		launchWorker:  launcher,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gate.Close() })

	var response Result
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	session, err := loadSession(root, response.Handle)
	if err != nil {
		t.Fatal(err)
	}
	runID := session.RunID
	requestPath := filepath.Join(dispatchRunPath(root), runID, workerRequestFile)
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("Start did not publish a worker request for the handle it returned: %v", err)
	}

	if err := RunWorker(root, runID, gate); err != nil {
		t.Fatalf("authorized worker failed to execute its published invocation: %v", err)
	}
	record, err := loadRunRecord(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateCompleted {
		t.Fatalf("worker run state = %q, want the invocation to complete", record.State)
	}
	answer, err := os.ReadFile(record.AnswerPath) // #nosec G304 -- Agent Layer-owned run directory in a test repository.
	if err != nil || string(answer) != "worker answer" {
		t.Fatalf("worker answer = %q, %v", answer, err)
	}
	if _, err := os.Stat(requestPath); !os.IsNotExist(err) {
		t.Fatalf("worker request survived execution and could be replayed: %v", err)
	}

	// A second worker for the same invocation must not reach the provider again.
	before, err := os.ReadFile(providerLog) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	replayGateRead, replayGateWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replayGateRead.Close() }()
	if _, err := replayGateWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := replayGateWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(root, runID, replayGateRead); err == nil {
		t.Fatal("a consumed invocation was executed a second time")
	}
	after, err := os.ReadFile(providerLog) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("replayed worker invoked the provider again")
	}
}

// TestWorkerRejectsAnInvocationItWasNotPreparedFor covers the identity check on
// the published request. A worker that executed a request belonging to another
// root or invocation would run a prompt against the wrong project, so the
// mismatch must terminalize the run as failed instead of proceeding.
func TestWorkerRejectsAnInvocationItWasNotPreparedFor(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	request := workerRequest{Root: filepath.Join(root, "other-project"), RunID: run.Record.ID, Prompt: []byte("Review this")}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, workerRequestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateRead.Close() }()
	if _, err := gateWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := gateWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(root, run.Record.ID, gateRead); err == nil {
		t.Fatal("worker executed a request prepared for another project")
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateFailed {
		t.Fatalf("run state = %q, want the mismatched invocation to fail", record.State)
	}
}
