package vscode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// Launch starts VS Code with CODEX_HOME set for the repo.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
	codexHome := filepath.Join(cfg.Root, ".codex")
	env = clients.SetEnv(env, "CODEX_HOME", codexHome)

	cmdArgs := []string{"."}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("code", cmdArgs...)
	cmd.Dir = cfg.Root
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsVSCodeExitErrorFmt, err)
	}

	return nil
}
