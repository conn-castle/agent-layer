package wizard

import (
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
)

type syncer func(root string) (*sync.Result, error)

var writeFileAtomic = fsutil.WriteFileAtomic

// applyChanges writes config/env updates and runs sync.
// root/configPath/envPath identify files; c holds wizard selections; runSync is the sync function to call; returns an error on failure.
func applyChanges(root, configPath, envPath string, c *Choices, runSync syncer, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}

	// Config
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	configPerm, err := filePermOr(configPath, 0644)
	if err != nil {
		return err
	}
	// Backup
	configBackupPath := configPath + ".bak"
	configBackupCreated, err := writeBackup(configBackupPath, rawConfig, configPerm)
	if err != nil {
		return fmt.Errorf(messages.WizardBackupConfigFailedFmt, err)
	}
	// Patch
	newConfig, err := PatchConfig(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}

	// Env
	// Backup if exists
	rawEnv, err := os.ReadFile(envPath)
	envPerm, permErr := filePermOr(envPath, 0600)
	if permErr != nil {
		if configBackupCreated {
			_ = os.Remove(configBackupPath)
		}
		return permErr
	}
	if err == nil {
		envBackupPath := envPath + ".bak"
		if _, err := writeBackup(envBackupPath, rawEnv, envPerm); err != nil {
			if configBackupCreated {
				_ = os.Remove(configBackupPath)
			}
			return fmt.Errorf(messages.WizardBackupEnvFailedFmt, err)
		}
	} else if !os.IsNotExist(err) {
		if configBackupCreated {
			_ = os.Remove(configBackupPath)
		}
		return err
	}
	// Patch
	if err := writeFileAtomic(configPath, []byte(newConfig), configPerm); err != nil {
		return fmt.Errorf(messages.WizardWriteConfigFailedFmt, err)
	}
	newEnv := envfile.Patch(string(rawEnv), c.Secrets)
	if err := writeFileAtomic(envPath, []byte(newEnv), envPerm); err != nil {
		if rollbackErr := writeFileAtomic(configPath, rawConfig, configPerm); rollbackErr != nil {
			return fmt.Errorf(messages.WizardWriteEnvRollbackConfigFailedFmt, err, rollbackErr)
		}
		return fmt.Errorf(messages.WizardWriteEnvFailedFmt, err)
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

// writeBackup writes a backup file and reports whether a new backup was created.
// path is the backup file path; data is the source content; perm is the file mode to apply.
func writeBackup(path string, data []byte, perm os.FileMode) (bool, error) {
	_, err := os.Stat(path)
	backupExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, err
	}
	return !backupExists, nil
}
