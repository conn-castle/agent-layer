package copilotcli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/run"
)

const flagYOLO = "--yolo"

const (
	flagModel               = "--model"
	flagAdditionalMCPConfig = "--additional-mcp-config"
	flagDisableMCPServer    = "--disable-mcp-server"
)

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// Launch starts the GitHub Copilot CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := cfg.Config.Agents.CopilotCLI.Model
	if model != "" {
		args = append(args, flagModel, model)
	}
	switch cfg.Config.Approvals.Mode {
	case config.ApprovalModeYOLO:
		args = append(args, flagYOLO)
	case config.ApprovalModeAll:
		args = append(args, "--allow-all-tools")
	}
	// Native Copilot does not discover the generated project config, so load it
	// explicitly for this session.
	args = append(args, flagAdditionalMCPConfig, "@"+filepath.Join(cfg.Root, ".copilot", "mcp-config.json"))
	// Native Copilot discovers the shared root file independently of its own
	// projection. Exclude only generated IDs not selected for this client.
	for _, id := range projection.RootMCPExclusions(cfg.Config, projection.ClientCopilot) {
		args = append(args, flagDisableMCPServer, id)
	}
	// Native `copilot --help` documents both MCP options as repeatable. New
	// entries augment the generated config/exclusions instead of replacing them.
	args = clients.MergeArgs(args, passArgs, map[string]string{"--allow-all": flagYOLO},
		[]string{flagModel, flagAdditionalMCPConfig, flagDisableMCPServer},
		flagAdditionalMCPConfig, flagDisableMCPServer)

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
