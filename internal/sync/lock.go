package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	stdsync "sync"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const projectSyncLockFile = "sync.lock"

var projectSyncProcessLocks stdsync.Map

type projectSyncLock struct {
	file        *os.File
	path        string
	processLock *stdsync.Mutex
}

func withProjectSyncLock(root string, fn func() (*Result, error)) (result *Result, err error) {
	lockPath := filepath.Join(root, ".agent-layer", projectSyncLockFile)
	processLock := processLockForSyncPath(lockPath)
	processLock.Lock()

	lock, err := acquireProjectSyncLock(lockPath, processLock)
	if err != nil {
		processLock.Unlock()
		return nil, err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil && err == nil {
			err = releaseErr
		}
	}()

	return fn()
}

func processLockForSyncPath(path string) *stdsync.Mutex {
	actual, _ := projectSyncProcessLocks.LoadOrStore(path, &stdsync.Mutex{})
	return actual.(*stdsync.Mutex)
}

func acquireProjectSyncLock(path string, processLock *stdsync.Mutex) (*projectSyncLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lock path is rooted under the caller-resolved repo's .agent-layer directory.
	if err != nil {
		return nil, fmt.Errorf(messages.SyncOpenLockFmt, path, err)
	}
	if err := lockProjectSyncFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf(messages.SyncLockFmt, path, err)
	}
	return &projectSyncLock{file: file, path: path, processLock: processLock}, nil
}

func lockProjectSyncFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX) //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func (l *projectSyncLock) release() error {
	defer l.processLock.Unlock()

	if err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = l.file.Close()
		return fmt.Errorf(messages.SyncUnlockFmt, l.path, err)
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf(messages.SyncCloseLockFmt, l.path, err)
	}
	return nil
}
