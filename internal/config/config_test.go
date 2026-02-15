package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValid(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true
model = "gpt-5.2-codex"
reasoning_effort = "high"

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = false

[mcp]
[[mcp.servers]]
id = "local"
enabled = false
transport = "stdio"
command = "tool"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Approvals.Mode != "all" {
		t.Fatalf("unexpected approvals mode: %s", cfg.Approvals.Mode)
	}
	if cfg.Agents.Gemini.Enabled == nil || !*cfg.Agents.Gemini.Enabled {
		t.Fatalf("expected gemini enabled")
	}
}

func TestLoadConfigInvalidApprovals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
[approvals]
mode = "bad"

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
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "approvals.mode") {
		t.Fatalf("expected approvals error, got: %v", err)
	}
}

func TestSubstituteEnvVars(t *testing.T) {
	env := map[string]string{
		"TOKEN": "abc",
	}
	value, err := SubstituteEnvVars("Bearer ${TOKEN}", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "Bearer abc" {
		t.Fatalf("unexpected value: %s", value)
	}

	value, err = SubstituteEnvVarsWith("Bearer ${TOKEN}", env, func(name string, _ string) string {
		return fmt.Sprintf("<%s>", name)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "Bearer <TOKEN>" {
		t.Fatalf("unexpected value: %s", value)
	}

	_, err = SubstituteEnvVars("${MISSING}", env)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestSubstituteEnvVars_EmptyValueIsAllowed(t *testing.T) {
	env := map[string]string{
		"TOKEN": "",
	}
	value, err := SubstituteEnvVars("Bearer ${TOKEN}", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "Bearer " {
		t.Fatalf("unexpected value: %q", value)
	}
}

func TestLoadConfigReservedMCPID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
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

[mcp]
[[mcp.servers]]
id = "agent-layer"
enabled = true
transport = "stdio"
command = "tool"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved id error, got: %v", err)
	}
}

func TestLoadConfigInvalidToml(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
[approvals
mode = "all"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected invalid config error, got: %v", err)
	}
}
