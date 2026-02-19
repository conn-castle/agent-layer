package main

import (
	"errors"
	"fmt"

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
			result, err := sync.RunWithProject(sync.RealSystem{}, root, project)
			if err != nil {
				return err
			}

			stderr := cmd.ErrOrStderr()
			if len(result.Warnings) > 0 {
				for _, w := range result.Warnings {
					_, _ = fmt.Fprintln(stderr, w.String())
				}
				return ErrSyncCompletedWithWarnings
			}
			if project.Config.Approvals.Mode == "yolo" {
				_, _ = fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck)
			}
			return nil
		},
	}

	return cmd
}
