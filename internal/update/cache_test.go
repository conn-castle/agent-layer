package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCachedCheckUsesLocalReleaseAndRecomparesCurrentVersion(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	withLatestReleaseClient(t, "https://example.invalid", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("cached check attempted network I/O")
		return nil, nil
	})})
	if err := saveReleaseCache("2.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{"1.0.0", "2.0.0", "3.0.0", "dev"} {
		result, err := CachedCheck(context.Background(), current)
		if err != nil || result.Latest != "2.0.0" || result.Outdated != (current == "1.0.0") || result.CurrentIsDev != (current == "dev") {
			t.Fatalf("current %s: %+v %v", current, result, err)
		}
	}
}

func TestReleaseCacheValidationAndFreshness(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	path, err := releaseCachePath()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, latest string
		checkedAt    time.Time
		valid, fresh bool
	}{
		{"fresh", "v2.0.0", time.Now().Add(-time.Minute), true, true},
		{"stale", "2.0.0", time.Now().Add(-48 * time.Hour), true, false},
		{"invalid version", "bad", time.Now().Add(-time.Minute), false, false},
		{"no timestamp", "2.0.0", time.Time{}, false, false},
		{"future timestamp", "2.0.0", time.Now().Add(time.Hour), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(releaseCache{Latest: tc.latest, CheckedAt: tc.checkedAt})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			} // #nosec G304 -- test cache.
			_, err = CachedCheck(context.Background(), "1.0.0")
			if (err == nil) != tc.valid || cacheIsFresh() != tc.fresh {
				t.Fatalf("valid=%v fresh=%v err=%v", tc.valid, tc.fresh, err)
			}
		})
	}
	for _, data := range [][]byte{nil, []byte("{")} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		} // #nosec G304 -- test cache.
		if _, err := CachedCheck(context.Background(), "1.0.0"); err == nil {
			t.Fatal("invalid cache accepted")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := CachedCheck(context.Background(), "1.0.0"); err == nil {
		t.Fatal("missing cache accepted")
	}
}

func TestRefreshCacheDeadlineAndFailurePreserveStaleRelease(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	t.Setenv("AL_NO_NETWORK", "")
	path, err := releaseCachePath()
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := json.Marshal(releaseCache{Latest: "2.0.0", CheckedAt: time.Now().Add(-48 * time.Hour)})
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	} // #nosec G304 -- test cache.
	withLatestReleaseServer(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	start := time.Now()
	RefreshCache(context.Background())
	elapsed := time.Since(start)
	if elapsed > refreshTimeout+time.Second {
		t.Fatalf("refresh exceeded total deadline: %s", elapsed)
	}
	got, err := os.ReadFile(path) // #nosec G304 -- test cache.
	if err != nil || string(got) != string(stale) {
		t.Fatalf("failed refresh changed cached release: %q %v", got, err)
	}
}

func TestRefreshCacheSavesReleaseAndSkipsFreshOrOfflineChecks(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	t.Setenv("AL_NO_NETWORK", "")
	var calls atomic.Int32
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	})
	RefreshCache(context.Background())
	result, err := CachedCheck(context.Background(), "1.0.0")
	if err != nil || !result.Outdated || result.Latest != "2.0.0" {
		t.Fatalf("cache was not refreshed: %+v %v", result, err)
	}
	RefreshCache(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("fresh cache triggered request: %d", calls.Load())
	}
	t.Setenv("AL_CACHE_DIR", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AL_NO_NETWORK", "1")
	RefreshCache(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("offline refresh triggered request: %d", calls.Load())
	}
}

func TestRetryWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := updateSleep(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("error=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("retry wait ignored cancellation")
	}
}

func TestRefreshHelperSurvivesExecHandoff(t *testing.T) {
	cacheDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	defer server.Close()
	// TestMain starts the real production helper chain and replaces the parent
	// with a long-lived sleep, mimicking a client that outlives the refresh.
	// The helper records its own parent PID after the exec gate: a detached
	// helper reports init (or the subreaper), never the long-lived client.
	cmd := exec.CommandContext(context.Background(), os.Args[0]) // #nosec G204,G702 -- current test executable.
	handoff := filepath.Join(cacheDir, "parent-exited")
	ppidFile := filepath.Join(cacheDir, "helper-ppid")
	cmd.Env = append(os.Environ(), "AL_UPDATE_TEST_HELPER=1", "AL_UPDATE_TEST_RELEASE_URL="+server.URL, "AL_CACHE_DIR="+cacheDir, "AL_NO_NETWORK=", "AL_UPDATE_TEST_HANDOFF_FILE="+handoff, "AL_UPDATE_TEST_PPID_FILE="+ppidFile)
	// Match production exit timing so the race detector's default one-second
	// exit sleep does not force the intermediate through its timeout path.
	cmd.Env = append(cmd.Env, "GORACE="+strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	clientPID := cmd.Process.Pid
	if !waitForExecTarget(t, clientPID) {
		t.Fatal("helper parent did not exec the long-lived target")
	}
	if err := os.WriteFile(handoff, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	helperPPID := waitForHelperPPID(t, ppidFile)
	if helperPPID == clientPID {
		t.Fatalf("helper is still a direct child of the long-lived client %d", clientPID)
	}
	t.Setenv("AL_CACHE_DIR", cacheDir)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if result, err := CachedCheck(context.Background(), "1.0.0"); err == nil && result.Latest == "2.0.0" {
			if runtime.GOOS == "linux" {
				if zombies := zombieChildren(clientPID); len(zombies) != 0 {
					t.Fatalf("helper left zombie children under long-lived client %d: %v", clientPID, zombies)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refresh did not complete after parent exec and exit")
}

// waitForExecTarget reports whether pid's command became the long-lived exec
// target, proving the helper parent replaced itself instead of exiting. It
// polls the portable ps utility so the test runs on every supported platform.
func waitForExecTarget(t *testing.T, pid int) bool {
	t.Helper()
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps utility unavailable for exec-target detection")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output() // #nosec G204 -- test inspects its own fixture process.
		if err == nil && filepath.Base(strings.TrimSpace(string(out))) == "sleep" {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForHelperPPID returns the parent PID the detached helper observed for
// itself after the exec gate. The helper writes it before starting network
// I/O, so the assertion cannot race the helper's exit.
func waitForHelperPPID(t *testing.T, ppidFile string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// #nosec G304 -- test reads its own fixture file.
		data, err := os.ReadFile(ppidFile)
		if err == nil {
			if ppid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && ppid > 0 {
				return ppid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper did not record its parent PID after the exec gate")
	return 0
}

// zombieChildren returns the PIDs of zombie processes parented to pid by
// scanning Linux process state directly.
func zombieChildren(pid int) []int {
	var zombies []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		child, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// #nosec G304 -- test scans fixture process state under /proc.
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		rest := string(data)
		idx := strings.LastIndex(rest, ")")
		if idx < 0 || idx+3 >= len(rest) {
			continue
		}
		fields := strings.Fields(rest[idx+2:])
		if len(fields) < 2 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		if ppid == pid && fields[0] == "Z" {
			zombies = append(zombies, child)
		}
	}
	return zombies
}

func TestTryClaimRefreshAttemptConcurrent(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	const launchers = 32
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range launchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tryClaimRefreshAttempt() {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("concurrent attempts granted %d spawns, want exactly 1", got)
	}
	if tryClaimRefreshAttempt() {
		t.Fatal("follow-up launch spawned while a recent attempt is in flight")
	}
}

func TestTryClaimRefreshAttemptThrottleWindow(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	if !tryClaimRefreshAttempt() {
		t.Fatal("first attempt was not granted")
	}
	if tryClaimRefreshAttempt() {
		t.Fatal("recent attempt did not throttle the next launch")
	}
	path, err := refreshAttemptPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		when time.Time
	}{
		{"expired", time.Now().Add(-refreshAttemptCooldown - time.Minute)},
		{"future", time.Now().Add(24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := tc.when.UTC().Format(time.RFC3339)
			if err := os.WriteFile(path, []byte(stamp), 0o600); err != nil { // #nosec G304 -- test cache.
				t.Fatal(err)
			}
			if !tryClaimRefreshAttempt() {
				t.Fatal("attempt marker blocked a fresh launch")
			}
			if tryClaimRefreshAttempt() {
				t.Fatal("renewed attempt did not throttle the next launch")
			}
		})
	}
}

func TestTryClaimRefreshAttemptExpiredRenewalConcurrent(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	path, err := refreshAttemptPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// Seed an expired claim, then race renewals: the persistent marker is only
	// ever rewritten under the advisory lock, so exactly one launcher wins.
	aged := time.Now().Add(-refreshAttemptCooldown - time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(path, []byte(aged), 0o600); err != nil { // #nosec G304 -- test cache.
		t.Fatal(err)
	}
	const launchers = 32
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range launchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tryClaimRefreshAttempt() {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("concurrent renewals granted %d spawns, want exactly 1", got)
	}
}

func TestTryClaimRefreshAttemptContentionFailsClosedFast(t *testing.T) {
	t.Setenv("AL_CACHE_DIR", t.TempDir())
	path, err := refreshAttemptPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- test holds the claim lock itself.
	held, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if err := unix.Flock(int(held.Fd()), unix.LOCK_EX); err != nil { //nolint:gosec // test-owned descriptor.
		t.Fatal(err)
	}
	defer func() { _ = unix.Flock(int(held.Fd()), unix.LOCK_UN) }() //nolint:gosec // test-owned descriptor.
	done := make(chan bool, 1)
	go func() { done <- tryClaimRefreshAttempt() }()
	select {
	case granted := <-done:
		if granted {
			t.Fatal("claim granted while another holder owns the attempt lock")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim waited on lock contention instead of failing closed")
	}
}

func TestRunRefreshForkSlowIntermediateRespectsDeadline(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "slow-fork")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 5\n"), 0o700); err != nil { // #nosec G306 -- executable test fixture.
		t.Fatal(err)
	}
	start := time.Now()
	if err := runRefreshFork(stub); err == nil {
		t.Fatal("slow intermediate did not hit the handoff deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("slow intermediate blocked launch for %s", elapsed)
	}
}

func TestStartCacheRefreshThrottlesConcurrentSpawns(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("AL_CACHE_DIR", cacheDir)
	stale, err := json.Marshal(releaseCache{Latest: "2.0.0", CheckedAt: time.Now().Add(-48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	path, err := releaseCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil { // #nosec G304 -- test cache.
		t.Fatal(err)
	}
	orig := forkRefreshHelper
	var spawns atomic.Int32
	forkRefreshHelper = func(string) error {
		spawns.Add(1)
		return nil
	}
	t.Cleanup(func() { forkRefreshHelper = orig })
	const launchers = 32
	var wg sync.WaitGroup
	for range launchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			StartCacheRefresh()
		}()
	}
	wg.Wait()
	if got := spawns.Load(); got != 1 {
		t.Fatalf("concurrent launches spawned %d helpers, want exactly 1", got)
	}
	StartCacheRefresh()
	if got := spawns.Load(); got != 1 {
		t.Fatalf("repeated launch spawned another helper, total %d, want 1", got)
	}
}
