package dispatch

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	path, err := ensureCachedBinary(cacheRoot, version)
	if err != nil {
		t.Fatalf("ensureCachedBinary failed: %v", err)
	}

	// Verify file exists and content
	if _, err := os.Stat(path); err != nil {
		t.Errorf("binary not found at %s", path)
	}
	gotContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(gotContent) != content {
		t.Errorf("content mismatch: got %q, want %q", string(gotContent), content)
	}

	// 4. Run test - Second time (cached)
	// Stop server to ensure we don't hit network
	server.Close()

	path2, err := ensureCachedBinary(cacheRoot, version)
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

	_, err := ensureCachedBinary(cacheRoot, version)
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

	_, err := ensureCachedBinary(cacheRoot, version)
	if err == nil {
		t.Fatal("expected error due to 404, got nil")
	}
}

func TestEnsureCachedBinary_NoNetwork(t *testing.T) {
	t.Setenv(EnvNoNetwork, "1")
	cacheRoot := t.TempDir()

	_, err := ensureCachedBinary(cacheRoot, "1.0.0")
	if err == nil {
		t.Fatal("expected error when network is disabled and binary missing")
	}
}

func TestEnsureCachedBinary_PlatformError(t *testing.T) {
	orig := platformStringsFunc
	defer func() { platformStringsFunc = orig }()
	platformStringsFunc = func() (string, string, error) {
		return "", "", fmt.Errorf("platform error")
	}

	_, err := ensureCachedBinary(t.TempDir(), "1.0.0")
	if err == nil {
		t.Fatal("expected error from platformStrings")
	}
}

func TestEnsureCachedBinary_ChmodError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not called on windows")
	}

	origChmod := osChmod
	defer func() { osChmod = origChmod }()
	osChmod = func(name string, mode os.FileMode) error {
		return fmt.Errorf("chmod failed")
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

	_, err := ensureCachedBinary(t.TempDir(), version)
	if err == nil {
		t.Fatal("expected error from chmod")
	}
}

func TestEnsureCachedBinary_StatError(t *testing.T) {
	origStat := osStat
	defer func() { osStat = origStat }()
	osStat = func(name string) (os.FileInfo, error) {
		return nil, fmt.Errorf("stat failed")
	}

	_, err := ensureCachedBinary(t.TempDir(), "1.0.0")
	if err == nil {
		t.Fatal("expected error from stat")
	}
}

func TestEnsureCachedBinary_RaceCondition(t *testing.T) {
	// Simulate:
	// 1. Stat -> NotExist (proceeds to lock)
	// 2. Lock acquired
	// 3. Stat -> Exists (returns success)

	origStat := osStat
	defer func() { osStat = origStat }()

	calls := 0
	osStat = func(name string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, os.ErrNotExist
		}
		// Second call (inside lock) returns success
		return nil, nil
	}

	cacheRoot := t.TempDir()
	version := "1.0.0"

	path, err := ensureCachedBinary(cacheRoot, version)
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

	origStat := osStat
	defer func() { osStat = origStat }()

	calls := 0
	osStat = func(name string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("internal stat failed")
	}

	_, err := ensureCachedBinary(t.TempDir(), "1.0.0")
	if err == nil {
		t.Fatal("expected error from internal stat")
	}
}

func TestEnsureCachedBinary_RenameError(t *testing.T) {
	origRename := osRename
	defer func() { osRename = origRename }()
	osRename = func(oldpath, newpath string) error {
		return fmt.Errorf("rename failed")
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

	_, err := ensureCachedBinary(t.TempDir(), version)
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
		{"windows", "amd64", "al-windows-amd64.exe"},
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
	if err := os.MkdirAll(filepath.Dir(dirToBlock), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a file at dirToBlock
	if err := os.WriteFile(dirToBlock, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ensureCachedBinary(cacheRoot, version)
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
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a directory at lockPath
	if err := os.Mkdir(lockPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ensureCachedBinary(cacheRoot, version)
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

	_, err := ensureCachedBinary(cacheRoot, version)
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

	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ensureCachedBinary(cacheRoot, version)
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
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = downloadToFile(server.URL, f)

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

	_, err := fetchChecksum(version, asset)
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

	_, err := fetchChecksum(version, asset)
	if err == nil {
		t.Fatal("expected error on 500 status")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestVerifyChecksum_FileOpenError(t *testing.T) {
	err := verifyChecksum("non-existent-file", "hash")
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestPlatformStrings(t *testing.T) {
	osName, arch, err := platformStrings()
	if err != nil {
		t.Fatalf("platformStrings failed: %v", err)
	}
	if osName != runtime.GOOS {
		t.Errorf("osName: got %s, want %s", osName, runtime.GOOS)
	}
	if arch != runtime.GOARCH {
		t.Errorf("arch: got %s, want %s", arch, runtime.GOARCH)
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
		{"windows", "amd64", false},
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

	_, err := fetchChecksum(version, asset)
	if err == nil {
		t.Fatal("expected error when checksum not found in file")
	}
}
