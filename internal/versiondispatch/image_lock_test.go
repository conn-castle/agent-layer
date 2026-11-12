package versiondispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestImageLockOpenFailureUsesDirectoryState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  string
		errno   error
		calls   int
		success bool
	}{
		{"removed EINVAL", "removed", unix.EINVAL, 2, true},
		{"removed EIO", "removed", unix.EIO, 2, true},
		{"replaced", "replaced", unix.EINVAL, 2, true},
		{"symlinked", "symlinked", unix.EINVAL, 1, false},
		{"unchanged EINVAL", "unchanged", unix.EINVAL, 1, false},
		{"unchanged ENOENT", "unchanged", unix.ENOENT, 1, false},
		{"bounded retries", "always removed", unix.EINVAL, imageLockAttempts, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), imagesDirName, strings.Repeat("a", 64))
			outside := t.TempDir()
			calls := 0
			injected := &os.PathError{Op: "open", Path: filepath.Join(dir, "al.lock"), Err: tc.errno}
			open := func(path string, create bool) (*os.File, error) {
				calls++
				if calls != 1 && tc.change != "always removed" {
					return openImageFile(path, create)
				}
				switch tc.change {
				case "removed", "always removed":
					require.NoError(t, os.Remove(dir))
				case "replaced", "symlinked":
					// Keep the old inode alive so replacement identity is certain.
					require.NoError(t, os.Rename(dir, dir+".detached"))
					if tc.change == "replaced" {
						require.NoError(t, os.Mkdir(dir, 0o700))
					} else {
						require.NoError(t, os.Symlink(outside, dir))
					}
				}
				return nil, injected
			}
			lock, err := lockImageModeWithOpen(context.Background(), RealSystem{}, dir, true, 0, open)
			require.Equal(t, tc.calls, calls)
			if tc.success {
				require.NoError(t, err)
				t.Cleanup(func() { _ = lock.release() })
				require.True(t, samePath(lock.file, filepath.Join(dir, "al.lock")))
				probe, err := openImageFile(filepath.Join(dir, "al.lock"), false)
				require.NoError(t, err)
				defer func() { _ = probe.Close() }()
				require.ErrorIs(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK)
			} else {
				require.Nil(t, lock)
				if tc.change == "unchanged" {
					require.Same(t, injected, err, "unchanged entries must preserve the actual open error")
				} else {
					require.ErrorIs(t, err, errImageMoved)
				}
			}
			require.NoFileExists(t, filepath.Join(outside, "al.lock"), "a swapped symlink must never receive a lock")
		})
	}
}

func TestImageLockExistingOnlyDoesNotRecreateMissingEntry(t *testing.T) {
	images := filepath.Join(t.TempDir(), imagesDirName)
	require.NoError(t, os.Mkdir(images, 0o700))
	dir := filepath.Join(images, strings.Repeat("a", 64))
	lock, err := lockImageModeWithOpen(context.Background(), RealSystem{}, dir, false, 0, func(string, bool) (*os.File, error) {
		t.Fatal("missing existing entry must not open or create a lock")
		return nil, nil
	})
	require.Nil(t, lock)
	require.ErrorIs(t, err, errImageMoved)
	require.NoDirExists(t, dir)
}
