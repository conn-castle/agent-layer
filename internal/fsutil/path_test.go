package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePaddedPathPrefersExistingLiteralEntry(t *testing.T) {
	dir := t.TempDir()
	literal := filepath.Join(dir, "question.txt ")
	trimmed := filepath.Join(dir, "question.txt")
	require.NoError(t, os.WriteFile(trimmed, []byte("sibling"), 0o600))

	require.Equal(t, trimmed, ResolvePaddedPath("  "+trimmed+"  "), "padding is trimmed when no literal entry exists")
	require.Equal(t, trimmed, ResolvePaddedPath(literal), "a missing literal falls back to the trimmed sibling")

	require.NoError(t, os.WriteFile(literal, []byte("selected"), 0o600))
	require.Equal(t, literal, ResolvePaddedPath(literal))

	require.NoError(t, os.Remove(literal))
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), literal))
	require.Equal(t, literal, ResolvePaddedPath(literal), "a dangling literal symlink is still the selected entry")
}

func TestResolvePaddedPathKeepsLiteralOnNonExistenceFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	literal := filepath.Join(locked, "question.txt ")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) // #nosec G302 -- restores traversal on a test directory so TempDir cleanup succeeds.

	require.Equal(t, literal, ResolvePaddedPath(literal), "an unverifiable literal entry must not select the trimmed sibling")
}
