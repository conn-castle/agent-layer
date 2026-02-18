package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// agentEnabled returns true when the pointer is non-nil and true.
func agentEnabled(p *bool) bool {
	return p != nil && *p
}

func newVSCodeCmd() *cobra.Command {
	return newNoSyncLaunchCmd(
		messages.VSCodeUse,
		messages.VSCodeShort,
		"vscode",
		func(cfg *config.Config) *bool {
			v := agentEnabled(cfg.Agents.VSCode.Enabled) || agentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
			return &v
		},
		vscode.Launch,
	)
}
