package antigravity

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

const disableAutoUpdateEnv = "AGY_CLI_DISABLE_AUTO_UPDATE"

// lookPathFunc is overridable for tests that need to simulate `agy` missing
// from PATH without manipulating the test process's actual PATH.
var lookPathFunc = exec.LookPath

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts Antigravity through agy with a repo-local --gemini_dir.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	if !filepath.IsAbs(cfg.Root) {
		return fmt.Errorf(messages.ClientsAntigravityRelativeRootFmt, cfg.Root)
	}
	// Preflight `agy` discovery BEFORE creating `.agy/` so a missing-binary
	// failure does not pollute the user's repo with a stray directory
	// (Round 2 F-B2-5).
	agyPath, err := lookPathFunc("agy")
	if err != nil {
		return fmt.Errorf(messages.ClientsAntigravityBinaryNotFoundFmt, err)
	}
	args, err := BaseArgs(cfg.Root, cfg.Config)
	if err != nil {
		return err
	}
	args = append(args, passArgs...)
	env = ConfigureEnvironment(env)

	argv := append([]string{"agy"}, args...)
	if err := execFunc(agyPath, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, "antigravity", err)
	}
	return nil
}

// BaseArgs prepares the documented Antigravity configuration arguments shared
// by interactive launch and headless dispatch. It creates only the provider's
// repository-local configuration directory.
func BaseArgs(root string, cfg config.Config) ([]string, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf(messages.ClientsAntigravityRelativeRootFmt, root)
	}
	geminiDir := filepath.Join(root, ".agy")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		return nil, fmt.Errorf(messages.ClientsAntigravityMkdirFailedFmt, geminiDir, err)
	}
	args := []string{"--gemini_dir=" + geminiDir}
	if cfg.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args, nil
}

// ConfigureEnvironment applies Antigravity's documented auto-update guard.
func ConfigureEnvironment(env []string) []string {
	return clients.SetEnv(env, disableAutoUpdateEnv, "1")
}
