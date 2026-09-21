package copilotcli

import (
	"fmt"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
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
	// Native Copilot discovers the shared root file independently of its own
	// projection. Exclude only generated IDs not selected for this client.
	for _, id := range projection.MuseSharedMCPExclusions(cfg.Config, projection.ClientCopilot) {
		args = append(args, "--disable-mcp-server", id)
	}
	args = append(args, passArgs...)

	path, err := exec.LookPath(executableName)
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, executableName, err)
	}

	argv := append([]string{executableName}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, executableName, err)
	}
	return nil
}
