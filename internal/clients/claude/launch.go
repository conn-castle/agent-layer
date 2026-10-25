package claude

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/herdr"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

const (
	flagModel  = "--model"
	flagEffort = "--effort"
)

// Launch starts the Claude Code CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := cfg.Config.Agents.Claude.Model
	if model != "" {
		args = append(args, flagModel, model)
	}
	// Pass --effort only when there is no agent_specific effortLevel override.
	// agent_specific.effortLevel is written to settings.json and CLI args take
	// precedence over settings, so emitting --effort would shadow the override.
	// Trim so " max " is forwarded as "max".
	effort := strings.TrimSpace(cfg.Config.Agents.Claude.ReasoningEffort)
	if effort != "" && !config.HasProviderPassthroughKey(cfg.Config.Agents.Claude.AgentSpecific, "effortLevel") {
		args = append(args, flagEffort, effort)
	}
	if cfg.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = clients.MergeArgs(args, passArgs, nil, []string{flagModel, flagEffort})

	if runInfo != nil {
		if err := herdr.CaptureLaunch(cfg.Root, runInfo.Dir, env, "claude"); err != nil {
			return fmt.Errorf("prepare Claude HerdR recovery: %w", err)
		}
	}
	env = ConfigureEnvironment(cfg.Root, env, cfg.Config.Agents.Claude, os.Stderr)

	path, err := exec.LookPath(executableName)
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, executableName, err)
	}

	argv := append([]string{executableName}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, executableName, err)
	}
	return nil
}

// ConfigureEnvironment applies the Claude environment rules shared by
// interactive launching and headless Agent Dispatch. warning receives a
// conflict message when a caller supplied a non-repository config directory.
func ConfigureEnvironment(root string, env []string, cfg config.ClaudeConfig, warning io.Writer) []string {
	if cfg.LocalConfigDir != nil && *cfg.LocalConfigDir {
		return ensureClaudeConfigDirWithWarning(root, env, warning)
	}
	return ClearStaleConfigDir(root, env)
}

// ClearStaleConfigDir removes CLAUDE_CONFIG_DIR from the environment only when
// its value selects the repo-local path Agent Layer would have set. This
// prevents a stale value from leaking across repos while preserving any
// intentional user override that points elsewhere.
func ClearStaleConfigDir(root string, env []string) []string {
	current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if ok && current != "" && clients.SamePath(selectedConfigDir(root, current), filepath.Join(root, configDirName)) {
		return clients.UnsetEnv(env, "CLAUDE_CONFIG_DIR")
	}
	return env
}

func ensureClaudeConfigDirWithWarning(root string, env []string, warning io.Writer) []string {
	expected := filepath.Join(root, configDirName)
	current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if !ok || current == "" {
		return clients.SetEnv(env, "CLAUDE_CONFIG_DIR", expected)
	}

	selected := selectedConfigDir(root, current)
	if selected != current {
		// Launch, dispatch, and discovery children run in different working
		// directories, so a relative config directory is passed on as an
		// absolute path.
		env = clients.SetEnv(env, "CLAUDE_CONFIG_DIR", selected)
	}
	if !clients.SamePath(selected, expected) {
		// Best-effort warning; a stderr write failure does not change the returned
		// env (the user's CLAUDE_CONFIG_DIR is kept, made absolute if relative).
		if warning != nil {
			_, _ = fmt.Fprintf(warning, messages.ClientsClaudeConfigDirWarningFmt, current, expected)
		}
	}

	return env
}

// selectedConfigDir returns the directory Claude would use for current, with a
// relative value resolved as al resolves it from any project subfolder.
func selectedConfigDir(root, current string) string {
	if filepath.IsAbs(current) {
		return current
	}
	return clients.ResolveRelativeHome(root, current, configDirName)
}
