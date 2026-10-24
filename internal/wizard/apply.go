package wizard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
	"github.com/conn-castle/agent-layer/internal/sync"
)

type syncer func(root string) (*sync.Result, error)

var writeFileAtomic = fsutil.WriteFileAtomic

// applyChanges writes config/env updates and runs sync.
// root/configPath/envPath identify files; c holds wizard selections; runSync is the sync function to call; returns an error on failure.
func applyChanges(root, configPath, envPath string, c *Choices, runSync syncer, out io.Writer) (err error) {
	sourcesApplied := false
	defer func() {
		if err != nil && sourcesApplied {
			err = fmt.Errorf("import source changes were committed; projection was not completed: %w", err)
		}
	}()
	if out == nil {
		out = os.Stdout
	}

	if err := skillmigration.WithRecovered(root, func() error {
		return writeConfigEnv(root, configPath, envPath, c)
	}); err != nil {
		return err
	}

	// Skills, instruction imports, and memory file seeds.
	// Run before sync so sync sees the final on-disk layout.
	skillsChangeSet, err := computeSkillsChangeSet(root, c)
	if err != nil {
		return fmt.Errorf(messages.WizardApplySkillsFailedFmt+"; config/env writes were applied; gitignore block updates, statusline source updates, and sync were not run", err)
	}
	if c.previewedSkills != nil {
		skillsChangeSet.adoptLegacy = c.previewedSkills.adoptLegacy
	}
	if err := applySkillsChanges(root, skillsChangeSet); err != nil {
		return fmt.Errorf(messages.WizardApplySkillsFailedFmt+"; config/env writes were applied; gitignore block updates, statusline source updates, and sync were not run", err)
	}
	sourcesApplied = len(skillsChangeSet.importSelectors)+len(skillsChangeSet.removeSelectors)+len(skillsChangeSet.instructionImports) > 0

	gitignoreChangeSet, err := computeGitignoreBlockChangeSet(root, c)
	if err != nil {
		return err
	}
	if err := applyGitignoreBlockChanges(root, gitignoreChangeSet); err != nil {
		return err
	}

	statuslineSourceChangeSet, err := computeStatuslineSourceChangeSet(root, c)
	if err != nil {
		return err
	}
	if err := applyStatuslineSourceChanges(root, statuslineSourceChangeSet); err != nil {
		return err
	}

	// Sync
	_, _ = fmt.Fprintln(out, messages.WizardRunningSync)
	result, err := runSync(root)
	if err != nil {
		return err
	}
	// Print any warnings from sync
	if len(result.Warnings) > 0 {
		_, _ = fmt.Fprintln(out)
		warnColor := color.New(color.FgYellow)
		for _, w := range result.Warnings {
			_, _ = warnColor.Fprintf(out, messages.WizardWarningFmt, w.Message)
		}
		_, _ = fmt.Fprintln(out)
	}
	return nil
}

func writeConfigEnv(root, configPath, envPath string, c *Choices) error {
	// Config
	rawConfig, err := os.ReadFile(configPath) // #nosec G304 -- configPath is the caller-resolved .agent-layer/config.toml path used by wizard apply.
	if err != nil {
		return err
	}
	configPerm, err := filePermOr(configPath, 0644)
	if err != nil {
		return err
	}
	// Prepare and validate the complete config before creating backups or writing.
	newConfig, err := PatchConfig(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}
	newConfig, err = config.MigrateInstructionOrderFS(os.DirFS(root), root, newConfig)
	if err != nil {
		return fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}
	// Env
	rawEnv, err := os.ReadFile(envPath) // #nosec G304 -- envPath is the caller-resolved .agent-layer/.env path used by wizard apply.
	envExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	envPerm, err := filePermOr(envPath, 0600)
	if err != nil {
		return err
	}
	newEnv := envfile.Patch(string(rawEnv), c.Secrets)

	// Back up and write only documents whose bytes change, so an unchanged
	// document keeps its previous recovery backup. A missing .env is created.
	configChanged := newConfig != string(rawConfig)
	envChanged := !envExists || newEnv != string(rawEnv)
	configBackupCreated := false
	if configChanged {
		configBackupCreated, err = writeBackup(root, configBackupName, rawConfig, configPerm)
		if err != nil {
			return fmt.Errorf(messages.WizardBackupConfigFailedFmt, err)
		}
	}
	if envExists && envChanged {
		if _, err := writeBackup(root, envBackupName, rawEnv, envPerm); err != nil {
			if configBackupCreated {
				_ = os.Remove(backupPath(root, configBackupName))
			}
			return fmt.Errorf(messages.WizardBackupEnvFailedFmt, err)
		}
	}
	// Patch
	if configChanged {
		if err := writeFileAtomic(configPath, []byte(newConfig), configPerm); err != nil {
			return fmt.Errorf(messages.WizardWriteConfigFailedFmt, err)
		}
	}
	if envChanged {
		if err := writeFileAtomic(envPath, []byte(newEnv), envPerm); err != nil {
			if !configChanged {
				return fmt.Errorf(messages.WizardWriteEnvFailedFmt, err)
			}
			if rollbackErr := writeFileAtomic(configPath, rawConfig, configPerm); rollbackErr != nil {
				return fmt.Errorf(messages.WizardWriteEnvRollbackConfigFailedFmt, err, rollbackErr)
			}
			return fmt.Errorf(messages.WizardWriteEnvFailedFmt, err)
		}
	}

	return nil
}

// filePermOr returns the file permission bits or a fallback when the file is missing.
// path is the file to inspect; fallback is the permission to use when the file does not exist.
func filePermOr(path string, fallback os.FileMode) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fallback, nil
		}
		return 0, err
	}
	return info.Mode().Perm(), nil
}

// writeBackup writes the named wizard backup and reports whether a new backup was created.
// root is the repo root; name is the backup file name; data is the source content; perm is the file mode to apply.
func writeBackup(root, name string, data []byte, perm os.FileMode) (bool, error) {
	if err := validateBackupStorage(root); err != nil {
		return false, err
	}
	path := backupPath(root, name)
	if err := fsutil.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	_, err := os.Stat(path)
	backupExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := fsutil.WriteFileAtomic(path, data, perm); err != nil {
		return false, err
	}
	return !backupExists, nil
}
