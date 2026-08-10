package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	err := WriteFileAtomic(path, []byte("hello"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}

func TestWriteFileAtomicOverwritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	err := os.WriteFile(path, []byte("old"), 0600)
	require.NoError(t, err)

	err = WriteFileAtomic(path, []byte("new"), 0600)
	require.NoError(t, err)

	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestWriteFileAtomicIfChangedPreservesIdenticalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := []byte("unchanged")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	before, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, WriteFileAtomicIfChanged(path, data, 0o600))
	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "identical write replaced the file")
}

func TestWriteFileAtomicIfChangedRepairsModeAndReplacesSymlink(t *testing.T) {
	t.Run("mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		// #nosec G306 -- the wider mode is the condition under test: an identical
		// file whose mode differs must still be rewritten with the wanted mode.
		require.NoError(t, os.WriteFile(path, []byte("same"), 0o644))
		require.NoError(t, WriteFileAtomicIfChanged(path, []byte("same"), 0o600))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode())
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		path := filepath.Join(dir, "config.toml")
		require.NoError(t, os.WriteFile(target, []byte("same"), 0o600))
		require.NoError(t, os.Symlink(target, path))
		require.NoError(t, WriteFileAtomicIfChanged(path, []byte("same"), 0o600))
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular(), "write preserved destination symlink")
	})
}

func TestWriteFileAtomicFailures(t *testing.T) {
	t.Run("invalid dir", func(t *testing.T) {
		err := WriteFileAtomic("/invalid/path/config.toml", []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "create temp file")
	})

	t.Run("target is directory", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		require.NoError(t, os.Mkdir(target, 0700))

		err := WriteFileAtomic(target, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rename temp file")
	})
}

func TestWriteFileAtomic_ErrorPaths(t *testing.T) {
	t.Run("chmod error", func(t *testing.T) {
		t.Cleanup(captureWriteFileAtomicDeps())
		chmodTempFile = func(_ *os.File, _ os.FileMode) error {
			return errors.New("chmod fail")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		err := WriteFileAtomic(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "set permissions for")
	})

	t.Run("write error", func(t *testing.T) {
		t.Cleanup(captureWriteFileAtomicDeps())
		writeTempFile = func(_ *os.File, _ []byte) (int, error) {
			return 0, errors.New("write fail")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		err := WriteFileAtomic(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write temp file")
	})

	t.Run("sync error", func(t *testing.T) {
		t.Cleanup(captureWriteFileAtomicDeps())
		syncTempFile = func(_ *os.File) error {
			return errors.New("sync fail")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		err := WriteFileAtomic(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sync temp file")
	})

	t.Run("close error", func(t *testing.T) {
		t.Cleanup(captureWriteFileAtomicDeps())
		closeTempFile = func(file *os.File) error {
			_ = file.Close()
			return errors.New("close fail")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		err := WriteFileAtomic(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "close temp file")
	})

	t.Run("sync dir error", func(t *testing.T) {
		t.Cleanup(captureWriteFileAtomicDeps())
		syncDirFunc = func(string) error {
			return errors.New("sync dir fail")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		err := WriteFileAtomic(path, []byte("data"), 0644)
		assert.Error(t, err)
		assert.Equal(t, "sync dir fail", err.Error())

		data, readErr := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
		require.NoError(t, readErr)
		assert.Equal(t, "data", string(data))
	})
}

func TestSyncDir(t *testing.T) {
	err := syncDir("/invalid/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "open dir")
}

func TestSyncDir_Success(t *testing.T) {
	dir := t.TempDir()
	err := syncDir(dir)
	assert.NoError(t, err)
}

func TestWriteFileAtomic_TempFileCleanup(t *testing.T) {
	// Test that temp file is cleaned up when rename fails
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	// Create target as directory so rename fails
	require.NoError(t, os.Mkdir(target, 0700))

	err := WriteFileAtomic(target, []byte("data"), 0644)
	assert.Error(t, err)

	// Verify no temp files remain
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Name() != "target" {
			t.Errorf("unexpected file found: %s", entry.Name())
		}
	}
}

func captureWriteFileAtomicDeps() func() {
	origCreateTemp := createTemp
	origChmodTempFile := chmodTempFile
	origWriteTempFile := writeTempFile
	origSyncTempFile := syncTempFile
	origCloseTempFile := closeTempFile
	origRenameFile := renameFile
	origSyncDirFunc := syncDirFunc

	return func() {
		createTemp = origCreateTemp
		chmodTempFile = origChmodTempFile
		writeTempFile = origWriteTempFile
		syncTempFile = origSyncTempFile
		closeTempFile = origCloseTempFile
		renameFile = origRenameFile
		syncDirFunc = origSyncDirFunc
	}
}
