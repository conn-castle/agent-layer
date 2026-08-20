package fsutil

import (
	"errors"
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
	link := filepath.Join(root, "home")
	require.NoError(t, os.Symlink(target, link))
	err := EnsurePrivateDir(link)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a real directory")

	file := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	err = EnsurePrivateDir(file)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a real directory")
}

func TestEnsurePrivateDirReportsFilesystemFailures(t *testing.T) {
	t.Cleanup(func() {
		privateDirLstat = os.Lstat
		privateDirMkdirAll = os.MkdirAll
		privateDirChmod = os.Chmod
	})
	injected := errors.New("injected")
	path := filepath.Join(t.TempDir(), "home")

	privateDirLstat = func(string) (os.FileInfo, error) { return nil, injected }
	err := EnsurePrivateDir(path)
	require.ErrorIs(t, err, injected)
	require.Contains(t, err.Error(), "stat")

	privateDirLstat = os.Lstat
	privateDirMkdirAll = func(string, os.FileMode) error { return injected }
	err = EnsurePrivateDir(path)
	require.ErrorIs(t, err, injected)
	require.Contains(t, err.Error(), "create")

	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.Chmod(path, 0o755)) // #nosec G302 -- fixture starts too open so the chmod failure path is reachable.
	privateDirMkdirAll = os.MkdirAll
	privateDirChmod = func(string, os.FileMode) error { return injected }
	err = EnsurePrivateDir(path)
	require.ErrorIs(t, err, injected)
	require.Contains(t, err.Error(), "restrict")
}
