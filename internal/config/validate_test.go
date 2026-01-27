package config

import (
	"strings"
	"testing"
)

func TestValidateConfigErrors(t *testing.T) {
	trueVal := true
	falseVal := false
	valid := Config{
		Approvals: ApprovalsConfig{Mode: "all"},
		Agents: AgentsConfig{
			Gemini:      AgentConfig{Enabled: &trueVal},
			Claude:      AgentConfig{Enabled: &trueVal},
			Codex:       CodexConfig{Enabled: &trueVal},
			VSCode:      AgentConfig{Enabled: &trueVal},
			Antigravity: AgentConfig{Enabled: &falseVal},
		},
		MCP: MCPConfig{},
	}

	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "invalid approvals",
			cfg:     withApprovals(valid, "bad"),
			wantErr: "approvals.mode",
		},
		{
			name:    "missing enabled",
			cfg:     withGeminiEnabled(valid, nil),
			wantErr: "agents.gemini.enabled",
		},
		{
			name: "missing server id",
			cfg: withServers(valid, []MCPServer{
				{Enabled: &trueVal, Transport: "http", URL: "https://example.com"},
			}),
			wantErr: "mcp.servers[0].id",
		},
		{
			name: "reserved server id",
			cfg: withServers(valid, []MCPServer{
				{ID: "agent-layer", Enabled: &trueVal, Transport: "http", URL: "https://example.com"},
			}),
			wantErr: "reserved",
		},
		{
			name: "missing server enabled",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Transport: "http", URL: "https://example.com"},
			}),
			wantErr: "enabled is required",
		},
		{
			name: "invalid transport",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "ftp"},
			}),
			wantErr: "transport must be http or stdio",
		},
		{
			name: "http missing url",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http"},
			}),
			wantErr: "url is required",
		},
		{
			name: "http with command",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http", URL: "https://example.com", Command: "tool"},
			}),
			wantErr: "command/args are not allowed",
		},
		{
			name: "http with env",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http", URL: "https://example.com", Env: map[string]string{"TOKEN": "x"}},
			}),
			wantErr: "env is not allowed",
		},
		{
			name: "http invalid http_transport",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http", URL: "https://example.com", HTTPTransport: "grpc"},
			}),
			wantErr: "http_transport must be sse or streamable",
		},
		{
			name: "stdio missing command",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "stdio"},
			}),
			wantErr: "command is required",
		},
		{
			name: "stdio with url",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "stdio", Command: "tool", URL: "https://example.com"},
			}),
			wantErr: "url is not allowed",
		},
		{
			name: "stdio with headers",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "stdio", Command: "tool", Headers: map[string]string{"X": "1"}},
			}),
			wantErr: "headers are not allowed",
		},
		{
			name: "stdio with http_transport",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "stdio", Command: "tool", HTTPTransport: "sse"},
			}),
			wantErr: "http_transport is only valid for http transport",
		},
		{
			name: "invalid client",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http", URL: "https://example.com", Clients: []string{"unknown"}},
			}),
			wantErr: "invalid client",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate("config.toml"); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func withApprovals(cfg Config, mode string) Config {
	cfg.Approvals.Mode = mode
	return cfg
}

func withGeminiEnabled(cfg Config, enabled *bool) Config {
	cfg.Agents.Gemini.Enabled = enabled
	return cfg
}

func withServers(cfg Config, servers []MCPServer) Config {
	cfg.MCP.Servers = servers
	return cfg
}

func TestValidateWarningsThresholds(t *testing.T) {
	enabled := true
	base := Config{
		Approvals: ApprovalsConfig{Mode: "all"},
		Agents: AgentsConfig{
			Gemini:      AgentConfig{Enabled: &enabled},
			Claude:      AgentConfig{Enabled: &enabled},
			Codex:       CodexConfig{Enabled: &enabled},
			VSCode:      AgentConfig{Enabled: &enabled},
			Antigravity: AgentConfig{Enabled: &enabled},
		},
	}

	intPtr := func(value int) *int { return &value }

	tests := []struct {
		name        string
		set         func(*Config)
		errContains string
	}{
		{
			name: "instruction token threshold",
			set: func(cfg *Config) {
				cfg.Warnings.InstructionTokenThreshold = intPtr(0)
			},
			errContains: "warnings.instruction_token_threshold",
		},
		{
			name: "mcp server threshold",
			set: func(cfg *Config) {
				cfg.Warnings.MCPServerThreshold = intPtr(-1)
			},
			errContains: "warnings.mcp_server_threshold",
		},
		{
			name: "mcp tools total threshold",
			set: func(cfg *Config) {
				cfg.Warnings.MCPToolsTotalThreshold = intPtr(0)
			},
			errContains: "warnings.mcp_tools_total_threshold",
		},
		{
			name: "mcp server tools threshold",
			set: func(cfg *Config) {
				cfg.Warnings.MCPServerToolsThreshold = intPtr(0)
			},
			errContains: "warnings.mcp_server_tools_threshold",
		},
		{
			name: "mcp schema tokens total threshold",
			set: func(cfg *Config) {
				cfg.Warnings.MCPSchemaTokensTotalThreshold = intPtr(0)
			},
			errContains: "warnings.mcp_schema_tokens_total_threshold",
		},
		{
			name: "mcp schema tokens server threshold",
			set: func(cfg *Config) {
				cfg.Warnings.MCPSchemaTokensServerThreshold = intPtr(0)
			},
			errContains: "warnings.mcp_schema_tokens_server_threshold",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.set(&cfg)
			err := cfg.Validate("config.toml")
			if err == nil || !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
			}
		})
	}
}
