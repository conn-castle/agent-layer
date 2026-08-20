package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsurePrivateDirCreatesOwnerOnlyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home")
	require.NoError(t, EnsurePrivateDir(path))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestEnsurePrivateDirCreatesMissingParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "home")
	require.NoError(t, EnsurePrivateDir(path))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestEnsurePrivateDirTightensBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.Chmod(path, 0o755)) // #nosec G302 -- fixture starts too open so production can tighten it.

	require.NoError(t, EnsurePrivateDir(path))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestEnsurePrivateDirLeavesOwnerOnlyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, EnsurePrivateDir(path))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestEnsurePrivateDirRejectsSymlinkAndFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	require.NoError(t, os.Chmod(target, 0o755)) // #nosec G302 -- target stays broad so a followed chmod would be visible.
	link := filepath.Join(root, "home")
	require.NoError(t, os.Symlink(target, link))
	err := EnsurePrivateDir(link)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a real directory")
	info, statErr := os.Lstat(target)
	require.NoError(t, statErr)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	file := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	err = EnsurePrivateDir(file)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a real directory")
}

func TestEnsurePrivateDirRejectsDanglingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Symlink("missing", path))
	err := EnsurePrivateDir(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a real directory")
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
}

func TestEnsurePrivateDirReportsFilesystemFailures(t *testing.T) {
	t.Run("parent is a file", func(t *testing.T) {
		root := t.TempDir()
		parent := filepath.Join(root, "file")
		require.NoError(t, os.WriteFile(parent, []byte("x"), 0o600))
		err := EnsurePrivateDir(filepath.Join(parent, "home"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "create")
	})

	t.Run("invalid path", func(t *testing.T) {
		err := EnsurePrivateDir("")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid")

		err = EnsurePrivateDir(".")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid")
	})
}
