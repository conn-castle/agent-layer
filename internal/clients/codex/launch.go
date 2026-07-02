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

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the Codex CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := append([]string{}, passArgs...)

	env = configureCodexHome(cfg.Root, env, cfg.Config.Agents.Codex)

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

func configureCodexHome(root string, env []string, cfg config.CodexConfig) []string {
	if !config.CodexLocalConfigDirEnabled(cfg) {
		return env
	}

	expected := filepath.Join(root, ".codex")
	current, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || current == "" {
		return clients.SetEnv(env, "CODEX_HOME", expected)
	}

	if !clients.SamePath(current, expected) {
		// Best-effort warning; a stderr write failure does not change the returned
		// env (the existing CODEX_HOME is preserved regardless).
		_, _ = fmt.Fprintf(os.Stderr, messages.ClientsCodexHomeWarningFmt, current, expected)
	}

	return env
}
