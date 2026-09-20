package muse

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

const (
	// ExecutableName is the Muse Code CLI binary.
	ExecutableName = "muse"
	// SupportedVersion is the Muse Code version covered by integration evidence.
	SupportedVersion = "1.3.0"
	// EnvConfigHome is Muse's XDG configuration root selector.
	EnvConfigHome = "XDG_CONFIG_HOME"
	// EnvDataHome is Muse's XDG data root selector.
	EnvDataHome = "XDG_DATA_HOME"
)

var execFunc = clients.ExecHandoff

// ConfigRoot returns Muse's private repo-local XDG configuration root.
func ConfigRoot(root string) string { return filepath.Join(root, ".muse-config") }

// DataRoot returns Muse's private repo-local XDG data root.
func DataRoot(root string) string { return filepath.Join(root, ".muse-data") }

// EnsureHomes creates or tightens Muse's private repo-local roots.
func EnsureHomes(root string) error {
	for _, path := range []string{ConfigRoot(root), filepath.Join(ConfigRoot(root), "muse"), DataRoot(root), filepath.Join(DataRoot(root), "muse")} {
		if err := fsutil.EnsurePrivateDir(path); err != nil {
			return fmt.Errorf("prepare Muse private directory %s: %w", path, err)
		}
	}
	return nil
}

// ConfigureEnvironment applies the shared launch, dispatch, and discovery XDG roots.
func ConfigureEnvironment(root string, env []string, warning io.Writer) []string {
	for key, expected := range map[string]string{EnvConfigHome: ConfigRoot(root), EnvDataHome: DataRoot(root)} {
		if current, ok := clients.GetEnv(env, key); ok && current != "" && !clients.SamePath(current, expected) && warning != nil {
			_, _ = fmt.Fprintf(warning, "warning: overriding %s=%s for Muse with repo-local %s; Muse child tools inherit this value\n", key, current, expected)
		}
		env = clients.SetEnv(env, key, expected)
	}
	return env
}

// BaseArgs returns common interactive Muse flags for trust and approvals.
func BaseArgs(root string, cfg config.Config) []string {
	args := []string{"--workspace", root, "--trust-workspace"}
	if model := strings.TrimSpace(cfg.Agents.Muse.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := strings.TrimSpace(cfg.Agents.Muse.ReasoningEffort); effort != "" {
		args = append(args, "--reasoning-effort", effort)
	}
	if cfg.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--yolo")
	} else {
		args = append(args, "--approval-judge", "off")
	}
	return args
}

// Launch replaces the current process with Muse Code for the project.
func Launch(project *config.ProjectConfig, _ *run.Info, env []string, passArgs []string) error {
	path, err := exec.LookPath(ExecutableName)
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, ExecutableName, err)
	}
	if err := EnsureHomes(project.Root); err != nil {
		return err
	}
	args := BaseArgs(project.Root, project.Config)
	args = append(args, passArgs...)
	env = ConfigureEnvironment(project.Root, env, os.Stderr)
	if err := execFunc(path, append([]string{ExecutableName}, args...), env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, ExecutableName, err)
	}
	return nil
}
