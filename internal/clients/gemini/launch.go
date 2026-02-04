package gemini

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// Launch starts the Gemini CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
	cmdArgs := []string{}
	model := cfg.Config.Agents.Gemini.Model
	if model != "" {
		cmdArgs = append(cmdArgs, "--model", model)
	}
	// Append any additional arguments passed from the CLI
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("gemini", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsGeminiExitErrorFmt, err)
	}

	return nil
}
