package config

import (
	"strings"
	"testing"
)

func TestValidateConfigErrors(t *testing.T) {
	trueVal := true
	valid := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &trueVal},
			Claude:       ClaudeConfig{Enabled: &trueVal},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &trueVal},
			Codex:        CodexConfig{Enabled: &trueVal},
			VSCode:       EnableOnlyConfig{Enabled: &trueVal},
			CopilotCLI:   AgentConfig{Enabled: &trueVal},
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
			name:    "missing antigravity enabled",
			cfg:     withAntigravityEnabled(valid, nil),
			wantErr: "agents.antigravity.enabled",
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
			name: "duplicate server id",
			cfg: withServers(valid, []MCPServer{
				{ID: "dup", Enabled: &trueVal, Transport: "http", URL: "https://example.com/one"},
				{ID: "dup", Enabled: &trueVal, Transport: "http", URL: "https://example.com/two"},
			}),
			wantErr: "duplicates",
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
			name: "invalid client",
			cfg: withServers(valid, []MCPServer{
				{ID: "x", Enabled: &trueVal, Transport: "http", URL: "https://example.com", Clients: []string{"unknown"}},
			}),
			wantErr: "invalid client",
		},
		{
			name:    "missing copilot_cli enabled",
			cfg:     withCopilotCLIEnabled(valid, nil),
			wantErr: "agents.copilot_cli.enabled",
		},
		{
			name:    "invalid claude dispatch default",
			cfg:     withClaudeDispatchDefault(valid, "copilot"),
			wantErr: "agents.claude.dispatch.default_agent",
		},
		{
			name:    "invalid codex dispatch default",
			cfg:     withCodexDispatchDefault(valid, "vscode"),
			wantErr: "agents.codex.dispatch.default_agent",
		},
		{
			name:    "invalid antigravity dispatch default",
			cfg:     withAntigravityDispatchDefault(valid, "agy"),
			wantErr: "agents.antigravity.dispatch.default_agent",
		},
		{
			name: "antigravity agent_specific model is unsupported",
			cfg: withAntigravityAgentSpecific(valid, map[string]any{
				"model": "Gemini 3.1 Pro (High)",
			}),
			wantErr: "agents.antigravity.agent_specific.model is not supported",
		},
		{
			name:    "copilot_cli reasoning effort unsupported",
			cfg:     withCopilotCLIReasoning(valid, "high"),
			wantErr: "agents.copilot_cli.reasoning_effort is not supported",
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

func withAntigravityEnabled(cfg Config, enabled *bool) Config {
	cfg.Agents.Antigravity.Enabled = enabled
	return cfg
}

func withAntigravityAgentSpecific(cfg Config, agentSpecific map[string]any) Config {
	cfg.Agents.Antigravity.AgentSpecific = agentSpecific
	return cfg
}

func withServers(cfg Config, servers []MCPServer) Config {
	cfg.MCP.Servers = servers
	return cfg
}

func withCopilotCLIEnabled(cfg Config, enabled *bool) Config {
	cfg.Agents.CopilotCLI.Enabled = enabled
	return cfg
}

func withCopilotCLIReasoning(cfg Config, effort string) Config {
	cfg.Agents.CopilotCLI.ReasoningEffort = effort
	return cfg
}

func withClaudeDispatchDefault(cfg Config, defaultAgent string) Config {
	cfg.Agents.Claude.Dispatch.DefaultAgent = defaultAgent
	return cfg
}

func withCodexDispatchDefault(cfg Config, defaultAgent string) Config {
	cfg.Agents.Codex.Dispatch.DefaultAgent = defaultAgent
	return cfg
}

func withAntigravityDispatchDefault(cfg Config, defaultAgent string) Config {
	cfg.Agents.Antigravity.Dispatch.DefaultAgent = defaultAgent
	return cfg
}

func TestValidateApprovalsYOLO(t *testing.T) {
	trueVal := true
	cfg := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeYOLO},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &trueVal},
			Claude:       ClaudeConfig{Enabled: &trueVal},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &trueVal},
			Codex:        CodexConfig{Enabled: &trueVal},
			VSCode:       EnableOnlyConfig{Enabled: &trueVal},
			CopilotCLI:   AgentConfig{Enabled: &trueVal},
		},
	}
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("expected yolo to be valid, got %v", err)
	}
}

func TestValidateClaudeReasoningEffortWithOpusModel(t *testing.T) {
	trueVal := true
	cfg := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &trueVal},
			Claude:       ClaudeConfig{Enabled: &trueVal, Model: "opus", ReasoningEffort: "high"},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &trueVal},
			Codex:        CodexConfig{Enabled: &trueVal},
			VSCode:       EnableOnlyConfig{Enabled: &trueVal},
			CopilotCLI:   AgentConfig{Enabled: &trueVal},
		},
	}
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("expected claude opus reasoning effort to be valid, got %v", err)
	}
}

func TestValidateClaudeReasoningEffortWithoutOpusModelAllowed(t *testing.T) {
	// Agent Layer no longer gates reasoning_effort on the model: an empty model
	// and non-Opus models (sonnet, haiku) all validate. Claude Code is the
	// authority on which model/effort combinations apply. This guards against
	// re-introducing the old Opus-only hard error.
	trueVal := true
	base := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &trueVal},
			Claude:       ClaudeConfig{Enabled: &trueVal},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &trueVal},
			Codex:        CodexConfig{Enabled: &trueVal},
			VSCode:       EnableOnlyConfig{Enabled: &trueVal},
			CopilotCLI:   AgentConfig{Enabled: &trueVal},
		},
	}
	cases := []struct {
		name   string
		model  string
		effort string
	}{
		{"empty model", "", "high"},
		{"sonnet model", "sonnet", "max"},
		{"haiku model", "haiku", "low"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Agents.Claude.Model = tc.model
			cfg.Agents.Claude.ReasoningEffort = tc.effort
			if err := cfg.Validate("config.toml"); err != nil {
				t.Fatalf("expected reasoning_effort %q with model %q to be valid, got %v", tc.effort, tc.model, err)
			}
		})
	}
}

func TestValidateClaudeReasoningEffortMaxWithOpusModel(t *testing.T) {
	trueVal := true
	cfg := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &trueVal},
			Claude:       ClaudeConfig{Enabled: &trueVal, Model: "opus", ReasoningEffort: "max"},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &trueVal},
			Codex:        CodexConfig{Enabled: &trueVal},
			VSCode:       EnableOnlyConfig{Enabled: &trueVal},
			CopilotCLI:   AgentConfig{Enabled: &trueVal},
		},
	}
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("expected claude opus max reasoning effort to be valid, got %v", err)
	}
}

func TestValidateWarningsThresholds(t *testing.T) {
	enabled := true
	base := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			CopilotCLI:   AgentConfig{Enabled: &enabled},
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
		{
			name: "invalid warning noise mode",
			set: func(cfg *Config) {
				cfg.Warnings.NoiseMode = "verbose"
			},
			errContains: "warnings.noise_mode",
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

func TestValidateWarningsNoiseModeQuiet(t *testing.T) {
	enabled := true
	cfg := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			CopilotCLI:   AgentConfig{Enabled: &enabled},
		},
		Warnings: WarningsConfig{NoiseMode: "quiet"},
	}
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("expected quiet noise_mode to be valid, got %v", err)
	}
}

func TestValidateSanitizesTransportIncompatibleFields(t *testing.T) {
	enabled := true
	base := Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			CopilotCLI:   AgentConfig{Enabled: &enabled},
		},
	}

	t.Run("stdio strips headers url and http_transport", func(t *testing.T) {
		cfg := base
		cfg.MCP.Servers = []MCPServer{{
			ID:            "s1",
			Enabled:       &enabled,
			Transport:     "stdio",
			Command:       "tool",
			Headers:       map[string]string{"X-Key": "val"},
			URL:           "https://leftover.example.com",
			HTTPTransport: "sse",
		}}
		if err := cfg.Validate("config.toml"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		srv := cfg.MCP.Servers[0]
		if srv.Headers != nil {
			t.Errorf("expected headers to be nil, got %v", srv.Headers)
		}
		if srv.URL != "" {
			t.Errorf("expected url to be empty, got %q", srv.URL)
		}
		if srv.HTTPTransport != "" {
			t.Errorf("expected http_transport to be empty, got %q", srv.HTTPTransport)
		}
		if srv.Command != "tool" {
			t.Errorf("expected command to be preserved, got %q", srv.Command)
		}
	})

	t.Run("http strips command args and env", func(t *testing.T) {
		cfg := base
		cfg.MCP.Servers = []MCPServer{{
			ID:        "s1",
			Enabled:   &enabled,
			Transport: "http",
			URL:       "https://example.com",
			Command:   "leftover",
			Args:      []string{"--flag"},
			Env:       map[string]string{"TOKEN": "x"},
		}}
		if err := cfg.Validate("config.toml"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		srv := cfg.MCP.Servers[0]
		if srv.Command != "" {
			t.Errorf("expected command to be empty, got %q", srv.Command)
		}
		if srv.Args != nil {
			t.Errorf("expected args to be nil, got %v", srv.Args)
		}
		if srv.Env != nil {
			t.Errorf("expected env to be nil, got %v", srv.Env)
		}
		if srv.URL != "https://example.com" {
			t.Errorf("expected url to be preserved, got %q", srv.URL)
		}
	})
}
