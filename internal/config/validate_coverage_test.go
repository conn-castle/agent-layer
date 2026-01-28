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
			Gemini:      AgentConfig{Enabled: &enabled},
			Claude:      AgentConfig{Enabled: &enabled},
			Codex:       CodexConfig{Enabled: &enabled},
			VSCode:      AgentConfig{Enabled: &enabled},
			Antigravity: AgentConfig{Enabled: &enabled},
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
			Gemini:      AgentConfig{Enabled: &enabled},
			Claude:      AgentConfig{Enabled: &enabled},
			Codex:       CodexConfig{Enabled: &enabled},
			VSCode:      AgentConfig{Enabled: &enabled},
			Antigravity: AgentConfig{Enabled: &enabled},
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
			name:        "http with command",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "http", URL: "x", Command: "c"},
			errContains: "command/args are not allowed for http",
		},
		{
			name:        "http with args",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "http", URL: "x", Args: []string{"a"}},
			errContains: "command/args are not allowed for http",
		},
		{
			name:        "http with env",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "http", URL: "x", Env: map[string]string{"k": "v"}},
			errContains: "env is not allowed for http",
		},
		{
			name:        "http invalid http_transport",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "http", URL: "x", HTTPTransport: "grpc"},
			errContains: "http_transport must be sse or streamable",
		},
		{
			name:        "stdio with url",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "c", URL: "u"},
			errContains: "url is not allowed for stdio",
		},
		{
			name:        "stdio with headers",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "c", Headers: map[string]string{"k": "v"}},
			errContains: "headers are not allowed for stdio",
		},
		{
			name:        "stdio with http_transport",
			server:      MCPServer{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "c", HTTPTransport: "sse"},
			errContains: "http_transport is only valid for http transport",
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
