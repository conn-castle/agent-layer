package codex

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the Codex CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := append([]string{}, passArgs...)

	env = ConfigureEnvironment(cfg.Root, env, cfg.Config.Agents.Codex, os.Stderr)

	path, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, "codex", err)
	}

	argv := append([]string{"codex"}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, "codex", err)
	}
	return nil
}

// ConfigureEnvironment applies the Codex environment rules shared by the
// interactive launcher and headless Agent Dispatch.
func ConfigureEnvironment(root string, env []string, cfg config.CodexConfig, warning io.Writer) []string {
	if !config.CodexLocalConfigDirEnabled(cfg) {
		return env
	}

	expected := filepath.Join(root, ".codex")
	current, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || current == "" {
		return clients.SetEnv(env, "CODEX_HOME", expected)
	}
	if !clients.SamePath(current, expected) && warning != nil {
		_, _ = fmt.Fprintf(warning, messages.ClientsCodexHomeWarningFmt, current, expected)
	}
	return env
}

func configureCodexHome(root string, env []string, cfg config.CodexConfig) []string {
	return ConfigureEnvironment(root, env, cfg, os.Stderr)
}
