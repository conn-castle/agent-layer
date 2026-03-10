package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWithFileLock(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.lock")

	err := withFileLock(RealSystem{}, path, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("withFileLock failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file not created")
	}
}

func TestWithFileLock_OpenError(t *testing.T) {
	tmp := t.TempDir()
	// Create a directory where the file should be
	path := filepath.Join(tmp, "test.lock")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := withFileLock(RealSystem{}, path, func() error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error opening lock file on directory")
	}
}

func TestWithFileLock_FnError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.lock")

	expectedErr := fmt.Errorf("callback error")
	err := withFileLock(RealSystem{}, path, func() error {
		return expectedErr
	})
	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestFileLock_Release_Nil(t *testing.T) {
	var l *fileLock
	if err := l.release(); err != nil {
		t.Errorf("expected nil error for nil lock release, got %v", err)
	}

	l = &fileLock{}
	if err := l.release(); err != nil {
		t.Errorf("expected nil error for nil file release, got %v", err)
	}
}

func TestAcquireFileLock_LockError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.lock")

	expectedErr := fmt.Errorf("lock error")
	sys := &testSystem{
		FlockFunc: func(fd int, how int) error {
			return expectedErr
		},
	}

	lock, err := acquireFileLock(sys, path)
	if lock != nil {
		t.Fatalf("expected nil lock on error, got %+v", lock)
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestFileLock_Release_UnlockError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.lock")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}

	expectedErr := fmt.Errorf("unlock error")
	sys := &testSystem{
		FlockFunc: func(fd int, how int) error {
			return expectedErr
		},
	}

	lock := &fileLock{file: file, sys: sys}
	if err := lock.release(); !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestLockFile_Timeout(t *testing.T) {
	origTimeout := lockWaitTimeout
	origPoll := lockPollEvery
	lockWaitTimeout = time.Nanosecond
	lockPollEvery = time.Nanosecond
	t.Cleanup(func() {
		lockWaitTimeout = origTimeout
		lockPollEvery = origPoll
	})

	sys := &testSystem{
		FlockFunc: func(fd int, how int) error {
			return unix.EWOULDBLOCK
		},
		SleepFunc: func(time.Duration) {},
	}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.lock")
	lock, err := acquireFileLock(sys, path)
	if lock != nil {
		t.Fatalf("expected no lock on timeout, got %+v", lock)
	}
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, unix.EWOULDBLOCK) && !strings.Contains(err.Error(), "timed out waiting for lock") {
		t.Fatalf("expected timeout-related error, got %v", err)
	}
}
