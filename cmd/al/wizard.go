package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/terminal"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
	"github.com/conn-castle/agent-layer/internal/wizard"
)

var runWizardProfile = func(root string, pinVersion string, profilePath string, apply bool, out io.Writer) error {
	return wizard.RunProfile(root, alsync.Run, pinVersion, profilePath, apply, out)
}

var cleanupWizardBackups = wizard.CleanupBackups

func newWizardCmd() *cobra.Command {
	var profilePath string
	var yes bool
	var cleanupBackups bool

	cmd := &cobra.Command{
		Use:   messages.WizardUse,
		Short: messages.WizardShort,
		Long:  messages.WizardLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveInitRoot()
			if err != nil {
				return err
			}

			if cleanupBackups {
				removed, cleanErr := cleanupWizardBackups(root)
				if cleanErr != nil {
					return cleanErr
				}
				if len(removed) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.WizardCleanupBackupsNone)
					return nil
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.WizardCleanupBackupsHeader)
				for _, path := range removed {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), messages.WizardCleanupBackupsPathFmt, path)
				}
				return nil
			}

			updatewarn.WarnIfOutdated(cmd.Context(), Version, cmd.ErrOrStderr())

			pinned, err := resolvePinVersion("", Version)
			if err != nil {
				return err
			}

			if profilePath != "" {
				return runWizardProfile(root, pinned, profilePath, yes, cmd.OutOrStdout())
			}

			if !isTerminal() {
				return fmt.Errorf(messages.WizardRequiresTerminal)
			}

			return runWizard(root, pinned)
		},
	}

	cmd.Flags().StringVar(&profilePath, "profile", "", messages.WizardProfileFlagHelp)
	cmd.Flags().BoolVar(&yes, "yes", false, messages.WizardProfileYesFlagHelp)
	cmd.Flags().BoolVar(&cleanupBackups, "cleanup-backups", false, messages.WizardCleanupBackupsFlagHelp)
	return cmd
}

// isTerminal is a seam for tests to force non-interactive behavior.
// The default implementation uses terminal.IsInteractive().
var isTerminal = terminal.IsInteractive
