package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadProjectConfig(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}

	config := `
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
enabled = true

[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "hello.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	project, err := LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig error: %v", err)
	}
	if project.Root != root {
		t.Fatalf("expected root %s, got %s", root, project.Root)
	}
	if len(project.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(project.Instructions))
	}
	if len(project.SlashCommands) != 1 {
		t.Fatalf("expected 1 slash command, got %d", len(project.SlashCommands))
	}
	if len(project.CommandsAllow) != 1 || project.CommandsAllow[0] != "git status" {
		t.Fatalf("unexpected commands allow: %v", project.CommandsAllow)
	}
}

func TestLoadProjectConfigMissingConfig(t *testing.T) {
	_, err := LoadProjectConfig(t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProjectConfigMissingEnv(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	config := `
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
enabled = true

[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "hello.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatalf("expected missing env error")
	}
	if !strings.Contains(err.Error(), "missing env file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProjectConfigMissingInstructions(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	config := `
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
enabled = true

[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "hello.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing instructions directory") {
		t.Fatalf("expected missing instructions error, got %v", err)
	}
}

func TestLoadProjectConfigMissingSlashCommands(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	config := `
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
enabled = true

[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing slash commands directory") {
		t.Fatalf("expected missing slash commands error, got %v", err)
	}
}

func TestLoadProjectConfigMissingCommandsAllow(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	config := `
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
enabled = true

[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "hello.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing commands allowlist") {
		t.Fatalf("expected missing commands allowlist error, got %v", err)
	}
}

func TestLoadEnvInvalidFormat(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	// Invalid env file - line without equals sign
	if err := os.WriteFile(path, []byte("INVALID"), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	_, err := LoadEnv(path)
	if err == nil {
		t.Fatalf("expected error for invalid env file")
	}
	if !strings.Contains(err.Error(), "invalid env file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTemplateConfig(t *testing.T) {
	cfg, err := LoadTemplateConfig()
	if err != nil {
		t.Fatalf("LoadTemplateConfig error: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	// Verify the template config has MCP servers
	if len(cfg.MCP.Servers) == 0 {
		t.Fatalf("expected MCP servers in template config")
	}
}

func TestLoadTemplateConfigReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := LoadTemplateConfig()
	if err == nil {
		t.Fatalf("expected error when template read fails")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfigLenient_ValidTOMLMissingRequiredFields(t *testing.T) {
	// A pre-v0.8.1 config missing [agents.claude-vscode] should succeed
	// with lenient parsing even though strict ParseConfig would reject it.
	toml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = false
`
	cfg, err := ParseConfigLenient([]byte(toml), "test")
	if err != nil {
		t.Fatalf("expected lenient parse to succeed, got: %v", err)
	}
	if cfg.Agents.ClaudeVSCode.Enabled != nil {
		t.Fatal("expected claude-vscode.enabled to be nil (missing from config)")
	}
	// Strict parse should fail on the same input.
	_, strictErr := ParseConfig([]byte(toml), "test")
	if strictErr == nil {
		t.Fatal("expected strict ParseConfig to fail for missing claude-vscode.enabled")
	}
}

func TestParseConfigLenient_InvalidTOMLSyntax(t *testing.T) {
	_, err := ParseConfigLenient([]byte("invalid toml [[["), "test")
	if err == nil {
		t.Fatal("expected error for invalid TOML syntax")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigLenient(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")

	// Write a config missing required fields.
	toml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigLenient(path)
	if err != nil {
		t.Fatalf("expected lenient load to succeed, got: %v", err)
	}
	if cfg.Approvals.Mode != "all" {
		t.Fatalf("expected approvals.mode = all, got %q", cfg.Approvals.Mode)
	}
}

func TestLoadConfigLenient_MissingFile(t *testing.T) {
	_, err := LoadConfigLenient("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "missing config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfig_ValidationErrorIncludesGuidance(t *testing.T) {
	// A config missing required fields should produce an error with guidance text.
	toml := `
[approvals]
mode = "all"
`
	_, err := ParseConfig([]byte(toml), "test")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "al wizard") || !strings.Contains(err.Error(), "al doctor") {
		t.Fatalf("expected error to contain guidance about wizard/doctor, got: %v", err)
	}
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("expected error to wrap ErrConfigValidation, got: %v", err)
	}
}

func TestParseConfig_TOMLSyntaxErrorIsNotValidationError(t *testing.T) {
	_, err := ParseConfig([]byte(`{{{`), "test")
	if err == nil {
		t.Fatal("expected TOML syntax error")
	}
	if errors.Is(err, ErrConfigValidation) {
		t.Fatalf("TOML syntax error should not match ErrConfigValidation, got: %v", err)
	}
}
