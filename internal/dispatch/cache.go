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
)

// ensureCachedBinary returns the cached binary path, downloading and verifying it if missing.
// Progress lines are written to progressOut when a download is required.
func ensureCachedBinary(cacheRoot string, version string, progressOut io.Writer) (string, error) {
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

	if noNetwork() {
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
		if err := downloadToFile(url, tmp); err != nil {
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

// noNetwork reports whether downloads are disabled via AL_NO_NETWORK.
func noNetwork() bool {
	return strings.TrimSpace(os.Getenv(EnvNoNetwork)) != ""
}

// downloadToFile fetches url and writes it to dest.
func downloadToFile(url string, dest *os.File) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		if isTimeoutError(err) {
			return fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
		}
		return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, resp.Status)
	}
	if _, err := io.Copy(dest, resp.Body); err != nil {
		return fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
	}
	return nil
}

// fetchChecksum retrieves the expected checksum for the asset from checksums.txt.
func fetchChecksum(version string, asset string) (string, error) {
	url := fmt.Sprintf("%s/download/v%s/checksums.txt", releaseBaseURL, version)
	resp, err := httpClient.Get(url)
	if err != nil {
		if isTimeoutError(err) {
			return "", fmt.Errorf(messages.DispatchDownloadTimeoutFmt, url)
		}
		return "", fmt.Errorf(messages.DispatchDownloadFailedFmt, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf(messages.DispatchDownload404Fmt, url, releaseBaseURL)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(messages.DispatchDownloadUnexpectedStatusFmt, url, resp.Status)
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
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf(messages.DispatchReadFailedFmt, url, err)
	}
	return "", fmt.Errorf(messages.DispatchChecksumNotFoundFmt, asset, url)
}

// isTimeoutError reports whether err is a network timeout.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
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
