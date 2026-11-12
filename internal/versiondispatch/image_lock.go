package versiondispatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/executableimage"
	"github.com/conn-castle/agent-layer/internal/imagelease"
)

var errImageMoved = errors.New("image cache entry moved or is unsafe")

const imageLockAttempts = 8

func ownedDir(path string) bool {
	i, err := os.Lstat(path) // #nosec G703 -- cache paths checked for ownership and symlinks.
	return err == nil && i.IsDir() && imagelease.Owned(i)
}

func samePath(f *os.File, path string) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	pi, err := os.Lstat(path) // #nosec G703 -- cache paths checked for ownership and symlinks.
	return err == nil && os.SameFile(fi, pi)
}

// openImageFile never follows a symlink or waits on a FIFO masquerading as a
// cache lock. Only regular, uid-owned files can participate in the protocol.
func openImageFile(path string, create bool) (*os.File, error) {
	flags := os.O_RDWR | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0o600) // #nosec G304,G703 -- validated image cache layout; O_NOFOLLOW forbids symlinks.
	if err != nil {
		return nil, err
	}
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || !imagelease.Owned(i) {
		_ = f.Close()
		return nil, errImageMoved
	}
	return f, nil
}

// lockImage retries when a pruner renamed the directory after we opened its
// lock. No caller may use an image through a detached lock inode.
func lockImage(ctx context.Context, sys System, dir string) (*fileLock, error) {
	return lockImageMode(ctx, sys, dir, true)
}

// lockImageMode's existing-entry mode never recreates a directory or binary.
// Check the running image before opening the lock as well as after locking, so
// a stale process cannot opt a replacement entry into the lease protocol.
func lockImageMode(ctx context.Context, sys System, dir string, create bool) (*fileLock, error) {
	images := filepath.Dir(dir)
	for range imageLockAttempts {
		if create {
			if err := os.MkdirAll(images, 0o755); err != nil { // #nosec G301,G703 -- executable cache directory.
				return nil, err
			}
		}
		if !ownedDir(images) {
			return nil, errImageMoved
		}
		if create {
			if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) { // #nosec G301,G703 -- executable cache entry.
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
		}
		if !ownedDir(dir) {
			continue
		}
		if !create {
			if err := verifyRunningImage(filepath.Join(dir, imageName)); err != nil {
				return nil, err
			}
		}
		path := filepath.Join(dir, imageName+".lock")
		f, err := openImageFile(path, true)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := lockFile(ctx, sys, f, imageLockWaitTimeout); err != nil {
			_ = f.Close()
			return nil, err
		}
		lock := &fileLock{file: f, sys: sys}
		if ownedDir(images) && ownedDir(dir) && samePath(f, path) {
			if !create {
				if err := verifyRunningImage(filepath.Join(dir, imageName)); err != nil {
					_ = lock.release()
					return nil, err
				}
			}
			return lock, nil
		}
		_ = lock.release()
	}
	return nil, errImageMoved
}

func verifyRunningImage(path string) error {
	i, err := os.Lstat(path) // #nosec G703 -- own image's existing cache entry.
	if err != nil || !i.Mode().IsRegular() || !imagelease.Owned(i) {
		return errImageMoved
	}
	return executableimage.SameAs(path)
}

// leaseImage is called only under the verified entry creation lock. Closing a
// lease releases this process's reference; never explicitly unlock shared OFDs.
func leaseImage(path string) (*os.File, error) {
	f, err := openImageFile(path+".lease", true)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err == nil {
		now := time.Now()
		err = os.Chtimes(path+".lease", now, now)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
