package wizard

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestMigrateBackups(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "empty destination"
		if conflict {
			name = "existing backups"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0700))
			for _, base := range []string{"config.toml.bak", ".env.bak"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", base), []byte("legacy "+base), 0600))
				if conflict {
					require.NoError(t, os.MkdirAll(filepath.Dir(backupPath(root, base)), 0700))
					require.NoError(t, os.WriteFile(backupPath(root, base), []byte("current"), 0600))
					require.NoError(t, os.WriteFile(backupPath(root, base+".legacy-1"), []byte("earlier"), 0600))
				}
			}
			require.NoError(t, MigrateBackups(root))
			require.NoError(t, MigrateBackups(root))
			for _, base := range []string{"config.toml.bak", ".env.bak"} {
				require.NoFileExists(t, filepath.Join(root, ".agent-layer", base))
				destination := backupPath(root, base)
				if conflict {
					current, err := os.ReadFile(destination)
					require.NoError(t, err)
					require.Equal(t, "current", string(current))
					earlier, err := os.ReadFile(destination + ".legacy-1")
					require.NoError(t, err)
					require.Equal(t, "earlier", string(earlier))
					destination += ".legacy-2"
				}
				data, err := os.ReadFile(destination)
				require.NoError(t, err)
				require.Equal(t, "legacy "+base, string(data))
				info, err := os.Stat(destination)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			}
			unrelated := backupPath(root, "notes.txt")
			require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0600))
			removed, err := CleanupBackups(root)
			require.NoError(t, err)
			expected := 2
			if conflict {
				expected = 6
			}
			require.Len(t, removed, expected)
			require.FileExists(t, unrelated)
			removed, err = CleanupBackups(root)
			require.NoError(t, err)
			require.Empty(t, removed)
		})
	}
}

func TestMigrateBackupsFailurePreservesSource(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0700))
	source := filepath.Join(root, ".agent-layer", ".env.bak")
	require.NoError(t, os.WriteFile(source, []byte("secret"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "state"), []byte("blocked"), 0600))
	require.ErrorContains(t, MigrateBackups(root), "must be a real directory")
	data, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "secret", string(data))
}

func TestMigrateBackupsRetriesAfterDirectorySyncFailure(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".agent-layer", configBackupName)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0700))
	require.NoError(t, os.WriteFile(legacy, []byte("recovery"), 0600))

	originalSync := syncBackupDir
	syncBackupDir = func(string) error { return errors.New("sync failed") }
	t.Cleanup(func() { syncBackupDir = originalSync })
	require.ErrorContains(t, MigrateBackups(root), "sync failed")
	require.FileExists(t, legacy)
	require.NoFileExists(t, backupPath(root, configBackupName))

	syncBackupDir = originalSync
	require.NoError(t, MigrateBackups(root))
	require.NoFileExists(t, legacy)
	require.FileExists(t, backupPath(root, configBackupName))
	require.NoFileExists(t, backupPath(root, configBackupName)+".legacy-1")
}

func TestBackupsRejectNonRegularFiles(t *testing.T) {
	for _, operation := range []string{"write", "migrate", "cleanup"} {
		for _, kind := range []string{"directory", "symlink"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, ".agent-layer", configBackupName)
				if operation != "migrate" {
					path = backupPath(root, configBackupName)
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				if kind == "directory" {
					require.NoError(t, os.Mkdir(path, 0700))
				} else {
					target := filepath.Join(root, "recovery")
					require.NoError(t, os.WriteFile(target, []byte("preserve"), 0600))
					require.NoError(t, os.Symlink(target, path))
				}
				var err error
				switch operation {
				case "write":
					_, err = writeBackup(root, configBackupName, []byte("new"), 0600)
				case "migrate":
					err = MigrateBackups(root)
				default:
					_, err = CleanupBackups(root)
				}
				require.ErrorContains(t, err, "must be a regular file")
				_, statErr := os.Lstat(path)
				require.NoError(t, statErr)
			})
		}
	}
}

func TestWriteBackupReplacesPermissionsAndPreservesLinkedFile(t *testing.T) {
	root := t.TempDir()
	path := backupPath(root, envBackupName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	// #nosec G306 -- deliberately permissive fixture verifies permissions are tightened on overwrite.
	require.NoError(t, os.WriteFile(path, []byte("old"), 0644))
	linked := filepath.Join(root, "original")
	require.NoError(t, os.Link(path, linked))
	_, err := writeBackup(root, envBackupName, []byte("secret"), 0600)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "secret", string(data))
	original, err := os.ReadFile(linked)
	require.NoError(t, err)
	require.Equal(t, "old", string(original))
}

func TestBackupOperationsRejectLinkedStorage(t *testing.T) {
	operations := map[string]func(root string) error{
		"write": func(root string) error {
			_, err := writeBackup(root, configBackupName, []byte("new"), 0600)
			return err
		},
		"migrate": MigrateBackups,
		"cleanup": func(root string) error {
			_, err := CleanupBackups(root)
			return err
		},
	}
	for _, linked := range []string{".agent-layer", ".agent-layer/state", ".agent-layer/state/wizard-backups"} {
		for name, operation := range operations {
			t.Run(linked+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Dir(backupPath(root, configBackupName)), 0700))
				require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", configBackupName), []byte("legacy"), 0600))
				require.NoError(t, os.WriteFile(backupPath(root, envBackupName), []byte("current"), 0600))
				linkPath := filepath.Join(root, filepath.FromSlash(linked))
				external := filepath.Join(t.TempDir(), "external")
				require.NoError(t, os.Rename(linkPath, external))
				require.NoError(t, os.Symlink(external, linkPath))
				before := snapshotFiles(t, external)

				require.ErrorContains(t, operation(root), "must be a real directory")
				require.Equal(t, before, snapshotFiles(t, external))

				// Restoring a real directory allows retry.
				require.NoError(t, os.Remove(linkPath))
				require.NoError(t, os.Rename(external, linkPath))
				require.NoError(t, operation(root))
			})
		}
	}
}

func TestApplyRejectsLinkedState(t *testing.T) {
	link := func(t *testing.T, root string) (string, string) {
		state := filepath.Join(root, ".agent-layer", "state")
		external := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.MkdirAll(filepath.Dir(state), 0700))
		require.NoError(t, os.MkdirAll(external, 0700))
		require.NoError(t, os.Symlink(external, state))
		return state, external
	}
	syncCalled := false
	runSync := func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}

	t.Run("interactive", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.toml")
		envPath := filepath.Join(root, ".env")
		initialConfig := "[approvals]\nmode = \"none\"\n"
		require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0600))
		require.NoError(t, os.WriteFile(envPath, []byte("OLD=value\n"), 0600))
		state, external := link(t, root)
		choices := NewChoices()
		choices.ApprovalMode = "all"
		choices.ApprovalModeTouched = true
		syncCalled = false

		require.ErrorContains(t, applyChanges(root, configPath, envPath, choices, runSync, io.Discard), "must be a real directory")
		require.False(t, syncCalled)
		require.Empty(t, snapshotFiles(t, external))
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Equal(t, initialConfig, string(data))

		require.NoError(t, os.Remove(state))
		require.NoError(t, applyChanges(root, configPath, envPath, choices, runSync, io.Discard))
		require.FileExists(t, backupPath(root, configBackupName))
	})

	t.Run("profile", func(t *testing.T) {
		root := t.TempDir()
		setupRepo(t, root)
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0600))
		profilePath := filepath.Join(root, "profile.toml")
		require.NoError(t, os.WriteFile(profilePath, []byte(strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)), 0600))
		state, external := link(t, root)
		syncCalled = false

		require.ErrorContains(t, RunProfile(root, runSync, "", profilePath, true, io.Discard), "must be a real directory")
		require.False(t, syncCalled)
		require.Empty(t, snapshotFiles(t, external))
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Equal(t, basicAgentConfig(), string(data))

		require.NoError(t, os.Remove(state))
		require.NoError(t, RunProfile(root, runSync, "", profilePath, true, io.Discard))
		require.FileExists(t, backupPath(root, configBackupName))
	})
}

// snapshotFiles maps each regular file below dir to its contents.
func snapshotFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- isolated test fixture tree during snapshot.
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[rel] = string(data)
		return err
	}))
	return files
}

func TestBackupOperationsTightenDirectoryPermissions(t *testing.T) {
	for _, operation := range []string{"write", "migrate"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			backupDir := filepath.Dir(backupPath(root, configBackupName))
			// #nosec G301 -- deliberately broad permissions verify they are tightened.
			require.NoError(t, os.MkdirAll(backupDir, 0755))
			require.NoError(t, os.Chmod(backupDir, 0755)) // #nosec G302 -- deliberately broad permissions verify they are tightened.
			if operation == "write" {
				_, err := writeBackup(root, configBackupName, []byte("new"), 0600)
				require.NoError(t, err)
			} else {
				legacy := filepath.Join(root, ".agent-layer", configBackupName)
				require.NoError(t, os.WriteFile(legacy, []byte("legacy"), 0600))
				require.NoError(t, MigrateBackups(root))
			}
			info, err := os.Stat(backupDir)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0700), info.Mode().Perm())
		})
	}
}
