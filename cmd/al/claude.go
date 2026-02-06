package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newClaudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.ClaudeUse,
		Short:              messages.ClaudeShort,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			passArgs := stripArgsSeparator(args)
			return clients.Run(cmd.Context(), root, "claude", func(cfg *config.Config) *bool {
				return cfg.Agents.Claude.Enabled
			}, claude.Launch, passArgs, Version)
		},
	}

	return cmd
}
