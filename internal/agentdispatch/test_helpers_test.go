package agentdispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// dispatchExecRequest configures one in-process engine execution for tests.
// It mirrors the live pipeline: Start/Continue prepare and publish an
// invocation, and the detached worker replays it through executeDispatch.
// Tests collapse those two halves into one synchronous call so they can
// inject stub providers and observe the terminal result directly.
type dispatchExecRequest struct {
	Root            string
	WorkDir         string
	Agent           string
	Model           string
	ReasoningEffort string
	Skill           string
	Prompt          string
	Env             []string
	Stdout          io.Writer
	Stderr          io.Writer
	LookPath        func(string) (string, error)
	NewCommand      CommandFactory
	VersionLookup   func(path string, agent string) (string, error)
}

// executeFreshDispatch runs one fresh conversation end to end in-process,
// following the same preparation and execution steps as Start plus RunWorker.
func executeFreshDispatch(req dispatchExecRequest) error {
	project, stderr, env, depth, err := loadDispatchProject(req.Root, req.Stderr, req.Env)
	if err != nil {
		return err
	}
	if err := pruneDispatchEvidence(req.Root, time.Now()); err != nil {
		return err
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		return err
	}
	target, ok := lookupTarget(req.Agent)
	if !ok {
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, req.Agent))
	}
	target, version, prompt, err := prepareFresh(project, target, runOptions{
		Root: req.Root, Model: req.Model, ReasoningEffort: req.ReasoningEffort,
		Skill: req.Skill, Prompt: req.Prompt, LookPath: req.LookPath, VersionLookup: req.VersionLookup,
	})
	if err != nil {
		return err
	}
	if err := prepareProjection(project, req.Root, stderr); err != nil {
		return err
	}
	projectionRoot, err := prepareTargetProjection(project, req.Root, req.WorkDir, target)
	if err != nil {
		return err
	}
	if err := validateSkillProjection(projectionRoot, target, req.Skill); err != nil {
		return err
	}
	run, err := newDispatchRun(req.Root, target.Name, version, dispatchModeFresh)
	if err != nil {
		return err
	}
	run.Record.Skill = strings.TrimSpace(req.Skill)
	session, err := reserveSession(req.Root, run)
	if err != nil {
		return err
	}
	return executeDispatch(dispatchExecution{
		Root: req.Root, WorkDir: req.WorkDir, Project: project, Target: target,
		Version: version, Prompt: prompt, Mode: dispatchModeFresh, Run: run, Session: session,
		Stdout: writerOrDiscard(req.Stdout), Stderr: stderr, Env: env, Depth: depth + 1,
		Model: req.Model, Effort: req.ReasoningEffort, Skill: req.Skill,
		NewCommand: req.NewCommand, VersionLookup: req.VersionLookup,
	})
}

// executeContinueDispatch runs the next invocation of an existing conversation
// in-process, following the same steps as Continue plus RunWorker.
func executeContinueDispatch(req dispatchExecRequest, handle string) error {
	project, stderr, env, depth, err := loadDispatchProject(req.Root, req.Stderr, req.Env)
	if err != nil {
		return err
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		return err
	}
	session, err := loadSession(req.Root, strings.TrimSpace(handle))
	if err != nil {
		return err
	}
	current, err := resolveWaitRun(req.Root, session.Name)
	if err != nil {
		return err
	}
	if !terminalDispatchState(current.State) {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is running", session.Name))
	}
	target, ok := lookupTarget(session.Agent)
	if !ok {
		return exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has unsupported provider %q", session.Name, session.Agent))
	}
	if !targetEnabled(project.Config, target.Name) {
		return exitError(ExitConfig, fmt.Sprintf("`al dispatch` target %s is disabled in config", target.Name))
	}
	lookPath := req.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(target.Binary)
	if err != nil {
		return exitError(ExitUnavailable, fmt.Sprintf("`al dispatch` target %s requires `%s` on PATH", target.Name, target.Binary))
	}
	target, version, err := compatibleTargetVersionCached(req.Root, path, target, req.VersionLookup)
	if err != nil {
		return err
	}
	prompt, err := BuildChildPrompt(project, target.Name, req.Prompt, "")
	if err != nil {
		return err
	}
	if err := prepareProjection(project, req.Root, stderr); err != nil {
		return err
	}
	if _, err := prepareTargetProjection(project, req.Root, req.WorkDir, target); err != nil {
		return err
	}
	run, err := newDispatchRun(req.Root, target.Name, version, dispatchModeResume)
	if err != nil {
		return err
	}
	run.Record.Name = session.Name
	run.Record.PreviousRunID = current.ID
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		return err
	}
	session, err = claimConversation(req.Root, session.Name, run.Record.ID)
	if err != nil {
		return finishRejectedResume(run, err, writeRunRecord)
	}
	mode := dispatchModeResume
	if session.ProviderSessionID == "" {
		mode = dispatchModeFresh
	}
	return executeDispatch(dispatchExecution{
		Root: req.Root, WorkDir: req.WorkDir, Project: project, Target: target,
		Version: version, Prompt: prompt, Mode: mode, Run: run, Session: session,
		Stdout: writerOrDiscard(req.Stdout), Stderr: stderr, Env: env, Depth: depth + 1,
		Model: session.Model, Effort: session.ReasoningEffort, TargetPinned: session.TargetPinned,
		NewCommand: req.NewCommand, VersionLookup: req.VersionLookup,
	})
}

func requireDispatchExitCode(t *testing.T, err error, code int) {
	t.Helper()
	_ = requireDispatchExitError(t, err, code)
}

func requireDispatchExitError(t *testing.T, err error, code int) *ExitError {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != code {
		t.Fatalf("expected ExitError code %d, got %T: %v", code, err, err)
	}
	return exitErr
}

func dispatchTestConfig(enabledAgents ...string) config.Config {
	cfg := config.Config{}
	for _, agent := range enabledAgents {
		switch agent {
		case AgentCodex:
			cfg.Agents.Codex.Enabled = boolPtr(true)
		case AgentClaude:
			cfg.Agents.Claude.Enabled = boolPtr(true)
		case AgentAntigravity:
			cfg.Agents.Antigravity.Enabled = boolPtr(true)
		}
	}
	return cfg
}

func boolPtr(value bool) *bool { return &value }

func alwaysFound(name string) (string, error) { return "/mock/" + name, nil }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func disableAgentInDispatchConfig(t *testing.T, root string, agent string) {
	t.Helper()
	replaceDispatchConfigText(t, root, "[agents."+agent+"]\nenabled = true", "[agents."+agent+"]\nenabled = false")
}

func replaceDispatchConfigText(t *testing.T, root string, old string, replacement string) {
	t.Helper()
	configPath := config.DefaultPaths(root).ConfigPath
	data, err := os.ReadFile(configPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if updated == string(data) {
		t.Fatalf("config did not contain %q", old)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil { // #nosec G306 G703 -- configPath is rooted in the test's temporary repository.
		t.Fatalf("write config: %v", err)
	}
}

func waitForTestPath(t *testing.T, path string) {
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

func waitForRunState(t *testing.T, root string, runID string, state string) {
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
