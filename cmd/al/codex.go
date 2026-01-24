package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newCodexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.CodexUse,
		Short: messages.CodexShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return clients.Run(root, "codex", func(cfg *config.Config) *bool {
				return cfg.Agents.Codex.Enabled
			}, codex.Launch)
		},
	}

	return cmd
}
