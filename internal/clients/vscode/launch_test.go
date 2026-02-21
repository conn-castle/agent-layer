package vscode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
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
	testutil.WriteStub(t, binDir, "code")

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
	testutil.WriteStubWithExit(t, binDir, "code", 1)

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

func TestCheckManagedSettingsConflict_ReadError(t *testing.T) {
	origReadFile := readFile
	t.Cleanup(func() { readFile = origReadFile })

	readFile = func(string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	err := checkManagedSettingsConflict(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestCheckManagedSettingsConflict_NoMarkers(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"editor.formatOnSave\":true}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := checkManagedSettingsConflict(root); err != nil {
		t.Fatalf("expected no conflict, got %v", err)
	}
}

func TestCheckManagedSettingsConflict_StartAfterEnd(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "{\n  // <<< agent-layer\n  // >>> agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := checkManagedSettingsConflict(root)
	if err == nil || !strings.Contains(err.Error(), "start marker appears after end marker") {
		t.Fatalf("expected marker order conflict, got %v", err)
	}
}

func TestCheckManagedSettingsConflict_ValidManagedBlock(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "{\n  // >>> agent-layer\n  \"x\": 1,\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := checkManagedSettingsConflict(root); err != nil {
		t.Fatalf("expected valid managed block, got %v", err)
	}
}

func TestHasPositionalArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nil args", nil, false},
		{"empty args", []string{}, false},
		{"long flags only", []string{"--new-window", "--reuse-window"}, false},
		{"short flags only", []string{"-n", "-r"}, false},
		{"mixed short and long flags", []string{"-n", "--reuse-window"}, false},
		{"positional path", []string{"/path/to/project"}, true},
		{"flag then positional", []string{"--new-window", "/path/to/project.code-workspace"}, true},
		{"short flag then positional", []string{"-n", "/path/to/project.code-workspace"}, true},
		{"bare double dash", []string{"--new-window", "--"}, true},
		{"relative path", []string{"."}, true},
		{"positional then flag", []string{"mydir", "--reuse-window"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasPositionalArg(tt.args)
			if got != tt.want {
				t.Fatalf("hasPositionalArg(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestLaunchVSCode_SkipsDotWhenPositionalArgPresent(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	// Write a stub that prints its arguments to a file so we can inspect them.
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %s\n", argsFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	cfg := &config.ProjectConfig{Root: root}
	t.Setenv("PATH", binDir)
	env := os.Environ()

	// Pass a positional workspace arg — "." should NOT be appended.
	passArgs := []string{"--new-window", "/path/to/project.code-workspace"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, passArgs); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsStr := strings.TrimSpace(string(got))
	if strings.HasSuffix(argsStr, " .") {
		t.Fatalf("expected no trailing '.', got args: %q", argsStr)
	}
	if !strings.Contains(argsStr, "/path/to/project.code-workspace") {
		t.Fatalf("expected workspace arg in output, got: %q", argsStr)
	}
}

func TestLaunchVSCode_AppendsDotWithShortFlagsOnly(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %s\n", argsFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	cfg := &config.ProjectConfig{Root: root}
	t.Setenv("PATH", binDir)
	env := os.Environ()

	// Pass only short flags — "." SHOULD still be appended.
	passArgs := []string{"-n", "-r"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, passArgs); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsStr := strings.TrimSpace(string(got))
	if !strings.HasSuffix(argsStr, ".") {
		t.Fatalf("expected trailing '.', got args: %q", argsStr)
	}
}

func TestLaunchVSCode_AppendsDotWhenNoPositionalArg(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %s\n", argsFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	cfg := &config.ProjectConfig{Root: root}
	t.Setenv("PATH", binDir)
	env := os.Environ()

	// Pass only flags — "." SHOULD be appended.
	passArgs := []string{"--new-window"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, passArgs); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsStr := strings.TrimSpace(string(got))
	if !strings.HasSuffix(argsStr, ".") {
		t.Fatalf("expected trailing '.', got args: %q", argsStr)
	}
}
