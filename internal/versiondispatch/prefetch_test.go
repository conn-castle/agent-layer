package versiondispatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrefetchVersion_DownloadsToConfiguredCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv(EnvCacheDir, cacheRoot)

	version := "1.2.3"
	content := "prefetch-binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)

	asset := assetName(runtime.GOOS, runtime.GOARCH)
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

	origReleaseBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() { releaseBaseURL = origReleaseBaseURL })

	var progress bytes.Buffer
	if err := PrefetchVersion(context.Background(), "v1.2.3", &progress); err != nil {
		t.Fatalf("PrefetchVersion: %v", err)
	}

	expectedPath := filepath.Join(cacheRoot, "versions", version, runtime.GOOS+"-"+runtime.GOARCH, asset)
	data, err := os.ReadFile(expectedPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read prefetched binary: %v", err)
	}
	if string(data) != content {
		t.Fatalf("prefetched content = %q, want %q", string(data), content)
	}
	if !strings.Contains(progress.String(), "Downloading al v1.2.3") {
		t.Fatalf("expected progress output, got %q", progress.String())
	}
}

func TestPrefetchVersion_CanceledWithCachedBinary(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv(EnvCacheDir, cacheRoot)

	version := "1.2.3"
	binPath := filepath.Join(cacheRoot, "versions", version, runtime.GOOS+"-"+runtime.GOARCH, assetName(runtime.GOOS, runtime.GOARCH))
	if err := os.MkdirAll(filepath.Dir(binPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("cached"), 0o700); err != nil { // #nosec G306 -- executable cache fixture.
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var progress bytes.Buffer
	err := PrefetchVersion(ctx, version, &progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if progress.Len() != 0 {
		t.Fatalf("expected no download progress for cached binary, got %q", progress.String())
	}
}

func TestPrefetchVersion_InvalidVersion(t *testing.T) {
	err := PrefetchVersion(context.Background(), "not-a-version", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid version error")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
