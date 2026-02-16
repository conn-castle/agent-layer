package dispatch

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPinnedVersion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	if err := os.WriteFile(path, []byte("v0.5.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, ok, warning, err := readPinnedVersion(RealSystem{}, root)
	if err != nil {
		t.Fatalf("readPinnedVersion error: %v", err)
	}
	if !ok {
		t.Fatalf("expected pinned version")
	}
	if got != "0.5.0" {
		t.Fatalf("expected 0.5.0, got %q", got)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
}

func TestReadPinnedVersion_CommentsAndBlankLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	content := "\n# repo pin\n\nv0.6.1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, ok, warning, err := readPinnedVersion(RealSystem{}, root)
	if err != nil {
		t.Fatalf("readPinnedVersion error: %v", err)
	}
	if !ok {
		t.Fatalf("expected pinned version")
	}
	if got != "0.6.1" {
		t.Fatalf("expected 0.6.1, got %q", got)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
}

func TestReadPinnedVersionEmptyFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ver, ok, warning, err := readPinnedVersion(RealSystem{}, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected found=false for empty pin")
	}
	if ver != "" {
		t.Fatalf("expected empty version, got %q", ver)
	}
	if warning == "" {
		t.Fatalf("expected warning for empty pin file")
	}
	if !strings.Contains(warning, "is empty") {
		t.Fatalf("expected warning to contain 'is empty', got %q", warning)
	}
}

func TestReadPinnedVersion_InvalidContent_ReturnsWarning(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	if err := os.WriteFile(path, []byte("not-a-version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ver, ok, warning, err := readPinnedVersion(RealSystem{}, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected found=false for invalid pin")
	}
	if ver != "" {
		t.Fatalf("expected empty version, got %q", ver)
	}
	if warning == "" {
		t.Fatalf("expected warning for invalid pin")
	}
	if !strings.Contains(warning, "invalid pinned version") {
		t.Fatalf("expected warning to contain 'invalid pinned version', got %q", warning)
	}
}

func TestReadPinnedVersion_MultipleVersionLines_ReturnsWarning(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	if err := os.WriteFile(path, []byte("0.5.0\n0.5.1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ver, ok, warning, err := readPinnedVersion(RealSystem{}, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected found=false for invalid pin")
	}
	if ver != "" {
		t.Fatalf("expected empty version, got %q", ver)
	}
	if !strings.Contains(warning, "multiple version lines") {
		t.Fatalf("expected warning to mention multiple lines, got %q", warning)
	}
}

func TestReadPinnedVersion_PermissionDenied_ReturnsError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "al.version")
	if err := os.WriteFile(path, []byte("1.0.0"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, warning, err := readPinnedVersion(RealSystem{}, root)
	if err == nil {
		t.Fatalf("expected hard error for permission denied")
	}
	if warning != "" {
		t.Fatalf("expected no warning on permission error, got: %s", warning)
	}
}

func TestResolveRequestedVersionPrefersOverride(t *testing.T) {
	t.Setenv(EnvVersionOverride, "v1.2.3")
	t.Setenv(EnvNoNetwork, "")

	got, source, warning, overridePinned, hasOverridePinned, err := resolveRequestedVersion(RealSystem{}, t.TempDir(), false, "0.5.0")
	if err != nil {
		t.Fatalf("resolveRequestedVersion error: %v", err)
	}
	if got != "1.2.3" {
		t.Fatalf("expected 1.2.3, got %q", got)
	}
	if source != EnvVersionOverride {
		t.Fatalf("expected source %s, got %s", EnvVersionOverride, source)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if hasOverridePinned {
		t.Fatalf("expected override pin flag false, got true with %q", overridePinned)
	}
}

func TestResolveRequestedVersionUsesPin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "al.version"), []byte("0.9.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, source, warning, overridePinned, hasOverridePinned, err := resolveRequestedVersion(RealSystem{}, root, true, "0.5.0")
	if err != nil {
		t.Fatalf("resolveRequestedVersion error: %v", err)
	}
	if got != "0.9.0" {
		t.Fatalf("expected 0.9.0, got %q", got)
	}
	if source != "pin" {
		t.Fatalf("expected source pin, got %s", source)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if hasOverridePinned {
		t.Fatalf("expected override pin flag false, got true with %q", overridePinned)
	}
}

func TestResolveRequestedVersionUsesCurrent(t *testing.T) {
	got, source, warning, overridePinned, hasOverridePinned, err := resolveRequestedVersion(RealSystem{}, t.TempDir(), false, "0.5.0")
	if err != nil {
		t.Fatalf("resolveRequestedVersion error: %v", err)
	}
	if got != "0.5.0" {
		t.Fatalf("expected 0.5.0, got %q", got)
	}
	if source != "current" {
		t.Fatalf("expected source current, got %s", source)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if hasOverridePinned {
		t.Fatalf("expected override pin flag false, got true with %q", overridePinned)
	}
}

func TestCacheRootDir(t *testing.T) {
	t.Setenv(EnvCacheDir, "/custom/cache")
	got, err := cacheRootDir(RealSystem{})
	if err != nil {
		t.Fatalf("cacheRootDir error: %v", err)
	}
	if got != "/custom/cache" {
		t.Errorf("got %q, want /custom/cache", got)
	}

	t.Setenv(EnvCacheDir, "")
	// Just check it doesn't error and looks like a path
	got, err = cacheRootDir(RealSystem{})
	if err != nil {
		t.Fatalf("cacheRootDir error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty cache dir")
	}
}

func TestMaybeExec_NoDispatchNeeded(t *testing.T) {
	cwd := t.TempDir()
	err := MaybeExec([]string{"cmd"}, "1.0.0", cwd, func(int) {})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMaybeExec_MissingArgs(t *testing.T) {
	err := MaybeExec([]string{}, "1.0.0", ".", func(int) {})
	if err == nil || err.Error() != "missing argv[0]" {
		t.Fatalf("expected missing argv[0], got %v", err)
	}
}

func TestMaybeExec_MissingCwd(t *testing.T) {
	err := MaybeExec([]string{"cmd"}, "1.0.0", "", func(int) {})
	if err == nil || err.Error() != "working directory is required" {
		t.Fatalf("expected working directory required, got %v", err)
	}
}

func TestMaybeExec_MissingExit(t *testing.T) {
	err := MaybeExec([]string{"cmd"}, "1.0.0", ".", nil)
	if err == nil || err.Error() != "exit handler is required" {
		t.Fatalf("expected exit handler required, got %v", err)
	}
}

func TestMaybeExec_InvalidCurrentVersion(t *testing.T) {
	err := MaybeExec([]string{"cmd"}, "invalid-version", ".", func(int) {})
	if err == nil {
		t.Fatal("expected error for invalid current version")
	}
}

func TestMaybeExec_DispatchAlreadyActive(t *testing.T) {
	t.Setenv(EnvShimActive, "1")
	t.Setenv(EnvVersionOverride, "1.1.0") // Different from current

	err := MaybeExec([]string{"cmd"}, "1.0.0", t.TempDir(), func(int) {})
	if err == nil {
		t.Fatal("expected error when dispatch active")
	}
}

func TestMaybeExec_DispatchSuccess(t *testing.T) {
	// Setup mock server for download
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)

	// We need platform strings to match ensureCachedBinary logic
	osName, arch, _ := platformStrings()
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

	var execCalled bool
	var execPath string
	sys := &testSystem{
		ExecBinaryFunc: func(path string, args []string, env []string, exit func(int)) error {
			execCalled = true
			execPath = path
			return nil // Simulate success (process replaced)
		},
	}

	// Setup env
	t.Setenv(EnvVersionOverride, version)
	cacheDir := t.TempDir()
	t.Setenv(EnvCacheDir, cacheDir)

	// Call MaybeExec
	err := MaybeExecWithSystem(sys, []string{"cmd"}, "0.9.0", ".", func(int) {})
	if err != ErrDispatched {
		t.Fatalf("expected ErrDispatched, got %v", err)
	}

	if !execCalled {
		t.Fatal("expected execBinary to be called")
	}

	expectedPath := filepath.Join(cacheDir, "versions", version, osName+"-"+arch, asset)
	if execPath != expectedPath {
		t.Errorf("exec path: got %s, want %s", execPath, expectedPath)
	}
}

func TestMaybeExec_OverrideSameAsCurrent(t *testing.T) {
	t.Setenv(EnvVersionOverride, "1.0.0")
	// If requested == current, it returns nil (no dispatch)
	err := MaybeExecWithSystem(RealSystem{}, []string{"cmd"}, "1.0.0", ".", func(int) {})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMaybeExec_OverrideWarnsWhenPinExists(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(alDir, "al.version"), []byte("0.9.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	t.Setenv(EnvVersionOverride, "1.0.0")
	var stderr bytes.Buffer
	sys := &testSystem{
		FindAgentLayerRootFunc: func(string) (string, bool, error) {
			return root, true, nil
		},
		StderrFunc: func() io.Writer {
			return &stderr
		},
	}

	err := MaybeExecWithSystem(sys, []string{"cmd"}, "1.0.0", root, func(int) {})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "version source: 1.0.0 (AL_VERSION)") {
		t.Fatalf("expected version source output, got %q", output)
	}
	if !strings.Contains(output, "overrides repo pin 0.9.0") {
		t.Fatalf("expected override warning, got %q", output)
	}
}

func TestMaybeExec_OverrideReadsPinOnce(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	readCount := 0

	sys := &testSystem{
		GetenvFunc: func(key string) string {
			if key == EnvVersionOverride {
				return "1.0.0"
			}
			return ""
		},
		FindAgentLayerRootFunc: func(string) (string, bool, error) {
			return root, true, nil
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			readCount++
			return []byte("0.9.0\n"), nil
		},
		StderrFunc: func() io.Writer {
			return &stderr
		},
	}

	err := MaybeExecWithSystem(sys, []string{"cmd"}, "1.0.0", root, func(int) {})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if readCount != 1 {
		t.Fatalf("expected pin to be read once, got %d reads", readCount)
	}
	if !strings.Contains(stderr.String(), "overrides repo pin 0.9.0") {
		t.Fatalf("expected override warning, got %q", stderr.String())
	}
}

func TestMaybeExec_InvalidOverride(t *testing.T) {
	t.Setenv(EnvVersionOverride, "invalid-version")
	err := MaybeExecWithSystem(RealSystem{}, []string{"cmd"}, "1.0.0", ".", func(int) {})
	if err == nil {
		t.Fatal("expected error for invalid override")
	}
}

func TestMaybeExec_ReadPinnedVersionError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pinFile := filepath.Join(alDir, "al.version")
	if err := os.WriteFile(pinFile, []byte("1.0.0"), 0o000); err != nil {
		t.Fatal(err)
	}
	// On Windows chmod 000 might not prevent reading?
	// But ensureCachedBinary isn't called if error occurs earlier.

	// Create a dummy config to ensure root is found
	if err := os.WriteFile(filepath.Join(alDir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	err := MaybeExecWithSystem(RealSystem{}, []string{"cmd"}, "0.9.0", root, func(int) {})
	if err == nil {
		t.Fatal("expected error reading pinned version")
	}
}

func TestMaybeExec_DevTarget(t *testing.T) {
	t.Setenv(EnvVersionOverride, "dev")
	err := MaybeExecWithSystem(RealSystem{}, []string{"cmd"}, "1.0.0", t.TempDir(), func(int) {})
	if err == nil {
		t.Fatal("expected error when dispatching to dev")
	}
}

func TestMaybeExec_ExecBinaryError(t *testing.T) {
	// Setup env
	t.Setenv(EnvVersionOverride, "1.0.0")
	// Must mock ensureCachedBinary or ensure it succeeds.
	// We can use the logic from DispatchSuccess but make exec fail.

	// Setup mock server for download to pass ensureCachedBinary
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
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

	sys := &testSystem{
		ExecBinaryFunc: func(path string, args []string, env []string, exit func(int)) error {
			return errors.New("exec failed")
		},
	}

	t.Setenv(EnvCacheDir, t.TempDir())

	err := MaybeExecWithSystem(sys, []string{"cmd"}, "0.9.0", ".", func(int) {})
	if err == nil || err.Error() != "exec failed" {
		t.Fatalf("expected exec failed, got %v", err)
	}
}

func TestMaybeExec_EnsureCachedBinaryError(t *testing.T) {
	t.Setenv(EnvVersionOverride, "1.0.0")
	t.Setenv(EnvCacheDir, t.TempDir())

	// No mock server -> download fails

	err := MaybeExecWithSystem(RealSystem{}, []string{"cmd"}, "0.9.0", ".", func(int) {})
	if err == nil {
		t.Fatal("expected error from ensureCachedBinary")
	}
}

func TestCacheRootDir_Error(t *testing.T) {
	sys := &testSystem{
		UserCacheDirFunc: func() (string, error) {
			return "", errors.New("user cache dir failed")
		},
	}

	t.Setenv(EnvCacheDir, "") // Ensure we use userCacheDir

	_, err := cacheRootDir(sys)
	if err == nil {
		t.Fatal("expected error from cacheRootDir")
	}
}

func TestMaybeExec_CacheRootDirError(t *testing.T) {
	sys := &testSystem{
		UserCacheDirFunc: func() (string, error) {
			return "", errors.New("user cache dir failed")
		},
	}

	t.Setenv(EnvCacheDir, "")
	t.Setenv(EnvVersionOverride, "1.0.0")

	err := MaybeExecWithSystem(sys, []string{"cmd"}, "0.9.0", ".", func(int) {})
	if err == nil {
		t.Fatal("expected error from MaybeExec when cacheRootDir fails")
	}
}

func TestMaybeExec_CurrentIsDev(t *testing.T) {
	// If current is dev, and no override/pin, it should just return nil (no dispatch)
	// assuming dev doesn't dispatch to itself?
	// resolveRequestedVersion returns current ("dev").
	// if requested == current -> return nil.

	err := MaybeExec([]string{"cmd"}, "dev", ".", func(int) {})
	if err != nil {
		t.Fatalf("expected nil error for dev current version, got %v", err)
	}
}

func TestMaybeExec_CorruptPinFallsThroughToCurrentVersion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write config.toml so root is found
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Write corrupt pin
	if err := os.WriteFile(filepath.Join(dir, "al.version"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stderrBuf bytes.Buffer
	sys := &testSystem{
		StderrFunc: func() io.Writer { return &stderrBuf },
	}

	// Current version is 1.0.0, corrupt pin falls through → requested == current → no dispatch
	err := MaybeExecWithSystem(sys, []string{"cmd"}, "1.0.0", root, func(int) {})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify warning was printed
	if !strings.Contains(stderrBuf.String(), "invalid pinned version") {
		t.Fatalf("expected warning about invalid pin, got %q", stderrBuf.String())
	}
}

func TestMaybeExec_ExecReturnsDispatched(t *testing.T) {
	t.Setenv(EnvVersionOverride, "1.0.0")
	t.Setenv(EnvCacheDir, t.TempDir())

	// Mock ensureCachedBinary success
	// We can cheat and point to a local file
	// Or use the server mock again.
	// Let's use server mock for completeness.
	version := "1.0.0"
	content := "binary-content"
	checksum := sha256.Sum256([]byte(content))
	checksumStr := fmt.Sprintf("%x", checksum)
	osName, arch, _ := platformStrings()
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

	sys := &testSystem{
		ExecBinaryFunc: func(path string, args []string, env []string, exit func(int)) error {
			return ErrDispatched
		},
	}

	err := MaybeExecWithSystem(sys, []string{"cmd"}, "0.9.0", ".", func(int) {})
	if err != ErrDispatched {
		t.Fatalf("expected ErrDispatched, got %v", err)
	}
}
