package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newUpgradeCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   messages.UpgradeUse,
		Short: messages.UpgradeShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			if !force && !isTerminal() {
				return fmt.Errorf(messages.UpgradeRequiresTerminal)
			}

			targetPin, err := resolvePinVersion("", Version)
			if err != nil {
				return err
			}
			opts := install.Options{
				Overwrite:  true,
				Force:      force,
				PinVersion: targetPin,
				System:     install.RealSystem{},
			}
			if !force {
				opts.Prompter = install.PromptFuncs{
					OverwriteAllFunc: func(paths []string) (bool, error) {
						if err := printFilePaths(cmd.OutOrStdout(), messages.UpgradeOverwriteManagedHeader, paths); err != nil {
							return false, err
						}
						return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeOverwriteAllPrompt, true)
					},
					OverwriteAllMemoryFunc: func(paths []string) (bool, error) {
						if err := printFilePaths(cmd.OutOrStdout(), messages.UpgradeOverwriteMemoryHeader, paths); err != nil {
							return false, err
						}
						return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeOverwriteMemoryAllPrompt, false)
					},
					OverwriteFunc: func(path string) (bool, error) {
						prompt := fmt.Sprintf(messages.UpgradeOverwritePromptFmt, path)
						return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, true)
					},
					DeleteUnknownAllFunc: func(paths []string) (bool, error) {
						if err := printFilePaths(cmd.OutOrStdout(), messages.InstallUnknownHeader, paths); err != nil {
							return false, err
						}
						return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.UpgradeDeleteUnknownAllPrompt, false)
					},
					DeleteUnknownFunc: func(path string) (bool, error) {
						prompt := fmt.Sprintf(messages.UpgradeDeleteUnknownPromptFmt, path)
						return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, false)
					},
				}
			}
			if err := installRun(root, opts); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.AddCommand(newUpgradePlanCmd())

	cmd.Flags().BoolVar(&force, "force", false, messages.UpgradeFlagForce)
	return cmd
}

func newUpgradePlanCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   messages.UpgradePlanUse,
		Short: messages.UpgradePlanShort,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			if outputJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(plan)
			}
			return renderUpgradePlanText(cmd.OutOrStdout(), plan)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, messages.UpgradePlanJSON)
	return cmd
}

func renderUpgradePlanText(out io.Writer, plan install.UpgradePlan) error {
	if _, err := fmt.Fprintln(out, "Upgrade plan (dry-run): no files were written."); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Template additions", plan.TemplateAdditions); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Template updates", plan.TemplateUpdates); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Section-aware updates (managed header only, user entries preserved)", plan.SectionAwareUpdates); err != nil {
		return err
	}
	if err := writeUpgradeRenameSection(out, "Template renames", plan.TemplateRenames); err != nil {
		return err
	}
	if err := writeUpgradeChangeSection(out, "Template removals/orphans", plan.TemplateRemovalsOrOrphans); err != nil {
		return err
	}
	if err := writeConfigMigrationSection(out, "Config key migrations", plan.ConfigKeyMigrations); err != nil {
		return err
	}
	if err := writePinVersionSection(out, plan.PinVersionChange); err != nil {
		return err
	}
	if err := writeOwnershipWarnings(out, plan); err != nil {
		return err
	}
	return nil
}

func writeUpgradeChangeSection(out io.Writer, title string, changes []install.UpgradeChange) error {
	if _, err := fmt.Fprintf(out, "\n%s:\n", title); err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(out, "  - (none)")
		return err
	}
	for _, change := range changes {
		if _, err := fmt.Fprintf(out, "  - %s [%s]\n", change.Path, change.Ownership.Display()); err != nil {
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
		if _, err := fmt.Fprintf(
			out,
			"  - %s -> %s [%s, confidence=%s, detection=%s]\n",
			rename.From,
			rename.To,
			rename.Ownership.Display(),
			rename.Confidence,
			rename.Detection,
		); err != nil {
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

func writePinVersionSection(out io.Writer, pin install.UpgradePinVersionDiff) error {
	if _, err := fmt.Fprintln(out, "\nPin version change:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - current: %q\n", pin.Current); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - target: %q\n", pin.Target); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  - action: %s\n", pin.Action); err != nil {
		return err
	}
	return nil
}

func writeOwnershipWarnings(out io.Writer, plan install.UpgradePlan) error {
	lines := collectUnknownOwnershipLines(plan)
	if len(lines) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nOwnership warnings:"); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(out, "  - %s\n", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, "  - action: run `al upgrade` (or `al upgrade --force`) to apply template changes and capture baseline evidence for ownership labels."); err != nil {
		return err
	}
	return nil
}

func collectUnknownOwnershipLines(plan install.UpgradePlan) []string {
	lines := make([]string, 0)
	appendChange := func(change install.UpgradeChange) {
		if change.OwnershipState != install.OwnershipStateUnknownNoBaseline {
			return
		}
		reasonText := "reasons=none"
		if len(change.OwnershipReasonCodes) != 0 {
			reasonText = "reasons=" + strings.Join(change.OwnershipReasonCodes, ",")
		}
		lines = append(lines, fmt.Sprintf("%s [%s, %s]", change.Path, change.Ownership.Display(), reasonText))
	}
	for _, change := range plan.TemplateAdditions {
		appendChange(change)
	}
	for _, change := range plan.TemplateUpdates {
		appendChange(change)
	}
	for _, change := range plan.SectionAwareUpdates {
		appendChange(change)
	}
	for _, change := range plan.TemplateRemovalsOrOrphans {
		appendChange(change)
	}
	for _, rename := range plan.TemplateRenames {
		if rename.OwnershipState != install.OwnershipStateUnknownNoBaseline {
			continue
		}
		reasonText := "reasons=none"
		if len(rename.OwnershipReasonCodes) != 0 {
			reasonText = "reasons=" + strings.Join(rename.OwnershipReasonCodes, ",")
		}
		lines = append(lines, fmt.Sprintf("%s -> %s [%s, %s]", rename.From, rename.To, rename.Ownership.Display(), reasonText))
	}
	sort.Strings(lines)
	return lines
}
