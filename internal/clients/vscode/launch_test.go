package vscode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestLaunchVSCode(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()
	writeStub(t, binDir, "code")

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

func TestLaunchVSCodeError(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "code", 1)

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

func TestLaunchVSCodePreflight_CodeMissing(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	lookPath = func(file string) (string, error) {
		return "", &os.PathError{Op: "exec", Path: file, Err: os.ErrNotExist}
	}

	root := t.TempDir()
	cfg := &config.ProjectConfig{Root: root}
	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, os.Environ(), nil)
	if err == nil {
		t.Fatal("expected code missing preflight error")
	}
	if !strings.Contains(err.Error(), "vscode preflight failed") {
		t.Fatalf("expected preflight error, got %v", err)
	}
}

func TestLaunchVSCodePreflight_ManagedBlockConflict(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	lookPath = func(file string) (string, error) {
		return filepath.Join(root, "code"), nil
	}

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "{\n  // >>> agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	cfg := &config.ProjectConfig{Root: root}
	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, os.Environ(), nil)
	if err == nil {
		t.Fatal("expected managed-block conflict error")
	}
	if !strings.Contains(err.Error(), "managed settings block conflict") {
		t.Fatalf("expected managed-block conflict, got %v", err)
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
