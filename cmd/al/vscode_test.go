package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunVSCodeNoSync(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	writeStub(t, binDir, "code")

	t.Setenv("PATH", binDir)
	if err := runVSCodeNoSync(root, []string{}); err != nil {
		t.Fatalf("runVSCodeNoSync error: %v", err)
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

	if err := runVSCodeNoSync(root, []string{}); err == nil {
		t.Fatal("expected error when VS Code is disabled")
	}
}
