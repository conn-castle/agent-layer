package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func writeTestRepo(t *testing.T, root string) {
	t.Helper()
	home := t.TempDir()
	origHome := alsync.UserHomeDir
	alsync.UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { alsync.UserHomeDir = origHome })

	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	command := `---
description: test
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SkillsDir, "alpha.md"), []byte(command), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeGitignoreBlock(t, root)
}

func writeTestRepoInvalidConfig(t *testing.T, root string) {
	t.Helper()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentLayerDir, "config.toml"), []byte("invalid = "), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

func writeTestRepoWithWarnings(t *testing.T, root string) {
	t.Helper()
	home := t.TempDir()
	origHome := alsync.UserHomeDir
	alsync.UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { alsync.UserHomeDir = origHome })

	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	// Config with very low instruction token threshold to trigger a warning
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[warnings]
instruction_token_threshold = 1
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	// Write large instructions to exceed the threshold
	largeContent := strings.Repeat("This is a test instruction that will exceed the low token threshold. ", 50)
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeGitignoreBlock(t, root)
}

func writeGitignoreBlock(t *testing.T, root string) {
	t.Helper()
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read gitignore.block template: %v", err)
	}
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(blockPath, templateBytes, 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}
}
