package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.UpgradeUse,
		Short: messages.UpgradeShort,
	}
	cmd.AddCommand(newUpgradePlanCmd())
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
