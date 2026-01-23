package dispatch

import (
	"fmt"
	"os"
)

type fileLock struct {
	file *os.File
}

var lockFileFn = lockFile
var unlockFileFn = unlockFile

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
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := lockFileFn(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
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
