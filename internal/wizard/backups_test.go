package wizard

import (
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
