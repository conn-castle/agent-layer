package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

var errConcurrentSyncOverlap = errors.New("concurrent sync writer overlap")

func TestRunWithProjectSerializesConcurrentRuns(t *testing.T) {
	// RunWithProject is the shared generated-write coordinator reached by both
	// `al sync` and `al dispatch`; blocking its atomic skill write proves the
	// concrete boundary that prevents their temporary-rename collision.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "gitignore.block"), []byte("/.agent-layer/\n"), 0o600); err != nil {
		t.Fatalf("write gitignore block: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
				Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(false)},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
				Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(false)},
				VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
				CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(false)},
			},
		},
		Instructions: []config.InstructionFile{{Name: "00_rules.md", Content: "Follow the rules."}},
		Skills:       []config.Skill{{Name: "alpha", Description: "Alpha skill.", Body: "Do alpha work."}},
		Root:         root,
	}

	target := filepath.Join(root, ".agents", "skills", "alpha", "SKILL.md")
	sys := newOverlapDetectingSystem(target)
	t.Cleanup(sys.releaseBlockedWrite)

	firstErr := make(chan error, 1)
	go func() {
		_, err := RunWithProject(sys, root, project)
		firstErr <- err
	}()

	select {
	case <-sys.firstWriteBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("first sync did not reach the generated skill write")
	}

	secondErr := make(chan error, 1)
	go func() {
		_, err := RunWithProject(sys, root, project)
		secondErr <- err
	}()

	// The first run is blocked mid-write while holding the lock. A correctly
	// serialized second run must block on lock acquisition: it can neither reach
	// the generated-file writer (overlap) nor run to completion until the first
	// releases. Broken serialization makes it do one of those. waitForOverlap
	// catches the writer overlap; the secondErr probe catches early completion so
	// the test does not silently pass just because the second run was slow to
	// reach the contended write within the window.
	overlapped := sys.waitForOverlap(2 * time.Second)
	if !overlapped {
		select {
		case err := <-secondErr:
			t.Fatalf("second sync finished while the first still held the lock; serialization is broken: %v", err)
		default:
		}
	}
	sys.releaseBlockedWrite()

	err1 := receiveSyncRunError(t, firstErr)
	err2 := receiveSyncRunError(t, secondErr)
	if overlapped {
		t.Fatalf("concurrent RunWithProject calls overlapped generated-file writers: first=%v second=%v", err1, err2)
	}
	if err1 != nil || err2 != nil {
		t.Fatalf("concurrent RunWithProject calls should both succeed after serialization: first=%v second=%v", err1, err2)
	}
}

// TestAcquireProjectSyncLockHoldsOSFileLock proves the production lock engages a
// real cross-open-file-description OS advisory lock (unix.Flock), not just the
// in-process mutex. It opens a second, independent file description on the same
// lock path and asserts a non-blocking exclusive flock is refused while the
// production lock is held, then granted once it is released. This fails if the
// unix.Flock(LOCK_EX) call in lockProjectSyncFile is removed, which the
// same-process goroutine test (TestRunWithProjectSerializesConcurrentRuns)
// cannot detect because its processLock mutex serializes the goroutines alone.
func TestAcquireProjectSyncLockHoldsOSFileLock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	lockPath := filepath.Join(root, ".agent-layer", projectSyncLockFile)

	// Mirror withProjectSyncLock: hold the process token before acquiring so
	// release() returns it after releasing the operating-system lock.
	processLock := &projectSyncProcessLock{token: make(chan struct{}, 1)}
	processLock.token <- struct{}{}
	if err := processLock.acquire(RealSystem{}, time.Now().Add(projectSyncLockWaitTimeout)); err != nil {
		t.Fatalf("acquire process lock: %v", err)
	}

	lock, err := acquireProjectSyncLock(RealSystem{}, lockPath, processLock, time.Now().Add(projectSyncLockWaitTimeout))
	if err != nil {
		processLock.release()
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

func TestProjectSyncProcessLockDeadlineAndRecovery(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	sys := &MockSystem{
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}
	lock := &projectSyncProcessLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}

	if err := lock.acquire(sys, now.Add(time.Second)); err != nil {
		t.Fatalf("acquire initial process lock: %v", err)
	}
	if err := lock.acquire(sys, now.Add(250*time.Millisecond)); !errors.Is(err, errProjectSyncLockDeadline) {
		t.Fatalf("contended process lock error = %v, want deadline error", err)
	}

	lock.release()
	if err := lock.acquire(sys, now.Add(time.Second)); err != nil {
		t.Fatalf("process lock did not recover after timeout: %v", err)
	}
	lock.release()
}

func TestProjectSyncProcessLockRejectsTokenReleasedAtDeadline(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	lock := &projectSyncProcessLock{token: make(chan struct{}, 1)}
	deadline := now.Add(projectSyncLockPollEvery)
	sys := &MockSystem{
		NowFunc: func() time.Time { return now },
		SleepFunc: func(d time.Duration) {
			now = now.Add(d)
			lock.release()
		},
	}

	if err := lock.acquire(sys, deadline); !errors.Is(err, errProjectSyncLockDeadline) {
		t.Fatalf("acquire error = %v, want deadline error", err)
	}
	if len(lock.token) != 1 {
		t.Fatal("token released at the deadline was consumed")
	}
}

func TestWithProjectSyncLockTimeoutDiagnosticsAndRecovery(t *testing.T) {
	root := newSyncLockTestRoot(t)
	lockPath := filepath.Join(root, ".agent-layer", projectSyncLockFile)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	contended := true
	sys := &MockSystem{
		Fallback: RealSystem{},
		FlockFunc: func(_ int, how int) error {
			if how == unix.LOCK_UN || !contended {
				return nil
			}
			return unix.EWOULDBLOCK
		},
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(d time.Duration) { now = now.Add(d) },
	}

	result, err := withProjectSyncLock(sys, root, func() (*Result, error) {
		return &Result{}, nil
	})
	if result != nil {
		t.Fatalf("timed-out lock returned result %#v", result)
	}
	if err == nil {
		t.Fatal("expected lock timeout")
	}
	for _, want := range []string{lockPath, "30s", "another sync may still be generating files"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("timeout error %q does not contain %q", err, want)
		}
	}

	contended = false
	want := &Result{}
	result, err = withProjectSyncLock(sys, root, func() (*Result, error) {
		return want, nil
	})
	if err != nil {
		t.Fatalf("lock did not recover after timeout: %v", err)
	}
	if result != want {
		t.Fatalf("result = %#v, want original populated result", result)
	}
}

func TestWithProjectSyncLockRetriesEINTRWithNonBlockingFlock(t *testing.T) {
	root := newSyncLockTestRoot(t)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	var flockCalls []int
	acquisitionCalls := 0
	sys := &MockSystem{
		Fallback: RealSystem{},
		FlockFunc: func(_ int, how int) error {
			flockCalls = append(flockCalls, how)
			if how == unix.LOCK_UN {
				return nil
			}
			acquisitionCalls++
			switch acquisitionCalls {
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

	if _, err := withProjectSyncLock(sys, root, func() (*Result, error) { return &Result{}, nil }); err != nil {
		t.Fatalf("withProjectSyncLock: %v", err)
	}
	if len(flockCalls) != 4 {
		t.Fatalf("flock calls = %v, want three acquisitions and one release", flockCalls)
	}
	for _, how := range flockCalls[:3] {
		if how != unix.LOCK_EX|unix.LOCK_NB {
			t.Fatalf("acquisition flock flags = %#x, want LOCK_EX|LOCK_NB", how)
		}
	}
	if flockCalls[3] != unix.LOCK_UN {
		t.Fatalf("release flock flags = %#x, want LOCK_UN", flockCalls[3])
	}
}

func TestWithProjectSyncLockReturnsNonContentionFlockErrorImmediately(t *testing.T) {
	root := newSyncLockTestRoot(t)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	flockErr := unix.EPERM
	slept := false
	sys := &MockSystem{
		Fallback:  RealSystem{},
		FlockFunc: func(int, int) error { return flockErr },
		NowFunc:   func() time.Time { return now },
		SleepFunc: func(time.Duration) { slept = true },
	}

	_, err := withProjectSyncLock(sys, root, func() (*Result, error) { return &Result{}, nil })
	if !errors.Is(err, flockErr) {
		t.Fatalf("error = %v, want flock error %v", err, flockErr)
	}
	if slept {
		t.Fatal("non-contention flock error unexpectedly slept")
	}
}

func TestWithProjectSyncLockPreservesSuccessfulResultOnCleanupFailure(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*MockSystem, error)
	}{
		{
			name: "unlock",
			configure: func(sys *MockSystem, cleanupErr error) {
				sys.FlockFunc = func(_ int, how int) error {
					if how == unix.LOCK_UN {
						return cleanupErr
					}
					return nil
				}
			},
		},
		{
			name: "close",
			configure: func(sys *MockSystem, cleanupErr error) {
				sys.CloseFunc = func(file *os.File) error {
					if err := file.Close(); err != nil {
						return err
					}
					return cleanupErr
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newSyncLockTestRoot(t)
			cleanupErr := errors.New(tt.name + " cleanup failure")
			sys := &MockSystem{Fallback: RealSystem{}}
			tt.configure(sys, cleanupErr)
			want := &Result{}

			result, err := withProjectSyncLock(sys, root, func() (*Result, error) { return want, nil })
			if result != want {
				t.Fatalf("result = %#v, want populated successful result", result)
			}
			if !errors.Is(err, ErrPostWriteLockCleanup) {
				t.Fatalf("error = %v, want ErrPostWriteLockCleanup", err)
			}
			for _, wantText := range []string{"generated writes succeeded", cleanupErr.Error()} {
				if !strings.Contains(err.Error(), wantText) {
					t.Fatalf("cleanup error %q does not contain %q", err, wantText)
				}
			}
		})
	}
}

func TestWithProjectSyncLockKeepsWorkFailurePrimaryWhenCleanupAlsoFails(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*MockSystem, error)
	}{
		{
			name: "unlock",
			configure: func(sys *MockSystem, cleanupErr error) {
				sys.FlockFunc = func(_ int, how int) error {
					if how == unix.LOCK_UN {
						return cleanupErr
					}
					return nil
				}
			},
		},
		{
			name: "close",
			configure: func(sys *MockSystem, cleanupErr error) {
				sys.CloseFunc = func(file *os.File) error {
					if err := file.Close(); err != nil {
						return err
					}
					return cleanupErr
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newSyncLockTestRoot(t)
			workErr := errors.New("generated write failure")
			cleanupErr := errors.New(tt.name + " cleanup failure")
			sys := &MockSystem{Fallback: RealSystem{}}
			tt.configure(sys, cleanupErr)

			_, err := withProjectSyncLock(sys, root, func() (*Result, error) { return nil, workErr })
			if !errors.Is(err, workErr) {
				t.Fatalf("error = %v, want primary work error %v", err, workErr)
			}
			if errors.Is(err, ErrPostWriteLockCleanup) {
				t.Fatalf("combined error incorrectly claims generated writes succeeded: %v", err)
			}
			if !strings.Contains(err.Error(), cleanupErr.Error()) {
				t.Fatalf("combined error %q does not retain cleanup detail %q", err, cleanupErr)
			}
		})
	}
}

func TestAcquireProjectSyncLockReportsAcquisitionCleanupFailure(t *testing.T) {
	root := newSyncLockTestRoot(t)
	path := filepath.Join(root, ".agent-layer", projectSyncLockFile)
	cleanupErr := errors.New("close during acquisition failed")
	sys := &MockSystem{
		Fallback:  RealSystem{},
		FlockFunc: func(int, int) error { return unix.EPERM },
		CloseFunc: func(file *os.File) error {
			if err := file.Close(); err != nil {
				return err
			}
			return cleanupErr
		},
	}
	processLock := &projectSyncProcessLock{token: make(chan struct{}, 1)}
	processLock.token <- struct{}{}
	if err := processLock.acquire(sys, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("acquire process lock: %v", err)
	}
	defer processLock.release()

	lock, err := acquireProjectSyncLock(sys, path, processLock, time.Now().Add(time.Second))
	if lock != nil {
		t.Fatalf("lock = %#v, want nil", lock)
	}
	if !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), cleanupErr.Error()) {
		t.Fatalf("acquisition error = %v, want flock and cleanup failures", err)
	}
}

func newSyncLockTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	return root
}

type overlapDetectingSystem struct {
	System

	target            string
	firstWriteBlocked chan struct{}
	releaseWrite      chan struct{}
	overlapDetected   chan struct{}

	mu           stdsync.Mutex
	releaseOnce  stdsync.Once
	overlapOnce  stdsync.Once
	blockOnce    stdsync.Once
	writeBlocked bool
}

func newOverlapDetectingSystem(target string) *overlapDetectingSystem {
	return &overlapDetectingSystem{
		System:            RealSystem{},
		target:            target,
		firstWriteBlocked: make(chan struct{}),
		releaseWrite:      make(chan struct{}),
		overlapDetected:   make(chan struct{}),
	}
}

func (s *overlapDetectingSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	if filename != s.target {
		return s.System.WriteFileAtomic(filename, data, perm)
	}

	shouldBlock := false
	s.mu.Lock()
	if s.writeBlocked {
		s.overlapOnce.Do(func() { close(s.overlapDetected) })
		s.mu.Unlock()
		return errConcurrentSyncOverlap
	}
	s.blockOnce.Do(func() {
		s.writeBlocked = true
		shouldBlock = true
		close(s.firstWriteBlocked)
	})
	s.mu.Unlock()

	if shouldBlock {
		<-s.releaseWrite
		err := s.System.WriteFileAtomic(filename, data, perm)
		s.mu.Lock()
		s.writeBlocked = false
		s.mu.Unlock()
		return err
	}

	return s.System.WriteFileAtomic(filename, data, perm)
}

func (s *overlapDetectingSystem) waitForOverlap(timeout time.Duration) bool {
	select {
	case <-s.overlapDetected:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *overlapDetectingSystem) releaseBlockedWrite() {
	s.releaseOnce.Do(func() { close(s.releaseWrite) })
}

func receiveSyncRunError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("sync run did not finish")
		return nil
	}
}
