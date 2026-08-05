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
	"github.com/conn-castle/agent-layer/internal/projectlock"
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
		Skills:       []config.Skill{projectionSkill(t, "alpha", projectionManifest("alpha"))},
		Root:         root,
	}

	// Projection stages each skill off-path before publishing it, so the
	// contended generated write is the staged manifest.
	target := filepath.Join(root, ".agents", "skills"+projectionStageSuffix, "alpha", "SKILL.md")
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

func TestWithProjectSyncLockTimeoutDiagnosticsAndRecovery(t *testing.T) {
	root := newSyncLockTestRoot(t)
	lockPath := projectlock.Path(root)
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
