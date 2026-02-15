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
}

var lockFileFn = lockFile
var unlockFileFn = unlockFile
var flockFn = unix.Flock
var lockSleep = time.Sleep

var (
	lockWaitTimeout = 30 * time.Second
	lockPollEvery   = 100 * time.Millisecond
)

// withFileLock acquires a lock for path, runs fn, and releases the lock.
func withFileLock(path string, fn func() error) error {
	lock, err := acquireFileLock(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.release()
	}()
	return fn()
}

// acquireFileLock opens or creates path and acquires an exclusive lock.
func acquireFileLock(path string) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf(messages.DispatchOpenLockFmt, path, err)
	}
	if err := lockFileFn(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf(messages.DispatchLockFmt, path, err)
	}
	return &fileLock{file: file}, nil
}

// release unlocks and closes the file lock.
func (l *fileLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := unlockFileFn(l.file); err != nil {
		_ = l.file.Close()
		return err
	}
	return l.file.Close()
}

// lockFile acquires an exclusive advisory lock on the file.
func lockFile(file *os.File) error {
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err := flockFn(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(messages.DispatchLockTimeoutFmt, lockWaitTimeout)
		}
		lockSleep(lockPollEvery)
	}
}

// unlockFile releases the advisory lock on the file.
func unlockFile(file *os.File) error {
	return flockFn(int(file.Fd()), unix.LOCK_UN)
}
