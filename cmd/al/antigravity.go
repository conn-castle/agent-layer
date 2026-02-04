package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newAntigravityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.AntigravityUse,
		Short:              messages.AntigravityShort,
		DisableFlagParsing: true, // Pass all flags through to the underlying agent
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return clients.Run(root, "antigravity", func(cfg *config.Config) *bool {
				return cfg.Agents.Antigravity.Enabled
			}, antigravity.Launch, args)
		},
	}

	return cmd
}
