package main

import (
	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/wizard"
)

func newWizardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wizard",
		Short: "Interactive setup wizard",
		Long:  `Run an interactive wizard to configure Agent Layer for this repository.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := getwd()
			if err != nil {
				return err
			}
			return wizard.Run(cmd.Context(), root)
		},
	}
}
