package sync

import (
	"errors"
	"os"
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

var errConcurrentSyncOverlap = errors.New("concurrent sync writer overlap")

func TestRunWithProjectSerializesConcurrentRuns(t *testing.T) {
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

	overlapped := sys.waitForOverlap(500 * time.Millisecond)
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

	// Mirror withProjectSyncLock: hold the process mutex before acquiring so the
	// deferred Unlock inside release() does not panic on an unlocked mutex.
	processLock := &stdsync.Mutex{}
	processLock.Lock()

	lock, err := acquireProjectSyncLock(lockPath, processLock)
	if err != nil {
		processLock.Unlock()
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
