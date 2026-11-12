// Package imagelease owns the lifetime lease shared by an image server and its workers.
package imagelease

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

// EnvFD is private handoff state, consumed before any command can start children.
const EnvFD = "_AL_IMAGE_LEASE_FD"

// current is deliberately retained until process exit. Closing a duplicate must
// not LOCK_UN: workers share the same open file description across fork/exec.
var current atomic.Pointer[os.File]

func init() {
	raw := os.Getenv(EnvFD)
	_ = os.Unsetenv(EnvFD)
	if fd, err := strconv.Atoi(raw); err == nil && fd >= 3 {
		current.Store(adopt(fd))
	}
	// Workers use explicit ExtraFiles, never environment state. Adopt before
	// command initialization can launch any other process.
	if current.Load() == nil && len(os.Args) > 1 && os.Args[1] == "__dispatch-worker" {
		current.Store(adopt(4))
	}
}

// Current returns this process's inherited or acquired lifetime lease, if any.
// Callers may pass it in ExtraFiles but must never close or unlock it.
func Current() *os.File { return current.Load() }

// Retain keeps a newly acquired startup lease until process exit. A redundant
// reference is closed, never unlocked: workers may share its open description.
func Retain(f *os.File) {
	if !current.CompareAndSwap(nil, f) {
		_ = f.Close()
	}
}

// ValidSHA reports whether a cache entry name is exactly a lowercase SHA-256.
func ValidSHA(s string) bool {
	return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// Owned reports whether info belongs to the current effective user.
func Owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Geteuid())
}

func adopt(fd int) *os.File {
	exe, err := os.Executable()
	if err != nil || !validFor(fd, exe) {
		return nil
	}
	// Until identity is proven this is a borrowed, untrusted descriptor. Do not
	// wrap it, change flags, flock it or close it on the rejection path.
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		_ = unix.Close(fd) // Identity proved ownership; never leave an orphan.
		return nil
	}
	// Reassert the shared lock, including when a matching but unlocked fd was
	// supplied. Never retain an unprotected descriptor.
	if unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB) != nil {
		_ = unix.Close(fd)
		return nil
	}
	return os.NewFile(uintptr(fd), "image-lease")
}

func validFor(fd int, exe string) bool {
	dir := filepath.Dir(exe)
	images := filepath.Dir(dir)
	if filepath.Base(exe) != "al" || !ValidSHA(filepath.Base(dir)) || filepath.Base(images) != "images" {
		return false
	}
	for _, p := range []string{images, dir} {
		i, err := os.Lstat(p)
		if err != nil || !i.IsDir() || !Owned(i) {
			return false
		}
	}
	i, err := os.Lstat(exe + ".lease")
	if err != nil || !i.Mode().IsRegular() || !Owned(i) {
		return false
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return false
	}
	pathStat, ok := i.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Dev) == uint64(pathStat.Dev) && stat.Ino == pathStat.Ino //nolint:unconvert // Dev types differ on Darwin.
}

// ExecEnv clears CLOEXEC only around the handoff. The returned cleanup restores
// it on a failed (or test-double) exec. Ordinary exec.Command children never get it.
func ExecEnv(f *os.File, env []string) ([]string, func(), error) {
	fd := int(f.Fd())
	// Go forkExec takes ForkLock exclusively during fork. Exclude concurrent
	// child creation during the only interval in which our lease lacks CLOEXEC.
	// syscall.Exec itself does not take this lock; success replaces its memory.
	syscall.ForkLock.Lock()
	if _, err := unix.FcntlInt(f.Fd(), unix.F_SETFD, 0); err != nil {
		syscall.ForkLock.Unlock()
		return nil, nil, err
	}
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, EnvFD+"=") {
			out = append(out, entry)
		}
	}
	out = append(out, EnvFD+"="+strconv.Itoa(fd))
	return out, func() {
		unix.CloseOnExec(fd)
		syscall.ForkLock.Unlock()
	}, nil
}
