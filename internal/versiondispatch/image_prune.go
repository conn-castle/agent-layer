package versiondispatch

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/imagelease"
)

const imagePruneGrace = 24 * time.Hour
const imagePruneSampleSize = 128

// pruneImages is silent and best effort. Name enumeration is O(N); candidate
// probes have a soft time budget that cannot preempt individual filesystem calls.
func pruneImages(images, own string) {
	pruneImagesWithRandom(images, own, mathrand.IntN)
}

func pruneImagesWithRandom(images, own string, choose func(int) int) {
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
	// Enumerate names only, across the whole directory, before expensive entry
	// checks. A fixed prefix or a per-process cursor would starve later entries
	// across handoffs. Reservoir sampling bounds memory and candidate work, not
	// enumeration latency: an exceptionally large/slow directory can take longer.
	names := sampleImageNames(dir, filepath.Base(own), choose)
	if len(names) == 0 {
		return
	}
	// Randomize the processing start too: an original reservoir slot must not
	// remain permanently behind the time budget's cutoff across processes.
	start := choose(len(names))
	deadline := time.Now().Add(25 * time.Millisecond)
	for offset := range names {
		if time.Now().After(deadline) {
			return
		}
		name := names[(start+offset)%len(names)]
		if isImageTrash(name) {
			pruneImageTrash(root, name)
		} else {
			pruneImage(root, name)
		}
	}
}

// Every candidate, regardless of directory position, has the same chance of
// selection on each independent handoff. No persistent cursor is needed.
func sampleImageNames(dir *os.File, own string, choose func(int) int) []string {
	sample := make([]string, 0, imagePruneSampleSize)
	seen := 0
	for {
		names, err := dir.Readdirnames(256)
		for _, name := range names {
			if name == own || (!imagelease.ValidSHA(name) && !isImageTrash(name)) {
				continue
			}
			seen++
			if len(sample) < imagePruneSampleSize {
				sample = append(sample, name)
			} else if slot := choose(seen); slot < len(sample) {
				sample[slot] = name
			}
		}
		if err != nil {
			return sample
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
	_, _, ok := imageEntryInfo(root, name)
	return ok
}

// imageEntryInfo also records the newest write, including the directory itself,
// so an unfinished copy gets a full grace period after its last visible change.
func imageEntryInfo(root *os.Root, name string) (newest time.Time, complete bool, ok bool) {
	dir, info, err := openImageEntry(root, name)
	if err != nil {
		return newest, false, false
	}
	defer func() { _ = dir.Close() }()
	newest = info.ModTime()
	names, err := dir.Readdirnames(65)
	if (err != nil && err != io.EOF) || len(names) > 64 {
		return newest, false, false
	}
	for _, child := range names {
		if child != "al" && child != "al.lock" && child != "al.lease" && !strings.HasPrefix(child, "al.tmp-") {
			return newest, false, false
		}
		info, err := root.Lstat(filepath.Join(name, child))
		if err != nil || !info.Mode().IsRegular() || !imagelease.Owned(info) {
			return newest, false, false
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		complete = complete || child == imageName
	}
	return newest, complete, true
}

// Return the validated directory's open descriptor and identity snapshot. Never
// follow a symlink at the entry, even when its target is an owned directory.
func openImageEntry(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	original, err := root.Lstat(name)
	if err != nil || !original.IsDir() || !imagelease.Owned(original) {
		return nil, nil, errImageMoved
	}
	dir, err := root.OpenFile(name, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := dir.Stat()
	if err != nil || !os.SameFile(original, info) || !imagelease.Owned(info) {
		_ = dir.Close()
		return nil, nil, errImageMoved
	}
	return dir, info, nil
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
	lockPath := filepath.Join(name, "al.lock")
	lock := tryImageLock(root, lockPath)
	if lock == nil {
		if _, err := root.Lstat(lockPath); os.IsNotExist(err) {
			pruneEmptyImage(root, name)
		}
		return
	}
	defer func() { _ = lock.Close() }()
	newest, complete, ok := imageEntryInfo(root, name)
	if !ok {
		return
	}
	// Complete legacy images may still be executing without leases. Incomplete
	// entries cannot supply an executable, but must have no busy writer, no live
	// lease (if present), and no recent directory/lock/temp-file modification.
	var lease *os.File
	leasePath := filepath.Join(name, "al.lease")
	if _, err := root.Lstat(leasePath); !os.IsNotExist(err) {
		lease = tryImageLock(root, leasePath)
		if lease == nil {
			return
		}
		defer func() { _ = lease.Close() }()
	} else if complete {
		return
	}
	if complete {
		i, err := lease.Stat()
		if err != nil {
			return
		}
		newest = i.ModTime()
	}
	if time.Since(newest) <= imagePruneGrace || !imageShape(root, name) {
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
	if lease != nil {
		_ = lease.Close()
	}
	_ = lock.Close()
	_ = root.RemoveAll(dest)
}

// A crash before al.lock creation can leave an empty directory. Rmdir needs no
// lock: it fails atomically if a writer creates anything, and a writer whose
// lock open loses to rmdir retries from Mkdir in lockImageMode.
func pruneEmptyImage(root *os.Root, name string) {
	entry, info, err := openImageEntry(root, name)
	if err != nil {
		return
	}
	defer func() { _ = entry.Close() }()
	if time.Since(info.ModTime()) <= imagePruneGrace {
		return
	}
	names, err := entry.Readdirnames(1)
	if err != io.EOF || len(names) != 0 {
		return
	}
	current, err := root.Lstat(name)
	// SameFile proves the directory type, but ownership and mtime can change on
	// that inode (including a writer creating then deleting its temporary file).
	if err != nil || !os.SameFile(info, current) || !imagelease.Owned(current) || time.Since(current.ModTime()) <= imagePruneGrace {
		return
	}
	parent, err := root.Open(".")
	if err != nil {
		return
	}
	defer func() { _ = parent.Close() }()
	// AT_REMOVEDIR also refuses a raced-in regular file or symlink. Never use
	// recursive removal here, and never create a lock solely for cleanup.
	_ = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
}
