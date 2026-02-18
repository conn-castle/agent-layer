package main

import (
	"errors"
	"fmt"
	"strings"

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

			// Print auto-approve info before warnings (warnings cause early error return).
			// Only relevant when at least one Claude agent is enabled.
			stderr := cmd.ErrOrStderr()
			claudeEnabled := project.Config.Agents.Claude.Enabled != nil && *project.Config.Agents.Claude.Enabled
			claudeVSCodeEnabled := project.Config.Agents.ClaudeVSCode.Enabled != nil && *project.Config.Agents.ClaudeVSCode.Enabled
			if len(result.AutoApprovedSkills) > 0 && (claudeEnabled || claudeVSCodeEnabled) {
				_, _ = fmt.Fprintf(stderr, "[auto-approve] skills: %s\n", strings.Join(result.AutoApprovedSkills, ", "))
			}

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
