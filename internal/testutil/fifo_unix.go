//go:build unix

package testutil

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// MakeFIFO creates a direct or linked FIFO fixture and returns the FIFO's path.
func MakeFIFO(t *testing.T, path string, linked bool) string {
	t.Helper()
	target := path
	if linked {
		target += ".fifo"
	}
	if err := unix.Mkfifo(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if linked {
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	return target
}

// BoundedFIFO runs a read without allowing a regressed blocking open to hang tests.
// On timeout it connects a writer, then closes it to release the reader with EOF.
func BoundedFIFO(t *testing.T, fifo string, read func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			_ = unix.Close(fd)
			break
		}
		if !errors.Is(err, unix.ENXIO) {
			t.Fatalf("release blocked FIFO reader %s: %v", fifo, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Errorf("FIFO reader %s did not exit after release", fifo)
	}
	t.Fatalf("read of FIFO %s blocked for more than 2s", fifo)
	return nil
}
