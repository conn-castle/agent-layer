// Package projectlock owns the per-project critical section that serializes
// every Agent Layer read and mutation of skill sources with ordinary
// projection.
//
// It was extracted from internal/sync so skill import operations and ordinary
// sync share one lock rather than each defining their own. The lock combines a
// per-process token (so goroutines in one binary serialize without relying on
// advisory-lock reentrancy) with an advisory flock on
// `.agent-layer/sync.lock` (so separate processes serialize too).
package projectlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// FileName is sourced from internal/install (the package that owns the
// .agent-layer layout and its known-paths set) so the lock this package creates
// and the file the installer recognizes can never drift.
const FileName = install.SyncLockFileName

// System abstracts the few operations lock acquisition needs so tests can
// inject clock and syscall faults. internal/sync's System satisfies it.
type System interface {
	Close(file *os.File) error
	Flock(fd int, how int) error
	Now() time.Time
	Sleep(d time.Duration)
}

var processLocks stdsync.Map

const (
	waitTimeout = 30 * time.Second
	pollEvery   = 100 * time.Millisecond
)

var (
	errDeadline = errors.New("sync lock acquisition deadline exceeded")

	// ErrPostWriteCleanup identifies a fatal lock cleanup failure after the
	// locked section already completed its writes successfully.
	ErrPostWriteCleanup = errors.New("sync generated writes succeeded but post-write lock cleanup failed")
)

type processLock struct {
	token chan struct{}
}

type heldLock struct {
	file    *os.File
	path    string
	sys     System
	process *processLock
}

// Path returns the lock file path for a repository root.
func Path(root string) string {
	return filepath.Join(root, ".agent-layer", FileName)
}

// With runs fn while holding the project lock.
//
// Callers must perform all source loading inside fn: loading configuration or
// skill sources before acquisition would let a concurrent import mutate the
// sources between the read and the projection built from it.
func With(sys System, root string, fn func() error) (err error) {
	lockPath := Path(root)
	process := processLockForPath(lockPath)
	deadline := sys.Now().Add(waitTimeout)
	if acquireErr := process.acquire(sys, deadline); acquireErr != nil {
		return timeoutError(lockPath)
	}

	lock, err := acquire(sys, lockPath, process, deadline)
	if err != nil {
		process.release()
		return err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; post-write lock cleanup also failed: %v", err, releaseErr)
				return
			}
			err = fmt.Errorf("%w: %v", ErrPostWriteCleanup, releaseErr)
		}
	}()

	return fn()
}

func processLockForPath(path string) *processLock {
	lock := &processLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	actual, _ := processLocks.LoadOrStore(path, lock)
	return actual.(*processLock)
}

func (l *processLock) acquire(sys System, deadline time.Time) error {
	for {
		now := sys.Now()
		if !now.Before(deadline) {
			return errDeadline
		}
		select {
		case <-l.token:
			return nil
		default:
		}

		wait := pollEvery
		if remaining := deadline.Sub(now); remaining < wait {
			wait = remaining
		}
		sys.Sleep(wait)
	}
}

func (l *processLock) release() {
	l.token <- struct{}{}
}

func acquire(sys System, path string, process *processLock, deadline time.Time) (*heldLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304,G302 -- lock path is rooted under the caller-resolved repo's .agent-layer directory; 0o644 lets a lock file (no sensitive data) be opened by other users/CI runners, matching internal/dispatch/lock.go.
	if err != nil {
		return nil, fmt.Errorf(messages.SyncOpenLockFmt, path, err)
	}
	if err := lockFile(sys, file, deadline); err != nil {
		closeErr := sys.Close(file)
		if errors.Is(err, errDeadline) {
			timeoutErr := timeoutError(path)
			if closeErr != nil {
				return nil, fmt.Errorf("%w; acquisition cleanup failed: %v", timeoutErr, closeErr)
			}
			return nil, timeoutErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf(messages.SyncLockFmt, path, fmt.Errorf("%w; acquisition cleanup failed: %v", err, closeErr))
		}
		return nil, fmt.Errorf(messages.SyncLockFmt, path, err)
	}
	return &heldLock{file: file, path: path, sys: sys, process: process}, nil
}

func lockFile(sys System, file *os.File, deadline time.Time) error {
	for {
		if !sys.Now().Before(deadline) {
			return errDeadline
		}
		err := sys.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		if err == nil {
			return nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}

		now := sys.Now()
		if !now.Before(deadline) {
			return errDeadline
		}
		wait := pollEvery
		if remaining := deadline.Sub(now); remaining < wait {
			wait = remaining
		}
		sys.Sleep(wait)
	}
}

func (l *heldLock) release() error {
	defer l.process.release()

	unlockErr := l.sys.Flock(int(l.file.Fd()), unix.LOCK_UN) //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
	closeErr := l.sys.Close(l.file)
	if unlockErr != nil && closeErr != nil {
		return errors.Join(
			fmt.Errorf(messages.SyncUnlockFmt, l.path, unlockErr),
			fmt.Errorf(messages.SyncCloseLockFmt, l.path, closeErr),
		)
	}
	if unlockErr != nil {
		return fmt.Errorf(messages.SyncUnlockFmt, l.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf(messages.SyncCloseLockFmt, l.path, closeErr)
	}
	return nil
}

func timeoutError(path string) error {
	return fmt.Errorf(messages.SyncLockTimeoutFmt, waitTimeout, path)
}
