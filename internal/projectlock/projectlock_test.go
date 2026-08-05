package projectlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// realSystem is the production System behavior used by lock tests that must
// exercise actual syscalls.
type realSystem struct{}

func (realSystem) Close(file *os.File) error   { return file.Close() }
func (realSystem) Flock(fd int, how int) error { return unix.Flock(fd, how) }
func (realSystem) Now() time.Time              { return time.Now() }
func (realSystem) Sleep(d time.Duration)       { time.Sleep(d) }

// mockSystem injects clock and syscall faults, falling back to Fallback for
// any behavior a test does not override.
type mockSystem struct {
	Fallback  System
	CloseFunc func(file *os.File) error
	FlockFunc func(fd int, how int) error
	NowFunc   func() time.Time
	SleepFunc func(d time.Duration)
}

func (m *mockSystem) Close(file *os.File) error {
	if m.CloseFunc != nil {
		return m.CloseFunc(file)
	}
	return m.Fallback.Close(file)
}

func (m *mockSystem) Flock(fd int, how int) error {
	if m.FlockFunc != nil {
		return m.FlockFunc(fd, how)
	}
	return m.Fallback.Flock(fd, how)
}

func (m *mockSystem) Now() time.Time {
	if m.NowFunc != nil {
		return m.NowFunc()
	}
	return m.Fallback.Now()
}

func (m *mockSystem) Sleep(d time.Duration) {
	if m.SleepFunc != nil {
		m.SleepFunc(d)
		return
	}
	m.Fallback.Sleep(d)
}

func newLockTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	return root
}

// TestAcquireProjectSyncLockHoldsOSFileLock proves the production lock engages a
// real cross-open-file-description OS advisory lock (unix.Flock), not just the
// in-process mutex. It opens a second, independent file description on the same
// lock path and asserts a non-blocking exclusive flock is refused while the
// production lock is held, then granted once it is released. This fails if the
// unix.Flock(LOCK_EX) call in lockFile is removed, which the
// same-process goroutine test (TestRunWithProjectSerializesConcurrentRuns)
// cannot detect because its processLock mutex serializes the goroutines alone.
func TestAcquireProjectSyncLockHoldsOSFileLock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	lockPath := Path(root)

	// Mirror withProjectSyncLock: hold the process token before acquiring so
	// release() returns it after releasing the operating-system lock.
	held := &processLock{token: make(chan struct{}, 1)}
	held.token <- struct{}{}
	if err := held.acquire(realSystem{}, time.Now().Add(waitTimeout)); err != nil {
		t.Fatalf("acquire process lock: %v", err)
	}

	lock, err := acquire(realSystem{}, lockPath, held, time.Now().Add(waitTimeout))
	if err != nil {
		held.release()
		t.Fatalf("acquireProjectSyncLock: %v", err)
	}

	// Independent open-file-description on the same path. flock locks are bound to
	// the open file description, so a non-blocking exclusive lock from this fd must
	// be refused while the production lock holds it — independent of the mutex.
	probe, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lockPath is rooted under a test-controlled t.TempDir().
	if err != nil {
		_ = lock.release()
		t.Fatalf("open probe descriptor: %v", err)
	}
	t.Cleanup(func() { _ = probe.Close() })

	err = unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		_ = lock.release()
		t.Fatalf("expected EWOULDBLOCK/EAGAIN from probe while production lock held, got: %v", err)
	}

	// Releasing the production lock must free the OS lock so the probe can take it.
	if err := lock.release(); err != nil {
		t.Fatalf("release production lock: %v", err)
	}

	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		t.Fatalf("expected probe to acquire lock after release, got: %v", err)
	}
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_UN); err != nil { //nolint:gosec // Unix file descriptors are small non-negative ints on supported platforms.
		t.Fatalf("unlock probe: %v", err)
	}
}

func TestProcessLockDeadlineAndRecovery(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	sys := &mockSystem{
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}
	lock := &processLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}

	if err := lock.acquire(sys, now.Add(time.Second)); err != nil {
		t.Fatalf("acquire initial process lock: %v", err)
	}
	if err := lock.acquire(sys, now.Add(250*time.Millisecond)); !errors.Is(err, errDeadline) {
		t.Fatalf("contended process lock error = %v, want deadline error", err)
	}

	lock.release()
	if err := lock.acquire(sys, now.Add(time.Second)); err != nil {
		t.Fatalf("process lock did not recover after timeout: %v", err)
	}
	lock.release()
}

func TestProcessLockRejectsTokenReleasedAtDeadline(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	lock := &processLock{token: make(chan struct{}, 1)}
	deadline := now.Add(pollEvery)
	sys := &mockSystem{
		NowFunc: func() time.Time { return now },
		SleepFunc: func(d time.Duration) {
			now = now.Add(d)
			lock.release()
		},
	}

	if err := lock.acquire(sys, deadline); !errors.Is(err, errDeadline) {
		t.Fatalf("acquire error = %v, want deadline error", err)
	}
	if len(lock.token) != 1 {
		t.Fatal("token released at the deadline was consumed")
	}
}

func TestAcquireProjectSyncLockReportsAcquisitionCleanupFailure(t *testing.T) {
	root := newLockTestRoot(t)
	path := Path(root)
	cleanupErr := errors.New("close during acquisition failed")
	sys := &mockSystem{
		Fallback:  realSystem{},
		FlockFunc: func(int, int) error { return unix.EPERM },
		CloseFunc: func(file *os.File) error {
			if err := file.Close(); err != nil {
				return err
			}
			return cleanupErr
		},
	}
	held := &processLock{token: make(chan struct{}, 1)}
	held.token <- struct{}{}
	if err := held.acquire(sys, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("acquire process lock: %v", err)
	}
	defer held.release()

	lock, err := acquire(sys, path, held, time.Now().Add(time.Second))
	if lock != nil {
		t.Fatalf("lock = %#v, want nil", lock)
	}
	if !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), cleanupErr.Error()) {
		t.Fatalf("acquisition error = %v, want flock and cleanup failures", err)
	}
}

// TestWithSerializesAndReportsCleanupFailures proves the shared critical
// section runs the caller's work, serializes independent holders, and reports a
// post-work cleanup failure without hiding it.
func TestWithSerializesAndReportsCleanupFailures(t *testing.T) {
	root := newLockTestRoot(t)

	ran := false
	if err := With(realSystem{}, root, func() error {
		ran = true
		// The lock file must exist while the section runs.
		if _, statErr := os.Stat(Path(root)); statErr != nil {
			t.Fatalf("lock file missing inside the critical section: %v", statErr)
		}
		return nil
	}); err != nil {
		t.Fatalf("With: %v", err)
	}
	if !ran {
		t.Fatal("With did not run the caller's work")
	}

	workErr := errors.New("work failed")
	if err := With(realSystem{}, root, func() error { return workErr }); !errors.Is(err, workErr) {
		t.Fatalf("error = %v, want the caller's error", err)
	}

	cleanupErr := errors.New("unlock failed")
	sys := &mockSystem{
		Fallback: realSystem{},
		FlockFunc: func(_ int, how int) error {
			if how == unix.LOCK_UN {
				return cleanupErr
			}
			return nil
		},
	}
	err := With(sys, root, func() error { return nil })
	if !errors.Is(err, ErrPostWriteCleanup) {
		t.Fatalf("error = %v, want ErrPostWriteCleanup", err)
	}
	if !strings.Contains(err.Error(), cleanupErr.Error()) {
		t.Fatalf("error %q does not retain the cleanup detail", err)
	}
}

// TestWithReturnsATimeoutWhenTheProcessTokenIsHeld proves a contended
// in-process holder produces the actionable timeout message rather than
// blocking forever.
func TestWithReturnsATimeoutWhenTheProcessTokenIsHeld(t *testing.T) {
	root := newLockTestRoot(t)
	lockPath := Path(root)

	held := processLockForPath(lockPath)
	if err := held.acquire(realSystem{}, time.Now().Add(waitTimeout)); err != nil {
		t.Fatalf("acquire process lock: %v", err)
	}
	defer held.release()

	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	sys := &mockSystem{
		Fallback:  realSystem{},
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}
	err := With(sys, root, func() error {
		t.Fatal("the critical section ran while the process token was held")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), lockPath) {
		t.Fatalf("error = %v, want a timeout naming %s", err, lockPath)
	}
}

// TestLockAcquisitionRetriesInterruptionsAndContention proves acquisition
// treats EINTR as a retry, waits out contention, and never spins on a
// non-blocking flock.
func TestLockAcquisitionRetriesInterruptionsAndContention(t *testing.T) {
	root := newLockTestRoot(t)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	var calls []int
	attempt := 0
	sys := &mockSystem{
		Fallback: realSystem{},
		FlockFunc: func(_ int, how int) error {
			calls = append(calls, how)
			if how == unix.LOCK_UN {
				return nil
			}
			attempt++
			switch attempt {
			case 1:
				return unix.EINTR
			case 2:
				return unix.EAGAIN
			default:
				return nil
			}
		},
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}

	if err := With(sys, root, func() error { return nil }); err != nil {
		t.Fatalf("With: %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("flock calls = %v, want three acquisitions and one release", calls)
	}
	for _, how := range calls[:3] {
		if how != unix.LOCK_EX|unix.LOCK_NB {
			t.Fatalf("acquisition flags = %#x, want LOCK_EX|LOCK_NB", how)
		}
	}
	if calls[3] != unix.LOCK_UN {
		t.Fatalf("release flags = %#x, want LOCK_UN", calls[3])
	}
}

// TestLockReturnsNonContentionErrorsImmediately proves a permission failure is
// surfaced at once rather than retried until the deadline.
func TestLockReturnsNonContentionErrorsImmediately(t *testing.T) {
	root := newLockTestRoot(t)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	slept := false
	sys := &mockSystem{
		Fallback:  realSystem{},
		FlockFunc: func(int, int) error { return unix.EPERM },
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(time.Duration) { slept = true },
	}

	if err := With(sys, root, func() error { return nil }); !errors.Is(err, unix.EPERM) {
		t.Fatalf("error = %v, want the flock error", err)
	}
	if slept {
		t.Fatal("a non-contention flock error was retried")
	}
}

// TestLockTimesOutUnderSustainedContention proves a lock another process holds
// produces the actionable timeout message instead of blocking forever.
func TestLockTimesOutUnderSustainedContention(t *testing.T) {
	root := newLockTestRoot(t)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	sys := &mockSystem{
		Fallback: realSystem{},
		FlockFunc: func(_ int, how int) error {
			if how == unix.LOCK_UN {
				return nil
			}
			return unix.EWOULDBLOCK
		},
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}

	err := With(sys, root, func() error {
		t.Fatal("the critical section ran while the lock was contended")
		return nil
	})
	if err == nil {
		t.Fatal("expected a timeout")
	}
	for _, want := range []string{Path(root), "30s", "another sync may still be generating files"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("timeout error %q does not contain %q", err, want)
		}
	}
}

// TestLockReportsAnUnopenableLockPath proves a lock file that cannot be created
// fails loudly rather than silently skipping serialization.
func TestLockReportsAnUnopenableLockPath(t *testing.T) {
	root := t.TempDir()
	// No .agent-layer directory exists, so the lock file cannot be created.
	if err := With(realSystem{}, root, func() error {
		t.Fatal("the critical section ran without a lock")
		return nil
	}); err == nil {
		t.Fatal("expected opening the lock file to fail")
	}
}
