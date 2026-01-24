package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           messages.RootUse,
		Short:         messages.RootShort,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			showVersion, _ := cmd.Flags().GetBool("version")
			if showVersion {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), cmd.Version); err != nil {
					return err
				}
				return nil
			}
			return cmd.Help()
		},
	}

	root.Flags().Bool("version", false, messages.RootVersionFlag)

	root.AddCommand(
		newInitCmd(),
		newSyncCmd(),
		newMcpPromptsCmd(),
		newGeminiCmd(),
		newClaudeCmd(),
		newCodexCmd(),
		newVSCodeCmd(),
		newAntigravityCmd(),
		newDoctorCmd(),
		newWizardCmd(),
	)
	addPlatformCommands(root)
	return root
}
