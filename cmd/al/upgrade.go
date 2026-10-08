package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
	"github.com/conn-castle/agent-layer/internal/wizard"
)

var installRepairGitignoreBlock = install.RepairGitignoreBlock
var dispatchPrefetchVersion = versiondispatch.PrefetchVersion

type upgradeKeepListUI interface {
	MultiSelect(title string, options []string, selected *[]string) error
}

var newUpgradeKeepListUI = func() upgradeKeepListUI { return wizard.NewHuhUI() }

func newUpgradeCmd() *cobra.Command {
	var yes bool
	var applyManagedUpdates bool
	var applyMemoryUpdates bool
	var applyDeletions bool
	var applyTmpDeletions bool
	var diffLines int
	var pinVersion string

	cmd := &cobra.Command{
		Use:   messages.UpgradeUse,
		Short: messages.UpgradeShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if diffLines <= 0 {
				return fmt.Errorf(messages.UpgradeDiffLinesInvalidFmt, diffLines)
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			policy, err := resolveUpgradeApplyPolicy(upgradeApplyInputs{
				interactive:       isTerminal(),
				yes:               yes,
				applyManaged:      applyManagedUpdates,
				applyMemory:       applyMemoryUpdates,
				applyDeletions:    applyDeletions,
				applyTmpDeletions: applyTmpDeletions,
			})
			if err != nil {
				return err
			}
			if err := writeUpgradeSkippedCategoryNotes(cmd.ErrOrStderr(), policy); err != nil {
				return err
			}

			targetPin, err := resolvePinVersionForInit(cmd.Context(), pinVersion, Version)
			if err != nil {
				return err
			}
			if err := requireUpgradeTargetCLI(targetPin); err != nil {
				return err
			}
			if strings.TrimSpace(pinVersion) != "" && !strings.EqualFold(strings.TrimSpace(pinVersion), "latest") {
				if err := validatePinnedReleaseVersionFunc(cmd.Context(), targetPin); err != nil {
					return err
				}
			}
			if err := writeUpgradeVersionBanner(cmd.OutOrStdout(), root, targetPin); err != nil {
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
			if err := runPostUpgradeSync(cmd.OutOrStdout(), cmd.ErrOrStderr(), root); err != nil {
				return err
			}
			if err := wizard.MigrateBackups(root); err != nil {
				return err
			}
			if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), messages.UpgradeSuccessful); writeErr != nil {
				return writeErr
			}
			_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), messages.UpgradeReviewSettingsHint)
			return writeErr
		},
	}
	cmd.AddCommand(
		newUpgradePlanCmd(&diffLines),
		newUpgradeRollbackCmd(),
		newUpgradePrefetchCmd(),
		newUpgradeRepairGitignoreBlockCmd(),
	)

	cmd.Flags().BoolVar(&yes, "yes", false, messages.UpgradeFlagYes)
	cmd.Flags().BoolVar(&applyManagedUpdates, "apply-managed-updates", false, messages.UpgradeFlagApplyManagedUpdates)
	cmd.Flags().BoolVar(&applyMemoryUpdates, "apply-memory-updates", false, messages.UpgradeFlagApplyMemoryUpdates)
	cmd.Flags().BoolVar(&applyDeletions, "apply-deletions", false, messages.UpgradeFlagApplyDeletions)
	cmd.Flags().BoolVar(&applyTmpDeletions, "apply-tmp-deletions", false, messages.UpgradeFlagApplyTmpDeletions)
	cmd.Flags().StringVar(&pinVersion, "version", "", messages.UpgradeFlagVersion)
	cmd.PersistentFlags().IntVar(&diffLines, "diff-lines", install.DefaultDiffMaxLines, messages.UpgradeFlagDiffLines)
	return cmd
}

func newUpgradeRollbackCmd() *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   messages.UpgradeRollbackUse,
		Short: messages.UpgradeRollbackShort,
		Args: func(cmd *cobra.Command, args []string) error {
			if list {
				return cobra.NoArgs(cmd, args)
			}
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
			if list {
				snapshots, err := install.ListUpgradeSnapshots(root, install.RealSystem{})
				if err != nil {
					return err
				}
				if len(snapshots) == 0 {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), messages.UpgradeRollbackNoSnapshots)
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.UpgradeRollbackListHeader)
				for _, s := range snapshots {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s (%s, status: %s)\n", s.ID, s.CreatedAtUTC, s.Status)
				}
				return nil
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
	cmd.Flags().BoolVar(&list, "list", false, messages.UpgradeRollbackFlagList)
	return cmd
}

func newUpgradePrefetchCmd() *cobra.Command {
	var versionFlag string
	cmd := &cobra.Command{
		Use:   messages.UpgradePrefetchUse,
		Short: messages.UpgradePrefetchShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetVersion, err := resolvePinVersionForInit(cmd.Context(), versionFlag, Version)
			if err != nil {
				return err
			}
			targetVersion = strings.TrimSpace(targetVersion)
			if targetVersion == "" {
				return fmt.Errorf(messages.UpgradePrefetchVersionRequired)
			}
			if err := dispatchPrefetchVersion(cmd.Context(), targetVersion, cmd.ErrOrStderr()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), messages.UpgradePrefetchDoneFmt, targetVersion)
			return err
		},
	}
	cmd.Flags().StringVar(&versionFlag, "version", "", messages.UpgradePrefetchVersionFlag)
	return cmd
}

func newUpgradeRepairGitignoreBlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.UpgradeRepairGitignoreUse,
		Short: messages.UpgradeRepairGitignoreShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if err := installRepairGitignoreBlock(root, install.RepairGitignoreBlockOptions{
				System: install.RealSystem{},
			}); err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), messages.UpgradeRepairGitignoreDone)
			return err
		},
	}
}

// runPostUpgradeSync regenerates client outputs after a successful install so
// retired projection paths and freshly-introduced templates are reconciled
// without requiring the user to invoke `al sync` manually. Sync warnings are
// surfaced on stderr; sync errors are wrapped to make clear that the upgrade
// itself succeeded.
func runPostUpgradeSync(stdout, stderr io.Writer, root string) error {
	_, _ = fmt.Fprintln(stdout, messages.UpgradeRunningSync)
	result, err := syncRun(root)
	if err != nil {
		return fmt.Errorf(messages.UpgradeSyncFailedFmt, err)
	}
	if result == nil {
		return nil
	}
	if len(result.Warnings) > 0 {
		warnColor := color.New(color.FgYellow)
		for _, w := range result.Warnings {
			_, _ = warnColor.Fprintf(stderr, messages.WizardWarningFmt, w.Message)
		}
	}
	return nil
}

type upgradeApplyInputs struct {
	interactive       bool
	yes               bool
	applyManaged      bool
	applyMemory       bool
	applyDeletions    bool
	applyTmpDeletions bool
}

func (in upgradeApplyInputs) hasAnyApply() bool {
	return in.applyManaged || in.applyMemory || in.applyDeletions || in.applyTmpDeletions
}

type upgradeApplyPolicy struct {
	interactive       bool
	yes               bool
	explicitCategory  bool
	applyManaged      bool
	applyMemory       bool
	applyDeletions    bool
	applyTmpDeletions bool
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
		interactive:       in.interactive,
		yes:               in.yes,
		explicitCategory:  in.hasAnyApply(),
		applyManaged:      in.applyManaged,
		applyMemory:       in.applyMemory,
		applyDeletions:    in.applyDeletions,
		applyTmpDeletions: in.applyTmpDeletions,
	}, nil
}

func buildUpgradePrompter(cmd *cobra.Command, policy upgradeApplyPolicy) *install.PromptFuncs {
	// Shared buffered reader for all prompts in this upgrade session. Creating
	// a single reader prevents buffered stdin bytes from being lost when
	// multiple prompts are issued sequentially (e.g., chained config_set_default
	// migration operations).
	stdinReader := bufio.NewReader(cmd.InOrStdin())

	statuslineSourceSkipAnnounced := false

	return &install.PromptFuncs{
		SelectUnknownsToKeepFunc: func(paths []string) ([]string, error) {
			if policy.yes || !policy.interactive || len(paths) == 0 || (policy.explicitCategory && !policy.applyDeletions) {
				return nil, nil
			}
			noun := "path"
			if len(paths) != 1 {
				noun = "paths"
			}
			header := fmt.Sprintf(messages.UpgradeKeepListCandidatesFmt, len(paths), noun)
			if err := printFilePaths(cmd.OutOrStdout(), header, paths); err != nil {
				return nil, err
			}
			add, err := promptYesNo(stdinReader, cmd.OutOrStdout(), messages.UpgradeAddToKeepListPrompt, true)
			if err != nil || !add {
				return nil, err
			}
			selected := []string{}
			if err := newUpgradeKeepListUI().MultiSelect(messages.UpgradeKeepListSelectTitle, paths, &selected); err != nil {
				if errors.Is(err, wizard.ErrBack) {
					return nil, nil
				}
				return nil, err
			}
			return selected, nil
		},
		ConfigSetDefaultFunc: func(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error) {
			if policy.yes {
				return manifestValue, nil
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), messages.UpgradeNewConfigKeyFmt, key, rationale)
			if err != nil {
				return nil, err
			}
			if field != nil {
				return promptConfigChoice(stdinReader, cmd.OutOrStdout(), key, manifestValue, *field)
			}
			// Fallback for keys not in the catalog.
			accept, promptErr := promptYesNo(stdinReader, cmd.OutOrStdout(), fmt.Sprintf(messages.UpgradeAcceptValueFmt, manifestValue, key), true)
			if promptErr != nil {
				return nil, promptErr
			}
			if accept {
				return manifestValue, nil
			}
			return nil, fmt.Errorf(messages.UpgradeDeclinedRequiredKeyFmt, key)
		},
		OverwriteAllUnifiedPreviewFunc: func(managedPreviews []install.DiffPreview, memoryPreviews []install.DiffPreview) (bool, bool, error) {
			if policy.explicitCategory {
				return policy.applyManaged, policy.applyMemory, nil
			}
			return promptUnifiedOverwriteSections(stdinReader, cmd.OutOrStdout(), managedPreviews, memoryPreviews)
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
			return promptYesNo(stdinReader, cmd.OutOrStdout(), prompt, true)
		},
		StatuslineSourcePreviewFunc: func(preview install.DiffPreview) (bool, error) {
			if policy.yes || !policy.interactive {
				if !statuslineSourceSkipAnnounced {
					if _, err := fmt.Fprintln(cmd.ErrOrStderr(), messages.UpgradeSkipStatuslineSourceUpdatesInfo); err != nil {
						return false, err
					}
					statuslineSourceSkipAnnounced = true
				}
				return false, nil
			}
			if err := printDiffPreviews(cmd.OutOrStdout(), messages.UpgradeStatuslineSourceDiffHeader, []install.DiffPreview{preview}); err != nil {
				return false, err
			}
			prompt := fmt.Sprintf(messages.UpgradeOverwriteStatuslineSourcePromptFmt, preview.Path)
			return promptYesNo(stdinReader, cmd.OutOrStdout(), prompt, false)
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
			return promptYesNo(stdinReader, cmd.OutOrStdout(), messages.UpgradeDeleteUnknownAllPrompt, false)
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
			return promptYesNo(stdinReader, cmd.OutOrStdout(), prompt, false)
		},
		DeleteUnknownTmpAllFunc: func(paths []string) (bool, error) {
			// Tmp deletion is destructive and not snapshot-rollback-protected,
			// so it is gated by its own flag (--apply-tmp-deletions),
			// independent of --apply-deletions. In interactive mode it
			// requires a destructive double-confirm; both prompts default to
			// "no" so a stray Enter cannot wipe tmp content.
			if policy.explicitCategory {
				if !policy.applyTmpDeletions {
					return false, nil
				}
				if policy.yes {
					return true, nil
				}
			}
			if err := printFilePaths(cmd.OutOrStdout(), messages.UpgradeDeleteUnknownTmpHeader, paths); err != nil {
				return false, err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), color.YellowString("%s", messages.UpgradeDeleteUnknownTmpDestructiveWarningHeader)); err != nil {
				return false, err
			}
			prompt := fmt.Sprintf(messages.UpgradeDeleteUnknownTmpAllPromptFmt, len(paths))
			answer, err := promptYesNo(stdinReader, cmd.OutOrStdout(), prompt, false)
			if err != nil {
				return false, err
			}
			if !answer {
				return false, nil
			}
			return promptYesNo(stdinReader, cmd.OutOrStdout(), messages.UpgradeDeleteUnknownTmpDestructiveConfirmPrompt, false)
		},
		ConfirmSkillsMigrationFunc: func(flatSkills []string, conflicts []install.SkillsMigrationConflict) (bool, error) {
			// Conflicts always block, even in headless mode.
			if len(conflicts) > 0 {
				return false, nil
			}
			if policy.yes {
				return true, nil
			}
			prompt := fmt.Sprintf(messages.UpgradeSkillsMigrationPromptFmt, len(flatSkills))
			return promptYesNo(stdinReader, cmd.OutOrStdout(), prompt, true)
		},
	}
}

// promptUnifiedOverwriteSections prints summary lists for both managed and
// memory previews, asks once whether to view the full diffs (default no),
// optionally renders them, and then asks the apply prompt for each section.
func promptUnifiedOverwriteSections(in io.Reader, out io.Writer, managedPreviews []install.DiffPreview, memoryPreviews []install.DiffPreview) (bool, bool, error) {
	reader := bufferedReader(in)
	if err := printDiffPreviewSummary(out, messages.UpgradeOverwriteManagedHeader, managedPreviews); err != nil {
		return false, false, err
	}
	if err := printDiffPreviewSummary(out, messages.UpgradeOverwriteMemoryHeader, memoryPreviews); err != nil {
		return false, false, err
	}
	combined := make([]install.DiffPreview, 0, len(managedPreviews)+len(memoryPreviews))
	combined = append(combined, managedPreviews...)
	combined = append(combined, memoryPreviews...)
	if err := promptOptionalViewDiff(reader, out, combined); err != nil {
		return false, false, err
	}
	applyManaged := false
	applyMemory := false
	var err error
	if len(managedPreviews) > 0 {
		applyManaged, err = promptYesNo(reader, out, messages.UpgradeOverwriteAllPrompt, true)
		if err != nil {
			return false, false, err
		}
	}
	if len(memoryPreviews) > 0 {
		applyMemory, err = promptYesNo(reader, out, messages.UpgradeOverwriteMemoryAllPrompt, false)
		if err != nil {
			return false, false, err
		}
	}
	return applyManaged, applyMemory, nil
}

// bufferedReader returns a *bufio.Reader for in, reusing it if in is already
// buffered. Sharing one reader across consecutive prompts prevents bytes
// buffered after the first newline from being silently dropped between calls.
func bufferedReader(in io.Reader) *bufio.Reader {
	if br, ok := in.(*bufio.Reader); ok {
		return br
	}
	return bufio.NewReader(in)
}

// promptOptionalViewDiff asks "View the full diff?" defaulting to no, and
// renders the unified diff bodies when the user accepts. The prompt is
// suppressed when no preview carries a non-empty diff body, since there is
// nothing to show.
func promptOptionalViewDiff(in io.Reader, out io.Writer, previews []install.DiffPreview) error {
	if !hasNonEmptyDiff(previews) {
		return nil
	}
	view, err := promptYesNo(in, out, messages.UpgradeViewDiffPrompt, false)
	if err != nil {
		return err
	}
	if !view {
		return nil
	}
	return printDiffPreviewBodies(out, previews)
}

// hasNonEmptyDiff reports whether any preview has a non-empty unified diff body.
func hasNonEmptyDiff(previews []install.DiffPreview) bool {
	for _, preview := range previews {
		if strings.TrimSpace(preview.UnifiedDiff) != "" {
			return true
		}
	}
	return false
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
	ew := &errWriter{w: out}
	if !policy.applyManaged {
		ew.println(messages.UpgradeSkipManagedUpdatesInfo)
	}
	if !policy.applyMemory {
		ew.println(messages.UpgradeSkipMemoryUpdatesInfo)
	}
	if !policy.applyDeletions {
		ew.println(messages.UpgradeSkipDeletionsInfo)
	}
	if !policy.applyTmpDeletions {
		ew.println(messages.UpgradeSkipTmpDeletionsInfo)
	}
	return ew.err
}

func newUpgradePlanCmd(diffLines *int) *cobra.Command {
	var pinVersion string
	cmd := &cobra.Command{
		Use:   messages.UpgradePlanUse,
		Short: messages.UpgradePlanShort,
		Args:  cobra.NoArgs,
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

			targetPin, err := resolvePinVersionForInit(cmd.Context(), pinVersion, Version)
			if err != nil {
				return err
			}
			if err := requireUpgradeTargetCLI(targetPin); err != nil {
				return err
			}
			if strings.TrimSpace(pinVersion) != "" && !strings.EqualFold(strings.TrimSpace(pinVersion), "latest") {
				if err := validatePinnedReleaseVersionFunc(cmd.Context(), targetPin); err != nil {
					return err
				}
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
	cmd.Flags().StringVar(&pinVersion, "version", "", messages.UpgradeFlagVersion)
	return cmd
}

func writeUpgradeVersionBanner(out io.Writer, root, targetPin string) error {
	current, err := currentRepoPinVersion(root)
	if err != nil {
		return err
	}
	target := strings.TrimSpace(targetPin)
	if target == "" {
		target = Version
	}
	_, err = fmt.Fprintf(out, messages.UpgradeStartFmt, formatCLIVersion(current), formatCLIVersion(target))
	return err
}

func currentRepoPinVersion(root string) (string, error) {
	pinned, ok, _, err := versiondispatch.ReadPinnedVersion(root)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return pinned, nil
}

// requireUpgradeTargetCLI rejects a release-build upgrade target other than the
// running CLI version, because only this version's templates are embedded.
func requireUpgradeTargetCLI(targetVersion string) error {
	return requireTargetCLI(targetVersion, messages.UpgradeTargetRequiresNewerCLIFmt, messages.UpgradeTargetOlderThanCLIFmt)
}

func renderUpgradePlanText(out io.Writer, plan install.UpgradePlan, previews map[string]install.DiffPreview) error {
	ew := &errWriter{w: out}
	ew.println(messages.UpgradePlanDryRunNoFiles)
	writeUpgradeSummary(ew, plan)
	allUpdates := make([]install.UpgradeChange, 0, len(plan.TemplateUpdates)+len(plan.SectionAwareUpdates))
	allUpdates = append(allUpdates, plan.TemplateUpdates...)
	allUpdates = append(allUpdates, plan.SectionAwareUpdates...)
	writeUpgradeChangeSection(ew, messages.UpgradePlanSectionFilesToAdd, plan.TemplateAdditions, previews)
	writeUpgradeChangeSection(ew, messages.UpgradePlanSectionStatuslineFilesToAdd, plan.StatuslineSourceAdditions, previews)
	writeUpgradeChangeSection(ew, messages.UpgradePlanSectionFilesToUpdate, allUpdates, previews)
	writeUpgradeChangeSection(ew, messages.UpgradePlanSectionStatuslineToReview, plan.StatuslineSourceUpdates, previews)
	ew.printf(messages.UpgradePlanSectionTitleFmt, messages.UpgradePlanSectionFilesToRename)
	writeUpgradePlanItems(ew, plan.TemplateRenames, func(rename install.UpgradeRename) {
		ew.printf(messages.UpgradePlanRenameItemFmt, rename.From, rename.To)
	})
	writeUpgradeChangeSection(ew, messages.UpgradePlanSectionFilesToReviewRemoval, plan.TemplateRemovalsOrOrphans, previews)
	ew.printf(messages.UpgradePlanSectionTitleFmt, messages.UpgradePlanSectionConfigUpdates)
	writeUpgradePlanItems(ew, plan.ConfigKeyMigrations, func(migration install.ConfigKeyMigration) {
		ew.printf(messages.UpgradePlanConfigItemFmt, migration.Key, migration.From, migration.To)
	})
	writeMigrationReportSection(ew, plan.MigrationReport)
	writePinVersionSection(ew, plan.PinVersionChange)
	writeReadinessSection(ew, plan.ReadinessChecks)
	return ew.err
}

// writeUpgradePlanItems writes each item of a plan section, or the "(none)"
// line when the section is empty.
func writeUpgradePlanItems[T any](ew *errWriter, items []T, writeItem func(T)) {
	if len(items) == 0 {
		ew.println(messages.UpgradePlanNone)
		return
	}
	for _, item := range items {
		writeItem(item)
	}
}

func writeUpgradeChangeSection(ew *errWriter, title string, changes []install.UpgradeChange, previews map[string]install.DiffPreview) {
	ew.printf(messages.UpgradePlanSectionTitleFmt, title)
	writeUpgradePlanItems(ew, changes, func(change install.UpgradeChange) {
		ew.printf(messages.UpgradePlanItemFmt, change.Path)
		writeSinglePreviewBlock(ew, previews[change.Path])
	})
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

func writeMigrationReportSection(ew *errWriter, report install.UpgradeMigrationReport) {
	ew.printf(messages.UpgradePlanSectionTitleFmt, messages.UpgradePlanSectionMigrations)
	if len(report.Entries) == 0 {
		ew.println(messages.UpgradePlanNone)
		return
	}
	ew.printf(messages.UpgradePlanMigrationTargetVersionFmt, report.TargetVersion)
	ew.printf(messages.UpgradePlanMigrationSourceVersionFmt, report.SourceVersion, report.SourceVersionOrigin)
	for _, note := range report.SourceResolutionNotes {
		ew.printf(messages.UpgradePlanMigrationSourceNoteFmt, note)
	}
	for _, entry := range report.Entries {
		ew.printf(messages.UpgradePlanMigrationEntryFmt, entry.Status, entry.ID, entry.Kind, entry.Rationale)
		if entry.SkipReason != "" {
			ew.printf(messages.UpgradePlanMigrationReasonFmt, entry.SkipReason)
		}
		if entry.Breaking && entry.Status == install.UpgradeMigrationStatusPlanned {
			if entry.BreakingNotice != "" {
				ew.println(color.YellowString(messages.UpgradePlanMigrationBreakingNoticeFmt, entry.BreakingNotice))
			}
			for _, detail := range entry.BreakingDetails {
				ew.println(color.YellowString(messages.UpgradePlanMigrationBreakingDetailFmt, detail))
			}
			ew.println(color.YellowString(messages.UpgradePlanMigrationBreakingRunHint))
		}
	}
}

func writePinVersionSection(ew *errWriter, pin install.UpgradePinVersionDiff) {
	ew.println(messages.UpgradePlanPinVersionHeader)
	ew.printf(messages.UpgradePlanPinCurrentFmt, pin.Current)
	ew.printf(messages.UpgradePlanPinTargetFmt, pin.Target)
	ew.printf(messages.UpgradePlanPinActionFmt, pin.Action)
}

func writeSinglePreviewBlock(ew *errWriter, preview install.DiffPreview) {
	if strings.TrimSpace(preview.UnifiedDiff) == "" {
		return
	}
	ew.println(messages.UpgradePlanDiffLabel)
	// Stop before the terminal check, and keep the assignment below from clearing an earlier error.
	if ew.err != nil {
		return
	}
	ew.err = writeUnifiedDiff(ew.w, preview.UnifiedDiff, shouldColorizeDiffOutput(), "      ")
}

// printDiffPreviews renders the file-list summary (with +/- stats) followed
// by every non-empty diff body. Used by the per-file overwrite prompt where
// the user has already chosen to inspect a single file.
func printDiffPreviews(out io.Writer, header string, previews []install.DiffPreview) error {
	if len(previews) == 0 {
		return nil
	}
	if err := printDiffPreviewSummary(out, header, previews); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return printDiffPreviewBodies(out, previews)
}

// printDiffPreviewSummary prints a header (when non-empty) followed by a list
// of "  - <path>  +N -M" lines, with the +N green and -M red when output is
// colorized. Paths are left-aligned in a single column for readability.
func printDiffPreviewSummary(out io.Writer, header string, previews []install.DiffPreview) error {
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
	maxPath := 0
	for _, preview := range previews {
		if n := len(preview.Path); n > maxPath {
			maxPath = n
		}
	}
	colorize := shouldColorizeDiffOutput()
	for _, preview := range previews {
		added := formatDiffStat(preview.LinesAdded, "+", diffColorAdded, colorize)
		removed := formatDiffStat(preview.LinesRemoved, "-", diffColorRemoved, colorize)
		if _, err := fmt.Fprintf(out, "  - %-*s  %s %s\n", maxPath, preview.Path, added, removed); err != nil {
			return err
		}
	}
	return nil
}

// printDiffPreviewBodies prints "Diff for <path>:" followed by each preview's
// unified diff body, separated by blank lines. Previews with empty diff
// bodies are skipped.
func printDiffPreviewBodies(out io.Writer, previews []install.DiffPreview) error {
	colorize := shouldColorizeDiffOutput()
	for _, preview := range previews {
		if strings.TrimSpace(preview.UnifiedDiff) == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, messages.UpgradePlanDiffForFmt, preview.Path); err != nil {
			return err
		}
		if err := writeUnifiedDiff(out, preview.UnifiedDiff, colorize, ""); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}

// formatDiffStat formats a stat token like "+5" or "-3", colorized when both
// colorize is true and the count is non-zero. Zero counts stay plain so the
// absence of changes does not draw the eye.
func formatDiffStat(count int, sign string, c *color.Color, colorize bool) string {
	text := fmt.Sprintf("%s%d", sign, count)
	if colorize && count > 0 {
		return c.Sprint(text)
	}
	return text
}

func shouldColorizeDiffOutput() bool {
	return isTerminal() && !color.NoColor
}

var (
	diffColorAdded   = color.New(color.FgGreen)
	diffColorRemoved = color.New(color.FgRed)
	diffColorHunk    = color.New(color.FgCyan)
)

func formatUnifiedDiffLine(line string, colorize bool) string {
	if !colorize {
		return line
	}
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return diffColorAdded.Sprint(line)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return diffColorRemoved.Sprint(line)
	case strings.HasPrefix(line, "@@"):
		return diffColorHunk.Sprint(line)
	default:
		return line
	}
}

func writeUnifiedDiff(out io.Writer, diff string, colorize bool, indent string) error {
	trimmed := strings.TrimRight(diff, "\n")
	lines := strings.Split(trimmed, "\n")
	hasTrailingNewline := strings.HasSuffix(diff, "\n")
	for idx, line := range lines {
		formatted := formatUnifiedDiffLine(line, colorize)
		if indent != "" {
			formatted = indent + formatted
		}
		isLast := idx == len(lines)-1
		if isLast && !hasTrailingNewline {
			if _, err := fmt.Fprint(out, formatted); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(out, formatted); err != nil {
			return err
		}
	}
	return nil
}

func writeReadinessSection(ew *errWriter, checks []install.UpgradeReadinessCheck) {
	ew.println(messages.UpgradePlanReadinessHeader)
	writeUpgradePlanItems(ew, checks, func(check install.UpgradeReadinessCheck) {
		ew.printf(messages.UpgradePlanReadinessItemFmt, color.YellowString("%s", check.Summary))
		if check.Action != "" {
			ew.printf(messages.UpgradePlanReadinessRecommendationFmt, check.Action)
		}
		details := check.Details
		if len(details) > 3 {
			details = details[:3]
		}
		for _, detail := range details {
			ew.printf(messages.UpgradePlanReadinessNoteFmt, detail)
		}
		if len(check.Details) > len(details) {
			ew.printf(messages.UpgradePlanReadinessNoteMoreFmt, len(check.Details)-len(details))
		}
	})
}

func writeUpgradeSummary(ew *errWriter, plan install.UpgradePlan) {
	filesToUpdate := len(plan.TemplateUpdates) + len(plan.SectionAwareUpdates)
	migrationsPlanned := 0
	for _, entry := range plan.MigrationReport.Entries {
		if entry.Status == install.UpgradeMigrationStatusPlanned {
			migrationsPlanned++
		}
	}
	needsReview := len(plan.ReadinessChecks) > 0
	reviewState := affirmativeResponse
	if !needsReview {
		reviewState = "no"
	}
	ew.println(messages.UpgradePlanSummaryHeader)
	ew.printf(messages.UpgradePlanSummaryFilesToAddFmt, len(plan.TemplateAdditions))
	ew.printf(messages.UpgradePlanSummaryFilesToUpdateFmt, filesToUpdate)
	ew.printf(messages.UpgradePlanSummaryFilesToRenameFmt, len(plan.TemplateRenames))
	removals := len(plan.TemplateRemovalsOrOrphans)
	writeHighlightedSummaryLine(ew, removals > 0, messages.UpgradePlanSummaryFilesToReviewFmt, removals)
	ew.printf(messages.UpgradePlanSummaryConfigUpdatesFmt, len(plan.ConfigKeyMigrations))
	ew.printf(messages.UpgradePlanSummaryMigrationsFmt, migrationsPlanned)
	writeHighlightedSummaryLine(ew, len(plan.ReadinessChecks) > 0, messages.UpgradePlanSummaryReadinessWarnFmt, len(plan.ReadinessChecks))
	writeHighlightedSummaryLine(ew, needsReview, messages.UpgradePlanSummaryNeedsReviewFmt, reviewState)
}

// writeHighlightedSummaryLine writes a "  - <text>\n" summary line, optionally
// highlighted in yellow when highlight is true.
func writeHighlightedSummaryLine(ew *errWriter, highlight bool, format string, a ...any) {
	if highlight {
		ew.printf(messages.UpgradePlanSummaryLineFmt, color.YellowString(format, a...))
		return
	}
	ew.printf("  - "+format+"\n", a...)
}

// promptConfigChoice presents a type-aware numbered choice prompt for a config field.
// Returns the selected value converted to the appropriate Go type (bool for FieldBool,
// string for FieldEnum).
func promptConfigChoice(in *bufio.Reader, out io.Writer, key string, manifestValue any, field config.FieldDef) (any, error) {
	switch field.Type {
	case config.FieldBool:
		return promptBoolChoice(in, out, manifestValue)
	case config.FieldEnum:
		return promptEnumChoice(in, out, manifestValue, field)
	default:
		// Freetext / unknown — present the migration value for acceptance.
		if _, err := fmt.Fprintf(out, messages.UpgradeConfigChoiceValueFmt, manifestValue); err != nil {
			return nil, err
		}
		accept, err := promptYesNo(in, out, fmt.Sprintf(messages.UpgradeAcceptValueFmt, manifestValue, key), true)
		if err != nil {
			return nil, err
		}
		if accept {
			return manifestValue, nil
		}
		return nil, fmt.Errorf(messages.UpgradeDeclinedRequiredKeyFmt, key)
	}
}

// promptBoolChoice presents a true/false numbered choice and returns the selected bool.
// Returns an error if manifestValue is not a bool (manifest/schema error).
func promptBoolChoice(in *bufio.Reader, out io.Writer, manifestValue any) (any, error) {
	manBool, ok := manifestValue.(bool)
	if !ok {
		return nil, fmt.Errorf(messages.UpgradeManifestBoolValueErrFmt, manifestValue, manifestValue)
	}
	options := []string{"true", "false"}
	defaultIdx := 1 // false
	if manBool {
		defaultIdx = 0 // true
	}
	chosen, err := promptNumberedChoice(in, out, options, defaultIdx)
	if err != nil {
		return nil, err
	}
	return chosen == 0, nil // index 0 = "true"
}

// promptEnumChoice presents a numbered list of enum options and returns the selected string.
// Returns an error if the manifest value is not in the option list for strict (non-AllowCustom) enums.
func promptEnumChoice(in *bufio.Reader, out io.Writer, manifestValue any, field config.FieldDef) (any, error) {
	manStr := fmt.Sprintf("%v", manifestValue)
	options := make([]string, len(field.Options))
	defaultIdx := -1
	for i, opt := range field.Options {
		label := opt.Value
		if opt.Description != "" {
			label += " - " + opt.Description
		}
		options[i] = label
		if opt.Value == manStr {
			defaultIdx = i
		}
	}
	if defaultIdx < 0 {
		if !field.AllowCustom {
			return nil, fmt.Errorf(messages.UpgradeManifestEnumValueErrFmt, manStr, field.Key)
		}
		// AllowCustom field with a custom manifest value — default to first option.
		defaultIdx = 0
	}
	chosen, err := promptNumberedChoice(in, out, options, defaultIdx)
	if err != nil {
		return nil, err
	}
	return field.Options[chosen].Value, nil
}

// promptNumberedChoice displays a numbered list and reads the user's selection.
// options are display labels; defaultIdx is the 0-based pre-selected option (accepted on Enter).
// Returns the 0-based index of the chosen option.
func promptNumberedChoice(in *bufio.Reader, out io.Writer, options []string, defaultIdx int) (int, error) {
	if _, err := fmt.Fprintln(out, messages.UpgradeNumberedChoiceHeader); err != nil {
		return 0, err
	}
	for i, opt := range options {
		if _, err := fmt.Fprintf(out, messages.UpgradeNumberedChoiceOptionFmt, i+1, opt); err != nil { //nolint:gosec // CLI output, not web
			return 0, err
		}
	}
	for {
		if _, err := fmt.Fprintf(out, messages.UpgradeNumberedChoiceEnterFmt, defaultIdx+1); err != nil {
			return 0, err
		}
		line, err := in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		input := strings.TrimSpace(line)
		if input == "" {
			return defaultIdx, nil
		}
		n, parseErr := strconv.Atoi(input)
		if parseErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		if errors.Is(err, io.EOF) {
			return 0, fmt.Errorf(messages.UpgradeNumberedChoiceInvalidFmt, input)
		}
		if _, retryErr := fmt.Fprintf(out, messages.UpgradeNumberedChoiceRetryFmt, len(options)); retryErr != nil { //nolint:gosec // CLI output, not web
			return 0, retryErr
		}
	}
}
