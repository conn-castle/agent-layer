package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
)

// ErrSyncCompletedWithWarnings is returned when sync completes but warnings were generated.
var ErrSyncCompletedWithWarnings = errors.New(messages.SyncCompletedWithWarnings)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.SyncUse,
		Short: messages.SyncShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			project, err := config.LoadProjectConfig(root)
			if err != nil {
				return err
			}
			if project.Config.Warnings.VersionUpdateOnSync != nil && *project.Config.Warnings.VersionUpdateOnSync {
				updatewarn.WarnIfOutdated(cmd.Context(), Version, cmd.ErrOrStderr())
			}
			warnings, err := sync.RunWithProject(sync.RealSystem{}, root, project)
			if err != nil {
				return err
			}
			if len(warnings) > 0 {
				hasErrors := false
				for _, w := range warnings {
					fmt.Fprintln(os.Stderr, w.String())
					if !w.NoiseSuppressible {
						hasErrors = true
					}
				}
				if hasErrors {
					return ErrSyncCompletedWithWarnings
				}
			}
			if project.Config.Approvals.Mode == "yolo" {
				fmt.Fprintln(os.Stderr, messages.WarningsPolicyYOLOAck)
			}
			return nil
		},
	}

	return cmd
}
