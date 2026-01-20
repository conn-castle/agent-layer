package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newWizardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wizard",
		Short: "Interactive setup wizard",
		Long:  `Run an interactive wizard to configure Agent Layer for this repository.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal() {
				return fmt.Errorf("Agent Layer wizard requires an interactive terminal")
			}

			root, err := getwd()
			if err != nil {
				return err
			}
			return runWizard(root)
		},
	}
}

// isTerminal is a seam for tests to force non-interactive behavior.
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
