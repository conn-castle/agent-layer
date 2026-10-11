package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
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
	// TestMain starts the real production helper and replaces the parent with sh.
	cmd := exec.CommandContext(context.Background(), os.Args[0]) // #nosec G204,G702 -- current test executable.
	cmd.Env = append(os.Environ(), "AL_UPDATE_TEST_HELPER=1", "AL_UPDATE_TEST_RELEASE_URL="+server.URL, "AL_CACHE_DIR="+cacheDir, "AL_NO_NETWORK=", "AL_UPDATE_TEST_HANDOFF_FILE="+filepath.Join(cacheDir, "parent-exited"))
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "parent-exited"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AL_CACHE_DIR", cacheDir)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if result, err := CachedCheck(context.Background(), "1.0.0"); err == nil && result.Latest == "2.0.0" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refresh did not complete after parent exec and exit")
}
