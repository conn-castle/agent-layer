package dispatch

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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

var (
	platformStringsFunc = platformStrings
	osChmod             = os.Chmod
	osRename            = os.Rename
	osStat              = os.Stat
	osCreateTemp        = os.CreateTemp
	httpClient          = &http.Client{Timeout: 30 * time.Second}
	dispatchSleep       = time.Sleep
)

const (
	defaultMaxDownloadBytes = int64(100 * 1024 * 1024) // 100 MiB
	downloadRetryCount      = 1
	downloadRetryBackoff    = 250 * time.Millisecond
)

// ensureCachedBinary returns the cached binary path, downloading and verifying it if missing.
// Progress lines are written to progressOut when a download is required.
func ensureCachedBinary(cacheRoot string, version string, progressOut io.Writer) (string, error) {
	return ensureCachedBinaryWithSystem(RealSystem{}, cacheRoot, version, progressOut)
}

func ensureCachedBinaryWithSystem(sys System, cacheRoot string, version string, progressOut io.Writer) (string, error) {
	if sys == nil {
		return "", fmt.Errorf(messages.DispatchSystemRequired)
	}
	osName, arch, err := platformStringsFunc()
	if err != nil {
		return "", err
	}
	asset := assetName(osName, arch)
	binPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)
	if _, err := osStat(binPath); err == nil {
		return binPath, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf(messages.DispatchCheckCachedBinaryFmt, binPath, err)
	}

	if noNetworkWithSystem(sys) {
		return "", fmt.Errorf(messages.DispatchVersionNotCachedFmt, version, binPath, EnvNoNetwork)
	}

	lockPath := binPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		return "", fmt.Errorf(messages.DispatchCreateCacheDirFmt, err)
	}

	if err := withFileLock(lockPath, func() error {
		if _, err := osStat(binPath); err == nil {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.DispatchCheckCachedBinaryFmt, binPath, err)
		}

		tmp, err := osCreateTemp(filepath.Dir(binPath), asset+".tmp-*")
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
		if err := downloadToFileWithSystem(sys, url, tmp); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf(messages.DispatchSyncTempFileFmt, err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf(messages.DispatchCloseTempFileFmt, err)
		}

		expected, err := fetchChecksum(version, asset)
		if err != nil {
			return err
		}
		if err := verifyChecksum(tmpName, expected); err != nil {
			return err
		}
		if err := osChmod(tmpName, 0o755); err != nil {
			return fmt.Errorf(messages.DispatchChmodCachedBinaryFmt, err)
		}

		if err := osRename(tmpName, binPath); err != nil {
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
	case "darwin", "linux":
	default:
		return "", "", fmt.Errorf(messages.DispatchUnsupportedOSFmt, osName)
	}

	switch arch {
	case "amd64", "arm64":
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
func downloadToFile(url string, dest *os.File) error {
	return downloadToFileWithSystem(RealSystem{}, url, dest)
}

func downloadToFileWithSystem(sys System, url string, dest *os.File) error {
	if sys == nil {
		return fmt.Errorf(messages.DispatchSystemRequired)
	}
	maxBytes := maxDownloadBytesWithSystem(sys)
	for attempt := 0; attempt <= downloadRetryCount; attempt++ {
		resp, err := httpClient.Get(url)
		if err != nil {
			if shouldRetryDownload(attempt, err, 0) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			if isTimeoutError(err) {
				return fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
			}
			return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
		}

		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
		}
		if resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			statusText := resp.Status
			_ = resp.Body.Close()
			if shouldRetryDownload(attempt, nil, status) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			return fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, statusText)
		}

		if err := dest.Truncate(0); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf(messages.DispatchTruncateTempFileFmt, err)
		}
		if _, err := dest.Seek(0, io.SeekStart); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf(messages.DispatchResetTempFileOffsetFmt, err)
		}

		n, copyErr := io.Copy(dest, io.LimitReader(resp.Body, maxBytes+1))
		_ = resp.Body.Close()
		if copyErr != nil {
			if shouldRetryDownload(attempt, copyErr, 0) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, copyErr)
		}
		if n > maxBytes {
			return fmt.Errorf(messages.DispatchDownloadTooLargeFmt, url, n, maxBytes)
		}
		return nil
	}
	return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, errors.New("retry budget exhausted"))
}

// fetchChecksum retrieves the expected checksum for the asset from checksums.txt.
func fetchChecksum(version string, asset string) (string, error) {
	url := fmt.Sprintf("%s/download/v%s/checksums.txt", releaseBaseURL, version)
	for attempt := 0; attempt <= downloadRetryCount; attempt++ {
		resp, err := httpClient.Get(url)
		if err != nil {
			if shouldRetryDownload(attempt, err, 0) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			if isTimeoutError(err) {
				return "", fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
			}
			return "", fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return "", fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
		}
		if resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			statusText := resp.Status
			_ = resp.Body.Close()
			if shouldRetryDownload(attempt, nil, status) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			return "", fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, statusText)
		}

		scanner := bufio.NewScanner(resp.Body)
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
				_ = resp.Body.Close()
				return fields[0], nil
			}
		}
		if err := scanner.Err(); err != nil {
			_ = resp.Body.Close()
			if shouldRetryDownload(attempt, err, 0) {
				dispatchSleep(downloadRetryBackoff)
				continue
			}
			return "", fmt.Errorf(messages.DispatchReadFailedFmt, url, err)
		}
		_ = resp.Body.Close()
		return "", fmt.Errorf(messages.DispatchChecksumNotFoundFmt, asset, url)
	}
	return "", fmt.Errorf(messages.DispatchDownloadFailedFmt, url, errors.New("retry budget exhausted"))
}

// isTimeoutError reports whether err is a network timeout.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func shouldRetryDownload(attempt int, err error, statusCode int) bool {
	if attempt >= downloadRetryCount {
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

// verifyChecksum computes the SHA-256 of path and compares it to expected.
func verifyChecksum(path string, expected string) error {
	file, err := os.Open(path)
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
