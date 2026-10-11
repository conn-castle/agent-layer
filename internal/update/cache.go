package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/version"
)

// RefreshCommand is an internal helper that survives the client's exec handoff.
const RefreshCommand = "__refresh-update-cache"

// RefreshForkCommand is the short-lived intermediate that detaches the refresh
// helper. StartCacheRefresh waits for the intermediate rather than the helper,
// so the helper is reparented before al replaces itself with the client.
const RefreshForkCommand = "__refresh-update-cache-fork"
const releaseCacheTTL = 24 * time.Hour
const refreshTimeout = 2 * time.Second

// refreshAttemptCooldown bounds helper spawn frequency while the cache stays
// stale. Each helper may spend unauthenticated GitHub rate-limit budget, so
// repeated launches must share one recent attempt instead of each spawning.
const refreshAttemptCooldown = 5 * time.Minute

// forkWaitTimeout bounds the wait for the short-lived fork intermediate, which
// performs no network I/O. The intermediate re-executes this same hot binary
// and exits after one spawn. The sub-second fail-safe bounds launch-side
// startup time while the refresh itself runs independently.
const forkWaitTimeout = 500 * time.Millisecond

// Keep these consistent with versiondispatch's environment contract. That
// package imports update, so importing it here would create a cycle.
const envCacheDir = "AL_CACHE_DIR"
const envNoNetwork = "AL_NO_NETWORK"

var errNoCachedRelease = errors.New("no cached release information")

type releaseCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

func releaseCacheDir() (string, error) {
	base := strings.TrimSpace(os.Getenv(envCacheDir))
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(base, "agent-layer")
	}
	return base, nil
}

func releaseCachePath() (string, error) {
	base, err := releaseCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "latest-release.json"), nil
}

// refreshAttemptPath locates the best-effort marker recording the last helper
// spawn. It lives beside the release cache so cache-isolated tests stay hermetic.
func refreshAttemptPath() (string, error) {
	base, err := releaseCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "latest-release.refresh-attempt"), nil
}

// tryClaimRefreshAttempt atomically records a refresh attempt before spawning
// and reports whether the caller may spawn the helper. The persistent marker
// is never unlinked: the claim is read and renewed under a nonblocking
// advisory lock, so two launches racing on an expired marker cannot both win.
// A lost race, an unwritable cache, and any lock fault all fail closed without
// spawning, since a helper that cannot record its attempt could not refresh a
// usable cache either. Marker writes are never synced, keeping launch overhead
// to one lock acquisition and one small unsynced write.
func tryClaimRefreshAttempt() bool {
	path, err := refreshAttemptPath()
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	// #nosec G304 -- resolved user cache path.
	marker, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer func() { _ = marker.Close() }()
	fd := int(marker.Fd()) //nolint:gosec // Unix file descriptor on supported platforms.
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return false
	}
	defer func() { _ = unix.Flock(fd, unix.LOCK_UN) }() //nolint:gosec // Unix file descriptor on supported platforms.
	data, err := io.ReadAll(marker)
	if err != nil {
		return false
	}
	if last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil {
		if time.Since(last) < refreshAttemptCooldown {
			return false
		}
	}
	if err := marker.Truncate(0); err != nil {
		return false
	}
	if _, err := marker.Seek(0, io.SeekStart); err != nil {
		return false
	}
	if _, err := marker.Write([]byte(time.Now().UTC().Format(time.RFC3339))); err != nil {
		return false
	}
	return true
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

var (
	// refreshExecutable resolves the helper binary. It is a variable so tests
	// can redirect spawns without touching the real executable path.
	refreshExecutable = os.Executable
	// forkRefreshHelper spawns the short-lived fork intermediate and waits for
	// it. It is a variable so concurrent-spawn tests can count attempts without
	// starting real helpers.
	forkRefreshHelper = runRefreshFork
)

// runRefreshFork runs the fork intermediate to completion. The intermediate
// performs no network I/O; the timeout is a fail-safe so a stuck intermediate
// can never block a client launch.
func runRefreshFork(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), forkWaitTimeout)
	defer cancel()
	// #nosec G204 -- current executable with a fixed internal command.
	cmd := exec.CommandContext(ctx, path, RefreshForkCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Run()
}

// StartCacheRefresh starts a silent, isolated helper without waiting for GitHub.
// A goroutine alone cannot finish after al replaces itself with the client, and
// a direct child would stay a zombie once exec destroys the waiting goroutine.
// The helper is therefore double-forked: this process waits only for a
// short-lived intermediate that detaches the real helper before the exec
// handoff, and concurrent launches share one attempt claim from before the
// spawn. The wait covers the intermediate only, never the helper's network I/O.
func StartCacheRefresh() {
	if cacheIsFresh() {
		return
	}
	if !tryClaimRefreshAttempt() {
		return
	}
	path, err := refreshExecutable()
	if err != nil {
		return
	}
	_ = forkRefreshHelper(path)
}

// SpawnDetachedRefresh starts the real refresh helper and returns immediately
// without waiting for it. It runs in the short-lived fork intermediate: the
// helper reparents to init when the intermediate exits, so no Wait goroutine
// must survive the client's exec handoff.
func SpawnDetachedRefresh() {
	path, err := refreshExecutable()
	if err != nil {
		return
	}
	// #nosec G204 -- current executable with a fixed internal command.
	cmd := exec.CommandContext(context.Background(), path, RefreshCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
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
