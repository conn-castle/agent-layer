package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/terminal"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
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

			updatewarn.WarnIfOutdated(cmd.Context(), Version, cmd.ErrOrStderr())

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
// The default implementation uses terminal.IsInteractive().
var isTerminal = terminal.IsInteractive
