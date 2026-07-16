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
)

var installRepairGitignoreBlock = install.RepairGitignoreBlock
var dispatchPrefetchVersion = versiondispatch.PrefetchVersion

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
			if strings.TrimSpace(pinVersion) != "" && !strings.EqualFold(strings.TrimSpace(pinVersion), "latest") {
				if err := validatePinnedReleaseVersionFunc(cmd.Context(), targetPin); err != nil {
					return err
				}
			}
			reviewState := buildUpgradeReviewState(policy)
			opts := install.Options{
				Overwrite:    true,
				PinVersion:   targetPin,
				DiffMaxLines: diffLines,
				System:       install.RealSystem{},
			}
			opts.Prompter = buildUpgradePrompter(cmd, policy, reviewState)
			if err := installRun(root, opts); err != nil {
				return err
			}
			if err := runPostUpgradeSync(cmd.OutOrStdout(), cmd.ErrOrStderr(), root); err != nil {
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
			if err := dispatchPrefetchVersion(targetVersion, cmd.ErrOrStderr()); err != nil {
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

type upgradeReviewState struct {
	enabled                       bool
	prompted                      bool
	statuslineSourceSkipAnnounced bool
	managedPreviews               []install.DiffPreview
	memoryPreviews                []install.DiffPreview
	applyManaged                  bool
	applyMemory                   bool
}

func buildUpgradeReviewState(policy upgradeApplyPolicy) *upgradeReviewState {
	state := &upgradeReviewState{enabled: false}
	if !policy.interactive || policy.explicitCategory {
		return state
	}
	state.enabled = true
	return state
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

func buildUpgradePrompter(cmd *cobra.Command, policy upgradeApplyPolicy, reviewState *upgradeReviewState) install.PromptFuncs {
	// Shared buffered reader for all prompts in this upgrade session. Creating
	// a single reader prevents buffered stdin bytes from being lost when
	// multiple prompts are issued sequentially (e.g., chained config_set_default
	// migration operations).
	stdinReader := bufio.NewReader(cmd.InOrStdin())

	return install.PromptFuncs{
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
		OverwriteAllPreviewFunc: func(previews []install.DiffPreview) (bool, error) {
			if policy.explicitCategory {
				return policy.applyManaged, nil
			}
			if reviewState != nil && reviewState.enabled {
				if err := promptUnifiedUpgradeReview(cmd, stdinReader, reviewState); err != nil {
					return false, err
				}
				return reviewState.applyManaged, nil
			}
			return promptOverwriteSection(stdinReader, cmd.OutOrStdout(), messages.UpgradeOverwriteManagedHeader, previews, messages.UpgradeOverwriteAllPrompt, true)
		},
		OverwriteAllMemoryPreviewFunc: func(previews []install.DiffPreview) (bool, error) {
			if policy.explicitCategory {
				return policy.applyMemory, nil
			}
			if reviewState != nil && reviewState.enabled {
				if err := promptUnifiedUpgradeReview(cmd, stdinReader, reviewState); err != nil {
					return false, err
				}
				return reviewState.applyMemory, nil
			}
			return promptOverwriteSection(stdinReader, cmd.OutOrStdout(), messages.UpgradeOverwriteMemoryHeader, previews, messages.UpgradeOverwriteMemoryAllPrompt, false)
		},
		OverwriteAllUnifiedPreviewFunc: func(managedPreviews []install.DiffPreview, memoryPreviews []install.DiffPreview) (bool, bool, error) {
			if policy.explicitCategory {
				return policy.applyManaged, policy.applyMemory, nil
			}
			if reviewState != nil && reviewState.enabled {
				reviewState.managedPreviews = managedPreviews
				reviewState.memoryPreviews = memoryPreviews
				if err := promptUnifiedUpgradeReview(cmd, stdinReader, reviewState); err != nil {
					return false, false, err
				}
				return reviewState.applyManaged, reviewState.applyMemory, nil
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
				if reviewState != nil && !reviewState.statuslineSourceSkipAnnounced {
					if _, err := fmt.Fprintln(cmd.ErrOrStderr(), messages.UpgradeSkipStatuslineSourceUpdatesInfo); err != nil {
						return false, err
					}
					reviewState.statuslineSourceSkipAnnounced = true
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

func promptUnifiedUpgradeReview(cmd *cobra.Command, in io.Reader, state *upgradeReviewState) error {
	if state.prompted {
		return nil
	}
	applyManaged, applyMemory, err := promptUnifiedOverwriteSections(in, cmd.OutOrStdout(), state.managedPreviews, state.memoryPreviews)
	if err != nil {
		return err
	}
	state.applyManaged = applyManaged
	state.applyMemory = applyMemory
	state.prompted = true
	return nil
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

// promptOverwriteSection renders the file list (with +/- stats), asks if the
// user wants to view the full diff (default no), optionally renders the diff
// bodies, and finally asks the apply prompt. Returns false with no prompts
// when previews is empty.
func promptOverwriteSection(in io.Reader, out io.Writer, header string, previews []install.DiffPreview, applyPrompt string, applyDefault bool) (bool, error) {
	if len(previews) == 0 {
		return false, nil
	}
	reader := bufferedReader(in)
	if err := printDiffPreviewSummary(out, header, previews); err != nil {
		return false, err
	}
	if err := promptOptionalViewDiff(reader, out, previews); err != nil {
		return false, err
	}
	return promptYesNo(reader, out, applyPrompt, applyDefault)
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
	if !policy.applyTmpDeletions {
		if _, err := fmt.Fprintln(out, messages.UpgradeSkipTmpDeletionsInfo); err != nil {
			return err
		}
	}
	return nil
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

func renderUpgradePlanText(out io.Writer, plan install.UpgradePlan, previews map[string]install.DiffPreview) error {
	if _, err := fmt.Fprintln(out, messages.UpgradePlanDryRunNoFiles); err != nil {
		return err
	}
	if err := writeUpgradeSummary(out, plan); err != nil {
		return err
	}
	allUpdates := make([]install.UpgradeChange, 0, len(plan.TemplateUpdates)+len(plan.SectionAwareUpdates))
	allUpdates = append(allUpdates, plan.TemplateUpdates...)
	allUpdates = append(allUpdates, plan.SectionAwareUpdates...)
	if err := writeUpgradeChangeSection(out, messages.UpgradePlanSectionFilesToAdd, plan.TemplateAdditions, previews); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, messages.UpgradePlanSectionStatuslineFilesToAdd, plan.StatuslineSourceAdditions, previews); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, messages.UpgradePlanSectionFilesToUpdate, allUpdates, previews); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, messages.UpgradePlanSectionStatuslineToReview, plan.StatuslineSourceUpdates, previews); err != nil {
		return err
	}
	if err := writeUpgradeRenameSection(out, messages.UpgradePlanSectionFilesToRename, plan.TemplateRenames); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, messages.UpgradePlanSectionFilesToReviewRemoval, plan.TemplateRemovalsOrOrphans, previews); err != nil {
		return err
	}
	if err := writeConfigMigrationSection(out, messages.UpgradePlanSectionConfigUpdates, plan.ConfigKeyMigrations); err != nil {
		return err
	}
	if err := writeMigrationReportSection(out, messages.UpgradePlanSectionMigrations, plan.MigrationReport); err != nil {
		return err
	}
	if err := writePinVersionSection(out, plan.PinVersionChange); err != nil {
		return err
	}
	if err := writeReadinessSection(out, plan.ReadinessChecks); err != nil {
		return err
	}
	return nil
}

func writeUpgradeChangeSection(out io.Writer, title string, changes []install.UpgradeChange, previews map[string]install.DiffPreview) error {
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSectionTitleFmt, title); err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(out, messages.UpgradePlanNone)
		return err
	}
	for _, change := range changes {
		if _, err := fmt.Fprintf(out, messages.UpgradePlanItemFmt, change.Path); err != nil {
			return err
		}
		if err := writeSinglePreviewBlock(out, previews[change.Path]); err != nil {
			return err
		}
	}
	return nil
}

func writeUpgradeRenameSection(out io.Writer, title string, renames []install.UpgradeRename) error {
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSectionTitleFmt, title); err != nil {
		return err
	}
	if len(renames) == 0 {
		_, err := fmt.Fprintln(out, messages.UpgradePlanNone)
		return err
	}
	for _, rename := range renames {
		if _, err := fmt.Fprintf(out, messages.UpgradePlanRenameItemFmt, rename.From, rename.To); err != nil {
			return err
		}
	}
	return nil
}

func writeConfigMigrationSection(out io.Writer, title string, migrations []install.ConfigKeyMigration) error {
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSectionTitleFmt, title); err != nil {
		return err
	}
	if len(migrations) == 0 {
		_, err := fmt.Fprintln(out, messages.UpgradePlanNone)
		return err
	}
	for _, migration := range migrations {
		if _, err := fmt.Fprintf(out, messages.UpgradePlanConfigItemFmt, migration.Key, migration.From, migration.To); err != nil {
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

func writeMigrationReportSection(out io.Writer, title string, report install.UpgradeMigrationReport) error { //nolint:unparam // title kept for consistency with other write*Section functions
	ew := &errWriter{w: out}
	ew.printf(messages.UpgradePlanSectionTitleFmt, title)
	if len(report.Entries) == 0 {
		ew.println(messages.UpgradePlanNone)
		return ew.err
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
	return ew.err
}

func writePinVersionSection(out io.Writer, pin install.UpgradePinVersionDiff) error {
	ew := &errWriter{w: out}
	ew.println(messages.UpgradePlanPinVersionHeader)
	ew.printf(messages.UpgradePlanPinCurrentFmt, pin.Current)
	ew.printf(messages.UpgradePlanPinTargetFmt, pin.Target)
	ew.printf(messages.UpgradePlanPinActionFmt, pin.Action)
	return ew.err
}

func writeSinglePreviewBlock(out io.Writer, preview install.DiffPreview) error {
	if strings.TrimSpace(preview.UnifiedDiff) == "" {
		return nil
	}
	if _, err := fmt.Fprintln(out, messages.UpgradePlanDiffLabel); err != nil {
		return err
	}
	if err := writeUnifiedDiff(out, preview.UnifiedDiff, shouldColorizeDiffOutput(), "      "); err != nil {
		return err
	}
	return nil
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

func writeReadinessSection(out io.Writer, checks []install.UpgradeReadinessCheck) error {
	if _, err := fmt.Fprintln(out, messages.UpgradePlanReadinessHeader); err != nil {
		return err
	}
	if len(checks) == 0 {
		_, err := fmt.Fprintln(out, messages.UpgradePlanNone)
		return err
	}
	for _, check := range checks {
		if _, err := fmt.Fprintf(out, messages.UpgradePlanReadinessItemFmt, color.YellowString("%s", readinessSummary(check))); err != nil {
			return err
		}
		action := readinessAction(check.ID)
		if action != "" {
			if _, err := fmt.Fprintf(out, messages.UpgradePlanReadinessRecommendationFmt, action); err != nil {
				return err
			}
		}
		details := check.Details
		if len(details) > 3 {
			details = details[:3]
		}
		for _, detail := range details {
			if _, err := fmt.Fprintf(out, messages.UpgradePlanReadinessNoteFmt, detail); err != nil {
				return err
			}
		}
		if len(check.Details) > len(details) {
			if _, err := fmt.Fprintf(out, messages.UpgradePlanReadinessNoteMoreFmt, len(check.Details)-len(details)); err != nil {
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
	if _, err := fmt.Fprintln(out, messages.UpgradePlanSummaryHeader); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSummaryFilesToAddFmt, len(plan.TemplateAdditions)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSummaryFilesToUpdateFmt, filesToUpdate); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSummaryFilesToRenameFmt, len(plan.TemplateRenames)); err != nil {
		return err
	}
	removals := len(plan.TemplateRemovalsOrOrphans)
	if err := writeHighlightedSummaryLine(out, removals > 0, messages.UpgradePlanSummaryFilesToReviewFmt, removals); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSummaryConfigUpdatesFmt, len(plan.ConfigKeyMigrations)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, messages.UpgradePlanSummaryMigrationsFmt, migrationsPlanned); err != nil {
		return err
	}
	if err := writeHighlightedSummaryLine(out, len(plan.ReadinessChecks) > 0, messages.UpgradePlanSummaryReadinessWarnFmt, len(plan.ReadinessChecks)); err != nil {
		return err
	}
	if err := writeHighlightedSummaryLine(out, needsReview, messages.UpgradePlanSummaryNeedsReviewFmt, reviewState); err != nil {
		return err
	}
	return nil
}

// writeHighlightedSummaryLine writes a "  - <text>\n" summary line, optionally
// highlighted in yellow when highlight is true.
func writeHighlightedSummaryLine(out io.Writer, highlight bool, format string, a ...any) error {
	if highlight {
		_, err := fmt.Fprintf(out, messages.UpgradePlanSummaryLineFmt, color.YellowString(format, a...))
		return err
	}
	_, err := fmt.Fprintf(out, "  - "+format+"\n", a...)
	return err
}

func readinessSummary(check install.UpgradeReadinessCheck) string {
	switch check.ID {
	case issueUnrecognizedConfigKeys:
		return messages.UpgradeReadinessUnrecognizedKeys
	case issueUnresolvedConfigPlaceholders:
		return messages.UpgradeReadinessUnresolvedPlaceholder
	case issueProcessEnvOverridesDotenv:
		return messages.UpgradeReadinessProcessEnvOverrides
	case issueIgnoredEmptyDotenvAssignments:
		return messages.UpgradeReadinessEmptyDotenv
	case issuePathExpansionAnomalies:
		return messages.UpgradeReadinessPathExpansion
	case issueVSCodeNoSyncOutputsStale:
		return messages.UpgradeReadinessVSCodeStale
	case issueFloatingExternalDependencySpecs:
		return messages.UpgradeReadinessFloatingDeps
	case issueStaleDisabledAgentArtifacts:
		return messages.UpgradeReadinessStaleDisabledAgents
	case issueMissingRequiredConfigFields:
		return messages.UpgradeReadinessMissingRequiredFields
	default:
		return check.Summary
	}
}

func readinessAction(id string) string {
	switch id {
	case issueUnrecognizedConfigKeys:
		return messages.UpgradeReadinessActionUnrecognizedKeys
	case issueUnresolvedConfigPlaceholders:
		return messages.UpgradeReadinessActionUnresolvedPlaceholder
	case issueProcessEnvOverridesDotenv:
		return messages.UpgradeReadinessActionProcessEnvOverrides
	case issueIgnoredEmptyDotenvAssignments:
		return messages.UpgradeReadinessActionEmptyDotenv
	case issuePathExpansionAnomalies:
		return messages.UpgradeReadinessActionPathExpansion
	case issueVSCodeNoSyncOutputsStale:
		return messages.UpgradeReadinessActionVSCodeStale
	case issueFloatingExternalDependencySpecs:
		return messages.UpgradeReadinessActionFloatingDeps
	case issueStaleDisabledAgentArtifacts:
		return messages.UpgradeReadinessActionStaleDisabledAgents
	case issueMissingRequiredConfigFields:
		return messages.UpgradeReadinessActionMissingRequiredFields
	default:
		return ""
	}
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
