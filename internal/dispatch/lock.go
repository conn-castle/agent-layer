package dispatch

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/messages"
)

type fileLock struct {
	file *os.File
	sys  System
}

var (
	lockWaitTimeout = 30 * time.Second
	lockPollEvery   = 100 * time.Millisecond
)

// withFileLock acquires a lock for path, runs fn, and releases the lock.
func withFileLock(sys System, path string, fn func() error) error {
	lock, err := acquireFileLock(sys, path)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.release()
	}()
	return fn()
}

// acquireFileLock opens or creates path and acquires an exclusive lock.
func acquireFileLock(sys System, path string) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf(messages.DispatchOpenLockFmt, path, err)
	}
	if err := lockFile(sys, file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf(messages.DispatchLockFmt, path, err)
	}
	return &fileLock{file: file, sys: sys}, nil
}

// release unlocks and closes the file lock.
func (l *fileLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := unlockFile(l.sys, l.file); err != nil {
		_ = l.file.Close()
		return err
	}
	return l.file.Close()
}

// lockFile acquires an exclusive advisory lock on the file.
func lockFile(sys System, file *os.File) error {
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err := sys.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // Unix file descriptors are small non-negative ints; cast is safe on all supported platforms
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(messages.DispatchLockTimeoutFmt, lockWaitTimeout)
		}
		sys.Sleep(lockPollEvery)
	}
}

// unlockFile releases the advisory lock on the file.
func unlockFile(sys System, file *os.File) error {
	return sys.Flock(int(file.Fd()), unix.LOCK_UN) //nolint:gosec // Unix file descriptors are small non-negative ints; cast is safe on all supported platforms
}
