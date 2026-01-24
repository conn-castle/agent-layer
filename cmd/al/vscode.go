package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newVSCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.VSCodeUse,
		Short: messages.VSCodeShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return clients.Run(root, "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, vscode.Launch)
		},
	}

	return cmd
}
