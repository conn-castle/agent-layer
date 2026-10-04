package versiondispatch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/update"
)

var releaseBaseURL = update.ReleasesBaseURL

const (
	defaultMaxDownloadBytes  = int64(100 * 1024 * 1024) // 100 MiB
	maxChecksumResponseBytes = int64(1 << 20)           // 1 MiB — checksums.txt is a few KB at most
	defaultDownloadTimeout   = 30 * time.Second
	defaultDownloadCeiling   = 10 * time.Minute
	downloadRetryCount       = 1
	downloadRetryBackoff     = 250 * time.Millisecond
	cacheLockWorkHeadroom    = 5 * time.Second
)

// ensureCachedBinary returns the cached binary path, downloading and verifying it if missing.
// Progress lines are written to progressOut when a download is required.
func ensureCachedBinary(ctx context.Context, cacheRoot string, version string, progressOut io.Writer) (string, error) {
	return ensureCachedBinaryWithSystem(ctx, RealSystem{}, cacheRoot, version, progressOut)
}

func ensureCachedBinaryWithSystem(ctx context.Context, sys System, cacheRoot string, version string, progressOut io.Writer) (string, error) {
	if sys == nil {
		return "", fmt.Errorf(messages.DispatchSystemRequired)
	}
	osName, arch, err := sys.PlatformStrings()
	if err != nil {
		return "", err
	}
	asset := assetName(osName, arch)
	binPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)
	if _, err := sys.Stat(binPath); err == nil {
		return binPath, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf(messages.DispatchCheckCachedBinaryFmt, binPath, err)
	}

	if noNetworkWithSystem(sys) {
		return "", fmt.Errorf(messages.DispatchVersionNotCachedFmt, version, binPath, EnvNoNetwork)
	}

	lockPath := binPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil { // #nosec G301 -- user-level cache dir (~/.cache/agent-layer/...) must be traversable by the user's shell to invoke the cached 0o755 binary; the lock file is created separately.
		return "", fmt.Errorf(messages.DispatchCreateCacheDirFmt, err)
	}

	if err := withFileLock(ctx, sys, lockPath, cacheLockWaitTimeoutWithSystem(sys), func() error {
		if _, err := sys.Stat(binPath); err == nil {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.DispatchCheckCachedBinaryFmt, binPath, err)
		}

		tmp, err := sys.CreateTemp(filepath.Dir(binPath), asset+".tmp-*")
		if err != nil {
			return fmt.Errorf(messages.DispatchCreateTempFileFmt, err)
		}
		tmpName := tmp.Name()
		committed := false
		defer func() {
			if !committed {
				_ = os.Remove(tmpName)
			}
		}()

		_, _ = fmt.Fprintf(progressOut, messages.DispatchDownloadingFmt, version)
		url := fmt.Sprintf("%s/download/v%s/%s", releaseBaseURL, version, asset)
		if err := downloadToFileWithSystem(ctx, sys, url, tmp); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := sys.FileSync(tmp); err != nil {
			_ = tmp.Close()
			return fmt.Errorf(messages.DispatchSyncTempFileFmt, err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf(messages.DispatchCloseTempFileFmt, err)
		}

		expected, err := fetchChecksumWithSystem(ctx, sys, version, asset)
		if err != nil {
			return err
		}
		if err := verifyChecksum(tmpName, expected); err != nil {
			return err
		}
		if err := sys.Chmod(tmpName, 0o755); err != nil {
			return fmt.Errorf(messages.DispatchChmodCachedBinaryFmt, err)
		}

		if err := sys.Rename(tmpName, binPath); err != nil {
			return fmt.Errorf(messages.DispatchMoveCachedBinaryFmt, err)
		}
		committed = true
		_, _ = fmt.Fprintf(progressOut, messages.DispatchDownloadedFmt, version)
		return nil
	}); err != nil {
		return "", err
	}

	return binPath, nil
}

// platformStrings returns the supported OS and architecture strings for release assets.
func platformStrings() (string, string, error) {
	return checkPlatform(runtime.GOOS, runtime.GOARCH)
}

func checkPlatform(osName, arch string) (string, string, error) {
	switch osName {
	case osDarwin, osLinux:
	default:
		return "", "", fmt.Errorf(messages.DispatchUnsupportedOSFmt, osName)
	}

	switch arch {
	case archAMD64, archARM64:
	default:
		return "", "", fmt.Errorf(messages.DispatchUnsupportedArchFmt, arch)
	}

	return osName, arch, nil
}

// assetName returns the release asset filename for the OS/arch pair.
func assetName(osName string, arch string) string {
	return fmt.Sprintf("al-%s-%s", osName, arch)
}

// noNetworkWithSystem reports whether downloads are disabled via AL_NO_NETWORK.
func noNetworkWithSystem(sys System) bool {
	return strings.TrimSpace(sys.Getenv(EnvNoNetwork)) != ""
}

// downloadToFile fetches url and writes it to dest.
func downloadToFile(ctx context.Context, url string, dest *os.File) error {
	return downloadToFileWithSystem(ctx, RealSystem{}, url, dest)
}

func downloadToFileWithSystem(ctx context.Context, sys System, url string, dest *os.File) error {
	if sys == nil {
		return fmt.Errorf(messages.DispatchSystemRequired)
	}
	return downloadToFileWithLimits(ctx, sys, url, dest, downloadLimitsWithSystem(sys))
}

func downloadToFileWithLimits(ctx context.Context, sys System, url string, dest *os.File, limits downloadLimits) error {
	opCtx, cancel := context.WithTimeout(ctx, limits.ceiling)
	defer cancel()
	client := downloadHTTPClientWithSystem(sys)
	maxBytes := maxDownloadBytesWithSystem(sys)
	for attempt := 0; attempt <= downloadRetryCount; attempt++ {
		if ctx.Err() != nil || opCtx.Err() != nil {
			_, err := classifyAttemptFailure(ctx, opCtx, opCtx, attempt, opCtx.Err(), url, messages.DispatchDownloadFailedFmt)
			return err
		}
		retry, err := func() (bool, error) {
			attemptCtx, watchdog, stop := startDownloadAttempt(opCtx, limits.stall)
			defer stop()
			req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, url, nil)
			if err != nil {
				return false, fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
			}
			resp, err := client.Do(req) // #nosec G704 -- callers construct URLs from the fixed release base URL and validated release asset names.
			if err != nil {
				return classifyAttemptFailure(ctx, opCtx, attemptCtx, attempt, err, url, messages.DispatchDownloadFailedFmt)
			}
			watchdog.reset()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusNotFound {
				return false, fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
			}
			if resp.StatusCode != http.StatusOK {
				if shouldRetryDownload(opCtx, attempt, nil, resp.StatusCode) {
					return true, nil
				}
				return false, fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, resp.Status)
			}

			if err := dest.Truncate(0); err != nil {
				return false, fmt.Errorf(messages.DispatchTruncateTempFileFmt, err)
			}
			if _, err := dest.Seek(0, io.SeekStart); err != nil {
				return false, fmt.Errorf(messages.DispatchResetTempFileOffsetFmt, err)
			}

			n, copyErr := io.Copy(dest, io.LimitReader(stallReader{r: resp.Body, w: watchdog}, maxBytes+1))
			if copyErr != nil {
				return classifyAttemptFailure(ctx, opCtx, attemptCtx, attempt, copyErr, url, messages.DispatchDownloadFailedFmt)
			}
			if n > maxBytes {
				return false, fmt.Errorf(messages.DispatchDownloadTooLargeFmt, url, n, maxBytes)
			}
			return false, nil
		}()
		if !retry {
			return err
		}
		sys.Sleep(downloadRetryBackoff)
	}
	return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, errors.New("retry budget exhausted"))
}

// fetchChecksum retrieves the expected checksum for the asset from checksums.txt.
func fetchChecksum(ctx context.Context, version string, asset string) (string, error) {
	return fetchChecksumWithSystem(ctx, RealSystem{}, version, asset)
}

// fetchChecksumWithSystem retrieves the expected checksum using the provided system for timeout/env resolution.
func fetchChecksumWithSystem(ctx context.Context, sys System, version string, asset string) (string, error) {
	if sys == nil {
		return "", fmt.Errorf(messages.DispatchSystemRequired)
	}
	return fetchChecksumWithLimits(ctx, sys, version, asset, downloadLimitsWithSystem(sys))
}

func fetchChecksumWithLimits(ctx context.Context, sys System, version string, asset string, limits downloadLimits) (string, error) {
	opCtx, cancel := context.WithTimeout(ctx, limits.ceiling)
	defer cancel()
	url := fmt.Sprintf("%s/download/v%s/checksums.txt", releaseBaseURL, version)
	client := downloadHTTPClientWithSystem(sys)
	for attempt := 0; attempt <= downloadRetryCount; attempt++ {
		if ctx.Err() != nil || opCtx.Err() != nil {
			_, err := classifyAttemptFailure(ctx, opCtx, opCtx, attempt, opCtx.Err(), url, messages.DispatchDownloadFailedFmt)
			return "", err
		}
		checksum, retry, err := func() (string, bool, error) {
			attemptCtx, watchdog, stop := startDownloadAttempt(opCtx, limits.stall)
			defer stop()
			req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, url, nil)
			if err != nil {
				return "", false, fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
			}
			resp, err := client.Do(req) // #nosec G704 -- URL uses the fixed release base URL and a validated semantic version.
			if err != nil {
				retry, err := classifyAttemptFailure(ctx, opCtx, attemptCtx, attempt, err, url, messages.DispatchDownloadFailedFmt)
				return "", retry, err
			}
			watchdog.reset()
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusNotFound {
				return "", false, fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
			}
			if resp.StatusCode != http.StatusOK {
				if shouldRetryDownload(opCtx, attempt, nil, resp.StatusCode) {
					return "", true, nil
				}
				return "", false, fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, resp.Status)
			}

			scanner := bufio.NewScanner(io.LimitReader(stallReader{r: resp.Body, w: watchdog}, maxChecksumResponseBytes))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				path := strings.TrimPrefix(fields[1], "./")
				path = strings.TrimPrefix(path, "*")
				if path == asset {
					return fields[0], false, nil
				}
			}
			if err := scanner.Err(); err != nil {
				retry, err := classifyAttemptFailure(ctx, opCtx, attemptCtx, attempt, err, url, messages.DispatchReadFailedFmt)
				return "", retry, err
			}
			return "", false, fmt.Errorf(messages.DispatchChecksumNotFoundFmt, asset, url)
		}()
		if !retry {
			return checksum, err
		}
		sys.Sleep(downloadRetryBackoff)
	}
	return "", fmt.Errorf(messages.DispatchDownloadFailedFmt, url, errors.New("retry budget exhausted"))
}

var errDownloadStalled = errors.New("download stalled")

type downloadLimits struct {
	stall, ceiling time.Duration
}

func downloadLimitsWithSystem(sys System) downloadLimits {
	stall := downloadTimeoutWithSystem(sys)
	attemptBudget := saturatingDurationMul(stall, downloadRetryCount+1)
	backoffBudget := saturatingDurationMul(downloadRetryBackoff, downloadRetryCount)
	ceiling := max(defaultDownloadCeiling, saturatingDurationAdd(attemptBudget, backoffBudget))
	return downloadLimits{stall: stall, ceiling: ceiling}
}

// saturatingDurationMul multiplies a nonnegative duration by a nonnegative factor,
// capping the result at the largest representable time.Duration.
func saturatingDurationMul(d time.Duration, factor int) time.Duration {
	if factor > 0 && d > time.Duration(math.MaxInt64)/time.Duration(factor) {
		return time.Duration(math.MaxInt64)
	}
	return d * time.Duration(factor)
}

// saturatingDurationAdd adds nonnegative durations, capping the result at the
// largest representable time.Duration.
func saturatingDurationAdd(a, b time.Duration) time.Duration {
	if a > time.Duration(math.MaxInt64)-b {
		return time.Duration(math.MaxInt64)
	}
	return a + b
}

type downloadWatchdog struct {
	timer *time.Timer
	stall time.Duration
}

func startDownloadAttempt(ctx context.Context, stall time.Duration) (context.Context, *downloadWatchdog, context.CancelFunc) {
	attemptCtx, cancel := context.WithCancelCause(ctx)
	watchdog := &downloadWatchdog{
		timer: time.AfterFunc(stall, func() { cancel(errDownloadStalled) }),
		stall: stall,
	}
	return attemptCtx, watchdog, func() {
		watchdog.timer.Stop()
		cancel(nil)
	}
}

func (w *downloadWatchdog) reset() {
	w.timer.Reset(w.stall)
}

type stallReader struct {
	r io.Reader
	w *downloadWatchdog
}

func (r stallReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.w.reset()
	}
	return n, err
}

func classifyAttemptFailure(ctx, opCtx, attemptCtx context.Context, attempt int, err error, url, genericFmt string) (bool, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if ctxErr == context.DeadlineExceeded {
			return false, fmt.Errorf("%s: %w", fmt.Sprintf(messages.DispatchDownloadTimeoutFmt, url), ctxErr)
		}
		if !errors.Is(err, ctxErr) {
			err = ctxErr
		}
		return false, fmt.Errorf(genericFmt, url, err)
	}
	if opCtx.Err() != nil {
		return false, fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
	}
	if errors.Is(context.Cause(attemptCtx), errDownloadStalled) || isTimeoutError(err) {
		if attempt < downloadRetryCount {
			return true, nil
		}
		return false, fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
	}
	if shouldRetryDownload(opCtx, attempt, err, 0) {
		return true, nil
	}
	return false, fmt.Errorf(genericFmt, url, err)
}

// isTimeoutError reports whether err is a network timeout.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func shouldRetryDownload(ctx context.Context, attempt int, err error, statusCode int) bool {
	// A canceled request surfaces as a *url.Error, which is a net.Error, so
	// check the context itself rather than the error type.
	if attempt >= downloadRetryCount || ctx.Err() != nil {
		return false
	}
	if err != nil {
		var netErr net.Error
		return errors.As(err, &netErr)
	}
	return statusCode >= 500 && statusCode <= 599
}

func maxDownloadBytesWithSystem(sys System) int64 {
	raw := strings.TrimSpace(sys.Getenv("AL_MAX_DOWNLOAD_BYTES"))
	if raw == "" {
		return defaultMaxDownloadBytes
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return defaultMaxDownloadBytes
	}
	return v
}

func downloadTimeoutWithSystem(sys System) time.Duration {
	raw := strings.TrimSpace(sys.Getenv("AL_DOWNLOAD_TIMEOUT"))
	if raw == "" {
		return defaultDownloadTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return defaultDownloadTimeout
	}
	return timeout
}

// cacheLockWaitTimeoutWithSystem covers the holder's binary and checksum
// operation ceilings, plus local-work and polling headroom for the waiter.
func cacheLockWaitTimeoutWithSystem(sys System) time.Duration {
	wait := saturatingDurationMul(downloadLimitsWithSystem(sys).ceiling, 2)
	return saturatingDurationAdd(wait, cacheLockWorkHeadroom)
}

func downloadHTTPClientWithSystem(sys System) *http.Client {
	if sys == nil {
		return defaultHTTPClient
	}
	client := sys.HTTPClient()
	if client == nil {
		client = defaultHTTPClient
	}
	if client.Timeout == 0 {
		return client
	}
	clientCopy := *client
	clientCopy.Timeout = 0
	return &clientCopy
}

// verifyChecksum computes the SHA-256 of path and compares it to expected.
func verifyChecksum(path string, expected string) error {
	file, err := os.Open(path) //nolint:gosec // path is an internally-resolved cache file
	if err != nil {
		return fmt.Errorf(messages.DispatchOpenFileFmt, path, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf(messages.DispatchHashFileFmt, path, err)
	}
	actual := fmt.Sprintf("%x", hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf(messages.DispatchChecksumMismatchFmt, path, expected, actual)
	}
	return nil
}
