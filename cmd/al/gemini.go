package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/gemini"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newGeminiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.GeminiUse,
		Short:              messages.GeminiShort,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			passArgs := stripArgsSeparator(args)
			return clients.Run(cmd.Context(), root, "gemini", func(cfg *config.Config) *bool {
				return cfg.Agents.Gemini.Enabled
			}, gemini.Launch, passArgs, Version)
		},
	}

	return cmd
}
