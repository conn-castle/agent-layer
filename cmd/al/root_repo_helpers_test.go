package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func writeTestRepo(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	configToml := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	command := `---
description: test
---

Do it.`
	alphaDir := filepath.Join(paths.SkillsDir, "alpha")
	if err := os.MkdirAll(alphaDir, 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(alphaDir, "SKILL.md"), []byte(command), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeGitignoreBlock(t, root)
}

func writeTestRepoInvalidConfig(t *testing.T, root string) {
	t.Helper()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentLayerDir, "config.toml"), []byte("invalid = "), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

func writeTestRepoWithWarnings(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	// Config with very low instruction token threshold to trigger a warning
	configToml := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true

[warnings]
instruction_token_threshold = 1
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	// Write large instructions to exceed the threshold
	largeContent := strings.Repeat("This is a test instruction that will exceed the low token threshold. ", 50)
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte(largeContent), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
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
	if err := os.WriteFile(blockPath, templateBytes, 0o600); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}
}

func writeDoctorTestRepo(t *testing.T, root string) {
	t.Helper()
	writeTestRepo(t, root)
	writeDoctorAgyStub(t)
}

func writeDoctorTestRepoWithWarnings(t *testing.T, root string) {
	t.Helper()
	writeTestRepoWithWarnings(t, root)
	writeDoctorAgyStub(t)
}

func writeDoctorAgyStub(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, "agy")
	content := `#!/usr/bin/env sh
if [ "$1" = "--version" ]; then
  printf 'agy 1.0.0\n'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for doctor subprocess checks.
		t.Fatalf("write agy stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
