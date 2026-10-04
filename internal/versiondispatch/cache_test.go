package versiondispatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/messages"
)

type failingRoundTripper struct {
	err error
}

func (f failingRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, f.err
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type partialTimeoutBody struct {
	wrote bool
}

func (b *partialTimeoutBody) Read(p []byte) (int, error) {
	if b.wrote {
		return 0, io.EOF
	}
	b.wrote = true
	n := copy(p, []byte("partial"))
	return n, &net.OpError{Op: "read", Net: "tcp", Err: &timeoutErr{}}
}

func (b *partialTimeoutBody) Close() error {
	return nil
}

func TestEnsureCachedBinary(t *testing.T) {
	// 1. Setup mock server
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)

	osName := runtime.GOOS
	arch := runtime.GOARCH
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/download/v%s/%s", version, asset):
			_, _ = w.Write([]byte(content))
		case fmt.Sprintf("/download/v%s/checksums.txt", version):
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
			_, _ = fmt.Fprintf(w, "otherhash otherfile\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Override URL
	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	// 2. Setup cache dir
	cacheRoot := t.TempDir()

	// 3. Run test - First time (download)
	path, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err != nil {
		t.Fatalf("ensureCachedBinary failed: %v", err)
	}

	// Verify file exists and content
	if _, err := os.Stat(path); err != nil {
		t.Errorf("binary not found at %s", path)
	}
	gotContent, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(gotContent) != content {
		t.Errorf("content mismatch: got %q, want %q", string(gotContent), content)
	}

	// 4. Run test - Second time (cached)
	// Stop server to ensure we don't hit network
	server.Close()

	path2, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err != nil {
		t.Fatalf("ensureCachedBinary cached failed: %v", err)
	}
	if path2 != path {
		t.Errorf("paths differ: %s vs %s", path2, path)
	}
}

func TestEnsureCachedBinary_ChecksumMismatch(t *testing.T) {
	version := "1.0.0"
	content := "binary-content"
	// Wrong checksum
	checksumStr := "badchecksum"

	osName := runtime.GOOS
	arch := runtime.GOARCH
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/download/v%s/%s", version, asset):
			_, _ = w.Write([]byte(content))
		case fmt.Sprintf("/download/v%s/checksums.txt", version):
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	cacheRoot := t.TempDir()

	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err == nil {
		t.Fatal("expected error due to checksum mismatch, got nil")
	}
}

func TestEnsureCachedBinary_Download404(t *testing.T) {
	version := "1.0.0"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	cacheRoot := t.TempDir()

	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err == nil {
		t.Fatal("expected error due to 404, got nil")
	}
}

func TestEnsureCachedBinary_NoNetwork(t *testing.T) {
	t.Setenv(EnvNoNetwork, "1")
	cacheRoot := t.TempDir()

	_, err := ensureCachedBinary(context.Background(), cacheRoot, "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error when network is disabled and binary missing")
	}
}

func TestEnsureCachedBinary_PlatformError(t *testing.T) {
	sys := &testSystem{
		PlatformStringsFunc: func() (string, string, error) {
			return "", "", fmt.Errorf("platform error")
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error from platformStrings")
	}
}

func TestEnsureCachedBinary_ChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not called on windows")
	}

	// Setup mock server
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			_, _ = w.Write([]byte(content))
		} else if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	sys := &testSystem{
		ChmodFunc: func(name string, mode os.FileMode) error {
			return fmt.Errorf("chmod failed")
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), version, io.Discard)
	if err == nil {
		t.Fatal("expected error from chmod")
	}
}

func TestEnsureCachedBinary_StatError(t *testing.T) {
	sys := &testSystem{
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, fmt.Errorf("stat failed")
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error from stat")
	}
}

func TestEnsureCachedBinary_RaceCondition(t *testing.T) {
	// Simulate:
	// 1. Stat -> NotExist (proceeds to lock)
	// 2. Lock acquired
	// 3. Stat -> Exists (returns success)

	calls := 0
	sys := &testSystem{
		StatFunc: func(name string) (os.FileInfo, error) {
			calls++
			if calls == 1 {
				return nil, os.ErrNotExist
			}
			// Second call (inside lock) returns success
			return nil, nil
		},
	}

	cacheRoot := t.TempDir()
	version := "1.0.0"

	path, err := ensureCachedBinaryWithSystem(context.Background(), sys, cacheRoot, version, io.Discard)
	if err != nil {
		t.Fatalf("ensureCachedBinary race condition failed: %v", err)
	}

	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	expectedPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)
	if path != expectedPath {
		t.Errorf("got %s, want %s", path, expectedPath)
	}
}

func TestEnsureCachedBinary_InternalStatError(t *testing.T) {
	// Simulate:
	// 1. Stat -> NotExist
	// 2. Lock
	// 3. Stat -> Error (not NotExist)

	calls := 0
	sys := &testSystem{
		StatFunc: func(name string) (os.FileInfo, error) {
			calls++
			if calls == 1 {
				return nil, os.ErrNotExist
			}
			return nil, fmt.Errorf("internal stat failed")
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error from internal stat")
	}
}

func TestEnsureCachedBinary_RenameError(t *testing.T) {
	// Setup mock server
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			_, _ = w.Write([]byte(content))
		} else if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	sys := &testSystem{
		RenameFunc: func(oldpath, newpath string) error {
			return fmt.Errorf("rename failed")
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), version, io.Discard)
	if err == nil {
		t.Fatal("expected error from rename")
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		os   string
		arch string
		want string
	}{
		{"linux", "amd64", "al-linux-amd64"},
		{"darwin", "arm64", "al-darwin-arm64"},
	}
	for _, tt := range tests {
		if got := assetName(tt.os, tt.arch); got != tt.want {
			t.Errorf("assetName(%q, %q) = %q, want %q", tt.os, tt.arch, got, tt.want)
		}
	}
}

func TestEnsureCachedBinary_MkdirError(t *testing.T) {
	cacheRoot := t.TempDir()
	version := "1.0.0"

	osName, arch, _ := platformStrings()
	dirToBlock := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch)

	// Create parent dirs
	if err := os.MkdirAll(filepath.Dir(dirToBlock), 0o700); err != nil {
		t.Fatal(err)
	}

	// Create a file at dirToBlock
	if err := os.WriteFile(dirToBlock, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err == nil {
		t.Fatal("expected error from MkdirAll")
	}
}

func TestEnsureCachedBinary_LockCreationError(t *testing.T) {
	cacheRoot := t.TempDir()
	version := "1.0.0"
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	binPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)
	lockPath := binPath + ".lock"

	// Create parent dirs
	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		t.Fatal(err)
	}

	// Create a directory at lockPath
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err == nil {
		t.Fatal("expected error when lock path is a directory")
	}
}

func TestEnsureCachedBinary_DownloadStatusError(t *testing.T) {
	version := "1.0.0"
	asset := assetName(runtime.GOOS, runtime.GOARCH)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	cacheRoot := t.TempDir()

	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err == nil {
		t.Fatal("expected error due to 500 status")
	}
}

func TestEnsureCachedBinary_NoNetwork_Exists(t *testing.T) {
	t.Setenv(EnvNoNetwork, "1")
	cacheRoot := t.TempDir()
	version := "1.0.0"

	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	binPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)

	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake-binary"), 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatal(err)
	}

	got, err := ensureCachedBinary(context.Background(), cacheRoot, version, io.Discard)
	if err != nil {
		t.Fatalf("expected success when binary exists even if no network, got %v", err)
	}
	if got != binPath {
		t.Errorf("got %s, want %s", got, binPath)
	}
}

func TestDownloadToFile_CopyError(t *testing.T) {
	// Simulate connection close during body read
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Easy way: Content-Length is larger than body.
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()

	tmp := filepath.Join(t.TempDir(), "partial")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), RealSystem{}, server.URL, f, downloadLimitsWithSystem(RealSystem{}))

	if err == nil {
		t.Fatal("expected error on short read")
	}
}

func TestFetchChecksum_ScannerError(t *testing.T) {
	version := "1.0.0"
	asset := "some-asset"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("   \n"))       // Empty line
		_, _ = w.Write([]byte("one-field\n")) // Not enough fields
	}))

	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), RealSystem{}, version, asset, downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected error when checksum not found")
	}
}

func TestFetchChecksum_StatusError(t *testing.T) {
	version := "1.0.0"
	asset := "some-asset"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), RealSystem{}, version, asset, downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected error on 500 status")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEnsureCachedBinaryWithSystem_ChecksumUsesProvidedSystemTimeout(t *testing.T) {
	version := "1.0.0"
	content := "binary-content"
	sum := sha256.Sum256([]byte(content))
	checksum := fmt.Sprintf("%x", sum)
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/download/v%s/%s", version, asset):
			_, _ = w.Write([]byte(content))
		case fmt.Sprintf("/download/v%s/checksums.txt", version):
			time.Sleep(20 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "%s %s\n", checksum, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	t.Setenv("AL_DOWNLOAD_TIMEOUT", "1ns")
	sys := &testSystem{
		GetenvFunc: func(key string) string {
			if key == "AL_DOWNLOAD_TIMEOUT" {
				return "300ms"
			}
			return ""
		},
	}

	cacheRoot := t.TempDir()
	if _, err := ensureCachedBinaryWithSystem(context.Background(), sys, cacheRoot, version, io.Discard); err != nil {
		t.Fatalf("ensureCachedBinaryWithSystem failed: %v", err)
	}
}

func TestVerifyChecksum_FileOpenError(t *testing.T) {
	err := verifyChecksum("non-existent-file", "hash")
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestCheckPlatform(t *testing.T) {
	tests := []struct {
		os      string
		arch    string
		wantErr bool
	}{
		{"darwin", "amd64", false},
		{"linux", "arm64", false},
		{"unknown", "amd64", true},
		{"darwin", "unknown", true},
	}
	for _, tt := range tests {
		_, _, err := checkPlatform(tt.os, tt.arch)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkPlatform(%q, %q) error = %v, wantErr %v", tt.os, tt.arch, err, tt.wantErr)
		}
	}
}

func TestFetchChecksum_NotFound(t *testing.T) {
	version := "1.0.0"
	asset := "some-asset"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			_, _ = fmt.Fprintln(w, "hash1 other-asset")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), RealSystem{}, version, asset, downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected error when checksum not found in file")
	}
}

func TestDownloadToFile_ClientGetError(t *testing.T) {
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: failingRoundTripper{err: fmt.Errorf("client get failed")},
			}
		},
	}

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), sys, "https://example.invalid/file", f, downloadLimitsWithSystem(sys))
	if err == nil {
		t.Fatal("expected error from client.Get")
	}
}

func TestFetchChecksum_ClientGetError(t *testing.T) {
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: failingRoundTripper{err: fmt.Errorf("client get failed")},
			}
		},
	}

	oldURL := releaseBaseURL
	releaseBaseURL = "http://invalid.test.invalid:99999"
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), sys, "1.0.0", "some-asset", downloadLimitsWithSystem(sys))
	if err == nil {
		t.Fatal("expected error from client.Get")
	}
}

func TestFetchChecksum_PathPrefixes(t *testing.T) {
	// Test that paths with ./ and * prefixes are handled
	version := "1.0.0"
	asset := "test-asset"
	checksum := "abcd1234"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Test with ./ prefix
		_, _ = fmt.Fprintf(w, "%s ./%s\n", checksum, asset)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	got, err := fetchChecksum(context.Background(), RealSystem{}, version, asset, downloadLimitsWithSystem(RealSystem{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != checksum {
		t.Errorf("got %s, want %s", got, checksum)
	}
}

func TestFetchChecksum_StarPrefix(t *testing.T) {
	// Test that paths with * prefix (binary mode) are handled
	version := "1.0.0"
	asset := "test-asset"
	checksum := "efgh5678"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Test with * prefix (binary mode indicator)
		_, _ = fmt.Fprintf(w, "%s *%s\n", checksum, asset)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	got, err := fetchChecksum(context.Background(), RealSystem{}, version, asset, downloadLimitsWithSystem(RealSystem{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != checksum {
		t.Errorf("got %s, want %s", got, checksum)
	}
}

func TestEnsureCachedBinary_CreateTempError(t *testing.T) {
	sys := &testSystem{
		CreateTempFunc: func(dir, pattern string) (*os.File, error) {
			return nil, fmt.Errorf("create temp failed")
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error from CreateTemp")
	}
	if !strings.Contains(err.Error(), "temp") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_HashMismatch(t *testing.T) {
	// Create a file with known content
	tmp := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(tmp, []byte("test content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Verify with wrong checksum
	err := verifyChecksum(tmp, "wrongchecksum")
	if err == nil {
		t.Fatal("expected error for checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsureCachedBinary_SyncSuccess(t *testing.T) {
	// Setup mock server
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			_, _ = w.Write([]byte(content))
		} else if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	// Mock CreateTemp to ensure the override path is exercised.
	sys := &testSystem{
		CreateTempFunc: func(dir, pattern string) (*os.File, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return f, nil
		},
	}

	// This test verifies the happy path completes (Sync doesn't fail in normal case).
	cacheRoot := t.TempDir()
	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, cacheRoot, version, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureCachedBinary_DownloadError(t *testing.T) {
	// Ensure stat returns NotExist so we proceed to download
	sys := &testSystem{
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}

	// Use a server that closes connection immediately to cause download error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		// Close immediately to cause connection error
		_ = conn.Close()
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := ensureCachedBinaryWithSystem(context.Background(), sys, t.TempDir(), "1.0.0", io.Discard)
	if err == nil {
		t.Fatal("expected error from download")
	}
}

func TestEnsureCachedBinary_FetchChecksumError(t *testing.T) {
	// Setup server that returns binary but fails on checksum
	version := "1.0.0"
	content := "binary-content"
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			_, _ = w.Write([]byte(content))
		} else if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			// Return 500 for checksum file
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := ensureCachedBinary(context.Background(), t.TempDir(), version, io.Discard)
	if err == nil {
		t.Fatal("expected error from fetchChecksum")
	}
}

func TestVerifyChecksum_ReadError(t *testing.T) {
	// On Unix, opening a directory succeeds but reading from it fails
	// This tests the io.Copy error path
	if runtime.GOOS == "windows" {
		t.Skip("directory read behavior differs on windows")
	}

	dir := t.TempDir()
	err := verifyChecksum(dir, "somehash")
	if err == nil {
		t.Fatal("expected error reading directory")
	}
}

func TestDownloadToFile_404_ActionableMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), RealSystem{}, server.URL+"/download/v99.0.0/al-darwin-arm64", f, downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected error for 404")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 404") {
		t.Errorf("expected HTTP 404 in error, got: %s", msg)
	}
	if !strings.Contains(msg, "Remediation") {
		t.Errorf("expected Remediation guidance in error, got: %s", msg)
	}
	if !strings.Contains(msg, "al upgrade") {
		t.Errorf("expected al upgrade guidance in error, got: %s", msg)
	}
	if !strings.Contains(msg, ".agent-layer/al.version") {
		t.Errorf("expected pin file guidance in error, got: %s", msg)
	}
}

// timeoutRoundTripper simulates a network timeout.
type timeoutRoundTripper struct{}

func (timeoutRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &timeoutErr{},
	}
}

// timeoutErr satisfies net.Error with Timeout() == true.
type timeoutErr struct{}

func (e *timeoutErr) Error() string   { return "i/o timeout" }
func (e *timeoutErr) Timeout() bool   { return true }
func (e *timeoutErr) Temporary() bool { return true }

func TestDownloadToFile_Timeout_ActionableMessage(t *testing.T) {
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: timeoutRoundTripper{},
				Timeout:   1 * time.Second,
			}
		},
	}

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), sys, "https://example.invalid/file", f, downloadLimitsWithSystem(sys))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "timed out") {
		t.Errorf("expected 'timed out' in error, got: %s", msg)
	}
	if !strings.Contains(msg, "Remediation") {
		t.Errorf("expected Remediation guidance in error, got: %s", msg)
	}
	if !strings.Contains(msg, "AL_NO_NETWORK") {
		t.Errorf("expected AL_NO_NETWORK in error, got: %s", msg)
	}
}

func TestFetchChecksum_404_ActionableMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), RealSystem{}, "99.0.0", "some-asset", downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected error for 404")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 404") {
		t.Errorf("expected HTTP 404 in error, got: %s", msg)
	}
	if !strings.Contains(msg, "Remediation") {
		t.Errorf("expected Remediation guidance in error, got: %s", msg)
	}
}

func TestEnsureCachedBinary_ProgressOutput(t *testing.T) {
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/download/v%s/%s", version, asset) {
			_, _ = w.Write([]byte(content))
		} else if r.URL.Path == fmt.Sprintf("/download/v%s/checksums.txt", version) {
			_, _ = fmt.Fprintf(w, "%s %s\n", checksumStr, asset)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := releaseBaseURL
	releaseBaseURL = server.URL
	defer func() { releaseBaseURL = oldURL }()

	var buf bytes.Buffer
	_, err := ensureCachedBinary(context.Background(), t.TempDir(), version, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Downloading al v1.0.0") {
		t.Errorf("expected 'Downloading al v1.0.0' in output, got: %q", output)
	}
	if !strings.Contains(output, "Downloaded al v1.0.0") {
		t.Errorf("expected 'Downloaded al v1.0.0' in output, got: %q", output)
	}
}

func TestEnsureCachedBinary_NoProgressOnCacheHit(t *testing.T) {
	version := "1.0.0"
	osName, arch, _ := platformStrings()
	asset := assetName(osName, arch)
	cacheRoot := t.TempDir()
	binPath := filepath.Join(cacheRoot, "versions", version, osName+"-"+arch, asset)

	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake-binary"), 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatal(err)
	}

	var buf bytes.Buffer
	_, err := ensureCachedBinary(context.Background(), cacheRoot, version, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no progress output on cache hit, got: %q", buf.String())
	}
}

func TestFetchChecksum_Timeout_ActionableMessage(t *testing.T) {
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: timeoutRoundTripper{},
				Timeout:   1 * time.Second,
			}
		},
	}

	oldURL := releaseBaseURL
	releaseBaseURL = "https://example.invalid"
	defer func() { releaseBaseURL = oldURL }()

	_, err := fetchChecksum(context.Background(), sys, "1.0.0", "some-asset", downloadLimitsWithSystem(sys))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "timed out") {
		t.Errorf("expected 'timed out' in error, got: %s", msg)
	}
	if !strings.Contains(msg, "Remediation") {
		t.Errorf("expected Remediation guidance in error, got: %s", msg)
	}
}

func TestRequestTimeout_ContextIdentity(t *testing.T) {
	for _, operation := range []string{"download", "checksum"} {
		for _, parentDeadline := range []bool{true, false} {
			name := "client_timeout"
			if parentDeadline {
				name = "parent_deadline"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				url := fmt.Sprintf("%s/download/v1.0.0/checksums.txt", releaseBaseURL)
				var dest *os.File
				if operation == "download" {
					url = "https://example.invalid/file"
					var err error
					dest, err = os.Create(filepath.Join(t.TempDir(), "file")) // #nosec G304 -- path is constructed from test-controlled inputs.
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = dest.Close() }()
				}

				requests := 0
				sys := &testSystem{
					HTTPClientFunc: func() *http.Client {
						return &http.Client{
							Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
								requests++
								if err := req.Context().Err(); err != nil {
									t.Fatalf("context expired before request started: %v", err)
								}
								<-req.Context().Done()
								return nil, req.Context().Err()
							}),
						}
					},
					GetenvFunc: func(key string) string {
						if key == "AL_DOWNLOAD_TIMEOUT" && !parentDeadline {
							return "10ms"
						}
						return ""
					},
					SleepFunc: func(time.Duration) {
						if parentDeadline {
							t.Fatal("parent deadline scheduled a retry")
						}
					},
				}
				ctx := context.Background()
				if parentDeadline {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
					defer cancel()
				}

				var err error
				if operation == "download" {
					err = downloadToFile(ctx, sys, url, dest, downloadLimitsWithSystem(sys))
				} else {
					_, err = fetchChecksum(ctx, sys, "1.0.0", "some-asset", downloadLimitsWithSystem(sys))
				}
				if err == nil {
					t.Fatal("expected timeout error")
				}
				wantMessage := fmt.Sprintf(messages.DispatchDownloadTimeoutFmt, url)
				wantRequests := downloadRetryCount + 1
				if parentDeadline {
					wantMessage += ": " + context.DeadlineExceeded.Error()
					wantRequests = 1
				} else if ctx.Err() != nil {
					t.Fatalf("client timeout expired parent context: %v", ctx.Err())
				}
				if errors.Is(err, context.DeadlineExceeded) != parentDeadline {
					t.Fatalf("deadline identity = %v, want %v: %v", errors.Is(err, context.DeadlineExceeded), parentDeadline, err)
				}
				if err.Error() != wantMessage {
					t.Fatalf("error = %q, want %q", err, wantMessage)
				}
				if requests != wantRequests {
					t.Fatalf("requests = %d, want %d", requests, wantRequests)
				}
			})
		}
	}
}

func TestDownloadToFile_TooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes
	}))
	defer server.Close()

	t.Setenv("AL_MAX_DOWNLOAD_BYTES", "5")
	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), RealSystem{}, server.URL, f, downloadLimitsWithSystem(RealSystem{}))
	if err == nil {
		t.Fatal("expected size-limit error")
	}
	if !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("expected size-limit message, got %v", err)
	}
}

func TestDownloadToFile_RetryOnTransientError(t *testing.T) {
	attempt := 0
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
					attempt++
					if attempt == 1 {
						return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &timeoutErr{}}
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Body:       io.NopCloser(strings.NewReader("ok")),
						Header:     make(http.Header),
					}, nil
				}),
			}
		},
	}

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := downloadToFile(context.Background(), sys, "https://example.invalid/file", f, downloadLimitsWithSystem(sys)); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempt)
	}
}

// cancelingTestSystem returns a system whose HTTP requests cancel ctx and then
// wait for their own context, failing the test if a retry is scheduled.
func cancelingTestSystem(t *testing.T, cancel context.CancelFunc, requests *int) *testSystem {
	t.Helper()
	return &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					*requests++
					cancel()
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(5 * time.Second):
						return nil, errors.New("request context not canceled")
					}
				}),
			}
		},
		SleepFunc: func(time.Duration) { t.Fatal("canceled request waited to retry") },
	}
}

func TestFetchChecksum_CanceledRequestIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	sys := cancelingTestSystem(t, cancel, &requests)

	if _, err := fetchChecksum(ctx, sys, "1.0.0", "al-linux-amd64", downloadLimitsWithSystem(sys)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestEnsureCachedBinary_CanceledDownloadIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	sys := cancelingTestSystem(t, cancel, &requests)

	cacheDir := t.TempDir()
	path, err := ensureCachedBinaryWithSystem(ctx, sys, cacheDir, "1.0.0", io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got path %q, err %v", path, err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	osName, arch, _ := platformStrings()
	entries, err := os.ReadDir(filepath.Join(cacheDir, "versions", "1.0.0", osName+"-"+arch))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatalf("unexpected cache entry after canceled download: %s", entry.Name())
		}
	}
}

func TestDownloadToFile_RetryOnCopyErrorResetsDestination(t *testing.T) {
	attempt := 0
	sys := &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{
				Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
					attempt++
					if attempt == 1 {
						return &http.Response{
							StatusCode: http.StatusOK,
							Status:     "200 OK",
							Body:       &partialTimeoutBody{},
							Header:     make(http.Header),
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Body:       io.NopCloser(strings.NewReader("ok")),
						Header:     make(http.Header),
					}, nil
				}),
			}
		},
	}

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := downloadToFile(context.Background(), sys, "https://example.invalid/file", f, downloadLimitsWithSystem(sys)); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempt)
	}
	data, err := os.ReadFile(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("expected retried content to replace partial bytes, got %q", string(data))
	}
}

func TestDownloadToFileWithSystem_TooLargeFromSystemEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes
	}))
	defer server.Close()

	sys := &testSystem{
		GetenvFunc: func(key string) string {
			if key == "AL_MAX_DOWNLOAD_BYTES" {
				return "5"
			}
			return ""
		},
	}

	tmp := filepath.Join(t.TempDir(), "file")
	f, err := os.Create(tmp) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(context.Background(), sys, server.URL, f, downloadLimitsWithSystem(sys))
	if err == nil {
		t.Fatal("expected size-limit error")
	}
	if !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("expected size-limit message, got %v", err)
	}
}

func TestDownloadTimeoutWithSystem(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "unset", raw: "", want: defaultDownloadTimeout},
		{name: "invalid", raw: "not-a-duration", want: defaultDownloadTimeout},
		{name: "zero", raw: "0s", want: defaultDownloadTimeout},
		{name: "negative", raw: "-1s", want: defaultDownloadTimeout},
		{name: "valid", raw: "45s", want: 45 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := &testSystem{
				GetenvFunc: func(key string) string {
					if key == "AL_DOWNLOAD_TIMEOUT" {
						return tt.raw
					}
					return ""
				},
			}
			if got := downloadTimeoutWithSystem(sys); got != tt.want {
				t.Fatalf("downloadTimeoutWithSystem() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheLockWaitTimeoutWithSystemCoversCompleteDownloadBudget(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "default", raw: "", want: 20*time.Minute + 5*time.Second},
		{name: "invalid fallback", raw: "invalid", want: 20*time.Minute + 5*time.Second},
		{name: "non-positive fallback", raw: "0s", want: 20*time.Minute + 5*time.Second},
		{name: "override", raw: "60s", want: 20*time.Minute + 5*time.Second},
		{name: "long stall", raw: "15m", want: time.Hour + 5500*time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := &testSystem{GetenvFunc: func(key string) string {
				if key == "AL_DOWNLOAD_TIMEOUT" {
					return tt.raw
				}
				return ""
			}}
			if got := cacheLockWaitTimeoutWithSystem(sys); got != tt.want {
				t.Fatalf("cacheLockWaitTimeoutWithSystem() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDownloadHTTPClientWithSystem_NoRequestTimeout(t *testing.T) {
	t.Parallel()
	for _, timeout := range []time.Duration{0, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			transport := &http.Transport{}
			baseClient := &http.Client{Timeout: timeout, Transport: transport}
			sys := &testSystem{
				HTTPClientFunc: func() *http.Client { return baseClient },
				GetenvFunc:     func(string) string { return "90s" },
			}
			client := downloadHTTPClientWithSystem(sys)
			if client.Timeout != 0 {
				t.Fatalf("client timeout = %v, want 0", client.Timeout)
			}
			if (client == baseClient) != (timeout == 0) {
				t.Fatal("expected a client copy only when the base timeout is nonzero")
			}
			if baseClient.Timeout != timeout || client.Transport != transport {
				t.Fatal("base client was changed or transport was not preserved")
			}
		})
	}
	if defaultHTTPClient.Timeout != 0 {
		t.Fatalf("default client timeout = %v, want 0", defaultHTTPClient.Timeout)
	}
	if downloadHTTPClientWithSystem(&testSystem{HTTPClientFunc: func() *http.Client { return nil }}) != defaultHTTPClient {
		t.Fatal("expected fallback to the shared default client")
	}
}

func TestDownloadLimitsWithSystem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want downloadLimits
	}{
		{raw: "", want: downloadLimits{stall: 30 * time.Second, ceiling: 10 * time.Minute}},
		{raw: "90s", want: downloadLimits{stall: 90 * time.Second, ceiling: 10 * time.Minute}},
		{raw: "15m", want: downloadLimits{stall: 15 * time.Minute, ceiling: 30*time.Minute + 250*time.Millisecond}},
		{raw: "invalid", want: downloadLimits{stall: 30 * time.Second, ceiling: 10 * time.Minute}},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			sys := &testSystem{GetenvFunc: func(string) string { return tt.raw }}
			if got := downloadLimitsWithSystem(sys); got != tt.want {
				t.Fatalf("limits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDownloadTimeoutBudgetsSaturate(t *testing.T) {
	t.Parallel()
	const maxDuration = time.Duration(math.MaxInt64)
	tests := []struct {
		name        string
		raw         string
		wantCeiling time.Duration
	}{
		{name: "ceiling multiplication", raw: "2000000h", wantCeiling: maxDuration},
		{name: "ceiling addition", raw: (maxDuration / 2).String(), wantCeiling: maxDuration},
		{name: "lock multiplication", raw: "1000000h", wantCeiling: 2000000*time.Hour + downloadRetryBackoff},
		// Twice this ceiling fits, but adding the lock headroom overflows.
		{name: "lock addition", raw: (maxDuration/4 - downloadRetryBackoff/2).String(), wantCeiling: maxDuration/2 - time.Nanosecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stall, err := time.ParseDuration(tt.raw)
			if err != nil {
				t.Fatalf("test timeout must parse: %v", err)
			}
			sys := &testSystem{GetenvFunc: func(key string) string {
				if key == "AL_DOWNLOAD_TIMEOUT" {
					return tt.raw
				}
				return ""
			}}
			want := downloadLimits{stall: stall, ceiling: tt.wantCeiling}
			if got := downloadLimitsWithSystem(sys); got != want {
				t.Errorf("limits = %v, want %v", got, want)
			}
			if got := cacheLockWaitTimeoutWithSystem(sys); got != maxDuration {
				t.Errorf("cache lock wait = %v, want %v", got, maxDuration)
			}
		})
	}
}

func downloadTestDestination(t *testing.T) *os.File {
	t.Helper()
	dest, err := os.Create(filepath.Join(t.TempDir(), "file")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })
	return dest
}

func downloadServerTestSystem(server *httptest.Server) *testSystem {
	return &testSystem{
		HTTPClientFunc: func() *http.Client {
			return &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				req = req.Clone(req.Context())
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return server.Client().Transport.RoundTrip(req)
			})}
		},
		GetenvFunc: func(string) string { return "" },
	}
}

func TestDownloadToFile_SlowSteadyStream(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		for i := 0; i < 8; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(60 * time.Millisecond):
			}
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	sys := downloadServerTestSystem(server)
	sys.SleepFunc = func(time.Duration) { t.Fatal("steady stream scheduled a retry") }
	dest := downloadTestDestination(t)
	limits := downloadLimits{stall: 200 * time.Millisecond, ceiling: 2 * time.Second}
	if err := downloadToFile(context.Background(), sys, server.URL, dest, limits); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.Repeat("chunk", 8) || requests.Load() != 1 {
		t.Fatalf("data = %q, requests = %d", data, requests.Load())
	}
}

func TestDownloadBodyStall_TimeoutMessage(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"download", "checksum"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			cleanup := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				content := "partial-long"
				if requests.Add(1) > 1 {
					content = "ok"
				}
				_, _ = w.Write([]byte(content))
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
				case <-cleanup:
				}
			}))
			defer server.Close()
			defer close(cleanup)
			sys := downloadServerTestSystem(server)
			sleeps := 0
			sys.SleepFunc = func(d time.Duration) {
				sleeps++
				if d != downloadRetryBackoff {
					t.Fatalf("backoff = %v, want %v", d, downloadRetryBackoff)
				}
			}
			limits := downloadLimits{stall: 100 * time.Millisecond, ceiling: 2 * time.Second}
			url := server.URL
			var err error
			dest := downloadTestDestination(t)
			if operation == "download" {
				err = downloadToFile(context.Background(), sys, url, dest, limits)
				data, readErr := os.ReadFile(dest.Name())
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(data) != "ok" {
					t.Fatalf("retried content did not replace partial bytes: %q", data)
				}
			} else {
				url = fmt.Sprintf("%s/download/v1.0.0/checksums.txt", releaseBaseURL)
				_, err = fetchChecksum(context.Background(), sys, "1.0.0", "asset", limits)
			}
			if err == nil || err.Error() != fmt.Sprintf(messages.DispatchDownloadTimeoutFmt, url) {
				t.Fatalf("expected plain timeout message, got %v", err)
			}
			if errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 || sleeps != 1 {
				t.Fatalf("deadline identity = %v, requests = %d, sleeps = %d", errors.Is(err, context.DeadlineExceeded), requests.Load(), sleeps)
			}
		})
	}
}

func TestDownloadOperationCeiling(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"download", "checksum"} {
		for _, backoff := range []bool{false, true} {
			name := "steady_stream"
			if backoff {
				name = "during_backoff"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				t.Parallel()
				var requests atomic.Int32
				cleanup := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if backoff {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					for {
						_, _ = w.Write([]byte("x"))
						w.(http.Flusher).Flush()
						select {
						case <-r.Context().Done():
							return
						case <-cleanup:
							return
						case <-time.After(20 * time.Millisecond):
						}
					}
				}))
				defer server.Close()
				defer close(cleanup)
				sys := downloadServerTestSystem(server)
				sleeps := 0
				sys.SleepFunc = func(time.Duration) {
					sleeps++
					if !backoff {
						t.Fatal("ceiling expiry scheduled a retry")
					}
					time.Sleep(time.Second)
				}
				limits := downloadLimits{stall: time.Second, ceiling: 300 * time.Millisecond}
				url := server.URL
				var err error
				if operation == "download" {
					err = downloadToFile(context.Background(), sys, url, downloadTestDestination(t), limits)
				} else {
					url = fmt.Sprintf("%s/download/v1.0.0/checksums.txt", releaseBaseURL)
					_, err = fetchChecksum(context.Background(), sys, "1.0.0", "asset", limits)
				}
				if err == nil || err.Error() != fmt.Sprintf(messages.DispatchDownloadTimeoutFmt, url) {
					t.Fatalf("expected plain timeout message, got %v", err)
				}
				wantSleeps := 0
				if backoff {
					wantSleeps = 1
				}
				if errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 || sleeps != wantSleeps {
					t.Fatalf("deadline identity = %v, requests = %d, sleeps = %d", errors.Is(err, context.DeadlineExceeded), requests.Load(), sleeps)
				}
			})
		}
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}

func TestDownloadBodyFailure_ParentContextIdentity(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"download", "checksum"} {
		for _, failure := range []string{"deadline", "stall_then_cancel", "cancel_with_read_error", "already_canceled"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(context.Background())
				if failure == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
				}
				defer cancel()
				if failure == "already_canceled" {
					cancel()
				}
				requests := 0
				sys := &testSystem{
					HTTPClientFunc: func() *http.Client {
						return &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
							requests++
							return &http.Response{
								StatusCode: http.StatusOK,
								Body: io.NopCloser(readerFunc(func([]byte) (int, error) {
									if failure == "cancel_with_read_error" {
										cancel()
										return 0, net.ErrClosed
									}
									<-req.Context().Done()
									if failure == "stall_then_cancel" {
										if !errors.Is(context.Cause(req.Context()), errDownloadStalled) {
											t.Fatal("expected stall before parent cancellation")
										}
										cancel()
									}
									return 0, context.Cause(req.Context())
								})),
							}, nil
						})}
					},
					SleepFunc:  func(time.Duration) { t.Fatal("parent context failure scheduled a retry") },
					GetenvFunc: func(string) string { return "" },
				}
				limits := downloadLimits{stall: 200 * time.Millisecond, ceiling: time.Second}
				if failure == "stall_then_cancel" {
					limits.stall = 20 * time.Millisecond
				}
				url := "https://example.invalid/file"
				genericFmt := messages.DispatchDownloadFailedFmt
				var err error
				if operation == "download" {
					err = downloadToFile(ctx, sys, url, downloadTestDestination(t), limits)
				} else {
					url = fmt.Sprintf("%s/download/v1.0.0/checksums.txt", releaseBaseURL)
					genericFmt = messages.DispatchReadFailedFmt
					if failure == "already_canceled" {
						genericFmt = messages.DispatchDownloadFailedFmt
					}
					_, err = fetchChecksum(ctx, sys, "1.0.0", "asset", limits)
				}
				wantIdentity := context.Canceled
				wantMessage := fmt.Errorf(genericFmt, url, context.Canceled).Error()
				if failure == "deadline" {
					wantIdentity = context.DeadlineExceeded
					wantMessage = fmt.Sprintf(messages.DispatchDownloadTimeoutFmt, url) + ": " + context.DeadlineExceeded.Error()
				}
				if !errors.Is(err, wantIdentity) || err.Error() != wantMessage {
					t.Fatalf("expected %v identity and message %q, got %v", wantIdentity, wantMessage, err)
				}
				wantRequests := 1
				if failure == "already_canceled" {
					wantRequests = 0
				}
				if requests != wantRequests {
					t.Fatalf("requests = %d, want %d", requests, wantRequests)
				}
			})
		}
	}
}
