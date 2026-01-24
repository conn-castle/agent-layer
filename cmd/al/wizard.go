package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func newWizardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.WizardUse,
		Short: messages.WizardShort,
		Long:  messages.WizardLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal() {
				return fmt.Errorf(messages.WizardRequiresTerminal)
			}

			root, err := resolveInitRoot()
			if err != nil {
				return err
			}
			pinned, err := resolvePinVersion("", Version)
			if err != nil {
				return err
			}
			return runWizard(root, pinned)
		},
	}
}

// isTerminal is a seam for tests to force non-interactive behavior.
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
