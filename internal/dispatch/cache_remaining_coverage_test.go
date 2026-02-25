package dispatch

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEnsureCachedBinaryWithSystem_MkdirAllErrorBranch(t *testing.T) {
	origStat := osStat
	osStat = func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { osStat = origStat })

	cacheRoot := t.TempDir()
	version := "1.0.0"
	osName, arch, err := platformStrings()
	if err != nil {
		t.Fatalf("platformStrings: %v", err)
	}
	blockedDir := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch)
	if err := os.MkdirAll(filepath.Dir(blockedDir), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(blockedDir, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	if _, err := ensureCachedBinary(cacheRoot, version, io.Discard); err == nil || !strings.Contains(err.Error(), "create cache dir") {
		t.Fatalf("expected mkdir error branch, got %v", err)
	}
}

func TestEnsureCachedBinaryWithSystem_SyncErrorBranch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sync behavior for os.DevNull differs on windows")
	}

	version := "1.0.0"
	content := "binary-content"
	osName, arch, err := platformStrings()
	if err != nil {
		t.Fatalf("platformStrings: %v", err)
	}
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/download/v%s/%s", version, asset):
			_, _ = w.Write([]byte(content))
		case fmt.Sprintf("/download/v%s/checksums.txt", version):
			// Not reached when sync fails, but kept for completeness.
			_, _ = fmt.Fprintf(w, "ignored %s\n", asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() { releaseBaseURL = origURL })

	origCreateTemp := osCreateTemp
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		dupFD, err := syscall.Dup(int(devNull.Fd()))
		_ = devNull.Close()
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(dupFD), filepath.Join(dir, "sync-error.tmp")), nil
	}
	t.Cleanup(func() { osCreateTemp = origCreateTemp })

	if _, err := ensureCachedBinary(t.TempDir(), version, io.Discard); err == nil || !strings.Contains(err.Error(), "sync temp file") {
		t.Fatalf("expected sync temp file error, got %v", err)
	}
}

func TestDownloadToFileWithSystem_TruncateErrorBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	tmpPath := filepath.Join(t.TempDir(), "readonly")
	if err := os.WriteFile(tmpPath, []byte("seed"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	f, err := os.Open(tmpPath) // read-only; truncate must fail
	if err != nil {
		t.Fatalf("open read-only file: %v", err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFileWithSystem(RealSystem{}, server.URL, f)
	if err == nil || !strings.Contains(err.Error(), "truncate temp file") {
		t.Fatalf("expected truncate error branch, got %v", err)
	}
}

func TestFetchChecksumWithSystem_ScannerRetryOnNetErrorBranch(t *testing.T) {
	origHTTP := httpClient
	origSleep := dispatchSleep
	httpClient = &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: io.NopCloser(&errorReaderAfterData{
					data: []byte("asset-without-checksum-fields"),
					err:  &net.OpError{Op: "read", Net: "tcp", Err: &timeoutErr{}},
				}),
			}, nil
		}),
		Timeout: 200 * time.Millisecond,
	}
	dispatchSleep = func(time.Duration) {}
	t.Cleanup(func() {
		httpClient = origHTTP
		dispatchSleep = origSleep
	})

	_, err := fetchChecksumWithSystem(&testSystem{}, "1.0.0", "asset")
	if err == nil {
		t.Fatal("expected scanner read error")
	}
	if !strings.Contains(err.Error(), "read") && !errors.Is(err, io.EOF) {
		t.Fatalf("expected read-related error, got %v", err)
	}
}
