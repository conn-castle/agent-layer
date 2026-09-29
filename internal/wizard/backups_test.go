package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.ErrorContains(t, MigrateBackups(root), "create wizard backup directory")
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
	for _, operation := range []string{"migrate", "cleanup"} {
		for _, kind := range []string{"directory", "symlink"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, ".agent-layer", configBackupName)
				if operation == "cleanup" {
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
				if operation == "migrate" {
					err = MigrateBackups(root)
				} else {
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
	created, err := writeBackup(path, []byte("secret"), 0600)
	require.NoError(t, err)
	require.False(t, created)
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

func TestBackupOperationsRejectSymlinkedDirectory(t *testing.T) {
	for _, operation := range []string{"write", "migrate", "cleanup"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			external := t.TempDir()
			backupDir := filepath.Dir(backupPath(root, configBackupName))
			require.NoError(t, os.MkdirAll(filepath.Dir(backupDir), 0700))
			require.NoError(t, os.Symlink(external, backupDir))
			externalBackup := filepath.Join(external, configBackupName)
			require.NoError(t, os.WriteFile(externalBackup, []byte("external"), 0600))
			legacy := filepath.Join(root, ".agent-layer", configBackupName)
			if operation != "write" {
				require.NoError(t, os.WriteFile(legacy, []byte("legacy"), 0600))
			}

			var err error
			switch operation {
			case "write":
				_, err = writeBackup(backupPath(root, configBackupName), []byte("new"), 0600)
			case "migrate":
				err = MigrateBackups(root)
			case "cleanup":
				_, err = CleanupBackups(root)
			}
			require.Error(t, err)
			contents, readErr := os.ReadFile(externalBackup)
			require.NoError(t, readErr)
			require.Equal(t, "external", string(contents))
			if operation != "write" {
				require.FileExists(t, legacy)
			}
		})
	}
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
				_, err := writeBackup(backupPath(root, configBackupName), []byte("new"), 0600)
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
