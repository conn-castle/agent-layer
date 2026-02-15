package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newUpgradeCmd() *cobra.Command {
	var yes bool
	var applyManagedUpdates bool
	var applyMemoryUpdates bool
	var applyDeletions bool
	var diffLines int

	cmd := &cobra.Command{
		Use:   messages.UpgradeUse,
		Short: messages.UpgradeShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			if diffLines <= 0 {
				return fmt.Errorf(messages.UpgradeDiffLinesInvalidFmt, diffLines)
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			policy, err := resolveUpgradeApplyPolicy(upgradeApplyInputs{
				interactive:    isTerminal(),
				yes:            yes,
				applyManaged:   applyManagedUpdates,
				applyMemory:    applyMemoryUpdates,
				applyDeletions: applyDeletions,
			})
			if err != nil {
				return err
			}
			if err := writeUpgradeSkippedCategoryNotes(cmd.ErrOrStderr(), policy); err != nil {
				return err
			}

			targetPin, err := resolvePinVersion("", Version)
			if err != nil {
				return err
			}
			opts := install.Options{
				Overwrite:    true,
				PinVersion:   targetPin,
				DiffMaxLines: diffLines,
				System:       install.RealSystem{},
			}
			opts.Prompter = buildUpgradePrompter(cmd, policy)
			if err := installRun(root, opts); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.AddCommand(newUpgradePlanCmd(&diffLines), newUpgradeRollbackCmd())

	cmd.Flags().BoolVar(&yes, "yes", false, messages.UpgradeFlagYes)
	cmd.Flags().BoolVar(&applyManagedUpdates, "apply-managed-updates", false, messages.UpgradeFlagApplyManagedUpdates)
	cmd.Flags().BoolVar(&applyMemoryUpdates, "apply-memory-updates", false, messages.UpgradeFlagApplyMemoryUpdates)
	cmd.Flags().BoolVar(&applyDeletions, "apply-deletions", false, messages.UpgradeFlagApplyDeletions)
	cmd.PersistentFlags().IntVar(&diffLines, "diff-lines", install.DefaultDiffMaxLines, messages.UpgradeFlagDiffLines)
	return cmd
}

func newUpgradeRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.UpgradeRollbackUse,
		Short: messages.UpgradeRollbackShort,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf(messages.UpgradeRollbackRequiresSnapshotID)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			snapshotID := strings.TrimSpace(args[0])
			if err := installRollbackUpgradeSnapshot(root, snapshotID, install.RollbackUpgradeSnapshotOptions{
				System: install.RealSystem{},
			}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), messages.UpgradeRollbackSuccessFmt, snapshotID)
			return err
		},
	}
}

type upgradeApplyInputs struct {
	interactive    bool
	yes            bool
	applyManaged   bool
	applyMemory    bool
	applyDeletions bool
}

func (in upgradeApplyInputs) hasAnyApply() bool {
	return in.applyManaged || in.applyMemory || in.applyDeletions
}

type upgradeApplyPolicy struct {
	interactive      bool
	yes              bool
	explicitCategory bool
	applyManaged     bool
	applyMemory      bool
	applyDeletions   bool
}

func resolveUpgradeApplyPolicy(in upgradeApplyInputs) (upgradeApplyPolicy, error) {
	if in.yes && !in.hasAnyApply() {
		return upgradeApplyPolicy{}, fmt.Errorf(messages.UpgradeYesRequiresApply)
	}
	if !in.interactive && !in.hasAnyApply() {
		return upgradeApplyPolicy{}, fmt.Errorf(messages.UpgradeRequiresTerminal)
	}
	if !in.interactive && !in.yes {
		return upgradeApplyPolicy{}, fmt.Errorf(messages.UpgradeNonInteractiveRequiresYesApply)
	}
	return upgradeApplyPolicy{
		interactive:      in.interactive,
		yes:              in.yes,
		explicitCategory: in.hasAnyApply(),
		applyManaged:     in.applyManaged,
		applyMemory:      in.applyMemory,
		applyDeletions:   in.applyDeletions,
	}, nil
}

func buildUpgradePrompter(cmd *cobra.Command, policy upgradeApplyPolicy) install.PromptFuncs {
	return install.PromptFuncs{
		OverwriteAllPreviewFunc: func(previews []install.DiffPreview) (bool, error) {
			if policy.explicitCategory {
				return policy.applyManaged, nil
			}
			if err := printDiffPreviews(cmd.OutOrStdout(), messages.UpgradeOverwriteManagedHeader, previews); err != nil {
				return false, err
			}
			return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeOverwriteAllPrompt, true)
		},
		OverwriteAllMemoryPreviewFunc: func(previews []install.DiffPreview) (bool, error) {
			if policy.explicitCategory {
				return policy.applyMemory, nil
			}
			if err := printDiffPreviews(cmd.OutOrStdout(), messages.UpgradeOverwriteMemoryHeader, previews); err != nil {
				return false, err
			}
			return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeOverwriteMemoryAllPrompt, false)
		},
		OverwritePreviewFunc: func(preview install.DiffPreview) (bool, error) {
			if policy.explicitCategory {
				if isMemoryPreviewPath(preview.Path) {
					return policy.applyMemory, nil
				}
				return policy.applyManaged, nil
			}
			if err := printDiffPreviews(cmd.OutOrStdout(), "", []install.DiffPreview{preview}); err != nil {
				return false, err
			}
			prompt := fmt.Sprintf(messages.UpgradeOverwritePromptFmt, preview.Path)
			return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, true)
		},
		DeleteUnknownAllFunc: func(paths []string) (bool, error) {
			if policy.explicitCategory {
				// Explicit deletion policy has three states:
				// 1) no --apply-deletions: skip all deletions,
				// 2) --apply-deletions --yes: auto-approve deletions,
				// 3) --apply-deletions without --yes: still prompt in interactive mode.
				if !policy.applyDeletions {
					return false, nil
				}
				if policy.yes {
					return true, nil
				}
			}
			if err := printFilePaths(cmd.OutOrStdout(), messages.InstallUnknownHeader, paths); err != nil {
				return false, err
			}
			return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeDeleteUnknownAllPrompt, false)
		},
		DeleteUnknownFunc: func(path string) (bool, error) {
			if policy.explicitCategory {
				// Mirror DeleteUnknownAllFunc so per-path prompts follow the same
				// explicit-category behavior (skip/auto-approve/prompt).
				if !policy.applyDeletions {
					return false, nil
				}
				if policy.yes {
					return true, nil
				}
			}
			prompt := fmt.Sprintf(messages.UpgradeDeleteUnknownPromptFmt, path)
			return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, false)
		},
	}
}

func isMemoryPreviewPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "docs/agent-layer" {
		return true
	}
	return strings.HasPrefix(path, "docs/agent-layer/")
}

func writeUpgradeSkippedCategoryNotes(out io.Writer, policy upgradeApplyPolicy) error {
	if !policy.explicitCategory {
		return nil
	}
	if !policy.applyManaged {
		if _, err := fmt.Fprintln(out, messages.UpgradeSkipManagedUpdatesInfo); err != nil {
			return err
		}
	}
	if !policy.applyMemory {
		if _, err := fmt.Fprintln(out, messages.UpgradeSkipMemoryUpdatesInfo); err != nil {
			return err
		}
	}
	if !policy.applyDeletions {
		if _, err := fmt.Fprintln(out, messages.UpgradeSkipDeletionsInfo); err != nil {
			return err
		}
	}
	return nil
}

func newUpgradePlanCmd(diffLines *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.UpgradePlanUse,
		Short: messages.UpgradePlanShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			if diffLines == nil {
				return fmt.Errorf(messages.UpgradeDiffLinesInvalidFmt, 0)
			}
			if *diffLines <= 0 {
				return fmt.Errorf(messages.UpgradeDiffLinesInvalidFmt, *diffLines)
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			targetPin, err := resolvePinVersion("", Version)
			if err != nil {
				return err
			}
			plan, err := install.BuildUpgradePlan(root, install.UpgradePlanOptions{
				TargetPinVersion: targetPin,
				System:           install.RealSystem{},
			})
			if err != nil {
				return err
			}
			previews, err := install.BuildUpgradePlanDiffPreviews(root, plan, install.UpgradePlanDiffPreviewOptions{
				System:       install.RealSystem{},
				MaxDiffLines: *diffLines,
			})
			if err != nil {
				return err
			}
			return renderUpgradePlanText(cmd.OutOrStdout(), plan, previews)
		},
	}
	return cmd
}

func renderUpgradePlanText(out io.Writer, plan install.UpgradePlan, previews map[string]install.DiffPreview) error {
	if _, err := fmt.Fprintln(out, "Upgrade plan (dry-run): no files were written."); err != nil {
		return err
	}
	if err := writeUpgradeSummary(out, plan); err != nil {
		return err
	}
	allUpdates := make([]install.UpgradeChange, 0, len(plan.TemplateUpdates)+len(plan.SectionAwareUpdates))
	allUpdates = append(allUpdates, plan.TemplateUpdates...)
	allUpdates = append(allUpdates, plan.SectionAwareUpdates...)
	if err := writeUpgradeChangeSection(out, "Files to add", plan.TemplateAdditions, previews); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Files to update", allUpdates, previews); err != nil {
		return err
	}
	if err := writeUpgradeRenameSection(out, "Files to rename", plan.TemplateRenames); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Files to review for removal", plan.TemplateRemovalsOrOrphans, previews); err != nil {
		return err
	}
	if err := writeConfigMigrationSection(out, "Config updates", plan.ConfigKeyMigrations); err != nil {
		return err
	}
	if err := writeMigrationReportSection(out, "Migrations", plan.MigrationReport); err != nil {
		return err
	}
	if err := writePinVersionSection(out, plan.PinVersionChange, previews); err != nil {
		return err
	}
	if err := writeReadinessSection(out, plan.ReadinessChecks); err != nil {
		return err
	}
	return nil
}

func writeUpgradeChangeSection(out io.Writer, title string, changes []install.UpgradeChange, previews map[string]install.DiffPreview) error {
	if _, err := fmt.Fprintf(out, "\n%s:\n", title); err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(out, "  - (none)")
		return err
	}
	for _, change := range changes {
		if _, err := fmt.Fprintf(out, "  - %s\n", change.Path); err != nil {
			return err
		}
		if err := writeSinglePreviewBlock(out, previews[change.Path]); err != nil {
			return err
		}
	}
	return nil
}

func writeUpgradeRenameSection(out io.Writer, title string, renames []install.UpgradeRename) error {
	if _, err := fmt.Fprintf(out, "\n%s:\n", title); err != nil {
		return err
	}
	if len(renames) == 0 {
		_, err := fmt.Fprintln(out, "  - (none)")
		return err
	}
	for _, rename := range renames {
		if _, err := fmt.Fprintf(out, "  - %s -> %s\n", rename.From, rename.To); err != nil {
			return err
		}
	}
	return nil
}

func writeConfigMigrationSection(out io.Writer, title string, migrations []install.ConfigKeyMigration) error {
	if _, err := fmt.Fprintf(out, "\n%s:\n", title); err != nil {
		return err
	}
	if len(migrations) == 0 {
		_, err := fmt.Fprintln(out, "  - (none)")
		return err
	}
	for _, migration := range migrations {
		if _, err := fmt.Fprintf(out, "  - %s: %s -> %s\n", migration.Key, migration.From, migration.To); err != nil {
			return err
		}
	}
	return nil
}

// errWriter wraps an io.Writer and accumulates the first error encountered,
// allowing sequential writes without per-call error checks.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) printf(format string, args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}

func (ew *errWriter) println(args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintln(ew.w, args...)
}

func writeMigrationReportSection(out io.Writer, title string, report install.UpgradeMigrationReport) error {
	ew := &errWriter{w: out}
	ew.printf("\n%s:\n", title)
	if len(report.Entries) == 0 {
		ew.println("  - (none)")
		return ew.err
	}
	ew.printf("  - target version: %s\n", report.TargetVersion)
	ew.printf("  - source version: %s (%s)\n", report.SourceVersion, report.SourceVersionOrigin)
	for _, note := range report.SourceResolutionNotes {
		ew.printf("  - source note: %s\n", note)
	}
	for _, entry := range report.Entries {
		ew.printf("  - [%s] %s (%s): %s\n", entry.Status, entry.ID, entry.Kind, entry.Rationale)
		if entry.SkipReason != "" {
			ew.printf("    reason: %s\n", entry.SkipReason)
		}
	}
	return ew.err
}

func writePinVersionSection(out io.Writer, pin install.UpgradePinVersionDiff, previews map[string]install.DiffPreview) error {
	ew := &errWriter{w: out}
	ew.println("\nPin version change:")
	ew.printf("  - current: %q\n", pin.Current)
	ew.printf("  - target: %q\n", pin.Target)
	ew.printf("  - action: %s\n", pin.Action)
	if ew.err != nil {
		return ew.err
	}
	if pin.Action != install.UpgradePinActionNone {
		if err := writeSinglePreviewBlock(out, previews[".agent-layer/al.version"]); err != nil {
			return err
		}
	}
	return nil
}

func writeSinglePreviewBlock(out io.Writer, preview install.DiffPreview) error {
	if strings.TrimSpace(preview.UnifiedDiff) == "" {
		return nil
	}
	if _, err := fmt.Fprintln(out, "    diff:"); err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(preview.UnifiedDiff, "\n"), "\n")
	for _, line := range lines {
		if _, err := fmt.Fprintf(out, "      %s\n", line); err != nil {
			return err
		}
	}
	return nil
}

func printDiffPreviews(out io.Writer, header string, previews []install.DiffPreview) error {
	if len(previews) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if strings.TrimSpace(header) != "" {
		if _, err := fmt.Fprintln(out, header); err != nil {
			return err
		}
	}
	for _, preview := range previews {
		if _, err := fmt.Fprintf(out, messages.InstallDiffLineFmt, preview.Path); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	for _, preview := range previews {
		if strings.TrimSpace(preview.UnifiedDiff) == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, "Diff for %s:\n", preview.Path); err != nil {
			return err
		}
		if _, err := fmt.Fprint(out, preview.UnifiedDiff); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}

func writeReadinessSection(out io.Writer, checks []install.UpgradeReadinessCheck) error {
	if _, err := fmt.Fprintln(out, "\nReadiness checks:"); err != nil {
		return err
	}
	if len(checks) == 0 {
		_, err := fmt.Fprintln(out, "  - (none)")
		return err
	}
	for _, check := range checks {
		if _, err := fmt.Fprintf(out, "  - %s\n", readinessSummary(check)); err != nil {
			return err
		}
		action := readinessAction(check.ID)
		if action != "" {
			if _, err := fmt.Fprintf(out, "    action: %s\n", action); err != nil {
				return err
			}
		}
		details := check.Details
		if len(details) > 3 {
			details = details[:3]
		}
		for _, detail := range details {
			if _, err := fmt.Fprintf(out, "    note: %s\n", detail); err != nil {
				return err
			}
		}
		if len(check.Details) > len(details) {
			if _, err := fmt.Fprintf(out, "    note: ... and %d more\n", len(check.Details)-len(details)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeUpgradeSummary(out io.Writer, plan install.UpgradePlan) error {
	filesToUpdate := len(plan.TemplateUpdates) + len(plan.SectionAwareUpdates)
	migrationsPlanned := 0
	for _, entry := range plan.MigrationReport.Entries {
		if entry.Status == install.UpgradeMigrationStatusPlanned {
			migrationsPlanned++
		}
	}
	needsReview := len(plan.ReadinessChecks) > 0
	reviewState := "yes"
	if !needsReview {
		reviewState = "no"
	}
	if _, err := fmt.Fprintln(out, "\nSummary:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - files to add: %d\n", len(plan.TemplateAdditions)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - files to update: %d\n", filesToUpdate); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - files to rename: %d\n", len(plan.TemplateRenames)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - files to review for removal: %d\n", len(plan.TemplateRemovalsOrOrphans)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - config updates: %d\n", len(plan.ConfigKeyMigrations)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - migrations planned: %d\n", migrationsPlanned); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - readiness warnings: %d\n", len(plan.ReadinessChecks)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - needs review before apply: %s\n", reviewState); err != nil {
		return err
	}
	return nil
}

func readinessSummary(check install.UpgradeReadinessCheck) string {
	switch check.ID {
	case "unrecognized_config_keys":
		return "Config needs review before upgrade."
	case "vscode_no_sync_outputs_stale":
		return "VS Code generated files may be stale."
	case "floating_external_dependency_specs":
		return "Some enabled MCP dependencies use floating versions."
	case "stale_disabled_agent_artifacts":
		return "Disabled-agent generated files are still present."
	default:
		return check.Summary
	}
}

func readinessAction(id string) string {
	switch id {
	case "unrecognized_config_keys":
		return "Fix unknown or invalid keys in `.agent-layer/config.toml` (or run `al wizard`) before applying."
	case "vscode_no_sync_outputs_stale":
		return "Run `al sync` before `al upgrade` so generated VS Code files match current config."
	case "floating_external_dependency_specs":
		return "Pin floating dependency specs in `.agent-layer/config.toml` for stable upgrades."
	case "stale_disabled_agent_artifacts":
		return "Remove stale generated files for disabled agents, or re-enable those agents."
	default:
		return ""
	}
}
