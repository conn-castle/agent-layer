package sync

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

// projectSyncLockFile is sourced from internal/install (the package that owns the
// .agent-layer layout and its known-paths set) so the lock this package creates
// and the file the installer recognizes can never drift. internal/sync already
// imports internal/install, so this is the cycle-safe home for the name.
const projectSyncLockFile = install.SyncLockFileName

var projectSyncProcessLocks stdsync.Map

const (
	projectSyncLockWaitTimeout = 30 * time.Second
	projectSyncLockPollEvery   = 100 * time.Millisecond
)

var (
	errProjectSyncLockDeadline = errors.New("sync lock acquisition deadline exceeded")

	// ErrPostWriteLockCleanup identifies a fatal lock cleanup failure after sync
	// has already written all generated outputs successfully.
	ErrPostWriteLockCleanup = errors.New("sync generated writes succeeded but post-write lock cleanup failed")
)

type projectSyncProcessLock struct {
	token chan struct{}
}

type projectSyncLock struct {
	file        *os.File
	path        string
	sys         System
	processLock *projectSyncProcessLock
}

func withProjectSyncLock(sys System, root string, fn func() (*Result, error)) (result *Result, err error) {
	lockPath := filepath.Join(root, ".agent-layer", projectSyncLockFile)
	processLock := processLockForSyncPath(lockPath)
	deadline := sys.Now().Add(projectSyncLockWaitTimeout)
	if err := processLock.acquire(sys, deadline); err != nil {
		return nil, syncLockTimeoutError(lockPath)
	}

	lock, err := acquireProjectSyncLock(sys, lockPath, processLock, deadline)
	if err != nil {
		processLock.release()
		return nil, err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; post-write lock cleanup also failed: %v", err, releaseErr)
				return
			}
			err = fmt.Errorf("%w: %v", ErrPostWriteLockCleanup, releaseErr)
		}
	}()

	return fn()
}

func processLockForSyncPath(path string) *projectSyncProcessLock {
	lock := &projectSyncProcessLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	actual, _ := projectSyncProcessLocks.LoadOrStore(path, lock)
	return actual.(*projectSyncProcessLock)
}

func (l *projectSyncProcessLock) acquire(sys System, deadline time.Time) error {
	for {
		now := sys.Now()
		if !now.Before(deadline) {
			return errProjectSyncLockDeadline
		}
		select {
		case <-l.token:
			return nil
		default:
		}

		wait := projectSyncLockPollEvery
		if remaining := deadline.Sub(now); remaining < wait {
			wait = remaining
		}
		sys.Sleep(wait)
	}
}

func (l *projectSyncProcessLock) release() {
	l.token <- struct{}{}
}

func acquireProjectSyncLock(sys System, path string, processLock *projectSyncProcessLock, deadline time.Time) (*projectSyncLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304,G302 -- lock path is rooted under the caller-resolved repo's .agent-layer directory; 0o644 lets a lock file (no sensitive data) be opened by other users/CI runners, matching internal/dispatch/lock.go.
	if err != nil {
		return nil, fmt.Errorf(messages.SyncOpenLockFmt, path, err)
	}
	if err := lockProjectSyncFile(sys, file, deadline); err != nil {
		closeErr := sys.Close(file)
		if errors.Is(err, errProjectSyncLockDeadline) {
			timeoutErr := syncLockTimeoutError(path)
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
	return &projectSyncLock{file: file, path: path, sys: sys, processLock: processLock}, nil
}

func lockProjectSyncFile(sys System, file *os.File, deadline time.Time) error {
	for {
		if !sys.Now().Before(deadline) {
			return errProjectSyncLockDeadline
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
			return errProjectSyncLockDeadline
		}
		wait := projectSyncLockPollEvery
		if remaining := deadline.Sub(now); remaining < wait {
			wait = remaining
		}
		sys.Sleep(wait)
	}
}

func (l *projectSyncLock) release() error {
	defer l.processLock.release()

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

func syncLockTimeoutError(path string) error {
	return fmt.Errorf(messages.SyncLockTimeoutFmt, projectSyncLockWaitTimeout, path)
}
