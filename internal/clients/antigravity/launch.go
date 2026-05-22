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

// Launch starts Antigravity through agy with a repo-local --gemini_dir.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	if !filepath.IsAbs(cfg.Root) {
		return fmt.Errorf(messages.ClientsAntigravityRelativeRootFmt, cfg.Root)
	}
	// filepath.Join with an absolute first segment always yields an absolute
	// path; the prior explicit `IsAbs(geminiDir)` check after this point
	// could never fire (Round 2 F-A2-3).
	geminiDir := filepath.Join(cfg.Root, ".agy")
	// Preflight `agy` discovery BEFORE creating `.agy/` so a missing-binary
	// failure does not pollute the user's repo with a stray directory
	// (Round 2 F-B2-5).
	agyPath, err := lookPathFunc("agy")
	if err != nil {
		return fmt.Errorf(messages.ClientsAntigravityBinaryNotFoundFmt, err)
	}
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		return fmt.Errorf(messages.ClientsAntigravityMkdirFailedFmt, geminiDir, err)
	}

	args := []string{"--gemini_dir=" + geminiDir}
	if cfg.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, passArgs...)
	env = clients.SetEnv(env, disableAutoUpdateEnv, "1")

	// #nosec G204 -- agyPath is resolved from lookPathFunc, the launcher's only entrypoint.
	cmd := exec.Command(agyPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsAntigravityExitErrorFmt, err)
	}

	return nil
}
