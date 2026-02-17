package claude

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// Launch starts the Claude Code CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := cfg.Config.Agents.Claude.Model
	if model != "" {
		args = append(args, "--model", model)
	}
	if cfg.Config.Approvals.Mode == "yolo" {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, passArgs...)

	cmd := exec.Command("claude", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsClaudeExitErrorFmt, err)
	}

	return nil
}
