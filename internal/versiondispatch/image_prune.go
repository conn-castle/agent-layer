package versiondispatch

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/imagelease"
)

const imagePruneGrace = 24 * time.Hour

// pruneImages is deliberately silent and best effort. Nonblocking flocks and a
// short scan budget keep cache maintenance off the handoff's critical path.
func pruneImages(images, own string) {
	i, err := os.Lstat(images)
	if err != nil || !i.IsDir() || !imagelease.Owned(i) {
		return
	}
	root, err := os.OpenRoot(images)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return
	}
	defer func() { _ = dir.Close() }()
	fi, err := dir.Stat()
	if err != nil || !os.SameFile(i, fi) {
		return
	}
	deadline := time.Now().Add(25 * time.Millisecond)
	for time.Now().Before(deadline) {
		names, err := dir.Readdirnames(16)
		for _, name := range names {
			if time.Now().After(deadline) {
				return
			}
			trash := isImageTrash(name)
			if (!imagelease.ValidSHA(name) && !trash) || filepath.Join(images, name) == own {
				continue
			}
			if trash {
				pruneImageTrash(root, name)
			} else {
				pruneImage(root, name)
			}
		}
		if err != nil {
			return
		}
	}
}

func isImageTrash(name string) bool {
	parts := strings.Split(name, "-")
	if len(parts) != 3 || parts[0] != ".prune" || !imagelease.ValidSHA(parts[1]) || len(parts[2]) != 16 {
		return false
	}
	_, err := hex.DecodeString(parts[2])
	return err == nil
}

// imageShape excludes symlinks, foreign owners, subdirectories and user files.
// Recheck under both locks before renaming, including leftover trash entries.
func imageShape(root *os.Root, name string) bool {
	i, err := root.Lstat(name)
	if err != nil || !i.IsDir() || !imagelease.Owned(i) {
		return false
	}
	dir, err := root.OpenFile(name, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer func() { _ = dir.Close() }()
	names, err := dir.Readdirnames(65)
	if (err != nil && err != io.EOF) || len(names) > 64 {
		return false
	}
	for _, child := range names {
		if child != "al" && child != "al.lock" && child != "al.lease" && !strings.HasPrefix(child, "al.tmp-") {
			return false
		}
		info, err := root.Lstat(filepath.Join(name, child))
		if err != nil || !info.Mode().IsRegular() || !imagelease.Owned(info) {
			return false
		}
	}
	return true
}

func tryImageLock(root *os.Root, path string) *os.File {
	f, err := root.OpenFile(path, os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	fi, err := f.Stat()
	pi, pathErr := root.Lstat(path)
	if err == nil && pathErr == nil && fi.Mode().IsRegular() && imagelease.Owned(fi) && os.SameFile(fi, pi) {
		if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil {
			// A concurrent pruner may have renamed after open, before flock.
			pi, err = root.Lstat(path)
			if err == nil && os.SameFile(fi, pi) {
				return f
			}
		}
	}
	_ = f.Close()
	return nil
}

// Once detached, an entry cannot gain legitimate users: every creation-lock
// waiter verifies the original path before proceeding. A crash during removal
// can leave partial trash without lock files, so existing locks are optional.
func pruneImageTrash(root *os.Root, name string) {
	if !imageShape(root, name) {
		return
	}
	locks := make([]*os.File, 0, 2)
	defer func() {
		for _, f := range locks {
			_ = f.Close()
		}
	}()
	for _, child := range []string{"al.lock", "al.lease"} {
		path := filepath.Join(name, child)
		if _, err := root.Lstat(path); os.IsNotExist(err) {
			continue
		}
		f := tryImageLock(root, path)
		if f == nil {
			return
		}
		locks = append(locks, f)
	}
	if !imageShape(root, name) {
		return
	}
	for _, f := range locks {
		_ = f.Close()
	}
	_ = root.RemoveAll(name)
}

func pruneImage(root *os.Root, name string) {
	if !imageShape(root, name) {
		return
	}
	lock := tryImageLock(root, filepath.Join(name, "al.lock"))
	if lock == nil {
		return
	}
	defer func() { _ = lock.Close() }()
	// Existence of al.lease is the opt-in marker. Never create it here:
	// binaries predating leases can still run from legacy entries indefinitely.
	lease := tryImageLock(root, filepath.Join(name, "al.lease"))
	if lease == nil {
		return
	}
	defer func() { _ = lease.Close() }()
	i, err := lease.Stat()
	if err != nil || time.Since(i.ModTime()) <= imagePruneGrace || !imageShape(root, name) {
		return
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return
	}
	dest := ".prune-" + name + "-" + hex.EncodeToString(nonce[:])
	if err := root.Rename(name, dest); err != nil {
		return
	}
	// Waiters with the old lock open must now retry. Release before removal;
	// bounded os.Root removal cannot follow a symlink out of images/.
	_ = lease.Close()
	_ = lock.Close()
	_ = root.RemoveAll(dest)
}
