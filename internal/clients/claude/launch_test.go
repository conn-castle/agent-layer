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
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
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
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLaunchClaudeYOLO(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubExpectArg(t, binDir, "claude", "--dangerously-skip-permissions")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents: config.AgentsConfig{
				Claude: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
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

func writeStubExpectArg(t *testing.T, dir string, name string, expected string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"%s\" ]; then exit 0; fi\ndone\nexit 1\n", expected))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}
