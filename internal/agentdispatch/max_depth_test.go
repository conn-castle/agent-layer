package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
)

func intPtr(v int) *int { return &v }

func stubWorkerCapture(t *testing.T, root string) (workerLauncher, func(runID string) workerRequest) {
	t.Helper()
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
	readReq := func(runID string) workerRequest {
		t.Helper()
		var req workerRequest
		if err := readJSON(filepath.Join(filepathForRun(root, runID), workerRequestFile), &req); err != nil {
			t.Fatalf("read worker request for %s: %v", runID, err)
		}
		return req
	}
	return launcher, readReq
}

func markInvocationTerminalConfirmed(t *testing.T, root string, runID string, handle string, state string, providerSessionID string) {
	t.Helper()
	dir := filepathForRun(root, runID)
	record, err := loadRunRecord(root, runID)
	if err != nil {
		t.Fatalf("load run %s: %v", runID, err)
	}
	if state == dispatchStateCompleted {
		if err := os.WriteFile(record.AnswerPath, []byte("ok\n"), 0o600); err != nil {
			t.Fatalf("write answer for %s: %v", runID, err)
		}
	}
	now := time.Now().UTC()
	updated, err := updateRunEvidence(dir, func(r *RunRecord) error {
		r.State = state
		r.CompletedAt = &now
		r.ProviderSessionID = providerSessionID
		if providerSessionID != "" {
			r.RecoveryState = recoveryResumeRequired
		} else {
			r.RecoveryState = recoveryRetrySafe
		}
		if state != dispatchStateCompleted {
			r.TerminalReason = "synthetic " + state
			r.TerminalExitCode = ExitTargetFailure
		}
		r.LaunchFenced = true
		r.TerminationConfirmed = true
		r.TerminationConfirmedAt = &now
		r.TerminationProof = terminationProofPrelaunchNoIntent
		return nil
	})
	if err != nil {
		t.Fatalf("update run %s: %v", runID, err)
	}
	if providerSessionID != "" {
		if _, err := persistOwnedSession(root, handle, runID, func(s *Session) {
			s.ProviderSessionID = providerSessionID
			s.State = sessionStateDurable
		}); err != nil {
			t.Fatalf("persist durable session %s: %v", handle, err)
		}
	}
	if err := releaseConversation(root, updated); err != nil {
		t.Fatalf("release conversation %s: %v", handle, err)
	}
	_ = removeWorkerRequest(dir)
}

func TestStartValidatesMaxDepth(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	for _, value := range []int{0, -1, -42} {
		t.Run("invalid_"+strconv.Itoa(value), func(t *testing.T) {
			err := Start(StartOptions{
				Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
				MaxDepth: intPtr(value), Env: []string{},
			})
			requireDispatchExitCode(t, err, ExitUsage)
			if !strings.Contains(err.Error(), "--max-depth") {
				t.Fatalf("expected --max-depth error, got %q", err.Error())
			}
		})
	}
}

func TestStartMaxDepthTightenOnlyAndChildEnv(t *testing.T) {
	t.Run("tightens below repo default and forwards env", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(1), Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		var res Result
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 1 {
			t.Fatalf("session.MaxDepth = %d, want 1", session.MaxDepth)
		}
		req := readReq(res.InvocationID)
		if req.Depth != 1 || req.MaxDepth != 1 {
			t.Fatalf("workerRequest depth/maxDepth = %d/%d, want 1/1", req.Depth, req.MaxDepth)
		}
	})

	t.Run("omitting max-depth leaves session uncapped and omits AL_DISPATCH_MAX_DEPTH", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		var res Result
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 0 {
			t.Fatalf("uncapped session.MaxDepth = %d, want 0", session.MaxDepth)
		}
		path, err := sessionPath(root, res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path) // #nosec G304 -- test-owned fixture session.
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "max_depth") {
			t.Fatalf("uncapped session.json unexpectedly persisted max_depth: %s", raw)
		}
		req := readReq(res.InvocationID)
		if req.Depth != 1 || req.MaxDepth != 0 {
			t.Fatalf("uncapped workerRequest depth/maxDepth = %d/%d, want 1/0", req.Depth, req.MaxDepth)
		}
		project, err := loadWorkerProject(root)
		if err != nil {
			t.Fatal(err)
		}
		runRecord, err := loadRunRecord(root, res.InvocationID)
		if err != nil {
			t.Fatal(err)
		}
		childEnv := dispatchEnvironment([]string{"KEEP=1"}, project, &dispatchRun{Record: runRecord, Dir: filepathForRun(root, res.InvocationID)}, req.Depth, req.MaxDepth)
		if value, ok := clients.GetEnv(childEnv, clients.EnvDispatchMaxDepth); ok {
			t.Fatalf("uncapped child env unexpectedly set %s=%q", clients.EnvDispatchMaxDepth, value)
		}
	})

	t.Run("never relaxes repo max_depth", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
		// Requesting --max-depth 5 at depth 2 still fails against repo dispatch.max_depth = 2.
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(5), Env: []string{clients.EnvDispatchActive + "=2"},
		})
		requireDispatchExitCode(t, err, ExitNested)

		// Starting at depth 1 with --max-depth 5 launches child at depth 2, where repo max_depth=2 blocks further dispatch.
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		err = Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(5), Stdout: &out, Env: []string{clients.EnvDispatchActive + "=1"},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		var res Result
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth = %d, want clamped repo cap 2", session.MaxDepth)
		}
		req := readReq(res.InvocationID)
		if req.MaxDepth != 2 {
			t.Fatalf("workerRequest.MaxDepth = %d, want clamped repo cap 2", req.MaxDepth)
		}
		childEnv := []string{
			clients.EnvDispatchActive + "=" + strconv.Itoa(req.Depth),
			clients.EnvDispatchMaxDepth + "=" + strconv.Itoa(req.MaxDepth),
		}
		requireDispatchExitCode(t, Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Too deep",
			Env: childEnv,
		}), ExitNested)
		// Even in another repo with a higher limit, the clamped cap 2 still blocks depth 2.
		higherRepo := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 5})
		requireDispatchExitCode(t, Start(StartOptions{
			Root: higherRepo, WorkDir: higherRepo, Agent: AgentCodex, Prompt: "Too deep in higher repo",
			Env: childEnv,
		}), ExitNested)
	})

	t.Run("never relaxes inherited cap and blocks when depth meets cap", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 5})
		// Caller at depth 1 with inherited cap 1 cannot relax by passing --max-depth 4.
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(4),
			Env:      []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=1"},
		})
		requireDispatchExitCode(t, err, ExitNested)

		// Caller at depth 1 with repo max_depth 5 requesting --max-depth 1 fails before launch.
		err = Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(1),
			Env:      []string{clients.EnvDispatchActive + "=1"},
		})
		requireDispatchExitCode(t, err, ExitNested)

		// Caller at depth 1 with inherited cap 2 requesting --max-depth 5 succeeds but clamps to 2.
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		err = Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Review",
			MaxDepth: intPtr(5), Stdout: &out,
			Env:      []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=2"},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start at depth 1 with cap 2 failed: %v", err)
		}
		var res Result
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth = %d, want inherited clamp 2", session.MaxDepth)
		}
		req := readReq(res.InvocationID)
		if req.Depth != 2 || req.MaxDepth != 2 {
			t.Fatalf("workerRequest depth/maxDepth = %d/%d, want 2/2", req.Depth, req.MaxDepth)
		}
	})

	t.Run("dispatched child process receives AL_DISPATCH_MAX_DEPTH", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
		binDir := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "codex.log")
		writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"codex ok"}\n'`)
		project, err := loadWorkerProject(root)
		if err != nil {
			t.Fatal(err)
		}
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		session, err := reserveSession(root, run, testDispatchSessionRetention)
		if err != nil {
			t.Fatal(err)
		}
		session.MaxDepth = 1
		if err := persistSession(root, session); err != nil {
			t.Fatal(err)
		}
		target, ok := lookupTarget(AgentCodex)
		if !ok {
			t.Fatal("missing codex target")
		}
		target.Binary = filepath.Join(binDir, "codex")
		err = executeDispatch(dispatchExecution{
			Root: root, WorkDir: root, Project: project, Target: target,
			Version: supportedProviderVersions[AgentCodex], Prompt: []byte("Review"),
			Mode: dispatchModeFresh, Run: run, Session: session,
			Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
			Env:      []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
			Depth:    1,
			MaxDepth: 1,
		})
		if err != nil {
			t.Fatalf("executeDispatch failed: %v", err)
		}
		assertFileContains(t, logPath, clients.EnvDispatchActive+"=1")
		assertFileContains(t, logPath, clients.EnvDispatchMaxDepth+"=1")
	})
}

func TestMaxDepthPropagatesAcrossRepositories(t *testing.T) {
	// Repo A has default max_depth = 3; Repo B has higher max_depth = 5.
	repoA := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	repoB := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 5})

	launcherA, readReqA := stubWorkerCapture(t, repoA)
	var outA bytes.Buffer
	err := Start(StartOptions{
		Root: repoA, WorkDir: repoA, Agent: AgentCodex, Prompt: "From A",
		MaxDepth: intPtr(2), Stdout: &outA, Env: []string{},
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcherA,
	})
	if err != nil {
		t.Fatalf("Start in repoA: %v", err)
	}
	var resA Result
	if err := json.Unmarshal(outA.Bytes(), &resA); err != nil {
		t.Fatal(err)
	}
	reqA := readReqA(resA.InvocationID)
	childEnv := []string{
		clients.EnvDispatchActive + "=" + strconv.Itoa(reqA.Depth),
		clients.EnvDispatchMaxDepth + "=" + strconv.Itoa(reqA.MaxDepth),
	}

	// Child from repoA (depth=1, cap=2) starts in repoB (repo max_depth=5):
	// allowed at depth 1, but inherits cap=2 rather than repoB's 5.
	launcherB, readReqB := stubWorkerCapture(t, repoB)
	var outB bytes.Buffer
	err = Start(StartOptions{
		Root: repoB, WorkDir: repoB, Agent: AgentCodex, Prompt: "From B",
		Stdout: &outB, Env: childEnv,
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcherB,
	})
	if err != nil {
		t.Fatalf("Start in repoB: %v", err)
	}
	var resB Result
	if err := json.Unmarshal(outB.Bytes(), &resB); err != nil {
		t.Fatal(err)
	}
	reqB := readReqB(resB.InvocationID)
	if reqB.Depth != 2 || reqB.MaxDepth != 2 {
		t.Fatalf("repoB grandchild depth/maxDepth = %d/%d, want 2/2", reqB.Depth, reqB.MaxDepth)
	}

	// Grandchild in repoB (depth=2, cap=2) is blocked on every mutating op despite repoB max_depth=5.
	grandchildEnv := []string{
		clients.EnvDispatchActive + "=" + strconv.Itoa(reqB.Depth),
		clients.EnvDispatchMaxDepth + "=" + strconv.Itoa(reqB.MaxDepth),
	}
	requireDispatchExitCode(t, Reserve(ReserveOptions{Root: repoB, Env: grandchildEnv}), ExitNested)
	requireDispatchExitCode(t, Start(StartOptions{Root: repoB, Agent: AgentCodex, Prompt: "Nested", Env: grandchildEnv}), ExitNested)
	requireDispatchExitCode(t, Continue(ContinueOptions{Root: repoB, Handle: resB.Handle, Prompt: "Nested", Env: grandchildEnv}), ExitNested)
	requireDispatchExitCode(t, Cancel(CancelRequest{Root: repoB, Handle: resB.Handle, Env: grandchildEnv}), ExitNested)

	// Uncapped worker from repoLow (max_depth=1) does not leak repoLow's default into repoB (max_depth=5).
	repoLow := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 1})
	launcherLow, readReqLow := stubWorkerCapture(t, repoLow)
	var outLow bytes.Buffer
	if err := Start(StartOptions{
		Root: repoLow, WorkDir: repoLow, Agent: AgentCodex, Prompt: "Uncapped in low repo",
		Stdout: &outLow, Env: []string{},
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcherLow,
	}); err != nil {
		t.Fatalf("Start in repoLow: %v", err)
	}
	var resLow Result
	if err := json.Unmarshal(outLow.Bytes(), &resLow); err != nil {
		t.Fatal(err)
	}
	reqLow := readReqLow(resLow.InvocationID)
	if reqLow.MaxDepth != 0 {
		t.Fatalf("uncapped repoLow workerRequest.MaxDepth = %d, want 0", reqLow.MaxDepth)
	}
	// Child from uncapped repoLow has AL_DISPATCH_ACTIVE=1 and no AL_DISPATCH_MAX_DEPTH,
	// so it can still start in repoB (max_depth=5) while repoLow (max_depth=1) blocks it.
	uncappedChildEnv := []string{clients.EnvDispatchActive + "=" + strconv.Itoa(reqLow.Depth)}
	requireDispatchExitCode(t, Start(StartOptions{Root: repoLow, Agent: AgentCodex, Prompt: "Blocked in repoLow", Env: uncappedChildEnv}), ExitNested)
	var outFromLowInB bytes.Buffer
	if err := Start(StartOptions{
		Root: repoB, WorkDir: repoB, Agent: AgentCodex, Prompt: "Allowed in repoB",
		Stdout: &outFromLowInB, Env: uncappedChildEnv,
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcherB,
	}); err != nil {
		t.Fatalf("uncapped worker from repoLow should use repoB max_depth=5: %v", err)
	}
}

func TestMaxDepthRejectsMalformedInheritedEnv(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	for _, value := range []string{"0", "-1", "bogus", "   ", "1.5"} {
		t.Run("value="+strconv.Quote(value), func(t *testing.T) {
			env := []string{clients.EnvDispatchMaxDepth + "=" + value}
			for name, err := range map[string]error{
				"reserve":  Reserve(ReserveOptions{Root: root, Env: env}),
				"start":    Start(StartOptions{Root: root, Agent: AgentCodex, Prompt: "Review", Env: env}),
				"continue": Continue(ContinueOptions{Root: root, Handle: "tiny-round-relay", Prompt: "Review", Env: env}),
				"cancel":   Cancel(CancelRequest{Root: root, Handle: "tiny-round-relay", Env: env}),
			} {
				var exitErr *ExitError
				if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
					t.Fatalf("%s with %s=%q: got %T (%v), want ExitNested", name, clients.EnvDispatchMaxDepth, value, err, err)
				}
				if !strings.Contains(exitErr.Error(), clients.EnvDispatchMaxDepth) {
					t.Fatalf("%s error %q did not name %s", name, exitErr.Error(), clients.EnvDispatchMaxDepth)
				}
			}
		})
	}

	// Empty string matches shared .mcp.json "${AL_DISPATCH_MAX_DEPTH:-}" in top-level sessions.
	launcher, readReq := stubWorkerCapture(t, root)
	var out bytes.Buffer
	err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Top level",
		Stdout: &out, Env: []string{clients.EnvDispatchActive + "=0", clients.EnvDispatchMaxDepth + "="},
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
	})
	if err != nil {
		t.Fatalf("empty inherited AL_DISPATCH_MAX_DEPTH should mean uncapped top-level: %v", err)
	}
	var res Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if got := readReq(res.InvocationID).MaxDepth; got != 0 {
		t.Fatalf("workerRequest.MaxDepth = %d, want 0 (uncapped)", got)
	}
}

func TestMaxDepthPersistsAcrossContinueAndReservations(t *testing.T) {
	t.Run("plain start and continue after completed, failed, and cancelled turns", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Turn 1",
			MaxDepth: intPtr(1), Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		var startRes Result
		if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
			t.Fatal(err)
		}
		if req := readReq(startRes.InvocationID); req.MaxDepth != 1 {
			t.Fatalf("turn 1 MaxDepth = %d, want 1", req.MaxDepth)
		}

		// Raise repo dispatch.max_depth to 5; persisted cap=1 must still hold across continuations.
		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		cfgBytes, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned fixture.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.ReplaceAll(string(cfgBytes), "max_depth = 3", "max_depth = 5")), 0o600); err != nil { // #nosec G703 -- test-owned fixture config.
			t.Fatal(err)
		}

		lastRunID := startRes.InvocationID
		for i, state := range []string{dispatchStateCompleted, dispatchStateFailed, dispatchStateCancelled} {
			markInvocationTerminalConfirmed(t, root, lastRunID, startRes.Handle, state, "provider-session-1")
			var contOut bytes.Buffer
			err := Continue(ContinueOptions{
				Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Follow up " + strconv.Itoa(i+2),
				Stdout: &contOut, Env: []string{},
				LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
			})
			if err != nil {
				t.Fatalf("Continue after %s: %v", state, err)
			}
			var contRes Result
			if err := json.Unmarshal(contOut.Bytes(), &contRes); err != nil {
				t.Fatal(err)
			}
			if req := readReq(contRes.InvocationID); req.MaxDepth != 1 {
				t.Fatalf("continue after %s MaxDepth = %d, want persisted 1", state, req.MaxDepth)
			}
			lastRunID = contRes.InvocationID
		}
	})

	t.Run("reserve + start --reservation and reserve --continue + continue --reservation", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
		launcher, readReq := stubWorkerCapture(t, root)

		var reserveOut bytes.Buffer
		if err := Reserve(ReserveOptions{Root: root, Stdout: &reserveOut, Env: []string{}}); err != nil {
			t.Fatalf("Reserve: %v", err)
		}
		var reserved Result
		if err := json.Unmarshal(reserveOut.Bytes(), &reserved); err != nil {
			t.Fatal(err)
		}

		var startOut bytes.Buffer
		err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Reserved turn 1",
			MaxDepth: intPtr(1), Reservation: &reserved.Handle, Stdout: &startOut, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Start --reservation: %v", err)
		}
		var started Result
		if err := json.Unmarshal(startOut.Bytes(), &started); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, started.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 1 {
			t.Fatalf("reserved session.MaxDepth = %d, want 1", session.MaxDepth)
		}
		if req := readReq(started.InvocationID); req.MaxDepth != 1 {
			t.Fatalf("reserved start MaxDepth = %d, want 1", req.MaxDepth)
		}

		markInvocationTerminalConfirmed(t, root, started.InvocationID, started.Handle, dispatchStateCompleted, "provider-session-res")

		var contResOut bytes.Buffer
		if err := Reserve(ReserveOptions{Root: root, ContinueHandle: &started.Handle, Stdout: &contResOut, Env: []string{}}); err != nil {
			t.Fatalf("Reserve --continue: %v", err)
		}
		var contReserved Result
		if err := json.Unmarshal(contResOut.Bytes(), &contReserved); err != nil {
			t.Fatal(err)
		}

		var contOut bytes.Buffer
		err = Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: started.Handle, Reservation: &contReserved.InvocationID,
			Prompt: "Reserved turn 2", Stdout: &contOut, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		})
		if err != nil {
			t.Fatalf("Continue --reservation: %v", err)
		}
		var continued Result
		if err := json.Unmarshal(contOut.Bytes(), &continued); err != nil {
			t.Fatal(err)
		}
		if req := readReq(continued.InvocationID); req.MaxDepth != 1 {
			t.Fatalf("reserved continue MaxDepth = %d, want persisted 1", req.MaxDepth)
		}
	})

	t.Run("uncapped conversation honors raised repo max_depth on continue", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 1})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		if err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Turn 1",
			Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatal(err)
		}
		var startRes Result
		if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
			t.Fatal(err)
		}
		if req := readReq(startRes.InvocationID); req.MaxDepth != 0 {
			t.Fatalf("uncapped start MaxDepth = %d, want 0", req.MaxDepth)
		}
		markInvocationTerminalConfirmed(t, root, startRes.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-uncapped")

		// Raise repo dispatch.max_depth from 1 to 3; continuing from depth 1 must succeed.
		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		cfgBytes, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned fixture.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.ReplaceAll(string(cfgBytes), "max_depth = 1", "max_depth = 3")), 0o600); err != nil { // #nosec G703 -- test-owned fixture config.
			t.Fatal(err)
		}

		var contOut bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Turn 2",
			Stdout: &contOut, Env: []string{clients.EnvDispatchActive + "=1"},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue after raising repo max_depth failed: %v", err)
		}
		var contRes Result
		if err := json.Unmarshal(contOut.Bytes(), &contRes); err != nil {
			t.Fatal(err)
		}
		if req := readReq(contRes.InvocationID); req.Depth != 2 || req.MaxDepth != 0 {
			t.Fatalf("uncapped continue depth/maxDepth = %d/%d, want 2/0", req.Depth, req.MaxDepth)
		}
	})

	t.Run("lowering repo max_depth enforces tighter repo cap on continue without mutating session cap", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		if err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Turn 1",
			MaxDepth: intPtr(2), Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatal(err)
		}
		var startRes Result
		if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
			t.Fatal(err)
		}
		markInvocationTerminalConfirmed(t, root, startRes.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-2")

		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		cfgBytes, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned fixture.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.ReplaceAll(string(cfgBytes), "max_depth = 3", "max_depth = 1")), 0o600); err != nil { // #nosec G703 -- test-owned fixture config.
			t.Fatal(err)
		}

		// Depth 1 is now blocked by lowered repo dispatch.max_depth = 1.
		requireDispatchExitCode(t, Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Blocked at depth 1",
			Env: []string{clients.EnvDispatchActive + "=1"},
		}), ExitNested)

		// Depth 0 still succeeds: workerRequest.MaxDepth is clamped to lowered repo cap (1)
		// without permanently overwriting the conversation's stored explicit cap (2).
		var contOut bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Turn 2",
			Stdout: &contOut, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue: %v", err)
		}
		var contRes Result
		if err := json.Unmarshal(contOut.Bytes(), &contRes); err != nil {
			t.Fatal(err)
		}
		if req := readReq(contRes.InvocationID); req.MaxDepth != 1 {
			t.Fatalf("continue MaxDepth = %d, want clamped repo cap 1", req.MaxDepth)
		}
		persisted, err := loadSession(root, startRes.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth = %d, want preserved explicit cap 2", persisted.MaxDepth)
		}
	})

	t.Run("continue persists tighter inherited cap across later uncapped turns", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 4})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		if err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Turn 1",
			Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatal(err)
		}
		var startRes Result
		if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
			t.Fatal(err)
		}
		markInvocationTerminalConfirmed(t, root, startRes.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-inherit-cont")

		// Turn 2: ordinary Continue with inherited AL_DISPATCH_MAX_DEPTH=2 tightens and persists cap=2.
		var cont2Out bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Turn 2",
			Stdout: &cont2Out, Env: []string{clients.EnvDispatchMaxDepth + "=2"},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue turn 2: %v", err)
		}
		var cont2Res Result
		if err := json.Unmarshal(cont2Out.Bytes(), &cont2Res); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, startRes.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth after turn 2 = %d, want 2", session.MaxDepth)
		}
		if req := readReq(cont2Res.InvocationID); req.MaxDepth != 2 {
			t.Fatalf("turn 2 MaxDepth = %d, want 2", req.MaxDepth)
		}

		// Turn 3: uncapped Continue still preserves persisted cap=2.
		markInvocationTerminalConfirmed(t, root, cont2Res.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-inherit-cont")
		var cont3Out bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Turn 3",
			Stdout: &cont3Out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue turn 3: %v", err)
		}
		var cont3Res Result
		if err := json.Unmarshal(cont3Out.Bytes(), &cont3Res); err != nil {
			t.Fatal(err)
		}
		if req := readReq(cont3Res.InvocationID); req.MaxDepth != 2 {
			t.Fatalf("turn 3 MaxDepth = %d, want persisted 2", req.MaxDepth)
		}

		// Turn 4: reserved continuation with tighter inherited cap=1 persists cap=1.
		markInvocationTerminalConfirmed(t, root, cont3Res.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-inherit-cont")
		var res4Out bytes.Buffer
		if err := Reserve(ReserveOptions{Root: root, ContinueHandle: &startRes.Handle, Stdout: &res4Out, Env: []string{}}); err != nil {
			t.Fatal(err)
		}
		var res4 Result
		if err := json.Unmarshal(res4Out.Bytes(), &res4); err != nil {
			t.Fatal(err)
		}
		var cont4Out bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Reservation: &res4.InvocationID,
			Prompt: "Turn 4", Stdout: &cont4Out, Env: []string{clients.EnvDispatchMaxDepth + "=1"},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue --reservation turn 4: %v", err)
		}
		var cont4Res Result
		if err := json.Unmarshal(cont4Out.Bytes(), &cont4Res); err != nil {
			t.Fatal(err)
		}
		session, err = loadSession(root, startRes.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 1 {
			t.Fatalf("session.MaxDepth after turn 4 = %d, want 1", session.MaxDepth)
		}
		if req := readReq(cont4Res.InvocationID); req.MaxDepth != 1 {
			t.Fatalf("turn 4 MaxDepth = %d, want 1", req.MaxDepth)
		}
	})

	t.Run("reserve under inherited cap preserves cap on uncapped start --reservation", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 4})
		launcher, readReq := stubWorkerCapture(t, root)
		var resOut bytes.Buffer
		if err := Reserve(ReserveOptions{
			Root: root, Stdout: &resOut,
			Env: []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=2"},
		}); err != nil {
			t.Fatalf("Reserve under cap: %v", err)
		}
		var reserved Result
		if err := json.Unmarshal(resOut.Bytes(), &reserved); err != nil {
			t.Fatal(err)
		}
		var startOut bytes.Buffer
		if err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Reservation: &reserved.Handle,
			Prompt: "Uncapped launcher", Stdout: &startOut, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Start --reservation: %v", err)
		}
		var started Result
		if err := json.Unmarshal(startOut.Bytes(), &started); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, started.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth = %d, want creator cap 2", session.MaxDepth)
		}
		if req := readReq(started.InvocationID); req.MaxDepth != 2 {
			t.Fatalf("workerRequest.MaxDepth = %d, want creator cap 2", req.MaxDepth)
		}

		// A depth-2 caller trying start --reservation on a cap-2 reservation fails with ExitNested before reading a missing prompt file.
		var res2Out bytes.Buffer
		if err := Reserve(ReserveOptions{
			Root: root, Stdout: &res2Out,
			Env: []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=2"},
		}); err != nil {
			t.Fatal(err)
		}
		var res2 Result
		if err := json.Unmarshal(res2Out.Bytes(), &res2); err != nil {
			t.Fatal(err)
		}
		requireDispatchExitCode(t, Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Reservation: &res2.Handle,
			PromptFile: filepath.Join(root, "missing-prompt.txt"),
			Env:        []string{clients.EnvDispatchActive + "=2"},
		}), ExitNested)
	})

	t.Run("reserve --continue under inherited cap preserves cap on uncapped continue --reservation", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 4})
		launcher, readReq := stubWorkerCapture(t, root)
		var out bytes.Buffer
		if err := Start(StartOptions{
			Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Uncapped start",
			Stdout: &out, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatal(err)
		}
		var startRes Result
		if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
			t.Fatal(err)
		}
		markInvocationTerminalConfirmed(t, root, startRes.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-cont-res-cap")

		var resOut bytes.Buffer
		if err := Reserve(ReserveOptions{
			Root: root, ContinueHandle: &startRes.Handle, Stdout: &resOut,
			Env: []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=2"},
		}); err != nil {
			t.Fatalf("Reserve --continue under cap: %v", err)
		}
		var reserved Result
		if err := json.Unmarshal(resOut.Bytes(), &reserved); err != nil {
			t.Fatal(err)
		}

		var contOut bytes.Buffer
		if err := Continue(ContinueOptions{
			Root: root, WorkDir: root, Handle: startRes.Handle, Reservation: &reserved.InvocationID,
			Prompt: "Uncapped continue launcher", Stdout: &contOut, Env: []string{},
			LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
		}); err != nil {
			t.Fatalf("Continue --reservation: %v", err)
		}
		var continued Result
		if err := json.Unmarshal(contOut.Bytes(), &continued); err != nil {
			t.Fatal(err)
		}
		session, err := loadSession(root, startRes.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if session.MaxDepth != 2 {
			t.Fatalf("session.MaxDepth = %d, want creator cap 2", session.MaxDepth)
		}
		if req := readReq(continued.InvocationID); req.MaxDepth != 2 {
			t.Fatalf("workerRequest.MaxDepth = %d, want creator cap 2", req.MaxDepth)
		}
	})
}

func TestContinueRejectsAtSessionCapBeforeHousekeepingWrites(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	launcher, _ := stubWorkerCapture(t, root)
	var out bytes.Buffer
	if err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Turn 1",
		MaxDepth: intPtr(1), Stdout: &out, Env: []string{},
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
	}); err != nil {
		t.Fatal(err)
	}
	var startRes Result
	if err := json.Unmarshal(out.Bytes(), &startRes); err != nil {
		t.Fatal(err)
	}
	markInvocationTerminalConfirmed(t, root, startRes.InvocationID, startRes.Handle, dispatchStateCompleted, "provider-session-early-cap")

	// Reserve a continuation and backdate its expiry so housekeeping under the session lock would retire it.
	var resOut bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, ContinueHandle: &startRes.Handle, Stdout: &resOut, Env: []string{}}); err != nil {
		t.Fatal(err)
	}
	var reserved Result
	if err := json.Unmarshal(resOut.Bytes(), &reserved); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := updateRunEvidence(filepathForRun(root, reserved.InvocationID), func(r *RunRecord) error {
		r.ReservationExpiresAt = &past
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Caller at depth 1 with no AL_DISPATCH_MAX_DEPTH in env (repo max_depth=3, session.MaxDepth=1):
	// both plain continue and continue --reservation must fail immediately with ExitNested before
	// retiring the expired continuation reservation.
	requireDispatchExitCode(t, Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: startRes.Handle, Prompt: "Blocked by session cap",
		Env: []string{clients.EnvDispatchActive + "=1"},
	}), ExitNested)
	// Also fails immediately before opening a missing --prompt-file.
	requireDispatchExitCode(t, Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: startRes.Handle, PromptFile: filepath.Join(root, "does-not-exist.txt"),
		Env: []string{clients.EnvDispatchActive + "=1"},
	}), ExitNested)
	requireDispatchExitCode(t, Continue(ContinueOptions{
		Root: root, WorkDir: root, Handle: startRes.Handle, Reservation: &reserved.InvocationID,
		Prompt: "Blocked reservation by session cap",
		Env:    []string{clients.EnvDispatchActive + "=1"},
	}), ExitNested)
	// Cancel also respects the conversation's stored session.MaxDepth for both handle and invocation ID.
	requireDispatchExitCode(t, Cancel(CancelRequest{
		Root: root, Handle: startRes.Handle, Env: []string{clients.EnvDispatchActive + "=1"},
	}), ExitNested)
	requireDispatchExitCode(t, Cancel(CancelRequest{
		Root: root, InvocationID: reserved.InvocationID, Env: []string{clients.EnvDispatchActive + "=1"},
	}), ExitNested)

	rec, err := loadRunRecord(root, reserved.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != dispatchStateReserved {
		t.Fatalf("expired continuation reservation was mutated to %q despite ExitNested", rec.State)
	}
	session, err := loadSession(root, startRes.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if session.ContinuationReservationID != reserved.InvocationID {
		t.Fatalf("session.ContinuationReservationID = %q, want untouched %q", session.ContinuationReservationID, reserved.InvocationID)
	}
}

func TestAtCapBlocksStateChangingOpsAndAllowsReadOnlyViaAPIAndMCP(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	launcher, _ := stubWorkerCapture(t, root)

	// Set up a completed conversation, an unstarted start reservation, and a continuation reservation at depth 0.
	var startOut bytes.Buffer
	if err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Initial",
		Stdout: &startOut, Env: []string{},
		LookPath: alwaysFound, VersionLookup: fixedCodexVersion, launchWorker: launcher,
	}); err != nil {
		t.Fatalf("setup Start: %v", err)
	}
	var completedRes Result
	if err := json.Unmarshal(startOut.Bytes(), &completedRes); err != nil {
		t.Fatal(err)
	}
	markInvocationTerminalConfirmed(t, root, completedRes.InvocationID, completedRes.Handle, dispatchStateCompleted, "provider-session-cap")

	var startResOut bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, Stdout: &startResOut, Env: []string{}}); err != nil {
		t.Fatalf("setup Reserve: %v", err)
	}
	var startReserved Result
	if err := json.Unmarshal(startResOut.Bytes(), &startReserved); err != nil {
		t.Fatal(err)
	}

	var contResOut bytes.Buffer
	if err := Reserve(ReserveOptions{Root: root, ContinueHandle: &completedRes.Handle, Stdout: &contResOut, Env: []string{}}); err != nil {
		t.Fatalf("setup Reserve --continue: %v", err)
	}
	var contReserved Result
	if err := json.Unmarshal(contResOut.Bytes(), &contReserved); err != nil {
		t.Fatal(err)
	}

	// Also create a running invocation to ensure Cancel at cap cannot cancel it.
	runningRun, runningSession := newRunningMCPTestRun(t, root)

	atCapEnv := []string{clients.EnvDispatchActive + "=1", clients.EnvDispatchMaxDepth + "=1"}

	// Go API: all state-changing operations fail with ExitNested without mutating state.
	requireDispatchExitCode(t, Reserve(ReserveOptions{Root: root, Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Reserve(ReserveOptions{Root: root, ContinueHandle: &completedRes.Handle, Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Start(StartOptions{Root: root, Agent: AgentCodex, Prompt: "Blocked", Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Start(StartOptions{Root: root, Agent: AgentCodex, Reservation: &startReserved.Handle, Prompt: "Blocked", Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Continue(ContinueOptions{Root: root, Handle: completedRes.Handle, Prompt: "Blocked", Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Continue(ContinueOptions{Root: root, Handle: completedRes.Handle, Reservation: &contReserved.InvocationID, Prompt: "Blocked", Env: atCapEnv}), ExitNested)
	requireDispatchExitCode(t, Cancel(CancelRequest{Root: root, Handle: runningSession.Name, Env: atCapEnv}), ExitNested)

	// Verify reservations and running invocation were untouched.
	if rec, err := loadRunRecord(root, startReserved.InvocationID); err != nil || rec.State != dispatchStateReserved {
		t.Fatalf("start reservation mutated by blocked call: %+v, %v", rec, err)
	}
	if rec, err := loadRunRecord(root, contReserved.InvocationID); err != nil || rec.State != dispatchStateReserved {
		t.Fatalf("continuation reservation mutated by blocked call: %+v, %v", rec, err)
	}
	if rec, err := loadRunRecord(root, runningRun.Record.ID); err != nil || rec.State != dispatchStateRunning {
		t.Fatalf("running invocation mutated by blocked cancel: %+v, %v", rec, err)
	}

	// MCP surface: fixed 7-tool list (no hiding), state-changing tools rejected, read-only tools allowed.
	mcpSession := newMCPTestSession(t, newMCPTestTools(root, atCapEnv...))
	listed, err := mcpSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 7 {
		t.Fatalf("ListTools returned %d tools at cap, want fixed 7 tools", len(listed.Tools))
	}

	for name, args := range map[string]any{
		ToolStart:    StartInput{Agent: AgentCodex, Prompt: "Blocked"},
		ToolContinue: ContinueInput{Handle: completedRes.Handle, Prompt: "Blocked"},
		ToolCancel:   SelectorInput{Handle: runningSession.Name},
	} {
		res := callMCPTool(t, mcpSession, name, args)
		if !res.IsError {
			t.Fatalf("MCP %s succeeded at cap, want error", name)
		}
		if !strings.Contains(toolResultText(res), "nested dispatch is blocked") {
			t.Fatalf("MCP %s error = %q, want depth-cap explanation", name, toolResultText(res))
		}
	}

	// Read-only MCP tools remain allowed at cap.
	if res := callMCPTool(t, mcpSession, ToolOptions, map[string]any{}); res.IsError {
		t.Fatalf("MCP dispatch_options failed at cap: %s", toolResultText(res))
	}
	if res := callMCPTool(t, mcpSession, ToolInspect, SelectorInput{InvocationID: completedRes.InvocationID}); res.IsError {
		t.Fatalf("MCP dispatch_inspect failed at cap: %s", toolResultText(res))
	}
	if res := callMCPTool(t, mcpSession, ToolWait, WaitInput{InvocationID: completedRes.InvocationID}); res.IsError {
		t.Fatalf("MCP dispatch_wait failed at cap: %s", toolResultText(res))
	}
	if res := callMCPTool(t, mcpSession, ToolOutput, OutputInput{InvocationID: completedRes.InvocationID, Artifact: artifactFinalAnswer}); res.IsError {
		t.Fatalf("MCP dispatch_output failed at cap: %s", toolResultText(res))
	}
}

func TestMCPStartInputMaxDepthValidationAndPropagation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 3})
	mcpSession := newMCPTestSession(t, newMCPTestTools(root))

	for _, invalid := range []int{0, -1} {
		res := callMCPTool(t, mcpSession, ToolStart, StartInput{
			Agent: AgentCodex, Prompt: "Review", MaxDepth: intPtr(invalid),
		})
		if !res.IsError {
			t.Fatalf("MCP dispatch_start with max_depth=%d succeeded, want error", invalid)
		}
		if !strings.Contains(toolResultText(res), "--max-depth") {
			t.Fatalf("MCP dispatch_start error = %q, want --max-depth validation", toolResultText(res))
		}
	}

	// Valid max_depth=1 from depth 1 fails immediately with nested-dispatch error.
	nestedSession := newMCPTestSession(t, newMCPTestTools(root, clients.EnvDispatchActive+"=1"))
	res := callMCPTool(t, nestedSession, ToolStart, StartInput{
		Agent: AgentCodex, Prompt: "Review", MaxDepth: intPtr(1),
	})
	if !res.IsError || !strings.Contains(toolResultText(res), "nested dispatch is blocked") {
		t.Fatalf("MCP dispatch_start at depth 1 with max_depth=1 = (err=%v, text=%q)", res.IsError, toolResultText(res))
	}
}

func fixedCodexVersion(string, string) (string, error) {
	return supportedProviderVersions[AgentCodex], nil
}
