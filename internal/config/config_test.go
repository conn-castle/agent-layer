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

[agents.claude-vscode]
enabled = true

[agents.codex]
enabled = true
model = "gpt-5.3-codex"
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

[agents.claude-vscode]
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

func TestSubstituteEnvVars_EmptyValueReturnsMissingError(t *testing.T) {
	env := map[string]string{
		"TOKEN": "",
	}
	_, err := SubstituteEnvVars("Bearer ${TOKEN}", env)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("expected TOKEN in error, got %v", err)
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

[agents.claude-vscode]
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

func TestLoadConfigStdioWithDottedHeaders(t *testing.T) {
	// Regression: configs with dotted-key headers (headers.Foo = "bar") on
	// stdio servers must load successfully. Validate() strips the headers
	// at struct level regardless of TOML syntax.
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
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

[mcp]
[[mcp.servers]]
id = "custom"
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "some-package"]
headers.Authorization = "Bearer token"
headers."X-Custom" = "value"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCP.Servers))
	}
	if cfg.MCP.Servers[0].Headers != nil {
		t.Errorf("expected headers to be nil after sanitization, got %v", cfg.MCP.Servers[0].Headers)
	}
	if cfg.MCP.Servers[0].Command != "npx" {
		t.Errorf("expected command to be preserved, got %q", cfg.MCP.Servers[0].Command)
	}
}

func TestLoadConfigHTTPWithDottedEnv(t *testing.T) {
	// Regression: configs with dotted-key env (env.TOKEN = "val") on
	// HTTP servers must load successfully.
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
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

[mcp]
[[mcp.servers]]
id = "custom"
enabled = true
transport = "http"
url = "https://api.example.com"
env.TOKEN = "secret"
command = "leftover"
args = ["--old"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	srv := cfg.MCP.Servers[0]
	if srv.Env != nil {
		t.Errorf("expected env to be nil after sanitization, got %v", srv.Env)
	}
	if srv.Command != "" {
		t.Errorf("expected command to be empty after sanitization, got %q", srv.Command)
	}
	if srv.Args != nil {
		t.Errorf("expected args to be nil after sanitization, got %v", srv.Args)
	}
	if srv.URL != "https://api.example.com" {
		t.Errorf("expected url to be preserved, got %q", srv.URL)
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
