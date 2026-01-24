package codex

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

// Launch starts the Codex CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string) error {
	args := []string{}

	env = ensureCodexHome(cfg.Root, env)

	cmd := exec.Command("codex", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsCodexExitErrorFmt, err)
	}

	return nil
}

func ensureCodexHome(root string, env []string) []string {
	expected := filepath.Join(root, ".codex")
	current, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || current == "" {
		return clients.SetEnv(env, "CODEX_HOME", expected)
	}

	if !samePath(current, expected) {
		if _, err := fmt.Fprintf(os.Stderr, messages.ClientsCodexHomeWarningFmt, current, expected); err != nil {
			return env
		}
	}

	return env
}

func samePath(a string, b string) bool {
	aResolved := resolvePath(a)
	bResolved := resolvePath(b)
	return aResolved == bResolved
}

func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return eval
	}
	return abs
}
