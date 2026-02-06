package antigravity

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestLaunchAntigravity(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStub(t, binDir, "antigravity")

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchAntigravityError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "antigravity", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func writeStub(t *testing.T, dir string, name string) {
	t.Helper()
	writeStubWithExit(t, dir, name, 0)
}

func writeStubWithExit(t *testing.T, dir string, name string, code int) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", code))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}
