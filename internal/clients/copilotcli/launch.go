package copilotcli

import (
	"fmt"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the GitHub Copilot CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := cfg.Config.Agents.CopilotCLI.Model
	if model != "" {
		args = append(args, "--model", model)
	}
	switch cfg.Config.Approvals.Mode {
	case config.ApprovalModeYOLO:
		args = append(args, "--yolo")
	case config.ApprovalModeAll:
		args = append(args, "--allow-all-tools")
	}
	args = append(args, passArgs...)

	path, err := exec.LookPath("copilot")
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, "copilot", err)
	}

	argv := append([]string{"copilot"}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, "copilot", err)
	}
	return nil
}
