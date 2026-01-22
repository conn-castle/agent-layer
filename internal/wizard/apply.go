package wizard

import (
	"fmt"
	"os"

	"github.com/nicholasjconn/agent-layer/internal/envfile"
	"github.com/nicholasjconn/agent-layer/internal/fsutil"
	"github.com/nicholasjconn/agent-layer/internal/warnings"
)

type syncer func(root string) ([]warnings.Warning, error)

// applyChanges writes config/env updates and runs sync.
// root/configPath/envPath identify files; c holds wizard selections; runSync is the sync function to call; returns an error on failure.
func applyChanges(root, configPath, envPath string, c *Choices, runSync syncer) error {
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
		return fmt.Errorf("failed to backup config: %w", err)
	}
	// Patch
	newConfig, err := PatchConfig(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf("failed to patch config: %w", err)
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
			return fmt.Errorf("failed to backup .env: %w", err)
		}
	} else if !os.IsNotExist(err) {
		if configBackupCreated {
			_ = os.Remove(configBackupPath)
		}
		return err
	}
	// Patch
	if err := fsutil.WriteFileAtomic(configPath, []byte(newConfig), configPerm); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	newEnv := envfile.Patch(string(rawEnv), c.Secrets)
	if err := fsutil.WriteFileAtomic(envPath, []byte(newEnv), envPerm); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Sync
	fmt.Println("Running sync...")
	warnings, err := runSync(root)
	if err != nil {
		return err
	}
	// Print any warnings from sync
	for _, w := range warnings {
		fmt.Printf("Warning: %s\n", w.Message)
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
