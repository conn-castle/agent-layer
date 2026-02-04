package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestEnsureCodexHomeSetsDefault(t *testing.T) {
	root := t.TempDir()
	env := []string{}

	env = ensureCodexHome(root, env)

	expected := filepath.Join(root, ".codex")
	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestEnsureCodexHomeKeepsMatching(t *testing.T) {
	root := t.TempDir()
	expected := filepath.Join(root, ".codex")
	env := []string{"CODEX_HOME=" + expected}

	env = ensureCodexHome(root, env)

	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestLaunchCodex(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "codex", 0)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Model:           "model",
					ReasoningEffort: "low",
				},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "other"))
	env := os.Environ()

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchCodexError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "codex", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Model:           "model",
					ReasoningEffort: "low",
				},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{}); err == nil {
		t.Fatalf("expected error")
	}
}

func writeStubWithExit(t *testing.T, dir string, name string, code int) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", code))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func TestLaunchCodexWithAdditionalArgs(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	argsFile := filepath.Join(root, "captured-args.txt")
	writeStubWithArgsCapture(t, binDir, "codex", argsFile)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	additionalArgs := []string{"--help", "--debug"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, additionalArgs); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	// Verify the additional arguments were passed
	content, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("Failed to read captured args: %v", err)
	}

	argsStr := string(content)
	if argsStr != "--help --debug\n" {
		t.Errorf("Expected args '--help --debug', got: %q", argsStr)
	}
}

func writeStubWithArgsCapture(t *testing.T, dir string, name string, argsFile string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\necho \"$@\" > %s\n", argsFile))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}
