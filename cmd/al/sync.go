package main

import (
	"errors"
	"fmt"
	"io"
	"os"
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			quietFlag, _ := cmd.Flags().GetBool("quiet")
			// Only base configuration is read here. The skill sources sync
			// projects are loaded inside the project lock by sync.Run so a
			// concurrent `al skills` mutation cannot be observed half-applied.
			cfg, err := config.LoadConfigFS(os.DirFS(root), root, config.DefaultPaths(root).ConfigPath)
			if err != nil {
				return err
			}
			effectiveQuiet := quietFlag || strings.EqualFold(strings.TrimSpace(cfg.Warnings.NoiseMode), warnings.NoiseModeQuiet)
			stderr := cmd.ErrOrStderr()
			if effectiveQuiet {
				stderr = io.Discard
			}
			if cfg.Warnings.VersionUpdateOnSync != nil && *cfg.Warnings.VersionUpdateOnSync {
				updatewarn.WarnIfOutdated(cmd.Context(), Version, stderr)
			}
			result, err := sync.Run(root)
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
			if cfg.Approvals.Mode == config.ApprovalModeYOLO {
				_, _ = fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck)
			}
			return nil
		},
	}

	return cmd
}
