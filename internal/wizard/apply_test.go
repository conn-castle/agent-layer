package wizard

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/warnings"
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
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".agent-layer", "state", "wizard-backups"), 0700))
		return tmpDir, configPath, envPath
	}

	t.Run("success with backups and sync", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		syncCalled := false
		mockSync := func(root string) (*alsync.Result, error) {
			syncCalled = true
			assert.Equal(t, tmpDir, root)
			return &alsync.Result{}, nil
		}

		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		require.NoError(t, err)
		assert.True(t, syncCalled)

		// Verify backups
		assert.FileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
		assert.FileExists(t, filepath.Join(filepath.Dir(envPath), ".agent-layer", "state", "wizard-backups", ".env.bak"))

		// Verify updates
		newConfig, _ := os.ReadFile(configPath)
		assert.Contains(t, string(newConfig), `mode = "all"`)

		newEnv, _ := os.ReadFile(envPath)
		assert.Contains(t, string(newEnv), `NEW=secret`)
	})

	t.Run("config backup failure", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		// Create a directory at configPath.bak to cause WriteFile to fail
		err := os.Mkdir(filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"), 0700)
		require.NoError(t, err)

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err = applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to backup config")
	})

	t.Run("env backup failure cleans up config backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		// Create a directory at envPath.bak to cause WriteFile to fail
		err := os.Mkdir(filepath.Join(filepath.Dir(envPath), ".agent-layer", "state", "wizard-backups", ".env.bak"), 0700)
		require.NoError(t, err)

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err = applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to backup .env")

		// Verify config backup was removed (rollback)
		assert.NoFileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
	})

	t.Run("sync failure", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		mockSync := func(root string) (*alsync.Result, error) {
			return nil, errors.New("sync exploded")
		}

		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sync exploded")

		// Files should still be updated though, as sync runs after write
		newConfig, _ := os.ReadFile(configPath)
		assert.Contains(t, string(newConfig), `mode = "all"`)
	})

	t.Run("env file missing", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		require.NoError(t, os.Remove(envPath)) // Env file missing

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		require.NoError(t, err)

		// Config backup should exist
		assert.FileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
		// Env backup should NOT exist (since no original env)
		assert.NoFileExists(t, filepath.Join(filepath.Dir(envPath), ".agent-layer", "state", "wizard-backups", ".env.bak"))

		// New env file should be created
		assert.FileExists(t, envPath)
		newEnv, _ := os.ReadFile(envPath)
		assert.Contains(t, string(newEnv), `NEW=secret`)
	})

	t.Run("backup exists", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Pre-create backups
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"), []byte("old-backup"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(envPath), ".agent-layer", "state", "wizard-backups", ".env.bak"), []byte("old-backup"), 0600))

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		require.NoError(t, err)

		// writeBackup always overwrites; it only returns whether the backup was new.
		bakData, _ := os.ReadFile(filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
		assert.Equal(t, initialConfig, string(bakData))
	})

	t.Run("config read error", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		envPath := filepath.Join(tmpDir, ".env")
		// Config does not exist
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
	})

	t.Run("config stat error", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		envPath := filepath.Join(tmpDir, ".env")
		// Create config but make parent dir unreadable for stat
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		// Replace config with a directory to cause stat to fail for a different error
		require.NoError(t, os.Remove(configPath))
		require.NoError(t, os.Mkdir(configPath, 0700))
		// Write a file inside so we can't read configPath as file
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "dummy"), []byte(""), 0600))

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
	})

	t.Run("env stat error creates no backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Make env path a directory with content
		require.NoError(t, os.Remove(envPath))
		require.NoError(t, os.Mkdir(envPath, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(envPath, "dummy"), []byte(""), 0600))

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		// No config backup is written before the env read fails
		assert.NoFileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
	})

	t.Run("env read error creates no backup", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)
		// Make env path unreadable
		require.NoError(t, os.Chmod(envPath, 0000))
		t.Cleanup(func() { _ = os.Chmod(envPath, 0600) })

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		// No config backup is written before the env read fails
		assert.NoFileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
	})

	t.Run("config write error", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			if path == configPath {
				return errors.New("write config failed")
			}
			return origWrite(path, data, perm)
		}

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write config")
	})

	t.Run("env write error", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			if path == envPath {
				return errors.New("write env failed")
			}
			return origWrite(path, data, perm)
		}

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write .env")

		rolledBackConfig, readErr := os.ReadFile(configPath)
		require.NoError(t, readErr)
		assert.Equal(t, initialConfig, string(rolledBackConfig))
	})

	t.Run("env write error and config rollback failure", func(t *testing.T) {
		tmpDir, configPath, envPath := setup(t)

		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			if path == envPath {
				return errors.New("write env failed")
			}
			if path == configPath && string(data) == initialConfig {
				return errors.New("rollback failed")
			}
			return origWrite(path, data, perm)
		}

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "also failed to rollback config")
	})

	t.Run("env perm error creates no backup", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))

		envRoot := filepath.Join(tmpDir, "env-root")
		require.NoError(t, os.WriteFile(envRoot, []byte("not a dir"), 0600))
		envPath := filepath.Join(envRoot, ".env")

		mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
		err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
		assert.Error(t, err)
		assert.NoFileExists(t, filepath.Join(filepath.Dir(configPath), ".agent-layer", "state", "wizard-backups", "config.toml.bak"))
	})
}

func TestFilePermOr(t *testing.T) {
	t.Run("file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(path, []byte("test"), 0755)) // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.

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
		require.NoError(t, os.WriteFile(file, []byte("x"), 0600))
		// Path through a file causes stat to fail with not ENOENT
		path := filepath.Join(file, "config.toml")

		perm, err := filePermOr(path, 0600)
		assert.Error(t, err)
		assert.Equal(t, os.FileMode(0), perm)
	})
}

func TestWriteBackup(t *testing.T) {
	t.Run("restore removes a new backup", func(t *testing.T) {
		tmpDir := t.TempDir()

		restore, err := writeBackup(tmpDir, configBackupName, []byte("data"), 0644)
		require.NoError(t, err)

		data, _ := os.ReadFile(backupPath(tmpDir, configBackupName))
		assert.Equal(t, "data", string(data))
		require.NoError(t, restore())
		assert.NoFileExists(t, backupPath(tmpDir, configBackupName))
	})

	t.Run("restore returns the previous backup", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := backupPath(tmpDir, configBackupName)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("old"), 0600))

		restore, err := writeBackup(tmpDir, configBackupName, []byte("new"), 0644)
		require.NoError(t, err)

		data, _ := os.ReadFile(path)
		assert.Equal(t, "new", string(data))
		require.NoError(t, restore())
		data, _ = os.ReadFile(path)
		assert.Equal(t, "old", string(data))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("write error", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Dir(backupPath(tmpDir, configBackupName)), 0700))
		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(string, []byte, os.FileMode) error { return errors.New("write failed") }

		_, err := writeBackup(tmpDir, configBackupName, []byte("data"), 0644)
		assert.ErrorContains(t, err, "write failed")
	})

	t.Run("write failure after rename restores the previous state", func(t *testing.T) {
		for _, previous := range []string{"", "old"} {
			tmpDir := t.TempDir()
			path := backupPath(tmpDir, configBackupName)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			if previous != "" {
				require.NoError(t, os.WriteFile(path, []byte(previous), 0600))
			}
			origWrite := writeFileAtomic
			writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
				if err := origWrite(path, data, perm); err != nil {
					return err
				}
				if string(data) == "new" {
					return errors.New("sync dir failed")
				}
				return nil
			}

			_, err := writeBackup(tmpDir, configBackupName, []byte("new"), 0644)
			writeFileAtomic = origWrite
			require.ErrorContains(t, err, "sync dir failed")
			if previous == "" {
				assert.NoFileExists(t, path)
			} else {
				data, _ := os.ReadFile(path)
				assert.Equal(t, previous, string(data))
			}
		}
	})

	t.Run("storage is not a directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".agent-layer"), []byte("x"), 0600))

		_, err := writeBackup(tmpDir, configBackupName, []byte("data"), 0644)
		assert.ErrorContains(t, err, "must be a real directory")
	})
}

func TestApplyChanges_AbortRestoresPreviousBackups(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = "all"
	choices.ApprovalModeTouched = true
	choices.Secrets["NEW"] = "secret"
	initialConfig := "[approvals]\nmode = \"none\"\n"
	initialEnv := "OLD=value\n"

	setup := func(t *testing.T) (string, string, string) {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.toml")
		envPath := filepath.Join(root, ".env")
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		for _, name := range []string{configBackupName, envBackupName} {
			_, err := writeBackup(root, name, []byte("previous "+name), 0600)
			require.NoError(t, err)
		}
		return root, configPath, envPath
	}
	failWrites := func(t *testing.T, fail func(path string, data []byte) error) {
		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			if err := fail(path, data); err != nil {
				return err
			}
			return origWrite(path, data, perm)
		}
	}
	requireFile := func(t *testing.T, path, want string) {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
	}
	syncOK := func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	t.Run("env backup failure", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		failWrites(t, func(path string, _ []byte) error {
			if path == backupPath(root, envBackupName) {
				return errors.New("env backup failed")
			}
			return nil
		})

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "failed to backup .env")

		requireFile(t, configPath, initialConfig)
		requireFile(t, backupPath(root, configBackupName), "previous "+configBackupName)
		requireFile(t, backupPath(root, envBackupName), "previous "+envBackupName)
	})

	t.Run("config write failure", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		failWrites(t, func(path string, _ []byte) error {
			if path == configPath {
				return errors.New("write config failed")
			}
			return nil
		})

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "failed to write config")

		requireFile(t, backupPath(root, configBackupName), "previous "+configBackupName)
		requireFile(t, backupPath(root, envBackupName), "previous "+envBackupName)
	})

	t.Run("env write failure after config rollback", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		failWrites(t, func(path string, _ []byte) error {
			if path == envPath {
				return errors.New("write env failed")
			}
			return nil
		})

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "failed to write .env")

		requireFile(t, configPath, initialConfig)
		requireFile(t, backupPath(root, configBackupName), "previous "+configBackupName)
		requireFile(t, backupPath(root, envBackupName), "previous "+envBackupName)
	})

	t.Run("failed config rollback keeps its original backup", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		failWrites(t, func(path string, data []byte) error {
			if path == envPath {
				return errors.New("write env failed")
			}
			if path == configPath && string(data) == initialConfig {
				return errors.New("rollback failed")
			}
			return nil
		})

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "also failed to rollback config")

		requireFile(t, backupPath(root, configBackupName), initialConfig)
		requireFile(t, backupPath(root, envBackupName), "previous "+envBackupName)
	})

	t.Run("backup restore failure is reported", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		failWrites(t, func(path string, data []byte) error {
			switch {
			case path == configPath:
				return errors.New("write config failed")
			case path == backupPath(root, configBackupName) && string(data) == "previous "+configBackupName:
				return errors.New("restore failed")
			}
			return nil
		})

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "failed to write config")
		require.ErrorContains(t, err, "restore wizard backup")
		requireFile(t, backupPath(root, envBackupName), "previous "+envBackupName)
	})
}

func TestApplyChanges_WritesOnlyChangedDocuments(t *testing.T) {
	noop := func() *Choices {
		choices := NewChoices()
		choices.ApprovalMode = "none"
		choices.ApprovalModeTouched = true
		return choices
	}
	// Start from patched output so replaying the same choices changes nothing.
	initialConfig, err := PatchConfig("[approvals]\nmode = \"none\"\n", noop())
	require.NoError(t, err)
	initialEnv := "OLD=value\n"

	setup := func(t *testing.T) (string, string, string) {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.toml")
		envPath := filepath.Join(root, ".env")
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
		require.NoError(t, os.WriteFile(envPath, []byte(initialEnv), 0600))
		for _, name := range []string{configBackupName, envBackupName} {
			_, err := writeBackup(root, name, []byte("previous "+name), 0600)
			require.NoError(t, err)
		}
		return root, configPath, envPath
	}
	stat := func(t *testing.T, path string) os.FileInfo {
		info, err := os.Stat(path)
		require.NoError(t, err)
		return info
	}
	requireBackup := func(t *testing.T, root, name, want string) {
		data, err := os.ReadFile(backupPath(root, name))
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
	}
	syncOK := func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	t.Run("unchanged documents keep files and backups", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		configBefore, envBefore := stat(t, configPath), stat(t, envPath)

		require.NoError(t, applyChanges(root, configPath, envPath, noop(), syncOK, io.Discard))

		assert.True(t, os.SameFile(configBefore, stat(t, configPath)))
		assert.True(t, os.SameFile(envBefore, stat(t, envPath)))
		requireBackup(t, root, configBackupName, "previous "+configBackupName)
		requireBackup(t, root, envBackupName, "previous "+envBackupName)
	})

	t.Run("env-only change keeps config and its backup", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		configBefore := stat(t, configPath)
		choices := noop()
		choices.Secrets["NEW"] = "secret"

		require.NoError(t, applyChanges(root, configPath, envPath, choices, syncOK, io.Discard))

		assert.True(t, os.SameFile(configBefore, stat(t, configPath)))
		requireBackup(t, root, configBackupName, "previous "+configBackupName)
		requireBackup(t, root, envBackupName, initialEnv)
		newEnv, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assert.Contains(t, string(newEnv), "NEW=secret")
	})

	t.Run("config-only change keeps env and its backup", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		envBefore := stat(t, envPath)
		choices := noop()
		choices.ApprovalMode = "all"

		require.NoError(t, applyChanges(root, configPath, envPath, choices, syncOK, io.Discard))

		assert.True(t, os.SameFile(envBefore, stat(t, envPath)))
		requireBackup(t, root, configBackupName, initialConfig)
		requireBackup(t, root, envBackupName, "previous "+envBackupName)
	})

	t.Run("env write failure does not rewrite unchanged config", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		choices := noop()
		choices.Secrets["NEW"] = "secret"
		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		var configWrites int
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			switch path {
			case envPath:
				return errors.New("write env failed")
			case configPath:
				configWrites++
			}
			return origWrite(path, data, perm)
		}

		err := applyChanges(root, configPath, envPath, choices, syncOK, io.Discard)
		require.ErrorContains(t, err, "failed to write .env")
		assert.NotContains(t, err.Error(), "rollback")
		assert.Zero(t, configWrites)
		requireBackup(t, root, envBackupName, "previous "+envBackupName)
	})

	t.Run("missing env is created without backup", func(t *testing.T) {
		root, configPath, envPath := setup(t)
		require.NoError(t, os.Remove(envPath))

		require.NoError(t, applyChanges(root, configPath, envPath, noop(), syncOK, io.Discard))

		assert.FileExists(t, envPath)
		requireBackup(t, root, envBackupName, "previous "+envBackupName)
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
	require.NoError(t, os.WriteFile(configPath, []byte(invalidConfig), 0600))
	require.NoError(t, os.WriteFile(envPath, []byte("KEY=val"), 0600))

	mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }
	err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, io.Discard)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to patch config")
}

func TestApplyChanges_WritesAntigravityModel(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	configPath := filepath.Join(configDir, "config.toml")
	envPath := filepath.Join(configDir, ".env")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600))
	require.NoError(t, os.WriteFile(envPath, nil, 0o600))

	choices := NewChoices()
	choices.AgentModels[AgentAntigravity] = AgentModelChoice{ModelTouched: true, Model: "Gemini 3.5 Flash (High)"}

	err := applyChanges(root, configPath, envPath, choices, func(string) (*alsync.Result, error) {
		return &alsync.Result{}, nil
	}, io.Discard)
	require.NoError(t, err)

	updated, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(updated), `model = "Gemini 3.5 Flash (High)"`)
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
	require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(envPath, []byte(""), 0600))

	// Return warnings from sync
	mockSync := func(root string) (*alsync.Result, error) {
		return &alsync.Result{Warnings: []warnings.Warning{
			{Code: "TEST_WARNING", Subject: "test", Message: "Test warning message"},
		}}, nil
	}

	var out bytes.Buffer
	err := applyChanges(tmpDir, configPath, envPath, choices, mockSync, &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), messages.WizardRunningSync)
	assert.Contains(t, out.String(), "Test warning message")
}
