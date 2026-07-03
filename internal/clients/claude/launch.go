package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the Claude Code CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := cfg.Config.Agents.Claude.Model
	if model != "" {
		args = append(args, "--model", model)
	}
	// Pass --effort only when there is no agent_specific effortLevel override.
	// agent_specific.effortLevel is written to settings.json and CLI args take
	// precedence over settings, so emitting --effort would shadow the override.
	// Trim so " max " is forwarded as "max".
	effort := strings.TrimSpace(cfg.Config.Agents.Claude.ReasoningEffort)
	if effort != "" && !config.HasProviderPassthroughKey(cfg.Config.Agents.Claude.AgentSpecific, "effortLevel") {
		args = append(args, "--effort", effort)
	}
	if cfg.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, passArgs...)

	if cfg.Config.Agents.Claude.LocalConfigDir != nil && *cfg.Config.Agents.Claude.LocalConfigDir {
		env = ensureClaudeConfigDir(cfg.Root, env)
	} else {
		env = clearStaleClaudeConfigDir(cfg.Root, env)
	}

	path, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, "claude", err)
	}

	argv := append([]string{"claude"}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, "claude", err)
	}
	return nil
}

// clearStaleClaudeConfigDir removes CLAUDE_CONFIG_DIR from the environment
// only when its value matches the repo-local path Agent Layer would have set.
// This prevents a stale value from leaking across repos while preserving any
// intentional user override that points elsewhere.
func clearStaleClaudeConfigDir(root string, env []string) []string {
	expected := filepath.Join(root, ".claude-config")
	current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if ok && clients.SamePath(current, expected) {
		return clients.UnsetEnv(env, "CLAUDE_CONFIG_DIR")
	}
	return env
}

func ensureClaudeConfigDir(root string, env []string) []string {
	expected := filepath.Join(root, ".claude-config")
	current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if !ok || current == "" {
		return clients.SetEnv(env, "CLAUDE_CONFIG_DIR", expected)
	}

	if !clients.SamePath(current, expected) {
		// Best-effort warning; a stderr write failure does not change the returned
		// env (the existing CLAUDE_CONFIG_DIR is preserved regardless).
		_, _ = fmt.Fprintf(os.Stderr, messages.ClientsClaudeConfigDirWarningFmt, current, expected)
	}

	return env
}
