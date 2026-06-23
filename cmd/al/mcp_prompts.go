package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func newMcpPromptsCmd() *cobra.Command {
	return &cobra.Command{
		Use:    messages.McpPromptsUse,
		Short:  messages.McpPromptsShort,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), messages.McpPromptsDeprecated)
			return nil
		},
	}
}
