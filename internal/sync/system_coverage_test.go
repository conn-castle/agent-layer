package sync

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRealSystem_LookPath_NotFound(t *testing.T) {
	sys := RealSystem{}
	_, err := sys.LookPath("this-binary-should-not-exist-abc123xyz")
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
}

func TestRealSystem_MkdirAll(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	target := dir + "/sub/dir"
	if err := sys.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected created directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", target)
	}
}

func TestRealSystem_ReadDir(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := sys.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestRealSystem_Remove(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	path := dir + "/remove-me.txt"
	if err := os.WriteFile(path, []byte("bye"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sys.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}
}

func TestRealSystem_RemoveAll(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	sub := dir + "/sub"
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sys.RemoveAll(sub); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed")
	}
}

func TestRealSystemLockLifecycle(t *testing.T) {
	sys := RealSystem{}
	path := t.TempDir() + "/sync.lock"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path is rooted under a test-owned temporary directory.
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	probe, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path is rooted under a test-owned temporary directory.
	if err != nil {
		_ = sys.Close(file)
		t.Fatalf("open probe: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(probe) })

	if err := sys.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = sys.Close(file)
		t.Fatalf("acquire lock: %v", err)
	}
	if err := sys.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = sys.Close(file)
		t.Fatalf("probe lock error = %v, want EWOULDBLOCK/EAGAIN", err)
	}
	if err := sys.Flock(int(file.Fd()), unix.LOCK_UN); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = sys.Close(file)
		t.Fatalf("unlock: %v", err)
	}
	if err := sys.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = sys.Close(file)
		t.Fatalf("probe acquire after unlock: %v", err)
	}
	if err := sys.Flock(int(probe.Fd()), unix.LOCK_UN); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		_ = sys.Close(file)
		t.Fatalf("probe unlock: %v", err)
	}
	if err := sys.Close(file); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := file.Stat(); err == nil {
		t.Fatal("closed file remained usable")
	}
}

func TestRealSystemNowAndSleep(t *testing.T) {
	sys := RealSystem{}
	before := sys.Now()
	sys.Sleep(2 * time.Millisecond)
	if elapsed := sys.Now().Sub(before); elapsed < time.Millisecond {
		t.Fatalf("Sleep returned too early: elapsed=%v", elapsed)
	}
}
