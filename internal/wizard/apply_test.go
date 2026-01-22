package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholasjconn/agent-layer/internal/warnings"
)

func TestApplyChanges(t *testing.T) {
	// Helper to create choices
	choices := NewChoices()
	choices.ApprovalMode = "all"
	choices.ApprovalModeTouched = true // Important!
	choices.Secrets["NEW"] = "secret"

	initialConfig := `[approvals]
mode = "none"
`
	initialEnv := `OLD=value`

	setup := func(t *testing.T) (string, string, string) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		envPath := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		return tmpDir, configPath, envPath
	}

	t.Run("success with backups and sync", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		syncCalled := false
		mockSync := func(root string) ([]warnings.Warning, error) {
			syncCalled = true
			assert.Equal(t, tmpDir, root)
			return nil, nil
		}

		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		require.NoError(t, err)
		assert.True(t, syncCalled)

		// Verify backups
		assert.FileExists(t, configPath+".bak")
		assert.FileExists(t, envPath+".bak")

		// Verify updates
		newConfig, _ := os.ReadFile(configPath)
		assert.Contains(t, string(newConfig), `mode = "all"`)

		newEnv, _ := os.ReadFile(envPath)
		assert.Contains(t, string(newEnv), `NEW=secret`)
	})

	t.Run("config backup failure", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		// Create a directory at configPath.bak to cause WriteFile to fail
		err := os.Mkdir(configPath+".bak", 0755)
		require.NoError(t, err)

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err = applyChanges(tmpDir, configPath, envPath, choices, mockSync)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to backup config")
	})

	t.Run("env backup failure cleans up config backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		// Create a directory at envPath.bak to cause WriteFile to fail
		err := os.Mkdir(envPath+".bak", 0755)
		require.NoError(t, err)

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err = applyChanges(tmpDir, configPath, envPath, choices, mockSync)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to backup .env")

		// Verify config backup was removed (rollback)
		assert.NoFileExists(t, configPath+".bak")
	})

	t.Run("sync failure", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		mockSync := func(root string) ([]warnings.Warning, error) {
			return nil, errors.New("sync exploded")
		}

		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sync exploded")

		// Files should still be updated though, as sync runs after write
		newConfig, _ := os.ReadFile(configPath)
		assert.Contains(t, string(newConfig), `mode = "all"`)
	})

	t.Run("env file missing", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		require.NoError(t, os.Remove(envPath)) // Env file missing

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		require.NoError(t, err)

		// Config backup should exist
		assert.FileExists(t, configPath+".bak")
		// Env backup should NOT exist (since no original env)
		assert.NoFileExists(t, envPath+".bak")

		// New env file should be created
		assert.FileExists(t, envPath)
		newEnv, _ := os.ReadFile(envPath)
		assert.Contains(t, string(newEnv), `NEW=secret`)
	})

	t.Run("backup exists", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Pre-create backups
		require.NoError(t, os.WriteFile(configPath+".bak", []byte("old-backup"), 0644))
		require.NoError(t, os.WriteFile(envPath+".bak", []byte("old-backup"), 0600))

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		require.NoError(t, err)

		// Verify backups were NOT overwritten (based on implementation of writeBackup)
		// Wait, writeBackup checks existence and returns false if exists.
		// Does it error? No.
		// "if _, err := writeBackup(...); err != nil"
		// So it should be fine.

		// Let's verify content is preserved if implementation says so.
		// Actually implementation:
		// _, err := os.Stat(path)
		// backupExists := err == nil
		// if err != nil && !os.IsNotExist(err) { return false, err }
		// if err := os.WriteFile(path, data, perm); err != nil { ... }
		// Wait! writeBackup calls os.WriteFile UNCONDITIONALLY?

		/*
			func writeBackup(path string, data []byte, perm os.FileMode) (bool, error) {
				_, err := os.Stat(path)
				backupExists := err == nil
				if err != nil && !os.IsNotExist(err) {
					return false, err
				}
				// It overwrites!
				if err := os.WriteFile(path, data, perm); err != nil {
					return false, err
				}
				return !backupExists, nil
			}
		*/

		// So it DOES overwrite. It just returns whether it was a *new* backup.
		// So I should expect overwrite.

		bakData, _ := os.ReadFile(configPath + ".bak")
		assert.Equal(t, initialConfig, string(bakData))
	})

	t.Run("config read error", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		envPath := filepath.Join(tmpDir, ".env")
		// Config does not exist
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		assert.Error(t, err)
	})

	t.Run("config stat error", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		envPath := filepath.Join(tmpDir, ".env")
		// Create config but make parent dir unreadable for stat
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		// Replace config with a directory to cause stat to fail for a different error
		require.NoError(t, os.Remove(configPath))
		require.NoError(t, os.Mkdir(configPath, 0755))
		// Write a file inside so we can't read configPath as file
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "dummy"), []byte(""), 0644))

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		assert.Error(t, err)
	})

	t.Run("env stat error cleans config backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Make env path a directory with content
		require.NoError(t, os.Remove(envPath))
		require.NoError(t, os.Mkdir(envPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(envPath, "dummy"), []byte(""), 0644))

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		assert.Error(t, err)
		// Config backup should be cleaned up
		assert.NoFileExists(t, configPath+".bak")
	})

	t.Run("env read error cleans config backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Make env path unreadable
		require.NoError(t, os.Chmod(envPath, 0000))
		t.Cleanup(func() { _ = os.Chmod(envPath, 0600) })

		mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
		assert.Error(t, err)
		// Config backup should be cleaned up
		assert.NoFileExists(t, configPath+".bak")
	})
}

func TestFilePermOr(t *testing.T) {
	t.Run("file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(path, []byte("test"), 0755))

		perm, err := filePermOr(path, 0644)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0755), perm)
	})

	t.Run("file not exists uses fallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "nonexistent.txt")

		perm, err := filePermOr(path, 0600)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), perm)
	})

	t.Run("stat error not ENOENT", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a file to use as "directory" in path
		file := filepath.Join(tmpDir, "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0644))
		// Path through a file causes stat to fail with not ENOENT
		path := filepath.Join(file, "config.toml")

		perm, err := filePermOr(path, 0600)
		assert.Error(t, err)
		assert.Equal(t, os.FileMode(0), perm)
	})
}

func TestWriteBackup(t *testing.T) {
	t.Run("new backup created", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "backup.bak")

		created, err := writeBackup(path, []byte("data"), 0644)
		require.NoError(t, err)
		assert.True(t, created)

		data, _ := os.ReadFile(path)
		assert.Equal(t, "data", string(data))
	})

	t.Run("backup already exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "backup.bak")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0644))

		created, err := writeBackup(path, []byte("new"), 0644)
		require.NoError(t, err)
		assert.False(t, created)

		data, _ := os.ReadFile(path)
		assert.Equal(t, "new", string(data))
	})

	t.Run("write error", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "backup.bak")
		// Create directory to cause write error
		require.NoError(t, os.Mkdir(path, 0755))

		created, err := writeBackup(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.False(t, created)
	})

	t.Run("stat error not ENOENT", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a file to use as "directory" in path
		file := filepath.Join(tmpDir, "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0644))
		// Path through a file causes stat to fail with not ENOENT
		path := filepath.Join(file, "backup.bak")

		created, err := writeBackup(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.False(t, created)
	})
}

func TestApplyChanges_PatchConfigError(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = "all"
	choices.ApprovalModeTouched = true

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	envPath := filepath.Join(tmpDir, ".env")

	// Invalid TOML to cause PatchConfig to fail
	invalidConfig := `[approvals
broken toml`
	require.NoError(t, os.WriteFile(configPath, []byte(invalidConfig), 0644))
	require.NoError(t, os.WriteFile(envPath, []byte("KEY=val"), 0600))

	mockSync := func(root string) ([]warnings.Warning, error) { return nil, nil }
	err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to patch config")
}

func TestApplyChanges_SyncWarnings(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = "all"
	choices.ApprovalModeTouched = true

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	envPath := filepath.Join(tmpDir, ".env")

	initialConfig := `[approvals]
mode = "none"
`
	require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(envPath, []byte(""), 0600))

	// Return warnings from sync
	mockSync := func(root string) ([]warnings.Warning, error) {
		return []warnings.Warning{
			{Code: "TEST_WARNING", Subject: "test", Message: "Test warning message"},
		}, nil
	}

	err := applyChanges(tmpDir, configPath, envPath, choices, mockSync)
	require.NoError(t, err)
}
