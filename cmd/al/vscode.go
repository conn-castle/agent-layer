package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newVSCodeCmd() *cobra.Command {
	return newNoSyncLaunchCmd(
		messages.VSCodeUse,
		messages.VSCodeShort,
		"vscode",
		func(cfg *config.Config) *bool {
			v := config.IsAgentEnabled(cfg.Agents.VSCode.Enabled) || config.IsAgentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
			return &v
		},
		vscode.Launch,
	)
}
