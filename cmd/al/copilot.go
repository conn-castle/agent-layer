package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/copilotcli"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newCopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.CopilotUse,
		Short:              messages.CopilotShort,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			quiet, passArgs, err := splitQuietArgs(args)
			if err != nil {
				return err
			}
			return clients.Run(cmd.Context(), root, "copilot", func(cfg *config.Config) *bool {
				return cfg.Agents.CopilotCLI.Enabled
			}, copilotcli.Launch, quiet, passArgs, Version)
		},
	}

	return cmd
}
