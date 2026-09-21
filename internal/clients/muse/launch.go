package muse

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

const (
	// ExecutableName is the Muse Code CLI binary.
	ExecutableName = "muse"
	// SupportedVersion is the Muse Code version covered by integration evidence.
	SupportedVersion = "1.3.0"
)

var execFunc = clients.ExecHandoff

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
		args = append(args, "--approval-mode", "untrusted", "--approval-judge", "off")
	}
	return args
}

// Launch replaces the current process with Muse Code for the project.
func Launch(project *config.ProjectConfig, _ *run.Info, env []string, passArgs []string) error {
	path, err := exec.LookPath(ExecutableName)
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, ExecutableName, err)
	}
	args := BaseArgs(project.Root, project.Config)
	args = append(args, passArgs...)
	if err := execFunc(path, append([]string{ExecutableName}, args...), env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, ExecutableName, err)
	}
	return nil
}
