package agentdispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
)

// Run starts one fresh provider conversation. Existing conversations are never
// selected implicitly; callers must use Resume with a durable friendly name.
func Run(opts RunOptions) error {
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
	caller, callerKnown := knownCallerFromEnv(env)
	resolved, err := resolveTarget(project.Config, opts, caller, callerKnown)
	if err != nil {
		return err
	}
	target, version, prompt, err := prepareFresh(project, resolved, opts)
	if err != nil {
		return err
	}
	if err := prepareProjection(project, opts.Root, stderr, opts.Quiet); err != nil {
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
	return launchExecution(dispatchExecution{
		Root:          opts.Root,
		WorkDir:       opts.WorkDir,
		Project:       project,
		Target:        target,
		Version:       version,
		Prompt:        prompt,
		Mode:          dispatchModeFresh,
		Run:           run,
		Session:       session,
		Stdout:        writerOrDiscard(opts.Stdout),
		Stderr:        stderr,
		Env:           env,
		Depth:         depth + 1,
		Model:         opts.Model,
		Effort:        opts.ReasoningEffort,
		Skill:         opts.Skill,
		NewCommand:    opts.NewCommand,
		VersionLookup: opts.VersionLookup,
	}).await()
}

// Resume continues exactly the provider session addressed by name.
func Resume(opts ResumeOptions) error {
	return resume(opts, writeRunRecord)
}

// resume continues a durable provider session and publishes every attempted
// run outcome. The rejected-run writer is injectable so publication failures
// can be verified without mutating process-wide state.
func resume(opts ResumeOptions, writeRejectedRunRecord func(string, *RunRecord) error) error {
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
	session, err := loadSession(opts.Root, strings.TrimSpace(opts.Name))
	if err != nil {
		return err
	}
	if session.State != sessionStateDurable || session.ProviderSessionID == "" {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch session %q is still pending and cannot be resumed", session.Name))
	}
	target, ok := lookupTarget(session.Agent)
	if !ok {
		return exitError(ExitConfig, fmt.Sprintf("dispatch session %q has unsupported provider %q", session.Name, session.Agent))
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
	promptText, err := ResolvePrompt(opts.PromptArgs, opts.Stdin, opts.ReadStdin)
	if err != nil {
		return err
	}
	prompt, err := BuildChildPrompt(project, target.Name, promptText, opts.Skill)
	if err != nil {
		return err
	}
	run, err := newDispatchRun(opts.Root, target.Name, version, dispatchModeResume)
	if err != nil {
		return err
	}
	run.Record.Skill = strings.TrimSpace(opts.Skill)
	if parent, ok := clients.GetEnv(env, "AL_RUN_ID"); ok {
		run.Record.ParentRunID = parent
	}
	run.Record.Name = session.Name
	run.Record.PreviousRunID = session.RunID
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		return err
	}
	session, err = claimConversation(opts.Root, session.Name, run.Record.ID)
	if err != nil {
		return finishRejectedResume(run, err, writeRejectedRunRecord)
	}
	preflightRequest := dispatchExecution{Root: opts.Root, WorkDir: opts.WorkDir, Project: project, Target: target, Version: version, Mode: dispatchModeResume, Run: run, Session: session}
	if err := prepareProjection(project, opts.Root, stderr, opts.Quiet); err != nil {
		return finishDispatchFailure(preflightRequest, &preStartFailure{err: err})
	}
	projectionRoot, err := prepareTargetProjection(project, opts.Root, opts.WorkDir, target)
	if err != nil {
		return finishDispatchFailure(preflightRequest, &preStartFailure{err: err})
	}
	if err := validateSkillProjection(projectionRoot, target, opts.Skill); err != nil {
		return finishDispatchFailure(preflightRequest, &preStartFailure{err: err})
	}
	return launchExecution(dispatchExecution{
		Root:          opts.Root,
		WorkDir:       opts.WorkDir,
		Project:       project,
		Target:        target,
		Version:       version,
		Prompt:        prompt,
		Mode:          dispatchModeResume,
		Run:           run,
		Session:       session,
		Stdout:        writerOrDiscard(opts.Stdout),
		Stderr:        stderr,
		Env:           env,
		Depth:         depth + 1,
		Model:         session.Model,
		Effort:        session.ReasoningEffort,
		Skill:         opts.Skill,
		NewCommand:    opts.NewCommand,
		VersionLookup: opts.VersionLookup,
	}).await()
}

// finishRejectedResume makes claim rejection and its attempted-run evidence one
// durable outcome. A publication failure is joined with the rejection because
// either error alone would hide material recovery state from the caller.
func finishRejectedResume(run *dispatchRun, claimErr error, publish func(string, *RunRecord) error) error {
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryRetrySafe
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = claimErr.Error()
	if err := publish(run.Dir, &run.Record); err != nil {
		message := fmt.Sprintf("resume claim rejected (%v); publish rejected resume terminal evidence: %v", claimErr, err)
		return wrapExitError(ExitConfig, message, errors.Join(claimErr, err))
	}
	return claimErr
}

type dispatchExecution struct {
	Root          string
	WorkDir       string
	Project       *config.ProjectConfig
	Target        targetMeta
	Version       string
	Prompt        []byte
	Mode          string
	Run           *dispatchRun
	Session       Session
	Stdout        io.Writer
	Stderr        io.Writer
	Env           []string
	Depth         int
	Model         string
	Effort        string
	Skill         string
	NewCommand    CommandFactory
	VersionLookup func(path string, agent string) (string, error)
}

func executeDispatch(request dispatchExecution) error {
	if request.Run == nil || request.Project == nil {
		return exitError(ExitConfig, "dispatch execution was not initialized")
	}
	if current, err := loadRunRecord(request.Root, request.Run.Record.ID); err == nil && current.State == dispatchStateCancelled {
		return finishDispatchCancellation(request)
	}
	session := request.Session
	if request.Mode == dispatchModeFresh && request.Target.Name == AgentClaude {
		id, err := newUUID()
		if err != nil {
			return wrapExitError(ExitTargetFailure, "generate Claude dispatch session ID", err)
		}
		session.ProviderSessionID = id
		session.State = sessionStateDurable
		if err := persistSession(request.Root, session); err != nil {
			return err
		}
	}
	if err := writeIdentity(request.Stderr, session.Name, request.Target.Name, request.Mode, session.State == sessionStateDurable); err != nil {
		return wrapExitError(ExitTargetFailure, "write dispatch identity", err)
	}
	// The version passed the gate in requireSupportedVersion (or came from the
	// capability cache, whose entries also passed it), so a comparison error
	// here means corrupted state and must fail loud.
	warning, compatErr := providerVersionCompatibility(request.Target.Name, request.Version)
	if compatErr != nil {
		return exitError(ExitUnavailable, compatErr.Error())
	}
	if warning != "" {
		if _, err := fmt.Fprintln(request.Stderr, warning); err != nil {
			return wrapExitError(ExitTargetFailure, "write dispatch compatibility warning", err)
		}
	}

	persist := func(id string) error {
		if request.Mode == dispatchModeResume && id != session.ProviderSessionID {
			return exitError(ExitTargetFailure, fmt.Sprintf("%s resume returned a different provider session ID", request.Target.Name))
		}
		session.ProviderSessionID = id
		session.Agent = request.Target.Name
		session.State = sessionStateDurable
		session.RunID = request.Run.Record.ID
		session.LastUsedAt = time.Now().UTC()
		return persistSession(request.Root, session)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		request.Run.Record.Attempt = attempt
		request.Run.Record.State = dispatchStateStarting
		request.Run.Record.RecoveryState = recoveryRetrySafe
		if err := writeRunRecord(request.Run.Dir, &request.Run.Record); err != nil {
			return finishDispatchFailure(request, err)
		}
		if request.Mode == dispatchModeFresh && request.Target.Name == AgentClaude && attempt == 2 {
			id, err := newUUID()
			if err != nil {
				return wrapExitError(ExitTargetFailure, "generate retry Claude session ID", err)
			}
			session.ProviderSessionID = id
			if err := persistSession(request.Root, session); err != nil {
				return err
			}
		}
		childEnv := dispatchEnvironment(request.Env, request.Project, request.Run, request.Depth, request.Target.Name)
		command, err := buildProviderCommand(request.Target, request.Project, childEnv, request.Prompt, request.Model, request.Effort, request.Mode, session.ProviderSessionID, request.Run, request.Stderr)
		if err != nil {
			return finishDispatchFailure(request, &preStartFailure{err: err})
		}
		session.Model = command.Model
		session.ReasoningEffort = command.Effort
		if session.State == sessionStateDurable {
			if err := persistSession(request.Root, session); err != nil {
				return finishDispatchFailure(request, &preStartFailure{err: err})
			}
		}
		command.WorkDir = request.WorkDir
		request.Run.Record.ProviderLogPath = command.LogPath
		request.Run.Record.Model = command.Model
		request.Run.Record.ReasoningEffort = command.Effort
		request.Run.Record.Skill = strings.TrimSpace(request.Skill)
		if err := writeRunRecord(request.Run.Dir, &request.Run.Record); err != nil {
			return finishDispatchFailure(request, err)
		}
		if current, err := loadRunRecord(request.Root, request.Run.Record.ID); err == nil && current.State == dispatchStateCancelled {
			return finishDispatchFailure(request, exitError(ExitTargetFailure, fmt.Sprintf("dispatch run %s was cancelled before provider launch", request.Run.Record.ID)))
		}
		result, err := executeProvider(command, request.Prompt, request.Run, request.Root, request.NewCommand, persist)
		if err != nil {
			if isSafePreStartFailure(err) && attempt == 1 {
				if cleanupErr := clearPreStartCaptures(request.Run.Record); cleanupErr != nil {
					return finishDispatchFailure(request, cleanupErr)
				}
				continue
			}
			return finishDispatchFailure(request, err)
		}
		if request.Target.Name == AgentAntigravity {
			id, logErr := antigravitySessionID(command.LogPath)
			if logErr != nil {
				return finishDispatchFailure(request, wrapExitError(ExitTargetFailure, "read Antigravity dispatch log", logErr))
			}
			if id == "" {
				result.NotResumable = true
				request.Run.Record.NotResumable = true
				if _, err := fmt.Fprintf(request.Stderr, "[%s] antigravity · not resumable · agy %s · diagnostics: %s\n", session.Name, request.Version, command.LogPath); err != nil {
					return finishDispatchFailure(request, wrapExitError(ExitTargetFailure, "write Antigravity capability warning", err))
				}
			} else {
				if request.Mode == dispatchModeResume && id != session.ProviderSessionID {
					return finishDispatchFailure(request, exitError(ExitTargetFailure, "Antigravity resume returned a different provider conversation ID"))
				}
				if err := persist(id); err != nil {
					return finishDispatchFailure(request, err)
				}
				if err := os.Remove(command.LogPath); err != nil {
					return finishDispatchFailure(request, wrapExitError(ExitConfig, "remove successful Antigravity dispatch log", err))
				}
				request.Run.Record.ProviderLogPath = ""
			}
		}
		return completeDispatchSuccess(request, result, session)
	}
	return finishDispatchFailure(request, exitError(ExitTargetFailure, "dispatch retry exhausted"))
}

func completeDispatchSuccess(request dispatchExecution, result executionResult, session Session) error {
	if err := writeBytesAtomic(request.Run.Record.AnswerPath, []byte(result.Answer), 0o600); err != nil {
		return finishDispatchFailure(request, wrapExitError(ExitConfig, "publish dispatch terminal answer", err))
	}
	now := time.Now().UTC()
	request.Run.Record.State = dispatchStateCompleted
	if result.NotResumable {
		request.Run.Record.RecoveryState = recoveryNotResumable
	} else {
		request.Run.Record.RecoveryState = recoveryResumeRequired
	}
	request.Run.Record.CompletedAt = &now
	request.Run.Record.ProviderSessionID = session.ProviderSessionID
	if err := writeRunRecord(request.Run.Dir, &request.Run.Record); err != nil {
		cause := err
		if removeErr := os.Remove(request.Run.Record.AnswerPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			cause = wrapExitError(ExitConfig, "retract dispatch answer after terminal record failure", errors.Join(err, removeErr))
		}
		return finishDispatchFailure(request, cause)
	}
	if err := releaseConversation(request.Root, session.Name, request.Run.Record.ID); err != nil {
		// The completed run record is authoritative and claimConversation can
		// replace a stale claim that points at a terminal run. Surface cleanup
		// failure without turning a completed provider turn into a failed child.
		_, _ = fmt.Fprintf(request.Stderr, "warning: dispatch run %s completed but active claim cleanup failed: %v\n", request.Run.Record.ID, err)
	}
	return replayAnswer(request.Run.Record.AnswerPath, request.Stdout)
}

func finishDispatchFailure(request dispatchExecution, cause error) error {
	var terminationFailure *unprovenProviderTerminationError
	if errors.As(cause, &terminationFailure) {
		// The provider group may still be live. Keep both the nonterminal run
		// evidence and active claim so no replacement can overlap it.
		return cause
	}
	if current, err := loadRunRecord(request.Root, request.Run.Record.ID); err == nil && current.State == dispatchStateCancelled {
		return finishDispatchCancellation(request)
	}
	now := time.Now().UTC()
	request.Run.Record.State = dispatchStateFailed
	switch {
	case request.Run.Record.ProviderSessionID != "", request.Mode == dispatchModeResume:
		request.Run.Record.RecoveryState = recoveryAcceptanceUnknown
	case isSafePreStartFailure(cause):
		request.Run.Record.RecoveryState = recoveryRetrySafe
	default:
		request.Run.Record.RecoveryState = recoveryAcceptanceUnknown
	}
	request.Run.Record.CompletedAt = &now
	request.Run.Record.TerminalReason = cause.Error()
	// Release the active claim even when the terminal write fails, so a
	// persistence error cannot leave the conversation stuck on a failed run.
	writeErr := writeRunRecord(request.Run.Dir, &request.Run.Record)
	if err := releaseConversation(request.Root, request.Session.Name, request.Run.Record.ID); err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	// A proven pre-start failure means the provider never created a
	// conversation; do not leave a pre-persisted durable mapping (fresh
	// Claude runs) advertising a session that does not exist.
	if request.Mode == dispatchModeFresh && request.Run.Record.RecoveryState == recoveryRetrySafe {
		if err := downgradeUnstartedSession(request.Root, request.Session.Name, request.Run.Record.ID); err != nil {
			return err
		}
	}
	return cause
}

// finishDispatchCancellation is called only by the owning execution after no
// provider was launched or the provider wait path returned. That ownership
// boundary, rather than publication of the cancelled state, releases the
// conversation for another execution.
func finishDispatchCancellation(request dispatchExecution) error {
	if err := releaseConversation(request.Root, request.Session.Name, request.Run.Record.ID); err != nil {
		return err
	}
	return exitError(ExitTargetFailure, fmt.Sprintf("dispatch run %s was cancelled", request.Run.Record.ID))
}

func isSafePreStartFailure(err error) bool {
	var start *preStartFailure
	return errors.As(err, &start)
}

// clearPreStartCaptures removes only empty/private artifacts created before a
// provider process could start, allowing the one permitted retry to reserve
// its capture paths without erasing evidence from a running provider.
func clearPreStartCaptures(record RunRecord) error {
	for _, path := range []string{record.AnswerPath, record.StdoutPath, record.StderrPath, record.EventsPath, record.ProviderLogPath} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return wrapExitError(ExitConfig, "remove pre-start dispatch capture", err)
		}
	}
	return nil
}

type preStartFailure struct{ err error }

func (e *preStartFailure) Error() string { return e.err.Error() }
func (e *preStartFailure) Unwrap() error { return e.err }

func writeIdentity(stderr io.Writer, name string, agent string, mode string, durable bool) error {
	if stderr == nil {
		return nil
	}
	line := fmt.Sprintf("[%s] %s · %s", name, agent, map[string]string{dispatchModeFresh: dispatchModeFresh, dispatchModeResume: "resumed"}[mode])
	if durable {
		line += " · durable"
	}
	_, err := fmt.Fprintln(stderr, line)
	return err
}

func loadDispatchProject(root string, stderr io.Writer, env []string) (*config.ProjectConfig, io.Writer, []string, int, error) {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, nil, nil, 0, wrapExitError(ExitConfig, err.Error(), err)
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if env == nil {
		env = os.Environ()
	}
	depth, err := dispatchDepthFromEnv(env)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return project, stderr, env, depth, nil
}

func checkDispatchDepth(cfg config.Config, depth int) error {
	maxDepth := config.DispatchMaxDepth(cfg)
	if depth >= maxDepth {
		return exitError(ExitNested, fmt.Sprintf("nested dispatch is blocked at depth %d by dispatch.max_depth = %d; this agent is already running inside `al dispatch`, use the built-in subagent tool instead", depth, maxDepth))
	}
	return nil
}

func prepareFresh(project *config.ProjectConfig, resolved resolution, opts RunOptions) (targetMeta, string, []byte, error) {
	target := resolved.Target
	if strings.TrimSpace(opts.Model) != "" && !agentoptions.Supports(target.Name, agentoptions.KindModel) {
		return targetMeta{}, "", nil, exitError(ExitUsage, fmt.Sprintf("%s does not support --model", target.Name))
	}
	if strings.TrimSpace(opts.ReasoningEffort) != "" && !agentoptions.Supports(target.Name, agentoptions.KindReasoningEffort) {
		return targetMeta{}, "", nil, exitError(ExitUsage, fmt.Sprintf("%s does not support --reasoning-effort", target.Name))
	}
	if !targetEnabled(project.Config, target.Name) {
		return targetMeta{}, "", nil, exitError(ExitConfig, fmt.Sprintf("`al dispatch` target %s is disabled in config", target.Name))
	}
	version := resolved.Version
	if version == "" {
		lookPath := opts.LookPath
		if lookPath == nil {
			lookPath = exec.LookPath
		}
		path, err := lookPath(target.Binary)
		if err != nil {
			return targetMeta{}, "", nil, exitError(ExitUnavailable, fmt.Sprintf("`al dispatch` target %s requires `%s` on PATH", target.Name, target.Binary))
		}
		target, version, err = compatibleTargetVersionCached(project.Root, path, target, opts.VersionLookup)
		if err != nil {
			return targetMeta{}, "", nil, err
		}
	}
	promptText, err := ResolvePrompt(opts.PromptArgs, opts.Stdin, opts.ReadStdin)
	if err != nil {
		return targetMeta{}, "", nil, err
	}
	prompt, err := BuildChildPrompt(project, target.Name, promptText, opts.Skill)
	if err != nil {
		return targetMeta{}, "", nil, err
	}
	return target, version, prompt, nil
}

func prepareProjection(project *config.ProjectConfig, root string, stderr io.Writer, quiet bool) error {
	result, err := sync.RunWithProject(sync.RealSystem{}, root, project)
	if err != nil {
		return syncRunExitError(err)
	}
	if quiet || strings.EqualFold(strings.TrimSpace(project.Config.Warnings.NoiseMode), "quiet") {
		return nil
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintln(stderr, warning.String()); err != nil {
			return wrapExitError(ExitTargetFailure, "write dispatch sync warning", err)
		}
	}
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		if _, err := fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck); err != nil {
			return wrapExitError(ExitTargetFailure, "write dispatch approvals acknowledgement", err)
		}
	}
	return nil
}

// prepareTargetProjection makes the configured skills visible from the
// provider launch directory. Dispatch state and the canonical generated
// projection remain rooted at root; a distinct working directory receives a
// derived target-specific projection so native skill references resolve there.
func prepareTargetProjection(project *config.ProjectConfig, root string, workingDir string, target targetMeta) (string, error) {
	projectionRoot := workingDir
	if projectionRoot == "" {
		projectionRoot = root
	}
	if filepath.Clean(projectionRoot) == filepath.Clean(root) {
		return projectionRoot, nil
	}

	var err error
	if target.SharedSkillProject {
		err = sync.WriteAgentSkills(sync.RealSystem{}, projectionRoot, project.Skills)
	} else {
		err = sync.WriteClaudeSkills(sync.RealSystem{}, projectionRoot, project.Skills)
	}
	if err != nil {
		return "", syncRunExitError(err)
	}
	return projectionRoot, nil
}

func syncRunExitError(err error) *ExitError {
	if errors.Is(err, sync.ErrPostWriteLockCleanup) {
		return wrapExitError(ExitConfig, fmt.Sprintf(messages.DispatchRunSyncCleanupFailedFmt, err), err)
	}
	return wrapExitError(ExitConfig, fmt.Sprintf(messages.DispatchRunSyncFailedFmt, err), err)
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}

// dispatchDepthFromEnv preserves the three intentional nesting boundaries.
func dispatchDepthFromEnv(env []string) (int, error) {
	value, ok := clients.GetEnv(env, clients.EnvDispatchActive)
	if !ok {
		return 0, nil
	}
	depth, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || depth < 0 {
		return 0, exitError(ExitNested, fmt.Sprintf("invalid %s value %q; expected a non-negative integer dispatch depth", clients.EnvDispatchActive, value))
	}
	return depth, nil
}
