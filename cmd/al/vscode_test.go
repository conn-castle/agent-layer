package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestRunVSCodeNoSync(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "code")

	t.Setenv("PATH", binDir)
	err := clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		v := agentEnabled(cfg.Agents.VSCode.Enabled) || agentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, vscode.Launch, nil)
	if err != nil {
		t.Fatalf("RunNoSync error: %v", err)
	}
}

func TestRunVSCodeNoSyncDisabled(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	paths := filepath.Join(root, ".agent-layer", "config.toml")
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude-vscode]
enabled = false

[agents.codex]
enabled = true

[agents.vscode]
enabled = false

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(paths, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		v := agentEnabled(cfg.Agents.VSCode.Enabled) || agentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, vscode.Launch, nil)
	if err == nil {
		t.Fatal("expected error when both VS Code agents are disabled")
	}
}

func TestRunVSCodeNoSyncEnabledViaClaudeVSCode(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	paths := filepath.Join(root, ".agent-layer", "config.toml")
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude-vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = false

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(paths, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "code")
	t.Setenv("PATH", binDir)

	err := clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		v := agentEnabled(cfg.Agents.VSCode.Enabled) || agentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, vscode.Launch, nil)
	if err != nil {
		t.Fatalf("expected success when claude-vscode is enabled: %v", err)
	}
}

func TestRunVSCodeNoSyncManagedBlockConflict(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\n// >>> agent-layer\n}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "code")
	t.Setenv("PATH", binDir)

	err := clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		v := agentEnabled(cfg.Agents.VSCode.Enabled) || agentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, vscode.Launch, nil)
	if err == nil {
		t.Fatal("expected managed-block conflict error")
	}
	if !strings.Contains(err.Error(), "managed settings block conflict") {
		t.Fatalf("unexpected error: %v", err)
	}
}
