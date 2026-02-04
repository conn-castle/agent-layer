package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestLaunchClaude(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStub(t, binDir, "claude")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchClaudeError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "claude", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.AgentConfig{Model: "test-model"},
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

func TestLaunchClaudeWithAdditionalArgs(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	argsFile := filepath.Join(root, "captured-args.txt")
	writeStubWithArgsCapture(t, binDir, "claude", argsFile)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	additionalArgs := []string{"--help", "--dangerously-skip-permissions"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, additionalArgs); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	// Verify the additional arguments were passed
	content, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("Failed to read captured args: %v", err)
	}

	argsStr := string(content)
	if argsStr != "--model test-model --help --dangerously-skip-permissions\n" {
		t.Errorf("Expected args '--model test-model --help --dangerously-skip-permissions', got: %q", argsStr)
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
