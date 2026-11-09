//go:build unix

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// TestDispatchMCPLauncherRunsPinnedServerFromImage starts the generated
// launcher for a project pinned to the installed release. The server process
// must run from the immutable image copy rather than the replaceable install.
func TestDispatchMCPLauncherRunsPinnedServerFromImage(t *testing.T) {
	base := t.TempDir()
	root := writeLauncherProject(t, filepath.Join(base, "repo"))
	installDir, cacheDir, home := filepath.Join(base, "install"), filepath.Join(base, "cache"), filepath.Join(base, "home")
	for _, dir := range []string{installDir, home} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	installed := filepath.Join(installDir, "al")
	build := exec.Command("go", "build", "-ldflags=-X main.Version=1.2.3", "-o", installed, ".") // #nosec G204 -- test build of this package.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "PATH=" + installDir + ":/usr/bin:/bin", versiondispatch.EnvNoNetwork + "=1", versiondispatch.EnvCacheDir + "=" + cacheDir}
	runLauncherAL(t, root, env, installed, "sync")
	command, args := codexDispatchLauncher(t, readGeneratedLauncherConfig(t, root))

	cmd := exec.Command(command, args...) // #nosec G204 -- generated launcher under test.
	cmd.Dir = t.TempDir()
	cmd.Env = env
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "image-test", Version: "test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	data, err := os.ReadFile(installed) // #nosec G304 -- test-built executable.
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := filepath.Join(cacheDir, "images", hex.EncodeToString(sum[:]), "al")
	if got := processImage(t, cmd.Process.Pid); got != resolveTestPath(t, want) {
		t.Fatalf("pinned server runs %q, want image copy %q", got, want)
	}
	if entries, _ := os.ReadDir(filepath.Join(cacheDir, "versions")); len(entries) != 0 {
		t.Fatalf("image handoff wrote the versions cache: %v", entries)
	}
}

// processImage returns the executable path a running process was started from.
func processImage(t *testing.T, pid int) string {
	t.Helper()
	if runtime.GOOS == "linux" {
		path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
		if err != nil {
			t.Fatal(err)
		}
		return resolveTestPath(t, path)
	}
	output, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output() // #nosec G204 -- fixed ps query for a test-owned process.
	if err != nil {
		t.Fatal(err)
	}
	return resolveTestPath(t, strings.TrimSpace(string(output)))
}

func resolveTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}
	return filepath.Join(resolved, filepath.Base(path))
}
