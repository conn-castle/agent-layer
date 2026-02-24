package wizard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// RunProfile applies a non-interactive wizard profile from a TOML config file.
// root is the repo root; runSync executes sync after writes; pinVersion is used when install is required.
// profilePath points to a TOML file; apply controls whether writes are performed or preview-only output is shown.
func RunProfile(root string, runSync syncer, pinVersion string, profilePath string, apply bool, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	if strings.TrimSpace(profilePath) == "" {
		return fmt.Errorf(messages.WizardProfilePathRequired)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := install.Run(root, install.Options{Overwrite: false, PinVersion: pinVersion, System: install.RealSystem{}}); err != nil {
			return fmt.Errorf(messages.WizardInstallFailedFmt, err)
		}
	} else if err != nil {
		return err
	}

	profileBytes, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf(messages.WizardProfileReadFailedFmt, profilePath, err)
	}
	if _, err := config.ParseConfig(profileBytes, profilePath); err != nil {
		return fmt.Errorf(messages.WizardProfileInvalidFmt, err)
	}

	currentConfig, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if _, err := config.ParseConfigLenient(currentConfig, configPath); err != nil {
		_, _ = fmt.Fprintf(out, messages.WizardProfileExistingConfigInvalidWarnFmt+"\n", err)
	}

	preview := strings.TrimSpace(udiff.Unified(
		".agent-layer/config.toml (current)",
		filepath.ToSlash(profilePath)+" (profile)",
		string(currentConfig),
		string(profileBytes),
	))
	if preview == "" {
		_, _ = fmt.Fprintln(out, messages.WizardProfileNoConfigChanges)
		if !apply {
			return nil
		}
	} else {
		_, _ = fmt.Fprintf(out, "%s\n\n%s\n", messages.WizardProfilePreviewHeader, preview)
		if !apply {
			_, _ = fmt.Fprintln(out, messages.WizardProfilePreviewOnly)
			return nil
		}
	}

	configPerm, err := filePermOr(configPath, 0o644)
	if err != nil {
		return err
	}
	configBackupPath := configPath + ".bak"
	if _, err := writeBackup(configBackupPath, currentConfig, configPerm); err != nil {
		return fmt.Errorf(messages.WizardBackupConfigFailedFmt, err)
	}
	if err := writeFileAtomic(configPath, profileBytes, configPerm); err != nil {
		return fmt.Errorf(messages.WizardWriteConfigFailedFmt, err)
	}

	_, _ = fmt.Fprintln(out, messages.WizardRunningSync)
	result, err := runSync(root)
	if err != nil {
		return err
	}
	if len(result.Warnings) > 0 {
		_, _ = fmt.Fprintln(out)
		warnColor := color.New(color.FgYellow)
		for _, warningItem := range result.Warnings {
			_, _ = warnColor.Fprintf(out, messages.WizardWarningFmt, warningItem.Message)
		}
		_, _ = fmt.Fprintln(out)
	}

	_, _ = fmt.Fprintln(out, messages.WizardCompleted)
	return nil
}

// CleanupBackups removes wizard backup files and returns removed paths relative to repo root.
func CleanupBackups(root string) ([]string, error) {
	candidates := []string{
		filepath.Join(root, ".agent-layer", "config.toml.bak"),
		filepath.Join(root, ".agent-layer", ".env.bak"),
	}

	removed := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			removed = append(removed, path)
			continue
		}
		removed = append(removed, filepath.ToSlash(rel))
	}

	sort.Strings(removed)
	return removed, nil
}
