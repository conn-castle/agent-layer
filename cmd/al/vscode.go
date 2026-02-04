package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newVSCodeCmd() *cobra.Command {
	var noSync bool
	cmd := &cobra.Command{
		Use:   messages.VSCodeUse,
		Short: messages.VSCodeShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if noSync {
				return runVSCodeNoSync(root, args)
			}
			return clients.Run(root, "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, vscode.Launch, args)
		},
	}

	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip sync before launching VS Code")

	return cmd
}

// runVSCodeNoSync loads project config and launches VS Code without running sync.
// root is the repo root; returns any load, validation, or launch error.
func runVSCodeNoSync(root string, args []string) error {
	return clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		return cfg.Agents.VSCode.Enabled
	}, vscode.Launch, args)
}
