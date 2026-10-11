package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/version"
)

// RefreshCommand is an internal helper that survives the client's exec handoff.
const RefreshCommand = "__refresh-update-cache"
const releaseCacheTTL = 24 * time.Hour
const refreshTimeout = 2 * time.Second

// Keep these consistent with versiondispatch's environment contract. That
// package imports update, so importing it here would create a cycle.
const envCacheDir = "AL_CACHE_DIR"
const envNoNetwork = "AL_NO_NETWORK"

var errNoCachedRelease = errors.New("no cached release information")

type releaseCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

func releaseCachePath() (string, error) {
	base := strings.TrimSpace(os.Getenv(envCacheDir))
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(base, "agent-layer")
	}
	return filepath.Join(base, "latest-release.json"), nil
}

func readReleaseCache() (releaseCache, error) {
	path, err := releaseCachePath()
	if err != nil {
		return releaseCache{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- resolved user cache path.
	if err != nil {
		return releaseCache{}, err
	}
	var cache releaseCache
	if err = json.Unmarshal(data, &cache); err != nil {
		return releaseCache{}, err
	}
	cache.Latest, err = version.Normalize(cache.Latest)
	if err != nil || cache.CheckedAt.IsZero() || cache.CheckedAt.After(time.Now()) {
		return releaseCache{}, errNoCachedRelease
	}
	return cache, nil
}

func saveReleaseCache(latest string) error {
	path, err := releaseCachePath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(releaseCache{Latest: latest, CheckedAt: time.Now()})
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

// CachedCheck never performs network I/O. Stale data remains usable for a notice
// while a detached helper refreshes it for subsequent launches.
func CachedCheck(_ context.Context, currentVersion string) (CheckResult, error) {
	current, isDev, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return CheckResult{}, err
	}
	cache, err := readReleaseCache()
	if err != nil {
		return CheckResult{}, err
	}
	return compareRelease(current, isDev, cache.Latest)
}

func cacheIsFresh() bool {
	cache, err := readReleaseCache()
	return err == nil && time.Since(cache.CheckedAt) < releaseCacheTTL
}

// StartCacheRefresh starts a silent, isolated helper without waiting for GitHub.
// A goroutine alone cannot finish after al replaces itself with the client.
func StartCacheRefresh() {
	if cacheIsFresh() {
		return
	}
	path, err := os.Executable()
	if err != nil {
		return
	}
	// #nosec G204 -- current executable with a fixed internal command.
	cmd := exec.CommandContext(context.Background(), path, RefreshCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

// RefreshCache has one short deadline shared by all requests and retry waits.
// Launch callers gate update suppression and version overrides before spawning.
func RefreshCache(ctx context.Context) {
	if strings.TrimSpace(os.Getenv(envNoNetwork)) != "" || cacheIsFresh() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	_, _ = Check(ctx, "dev")
}
