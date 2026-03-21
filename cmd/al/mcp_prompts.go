package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMcpPromptsCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp-prompts",
		Short:  "Start the MCP prompt server (deprecated)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "al mcp-prompts is deprecated: skills are now synced natively. Run 'al sync' to update.")
			return nil
		},
	}
}
