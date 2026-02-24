package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
	"github.com/conn-castle/agent-layer/internal/warnings"
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
			quietFlag, _ := cmd.Flags().GetBool("quiet")
			project, err := config.LoadProjectConfig(root)
			if err != nil {
				return err
			}
			effectiveQuiet := quietFlag || strings.EqualFold(strings.TrimSpace(project.Config.Warnings.NoiseMode), warnings.NoiseModeQuiet)
			stderr := cmd.ErrOrStderr()
			if effectiveQuiet {
				stderr = io.Discard
			}
			if project.Config.Warnings.VersionUpdateOnSync != nil && *project.Config.Warnings.VersionUpdateOnSync {
				updatewarn.WarnIfOutdated(cmd.Context(), Version, stderr)
			}
			result, err := sync.RunWithProject(sync.RealSystem{}, root, project)
			if err != nil {
				return err
			}

			if len(result.AllWarnings) > 0 {
				if effectiveQuiet {
					return &SilentExitError{Code: 1}
				}
				if len(result.Warnings) > 0 {
					for _, w := range result.Warnings {
						_, _ = fmt.Fprintln(stderr, w.String())
					}
					return ErrSyncCompletedWithWarnings
				}
			}
			if project.Config.Approvals.Mode == "yolo" {
				_, _ = fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck)
			}
			return nil
		},
	}

	return cmd
}
