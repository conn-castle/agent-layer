package config

import (
	"strings"
	"testing"
	"time"
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
			Grok:         GrokConfig{Enabled: &trueVal},
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
			name:    "missing grok enabled",
			cfg:     withGrokEnabled(valid, nil),
			wantErr: "agents.grok.enabled",
		},
		{
			name:    "invalid dispatch max depth",
			cfg:     withDispatchMaxDepth(valid, 0),
			wantErr: "dispatch.max_depth",
		},
		{
			name:    "non-positive mcp wait timeout",
			cfg:     withDispatchMCPTimeouts(valid, ptr(0), nil),
			wantErr: "dispatch.mcp_wait_timeout_minutes must be greater than zero",
		},
		{
			name:    "non-positive mcp tool timeout",
			cfg:     withDispatchMCPTimeouts(valid, nil, ptr(-1)),
			wantErr: "dispatch.mcp_tool_timeout_minutes must be greater than zero",
		},
		{
			name:    "mcp tool timeout not above wait timeout",
			cfg:     withDispatchMCPTimeouts(valid, ptr(45), ptr(45)),
			wantErr: "must be greater than dispatch.mcp_wait_timeout_minutes",
		},
		{
			// A wait raised above the default hard bound must fail even though the
			// tool timeout itself was never configured: the resolved default is
			// what the server would actually enforce.
			name:    "configured wait exceeds default tool timeout",
			cfg:     withDispatchMCPTimeouts(valid, ptr(60), nil),
			wantErr: "must be greater than dispatch.mcp_wait_timeout_minutes",
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
		{
			name: "grok agent_specific permission is unsupported",
			cfg: withGrokAgentSpecific(valid, map[string]any{
				"permission": map[string]any{"allow": []string{"Bash(git *)"}},
			}),
			wantErr: "agents.grok.agent_specific.permission is not supported",
		},
		{
			name: "grok agent_specific mcp_servers is unsupported",
			cfg: withGrokAgentSpecific(valid, map[string]any{
				"mcp_servers": map[string]any{"example": map[string]any{"url": "https://example.com"}},
			}),
			wantErr: "agents.grok.agent_specific.mcp_servers is not supported",
		},
		{
			name: "grok agent_specific hooks is unsupported",
			cfg: withGrokAgentSpecific(valid, map[string]any{
				"hooks": map[string]any{},
			}),
			wantErr: "agents.grok.agent_specific.hooks is not supported",
		},
		{
			name: "grok user-level agent_specific setting is unsupported",
			cfg: withGrokAgentSpecific(valid, map[string]any{
				"ui": map[string]any{"screen_mode": "minimal"},
			}),
			wantErr: "agents.grok.agent_specific.ui is not supported",
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

func TestValidateRequiresEveryCoreAgentEnablementDecision(t *testing.T) {
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
			Grok:         GrokConfig{Enabled: &enabled},
		},
	}
	for _, test := range []struct {
		name  string
		agent string
		omit  func(*Config)
	}{
		{name: "claude", agent: "claude", omit: func(cfg *Config) { cfg.Agents.Claude.Enabled = nil }},
		{name: "claude vscode", agent: "claude_vscode", omit: func(cfg *Config) { cfg.Agents.ClaudeVSCode.Enabled = nil }},
		{name: "codex", agent: "codex", omit: func(cfg *Config) { cfg.Agents.Codex.Enabled = nil }},
		{name: "VS Code", agent: strings.Join([]string{"vs", "code"}, ""), omit: func(cfg *Config) { cfg.Agents.VSCode.Enabled = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.omit(&cfg)
			key := strings.Join([]string{"agents", test.agent, "enabled"}, ".")
			if err := cfg.Validate("config.toml"); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("missing %s error = %v", key, err)
			}
		})
	}
}

func TestValidateGrokPluginPassthrough(t *testing.T) {
	cfg := validTimeoutConfig()
	cfg.Agents.Grok.AgentSpecific = map[string]any{
		"plugins": map[string]any{"example": map[string]any{"enabled": true}},
	}
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("valid Grok plugin passthrough rejected: %v", err)
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

func withGrokEnabled(cfg Config, enabled *bool) Config {
	cfg.Agents.Grok.Enabled = enabled
	return cfg
}

func withGrokAgentSpecific(cfg Config, agentSpecific map[string]any) Config {
	cfg.Agents.Grok.AgentSpecific = agentSpecific
	return cfg
}

func withCopilotCLIReasoning(cfg Config, effort string) Config {
	cfg.Agents.CopilotCLI.ReasoningEffort = effort
	return cfg
}

func withDispatchMaxDepth(cfg Config, maxDepth int) Config {
	cfg.Dispatch.MaxDepth = &maxDepth
	return cfg
}

func withDispatchMCPTimeouts(cfg Config, wait *int, tool *int) Config {
	cfg.Dispatch.MCPWaitTimeoutMinutes = wait
	cfg.Dispatch.MCPToolTimeoutMinutes = tool
	return cfg
}

func ptr(value int) *int { return &value }

// TestDispatchMCPTimeoutDefaults proves that a config predating the MCP
// interface keeps working: both accessors resolve to the documented product
// defaults, and the hard bound stays above the bounded wait.
func TestDispatchMCPTimeoutDefaults(t *testing.T) {
	var cfg Config
	if got := DispatchMCPWaitTimeout(cfg); got != 30*time.Minute {
		t.Fatalf("default wait timeout = %s, want 30m", got)
	}
	if got := DispatchMCPToolTimeout(cfg); got != 40*time.Minute {
		t.Fatalf("default tool timeout = %s, want 40m", got)
	}
	defaults := withDispatchMCPTimeouts(validTimeoutConfig(), nil, nil)
	if err := defaults.Validate("config.toml"); err != nil {
		t.Fatalf("omitted MCP timeouts must validate: %v", err)
	}
}

// TestDispatchMCPTimeoutOverrides proves custom valid minute values reach both
// accessors so projection and the server-side guard agree with config.
func TestDispatchMCPTimeoutOverrides(t *testing.T) {
	cfg := withDispatchMCPTimeouts(validTimeoutConfig(), ptr(5), ptr(9))
	if err := cfg.Validate("config.toml"); err != nil {
		t.Fatalf("valid MCP timeouts rejected: %v", err)
	}
	if got := DispatchMCPWaitTimeout(cfg); got != 5*time.Minute {
		t.Fatalf("wait timeout = %s, want 5m", got)
	}
	if got := DispatchMCPToolTimeout(cfg); got != 9*time.Minute {
		t.Fatalf("tool timeout = %s, want 9m", got)
	}
}

func validTimeoutConfig() Config {
	enabled := true
	return Config{
		Approvals: ApprovalsConfig{Mode: ApprovalModeAll},
		Agents: AgentsConfig{
			Antigravity:  AntigravityConfig{Enabled: &enabled},
			Claude:       ClaudeConfig{Enabled: &enabled},
			ClaudeVSCode: EnableOnlyConfig{Enabled: &enabled},
			Codex:        CodexConfig{Enabled: &enabled},
			VSCode:       EnableOnlyConfig{Enabled: &enabled},
			CopilotCLI:   AgentConfig{Enabled: &enabled},
			Grok:         GrokConfig{Enabled: &enabled},
		},
	}
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
			Grok:         GrokConfig{Enabled: &trueVal},
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
			Grok:         GrokConfig{Enabled: &trueVal},
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
			Grok:         GrokConfig{Enabled: &trueVal},
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
			Grok:         GrokConfig{Enabled: &trueVal},
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
			Grok:         GrokConfig{Enabled: &enabled},
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
			Grok:         GrokConfig{Enabled: &enabled},
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
			Grok:         GrokConfig{Enabled: &enabled},
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
