package codex

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/herdr"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the Codex CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := append([]string{}, passArgs...)

	if runInfo != nil {
		if err := herdr.CaptureLaunch(cfg.Root, runInfo.Dir, env, "codex"); err != nil {
			return fmt.Errorf("prepare Codex HerdR recovery: %w", err)
		}
	}
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
	selected := current
	if !filepath.IsAbs(current) {
		// Launch, dispatch, and discovery children run in different working
		// directories, so a relative home is passed on as an absolute path.
		selected = relativeHome(root, current)
		env = clients.SetEnv(env, "CODEX_HOME", selected)
	}
	if !clients.SamePath(selected, expected) && warning != nil {
		_, _ = fmt.Fprintf(warning, messages.ClientsCodexHomeWarningFmt, current, expected)
	}
	return env
}

// relativeHome resolves a relative CODEX_HOME from the caller's directory.
// Because al runs from any project subfolder, .codex also selects the project
// home when nothing exists at the caller-relative path.
func relativeHome(root, current string) string {
	expected := filepath.Join(root, ".codex")
	home, err := filepath.Abs(current)
	if err != nil {
		return current
	}
	// Compare parents: symlink resolution cannot match aliases of a missing leaf.
	if filepath.Base(home) == ".codex" && clients.SamePath(filepath.Dir(home), root) {
		return expected
	}
	if filepath.Clean(current) == ".codex" {
		if _, err := os.Lstat(home); errors.Is(err, fs.ErrNotExist) {
			return expected
		}
	}
	return home
}
