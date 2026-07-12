package agentdispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/sync"
)

// Run executes one Agent Dispatch target according to the v1 dispatch contract.
func Run(opts RunOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}

	project, err := config.LoadProjectConfig(opts.Root)
	if err != nil {
		return wrapExitError(ExitConfig, err.Error(), err)
	}
	currentDepth, err := dispatchDepthFromEnv(env)
	if err != nil {
		return err
	}
	maxDepth := config.DispatchMaxDepth(project.Config)
	if currentDepth >= maxDepth {
		return exitError(ExitNested, fmt.Sprintf(messages.DispatchNestedActiveFmt, currentDepth, maxDepth))
	}
	infoStderr := stderr
	if opts.Quiet || strings.EqualFold(strings.TrimSpace(project.Config.Warnings.NoiseMode), "quiet") {
		infoStderr = io.Discard
	}
	caller, callerKnown := knownCallerFromEnv(env)
	resolved, err := resolveTarget(project.Config, opts, caller, callerKnown)
	if err != nil {
		return err
	}
	target := resolved.Target
	if strings.TrimSpace(opts.Model) != "" && !agentoptions.Supports(target.Name, agentoptions.KindModel) {
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnsupportedModelFmt, target.Name))
	}
	if strings.TrimSpace(opts.ReasoningEffort) != "" && !agentoptions.Supports(target.Name, agentoptions.KindReasoningEffort) {
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnsupportedReasoningEffortFmt, target.Name))
	}
	if !targetEnabled(project.Config, target.Name) {
		return exitError(ExitConfig, fmt.Sprintf(messages.DispatchDisabledTargetFmt, target.Name))
	}
	binaryPath, err := opts.LookPath(target.Binary)
	if err != nil {
		return exitError(ExitUnavailable, fmt.Sprintf(messages.DispatchMissingBinaryFmt, target.Name, target.Binary))
	}
	target.Binary = binaryPath
	if resolved.Notice != "" {
		_, _ = fmt.Fprintln(infoStderr, resolved.Notice)
	}

	prompt, err := ResolvePrompt(opts.PromptArgs, opts.Stdin, opts.ReadStdin)
	if err != nil {
		return err
	}
	childPrompt, err := BuildChildPrompt(project, target.Name, prompt, opts.Skill)
	if err != nil {
		return err
	}

	result, err := sync.RunWithProject(sync.RealSystem{}, opts.Root, project)
	if err != nil {
		return syncRunExitError(err)
	}
	for _, warning := range result.Warnings {
		_, _ = fmt.Fprintln(infoStderr, warning.String())
	}
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		_, _ = fmt.Fprintln(infoStderr, messages.WarningsPolicyYOLOAck)
	}

	if err := validateSkillProjection(project.Root, target, opts.Skill); err != nil {
		return err
	}
	runInfo, err := run.Create(opts.Root)
	if err != nil {
		return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchRunCreateFailedFmt, err), err)
	}
	childEnv := clients.BuildEnvForAgent(env, project.Env, runInfo, target.Name)
	childEnv = clients.SetEnv(childEnv, clients.EnvDispatchActive, strconv.Itoa(currentDepth+1))
	opts.Stdout = stdout
	opts.Stderr = stderr
	return runTarget(target, project, childEnv, childPrompt, opts)
}

func syncRunExitError(err error) *ExitError {
	if errors.Is(err, sync.ErrPostWriteLockCleanup) {
		return wrapExitError(ExitConfig, fmt.Sprintf(messages.DispatchRunSyncCleanupFailedFmt, err), err)
	}
	return wrapExitError(ExitConfig, fmt.Sprintf(messages.DispatchRunSyncFailedFmt, err), err)
}

func dispatchDepthFromEnv(env []string) (int, error) {
	value, ok := clients.GetEnv(env, clients.EnvDispatchActive)
	if !ok {
		return 0, nil
	}
	depth, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || depth < 0 {
		return 0, exitError(ExitNested, fmt.Sprintf(messages.DispatchActiveDepthInvalidFmt, clients.EnvDispatchActive, value))
	}
	return depth, nil
}
