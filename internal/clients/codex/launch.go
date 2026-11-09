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

// homeDirName is the repo-local CODEX_HOME directory under the project root.
const homeDirName = ".codex"

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
// interactive launcher, headless Agent Dispatch, and model discovery.
func ConfigureEnvironment(root string, env []string, cfg config.CodexConfig, warning io.Writer) []string {
	if !config.CodexLocalConfigDirEnabled(cfg) {
		return env
	}

	expected := filepath.Join(root, homeDirName)
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

// EnsureSelectedHome creates the repo-local CODEX_HOME when local config is
// enabled and env selects it, because native Codex refuses to start with a
// missing home. Existing homes, including symlinked directories, are left
// unchanged; inherited homes elsewhere are never created. Callers pass the
// environment ConfigureEnvironment returned.
func EnsureSelectedHome(root string, env []string, cfg config.CodexConfig) error {
	home := filepath.Join(root, homeDirName)
	// Compare parents: symlink resolution cannot match aliases of a missing leaf.
	current, ok := clients.GetEnv(env, "CODEX_HOME")
	current = filepath.Clean(current)
	if !config.CodexLocalConfigDirEnabled(cfg) || !ok || filepath.Base(current) != homeDirName || !clients.SamePath(filepath.Dir(current), root) {
		return nil
	}
	err := os.Mkdir(home, 0o700)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf(messages.ClientsCodexHomeEnsureFailedFmt, home, err)
	}
	info, err := os.Stat(home)
	if err == nil && info.IsDir() {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf(messages.ClientsCodexHomeEnsureFailedFmt, home, err)
	}
	// A non-directory or a dangling symlink occupies the home path.
	return fmt.Errorf(messages.ClientsCodexHomeNotDirectoryFmt, home)
}

// relativeHome resolves a relative CODEX_HOME from the caller's directory.
// Because al runs from any project subfolder, .codex also selects the project
// home when nothing exists at the caller-relative path.
func relativeHome(root, current string) string {
	expected := filepath.Join(root, homeDirName)
	home, err := filepath.Abs(current)
	if err != nil {
		return current
	}
	// Compare parents: symlink resolution cannot match aliases of a missing leaf.
	if filepath.Base(home) == homeDirName && clients.SamePath(filepath.Dir(home), root) {
		return expected
	}
	if filepath.Clean(current) == homeDirName {
		if _, err := os.Lstat(home); errors.Is(err, fs.ErrNotExist) {
			return expected
		}
	}
	return home
}
