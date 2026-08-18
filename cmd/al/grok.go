package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newGrokCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.GrokUse,
		Short:              messages.GrokShort,
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
			return clients.RunWithStderr(cmd.Context(), root, "grok", func(cfg *config.Config) *bool {
				return cfg.Agents.Grok.Enabled
			}, grok.Launch, quiet, passArgs, Version, cmd.ErrOrStderr())
		},
	}

	return cmd
}
