package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateDirAvoidsSecondLevelCollisions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-root")
	probedAt := time.Date(2026, 5, 22, 3, 46, 18, 0, time.UTC)

	first, err := CreateDir(root, "grok", probedAt)
	if err != nil {
		t.Fatalf("create first probe dir: %v", err)
	}
	second, err := CreateDir(root, "grok", probedAt)
	if err != nil {
		t.Fatalf("create second probe dir: %v", err)
	}

	if first == second {
		t.Fatalf("expected unique probe dirs, got %s twice", first)
	}
	for _, path := range []string{first, second} {
		if !strings.HasPrefix(filepath.Base(path), "probe-grok-20260522T034618Z-") {
			t.Fatalf("probe dir %s does not carry the provider and timestamp", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat probe dir %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected probe path to be a directory: %s", path)
		}
	}
}

func TestExitCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("success exit code = %d", got)
	}
	// Cancellation captured during execution wins over other errors.
	if got := ExitCode(errors.Join(ctx.Err(), errors.New("cancelled"))); got != 124 {
		t.Fatalf("cancelled exit code = %d", got)
	}
	if got := ExitCode(context.DeadlineExceeded); got != 124 {
		t.Fatalf("deadline exit code = %d", got)
	}
	err := exec.Command("/bin/sh", "-c", "exit 7").Run()
	if got := ExitCode(err); got != 7 {
		t.Fatalf("process exit code = %d, err %v", got, err)
	}
	if got := ExitCode(errors.New("launch")); got != -1 {
		t.Fatalf("launch exit code = %d", got)
	}
}
