package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/mcp"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var runPromptServer = mcp.RunPromptServer

func newMcpPromptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.McpPromptsUse,
		Short: messages.McpPromptsShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			project, err := config.LoadProjectConfig(root)
			if err != nil {
				return err
			}
			return runPromptServer(context.Background(), Version, project.SlashCommands)
		},
	}

	return cmd
}
