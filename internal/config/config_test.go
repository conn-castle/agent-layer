package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseConfigFile reads and parses a config.toml at path, mirroring the
// production read+parse path (LoadConfigFS -> ParseConfig) used by callers.
func parseConfigFile(t *testing.T, path string) (*Config, error) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path under t.TempDir().
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return ParseConfig(data, path)
}

func TestLoadConfigValid(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
[approvals]
mode = "all"

[dispatch]
max_depth = 2

[agents.antigravity]
enabled = true
[agents.antigravity.dispatch]
default_agent = "claude"

[agents.claude]
enabled = true
[agents.claude.dispatch]
default_agent = "random"

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true
model = "gpt-5.3-codex"
reasoning_effort = "high"
[agents.codex.dispatch]
default_agent = "antigravity"

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false

[mcp]
[[mcp.servers]]
id = "local"
enabled = false
transport = "stdio"
command = "tool"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := parseConfigFile(t, path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Approvals.Mode != ApprovalModeAll {
		t.Fatalf("unexpected approvals mode: %s", cfg.Approvals.Mode)
	}
	if cfg.Agents.Antigravity.Enabled == nil || !*cfg.Agents.Antigravity.Enabled {
		t.Fatalf("expected antigravity enabled")
	}
	if cfg.Agents.Antigravity.Dispatch.DefaultAgent != "claude" {
		t.Fatalf("unexpected antigravity dispatch default: %q", cfg.Agents.Antigravity.Dispatch.DefaultAgent)
	}
	if cfg.Agents.Claude.Dispatch.DefaultAgent != "random" {
		t.Fatalf("unexpected claude dispatch default: %q", cfg.Agents.Claude.Dispatch.DefaultAgent)
	}
	if cfg.Agents.Codex.Dispatch.DefaultAgent != "antigravity" {
		t.Fatalf("unexpected codex dispatch default: %q", cfg.Agents.Codex.Dispatch.DefaultAgent)
	}
	if cfg.Dispatch.MaxDepth == nil || *cfg.Dispatch.MaxDepth != 2 {
		t.Fatalf("unexpected dispatch max_depth: %#v", cfg.Dispatch.MaxDepth)
	}
	if got := DispatchMaxDepth(*cfg); got != 2 {
		t.Fatalf("DispatchMaxDepth = %d, want 2", got)
	}
}

func TestDispatchMaxDepthDefaultsToOne(t *testing.T) {
	if got := DispatchMaxDepth(Config{}); got != DefaultDispatchMaxDepth {
		t.Fatalf("DispatchMaxDepth = %d, want %d", got, DefaultDispatchMaxDepth)
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

func TestLoadConfigStdioWithDottedHeaders(t *testing.T) {
	// Regression: configs with dotted-key headers (headers.Foo = "bar") on
	// stdio servers must load successfully. Validate() strips the headers
	// at struct level regardless of TOML syntax.
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	content := `
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := parseConfigFile(t, path)
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := parseConfigFile(t, path)
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := parseConfigFile(t, path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected invalid config error, got: %v", err)
	}
}
