package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	workerRequestFile = "worker-request.json"
	workerLogFile     = "worker.log"
)

type publicResult struct {
	Handle     string `json:"handle"`
	State      string `json:"state"`
	ResultPath string `json:"result_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

type workerRequest struct {
	Root         string `json:"root"`
	WorkDir      string `json:"work_dir"`
	RunID        string `json:"run_id"`
	Mode         string `json:"mode"`
	Prompt       []byte `json:"prompt"`
	Depth        int    `json:"depth"`
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	Skill        string `json:"skill,omitempty"`
	TargetPinned bool   `json:"target_pinned,omitempty"`
}

type launchedWorker struct {
	gate          *os.File
	pid           int
	startIdentity string
}

type workerLauncher func(root string, runID string, logPath string) (launchedWorker, error)

// Start creates a durable conversation and returns as soon as its worker is
// authorized to begin. The worker cannot contact the provider before the
// complete handle response has been written.
func Start(opts StartOptions) error {
	if strings.TrimSpace(opts.Agent) == "" || strings.TrimSpace(opts.Agent) == AgentRandom {
		return exitError(ExitUsage, "dispatch start requires an explicit --agent")
	}
	promptText, err := resolvePromptSource(opts.Prompt, opts.PromptFile)
	if err != nil {
		return err
	}
	project, stderr, env, depth, err := loadDispatchProject(opts.Root, opts.Stderr, opts.Env)
	if err != nil {
		return err
	}
	if err := pruneDispatchEvidence(opts.Root, time.Now()); err != nil {
		return err
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		return err
	}
	requested, ok := lookupTarget(opts.Agent)
	if !ok {
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, opts.Agent))
	}
	target, version, prompt, err := prepareFresh(project, requested, runOptions{
		Root: opts.Root, Model: opts.Model, ReasoningEffort: opts.ReasoningEffort,
		Skill: opts.Skill, Prompt: promptText, LookPath: opts.LookPath,
		VersionLookup: opts.VersionLookup,
	})
	if err != nil {
		return err
	}
	if err := prepareProjection(project, opts.Root, stderr); err != nil {
		return err
	}
	projectionRoot, err := prepareTargetProjection(project, opts.Root, opts.WorkDir, target)
	if err != nil {
		return err
	}
	if err := validateSkillProjection(projectionRoot, target, opts.Skill); err != nil {
		return err
	}
	run, err := newDispatchRun(opts.Root, target.Name, version, dispatchModeFresh)
	if err != nil {
		return err
	}
	run.Record.Skill = strings.TrimSpace(opts.Skill)
	if parent, ok := clients.GetEnv(env, "AL_RUN_ID"); ok {
		run.Record.ParentRunID = parent
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			return err
		}
	}
	session, err := reserveSession(opts.Root, run)
	if err != nil {
		return err
	}
	session.Model = opts.Model
	session.ReasoningEffort = opts.ReasoningEffort
	if err := persistSession(opts.Root, session); err != nil {
		return finishDispatchFailure(dispatchExecution{Root: opts.Root, Run: run, Session: session}, err)
	}
	request := workerRequest{Root: opts.Root, WorkDir: opts.WorkDir, RunID: run.Record.ID, Mode: dispatchModeFresh, Prompt: prompt, Depth: depth + 1, Model: opts.Model, Effort: opts.ReasoningEffort, Skill: opts.Skill}
	return publishInvocation(opts.Root, run, session, request, writerOrDiscard(opts.Stdout), opts.launchWorker)
}

// Continue starts the next invocation in an existing terminal conversation.
func Continue(opts ContinueOptions) error {
	promptText, err := resolvePromptSource(opts.Prompt, opts.PromptFile)
	if err != nil {
		return err
	}
	project, stderr, env, depth, err := loadDispatchProject(opts.Root, opts.Stderr, opts.Env)
	if err != nil {
		return err
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		return err
	}
	session, err := loadSession(opts.Root, strings.TrimSpace(opts.Handle))
	if err != nil {
		return err
	}
	current, err := resolveWaitRun(opts.Root, session.Name)
	if err != nil {
		return err
	}
	if !terminalDispatchState(current.State) {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is running", session.Name))
	}
	if current.State == dispatchStateCompleted {
		if _, err := completedResultPath(current); err != nil {
			return err
		}
	}
	target, ok := lookupTarget(session.Agent)
	if !ok {
		return exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has unsupported provider %q", session.Name, session.Agent))
	}
	if !targetEnabled(project.Config, target.Name) {
		return exitError(ExitConfig, fmt.Sprintf("`al dispatch` target %s is disabled in config", target.Name))
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(target.Binary)
	if err != nil {
		return exitError(ExitUnavailable, fmt.Sprintf("`al dispatch` target %s requires `%s` on PATH", target.Name, target.Binary))
	}
	target, version, err := compatibleTargetVersionCached(opts.Root, path, target, opts.VersionLookup)
	if err != nil {
		return err
	}
	prompt, err := BuildChildPrompt(project, target.Name, promptText, "")
	if err != nil {
		return err
	}
	if err := prepareProjection(project, opts.Root, stderr); err != nil {
		return err
	}
	if _, err := prepareTargetProjection(project, opts.Root, opts.WorkDir, target); err != nil {
		return err
	}
	run, err := newDispatchRun(opts.Root, target.Name, version, dispatchModeResume)
	if err != nil {
		return err
	}
	run.Record.Name = session.Name
	run.Record.PreviousRunID = current.ID
	if parent, ok := clients.GetEnv(env, "AL_RUN_ID"); ok {
		run.Record.ParentRunID = parent
	}
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		return err
	}
	session, err = claimConversation(opts.Root, session.Name, run.Record.ID)
	if err != nil {
		return finishRejectedResume(run, err, writeRunRecord)
	}
	mode := dispatchModeResume
	if session.ProviderSessionID == "" {
		mode = dispatchModeFresh
	}
	request := workerRequest{Root: opts.Root, WorkDir: opts.WorkDir, RunID: run.Record.ID, Mode: mode, Prompt: prompt, Depth: depth + 1, Model: session.Model, Effort: session.ReasoningEffort, TargetPinned: session.TargetPinned}
	return publishInvocation(opts.Root, run, session, request, writerOrDiscard(opts.Stdout), opts.launchWorker)
}

func resolvePromptSource(prompt string, promptFile string) (string, error) {
	trimmedPrompt := strings.TrimSpace(prompt)
	trimmedPromptFile := strings.TrimSpace(promptFile)
	hasText := trimmedPrompt != ""
	hasFile := trimmedPromptFile != ""
	if hasText == hasFile {
		return "", exitError(ExitUsage, "exactly one of --prompt or --prompt-file is required")
	}
	if hasText {
		if len(prompt) > MaxStdinPromptBytes {
			return "", exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(prompt), MaxStdinPromptBytes))
		}
		return prompt, nil
	}
	data, err := os.ReadFile(trimmedPromptFile) // #nosec G304 -- caller explicitly selected the prompt file.
	if err != nil {
		return "", wrapExitError(ExitUsage, "read dispatch prompt file", err)
	}
	if len(data) > MaxStdinPromptBytes {
		return "", exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(data), MaxStdinPromptBytes))
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", exitError(ExitUsage, "dispatch prompt file is empty or whitespace")
	}
	return string(data), nil
}

func publishInvocation(root string, run *dispatchRun, session Session, request workerRequest, stdout io.Writer, launcher workerLauncher) error {
	requestPath := filepath.Join(run.Dir, workerRequestFile)
	if err := writeJSONAtomic(requestPath, request); err != nil {
		return failBeforePublication(root, run, session, wrapExitError(ExitConfig, "write dispatch worker request", err))
	}
	if launcher == nil {
		launcher = launchDetachedWorker
	}
	worker, err := launcher(root, run.Record.ID, filepath.Join(run.Dir, workerLogFile))
	if err != nil {
		return failBeforePublication(root, run, session, wrapExitError(ExitUnavailable, "start dispatch worker", err))
	}
	run.Record.SupervisorPID = worker.pid
	run.Record.SupervisorStartIdentity = worker.startIdentity
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		_ = worker.gate.Close()
		return failBeforePublication(root, run, session, err)
	}
	if err := writePublicResult(stdout, publicResult{Handle: session.Name, State: dispatchStateRunning}); err != nil {
		_ = worker.gate.Close()
		return errors.Join(err, removeWorkerRequest(run.Dir))
	}
	if _, err := worker.gate.Write([]byte{1}); err != nil {
		_ = worker.gate.Close()
		return errors.Join(wrapExitError(ExitUnavailable, "authorize dispatch worker", err), removeWorkerRequest(run.Dir))
	}
	_ = worker.gate.Close()
	return nil
}

func failBeforePublication(root string, run *dispatchRun, session Session, cause error) error {
	failure := finishDispatchFailure(dispatchExecution{Root: root, Run: run, Session: session}, errors.Join(cause, removeWorkerRequest(run.Dir)))
	if run.Record.PreviousRunID == "" {
		return failure
	}
	restoreErr := restorePreviousInvocation(root, session.Name, run.Record.ID, run.Record.PreviousRunID)
	return errors.Join(failure, restoreErr)
}

func removeWorkerRequest(runDir string) error {
	err := os.Remove(filepath.Join(runDir, workerRequestFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapExitError(ExitConfig, "remove dispatch worker request", err)
	}
	return nil
}

func restorePreviousInvocation(root string, handle string, rejectedRunID string, previousRunID string) error {
	return withSessionLock(root, handle, func() error {
		path, err := sessionPath(root, handle)
		if err != nil {
			return err
		}
		var session Session
		if err := readJSON(path, &session); err != nil {
			return wrapExitError(ExitConfig, "read dispatch conversation for invocation rollback", err)
		}
		if session.RunID != rejectedRunID {
			return nil
		}
		session.RunID = previousRunID
		if session.ActiveRunID == rejectedRunID {
			session.ActiveRunID = ""
			session.ActiveClaimKnown = true
		}
		return writeJSONAtomic(path, session)
	})
}

func launchDetachedWorker(root string, runID string, logPath string) (launchedWorker, error) {
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		return launchedWorker{}, err
	}
	cleanup := func() { _ = gateRead.Close(); _ = gateWrite.Close() }
	executable, err := os.Executable()
	if err != nil {
		cleanup()
		return launchedWorker{}, err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- Agent Layer-owned run directory.
	if err != nil {
		cleanup()
		return launchedWorker{}, err
	}
	cmd := exec.CommandContext(context.Background(), executable, "__dispatch-worker", "--root", root, "--run", runID) // #nosec G204 -- executable is the current Agent Layer binary; the background worker intentionally outlives its caller.
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.ExtraFiles = []*os.File{gateRead}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		cleanup()
		return launchedWorker{}, err
	}
	_ = gateRead.Close()
	_ = log.Close()
	identity := processStartIdentity(cmd.Process.Pid)
	pid := cmd.Process.Pid
	if identity == "" {
		_ = gateWrite.Close()
		_ = cmd.Process.Release()
		return launchedWorker{}, errors.New("capture dispatch worker process identity")
	}
	if err := cmd.Process.Release(); err != nil {
		_ = gateWrite.Close()
		return launchedWorker{}, err
	}
	return launchedWorker{gate: gateWrite, pid: pid, startIdentity: identity}, nil
}

// RunWorker executes one prepared invocation after its parent authorizes it.
func RunWorker(root string, runID string, gate io.Reader) error {
	var token [1]byte
	if _, err := io.ReadFull(gate, token[:]); err != nil || token[0] != 1 {
		cause := errors.New("dispatch worker was not authorized")
		record, loadErr := loadRunRecord(root, runID)
		if loadErr != nil {
			return errors.Join(cause, loadErr)
		}
		// Only the recorded worker may terminalize its own invocation. Any
		// other unauthorized caller (for example a manual `__dispatch-worker`
		// invocation against a live run) must not touch run state; a genuinely
		// abandoned run is reconciled from its recorded process identities.
		if record.SupervisorPID != os.Getpid() || record.SupervisorStartIdentity != processStartIdentity(os.Getpid()) {
			return cause
		}
		return failWorkerBeforeExecution(root, runID, cause)
	}
	var request workerRequest
	requestPath := filepath.Join(dispatchRunPath(root), runID, workerRequestFile)
	if err := readJSON(requestPath, &request); err != nil {
		return failWorkerBeforeExecution(root, runID, wrapExitError(ExitConfig, "read dispatch worker request", err))
	}
	if request.Root != root || request.RunID != runID {
		return failWorkerBeforeExecution(root, runID, exitError(ExitConfig, "dispatch worker request identity does not match its invocation"))
	}
	if err := os.Remove(requestPath); err != nil {
		return failWorkerBeforeExecution(root, runID, wrapExitError(ExitConfig, "remove consumed dispatch worker request", err))
	}
	record, err := loadRunRecord(root, runID)
	if err != nil {
		return err
	}
	session, err := loadSession(root, record.Name)
	if err != nil {
		return failWorkerBeforeExecution(root, runID, err)
	}
	project, err := loadWorkerProject(root)
	if err != nil {
		return failWorkerBeforeExecution(root, runID, err)
	}
	target, ok := lookupTarget(record.Agent)
	if !ok {
		return failWorkerBeforeExecution(root, runID, exitError(ExitConfig, fmt.Sprintf("unsupported dispatch provider %q", record.Agent)))
	}
	return executeDispatch(dispatchExecution{
		Root: root, WorkDir: request.WorkDir, Project: project, Target: target,
		Version: record.ProviderVersion, Prompt: request.Prompt, Mode: request.Mode,
		Run: &dispatchRun{Record: record, Dir: filepathForRun(root, runID)}, Session: session,
		Stdout: io.Discard, Stderr: io.Discard, Env: os.Environ(), Depth: request.Depth,
		Model: request.Model, Effort: request.Effort, TargetPinned: request.TargetPinned, Skill: request.Skill,
	})
}

func loadWorkerProject(root string) (*config.ProjectConfig, error) {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, wrapExitError(ExitConfig, err.Error(), err)
	}
	return project, nil
}

func failWorkerBeforeExecution(root string, runID string, cause error) error {
	record, err := loadRunRecord(root, runID)
	if err != nil {
		return errors.Join(cause, err)
	}
	session, err := loadSession(root, record.Name)
	if err != nil {
		return errors.Join(cause, err)
	}
	request := dispatchExecution{Root: root, Run: &dispatchRun{Record: record, Dir: filepathForRun(root, runID)}, Session: session}
	return finishDispatchFailure(request, cause)
}

func writePublicResult(stdout io.Writer, result publicResult) error {
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return wrapExitError(ExitTargetFailure, "write dispatch response", err)
	}
	return nil
}
