package config

import (
	"strings"
	"testing"
)

func TestValidate_TopLevelErrors(t *testing.T) {
	enabled := true
	valid := Config{
		Approvals: ApprovalsConfig{Mode: "all"},
		Agents: AgentsConfig{
			Gemini:       AgentConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			Antigravity:  EnableOnlyConfig{Enabled: &enabled},
		},
	}

	tests := []struct {
		name        string
		modify      func(*Config)
		errContains string
	}{
		{
			name:        "invalid approval mode",
			modify:      func(c *Config) { c.Approvals.Mode = "invalid" },
			errContains: "approvals.mode must be one of",
		},
		{
			name:        "missing gemini enabled",
			modify:      func(c *Config) { c.Agents.Gemini.Enabled = nil },
			errContains: "agents.gemini.enabled is required",
		},
		{
			name:        "missing claude enabled",
			modify:      func(c *Config) { c.Agents.Claude.Enabled = nil },
			errContains: "agents.claude.enabled is required",
		},
		{
			name:        "missing claude-vscode enabled",
			modify:      func(c *Config) { c.Agents.ClaudeVSCode.Enabled = nil },
			errContains: "agents.claude-vscode.enabled is required",
		},
		{
			name:        "missing codex enabled",
			modify:      func(c *Config) { c.Agents.Codex.Enabled = nil },
			errContains: "agents.codex.enabled is required",
		},
		{
			name:        "missing vscode enabled",
			modify:      func(c *Config) { c.Agents.VSCode.Enabled = nil },
			errContains: "agents.vscode.enabled is required",
		},
		{
			name:        "missing antigravity enabled",
			modify:      func(c *Config) { c.Agents.Antigravity.Enabled = nil },
			errContains: "agents.antigravity.enabled is required",
		},
		{
			name: "missing mcp id",
			modify: func(c *Config) {
				c.MCP.Servers = []MCPServer{{ID: "", Enabled: &enabled}}
			},
			errContains: "id is required",
		},
		{
			name: "missing mcp enabled",
			modify: func(c *Config) {
				c.MCP.Servers = []MCPServer{{ID: "s1", Enabled: nil}}
			},
			errContains: "enabled is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid // Shallow copy; modify overwrites pointers in cfg, leaving valid untouched.
			tt.modify(&cfg)
			err := cfg.Validate("config.toml")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

func TestValidate_MCPServerErrors(t *testing.T) {
	enabled := true
	baseConfig := Config{
		Approvals: ApprovalsConfig{Mode: "all"},
		Agents: AgentsConfig{
			Gemini:       AgentConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			Antigravity:  EnableOnlyConfig{Enabled: &enabled},
		},
	}

	tests := []struct {
		name        string
		server      MCPServer
		errContains string
	}{
		{
			name:        "reserved id",
			server:      MCPServer{ID: "agent-layer", Enabled: &enabled, Transport: "http", URL: "x"},
			errContains: "reserved for the internal prompt server",
		},
		{
			name:        "http invalid http_transport",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "http", URL: "x", HTTPTransport: "grpc"},
			errContains: "http_transport must be sse or streamable",
		},
		{
			name:        "invalid client",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "c", Clients: []string{"invalid"}},
			errContains: "invalid client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig
			cfg.MCP.Servers = []MCPServer{tt.server}
			err := cfg.Validate("config.toml")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}
