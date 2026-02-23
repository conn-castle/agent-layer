package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestSplitCodexHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer ${TOKEN}",
		"X-Api-Key":     "${API_KEY}",
		"X-Toolsets":    "actions,issues",
	}

	spec, err := splitCodexHeaders(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.BearerTokenEnvVar != "TOKEN" {
		t.Fatalf("expected TOKEN, got %s", spec.BearerTokenEnvVar)
	}
	if spec.EnvHeaders["X-Api-Key"] != "API_KEY" {
		t.Fatalf("expected API_KEY env header, got %q", spec.EnvHeaders["X-Api-Key"])
	}
	if spec.HTTPHeaders["X-Toolsets"] != "actions,issues" {
		t.Fatalf("expected X-Toolsets header, got %q", spec.HTTPHeaders["X-Toolsets"])
	}
}

func TestSplitCodexHeadersAuthorizationEnv(t *testing.T) {
	headers := map[string]string{
		"Authorization": "${TOKEN}",
	}

	spec, err := splitCodexHeaders(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.BearerTokenEnvVar != "" {
		t.Fatalf("unexpected bearer token env var: %q", spec.BearerTokenEnvVar)
	}
	if spec.EnvHeaders["Authorization"] != "TOKEN" {
		t.Fatalf("expected Authorization env header, got %q", spec.EnvHeaders["Authorization"])
	}
}

func TestSplitCodexHeadersErrors(t *testing.T) {
	t.Run("authorization with unsupported placeholder", func(t *testing.T) {
		_, err := splitCodexHeaders(map[string]string{"Authorization": "Token ${TOKEN}"})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
	t.Run("non-authorization with unsupported placeholder", func(t *testing.T) {
		_, err := splitCodexHeaders(map[string]string{"X-Test": "Token ${TOKEN}"})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestBuildCodexConfigHTTP(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "github",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
						Headers: map[string]string{
							"Authorization": "Bearer ${TOKEN}",
							"X-Api-Key":     "${API_KEY}",
							"X-Toolsets":    "actions,issues",
						},
					},
				},
			},
		},
		Env: map[string]string{"TOKEN": "abc", "API_KEY": "def"},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "bearer_token_env_var = \"TOKEN\"") {
		t.Fatalf("missing bearer_token_env_var in output:\n%s", output)
	}
	if !strings.Contains(output, "env_http_headers = { X-Api-Key = \"API_KEY\" }") {
		t.Fatalf("missing env_http_headers in output:\n%s", output)
	}
	if !strings.Contains(output, "http_headers = { X-Toolsets = \"actions,issues\" }") {
		t.Fatalf("missing http_headers in output:\n%s", output)
	}
	// URL should have resolved value (not placeholder) since Codex doesn't support ${VAR} in URLs.
	if !strings.Contains(output, "url = \"https://example.com?token=abc\"") {
		t.Fatalf("missing url in output:\n%s", output)
	}
}

func TestBuildCodexConfigStdio(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "local",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "tool",
						Args:      []string{"--flag", "value"},
						Env: map[string]string{
							"TOKEN": "${TOKEN}",
						},
					},
				},
			},
		},
		Env: map[string]string{"TOKEN": "abc"},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "command = \"tool\"") {
		t.Fatalf("missing command in output:\n%s", output)
	}
	if !strings.Contains(output, "args = [\"--flag\", \"value\"]") {
		t.Fatalf("missing args in output:\n%s", output)
	}
	// Env should have resolved value (not placeholder) since Codex doesn't support ${VAR} in env vars.
	if !strings.Contains(output, "env = { TOKEN = \"abc\" }") {
		t.Fatalf("missing env in output:\n%s", output)
	}
}

func TestBuildCodexConfigHeaderPrecedesModelSettings(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled:         &enabled,
					Model:           "gpt-5.3-codex",
					ReasoningEffort: "high",
				},
			},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(output, codexHeader) {
		t.Fatalf("expected codex header at top of file, got:\n%s", output)
	}

	headerIndex := strings.Index(output, "# GENERATED FILE")
	modelIndex := strings.Index(output, "model = \"gpt-5.3-codex\"")
	reasoningIndex := strings.Index(output, "model_reasoning_effort = \"high\"")
	if modelIndex == -1 || reasoningIndex == -1 {
		t.Fatalf("missing model settings in output:\n%s", output)
	}
	if headerIndex == -1 {
		t.Fatalf("missing header in output:\n%s", output)
	}
	if modelIndex < headerIndex || reasoningIndex < headerIndex {
		t.Fatalf("expected model settings after header, got:\n%s", output)
	}
}

func TestBuildCodexConfigYOLO(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, `approval_policy = "never"`) {
		t.Fatalf("expected approval_policy in output:\n%s", output)
	}
	if !strings.Contains(output, `sandbox_mode = "danger-full-access"`) {
		t.Fatalf("expected sandbox_mode in output:\n%s", output)
	}
	if !strings.Contains(output, `web_search = "live"`) {
		t.Fatalf("expected web_search in output:\n%s", output)
	}
}

func TestBuildCodexConfigAgentSpecific(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled: &enabled,
					AgentSpecific: map[string]any{
						"features": map[string]any{
							"multi_agent":        true,
							"prevent_idle_sleep": true,
						},
						"nested": map[string]any{
							"sub": map[string]any{
								"flag": true,
							},
						},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "[features]\n") {
		t.Fatalf("expected features table in output:\n%s", output)
	}
	if !strings.Contains(output, "multi_agent = true") {
		t.Fatalf("expected multi_agent in output:\n%s", output)
	}
	if !strings.Contains(output, "prevent_idle_sleep = true") {
		t.Fatalf("expected prevent_idle_sleep in output:\n%s", output)
	}
	if !strings.Contains(output, "[nested.sub]\n") {
		t.Fatalf("expected nested.sub table in output:\n%s", output)
	}
	if !strings.Contains(output, "flag = true") {
		t.Fatalf("expected nested flag in output:\n%s", output)
	}
}

func TestBuildCodexConfigAgentSpecificOverrides(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled: &enabled,
					Model:   "gpt-5.3-codex",
					AgentSpecific: map[string]any{
						"approval_policy": "untrusted",
						"model":           "override-model",
						"mcp_servers": map[string]any{
							"example": map[string]any{
								"url": "https://example.com",
							},
						},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(output, `approval_policy = "never"`) {
		t.Fatalf("expected yolo approval_policy to be suppressed by agent-specific override:\n%s", output)
	}
	if strings.Contains(output, `model = "gpt-5.3-codex"`) {
		t.Fatalf("expected model to be suppressed by agent-specific override:\n%s", output)
	}
	if !strings.Contains(output, "approval_policy = 'untrusted'") {
		t.Fatalf("expected agent-specific approval_policy in output:\n%s", output)
	}
	if !strings.Contains(output, "model = 'override-model'") {
		t.Fatalf("expected agent-specific model in output:\n%s", output)
	}
	if !strings.Contains(output, "[mcp_servers.example]\n") {
		t.Fatalf("expected agent-specific mcp_servers table in output:\n%s", output)
	}
}

func TestBuildCodexConfigAgentSpecificRootOverridesRemainTopLevelWithManagedMCP(t *testing.T) {
	t.Parallel()

	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled: &enabled,
					AgentSpecific: map[string]any{
						"approval_policy": "on-request",
					},
				},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com/mcp",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rootOverridePos := strings.Index(output, "approval_policy = 'on-request'")
	mcpTablePos := strings.Index(output, "[mcp_servers.example]\n")
	if rootOverridePos == -1 {
		t.Fatalf("expected agent-specific root override in output:\n%s", output)
	}
	if mcpTablePos == -1 {
		t.Fatalf("expected managed mcp table in output:\n%s", output)
	}
	if rootOverridePos > mcpTablePos {
		t.Fatalf("expected root override before managed mcp tables:\n%s", output)
	}

	var parsed map[string]any
	if err := toml.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("parse generated toml: %v", err)
	}
	if got, ok := parsed["approval_policy"].(string); !ok || got != "on-request" {
		t.Fatalf("expected root approval_policy on-request, got %#v", parsed["approval_policy"])
	}

	mcpValue, ok := parsed["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcp_servers map, got %#v", parsed["mcp_servers"])
	}
	exampleValue, ok := mcpValue["example"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcp_servers.example map, got %#v", mcpValue["example"])
	}
	if _, exists := exampleValue["approval_policy"]; exists {
		t.Fatalf("expected mcp_servers.example to not contain approval_policy, got %#v", exampleValue)
	}
}

func TestBuildCodexConfigUnsupportedHeaderPlaceholder(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "github",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
						Headers:   map[string]string{"X-Test": "Token ${TOKEN}"},
					},
				},
			},
		},
		Env: map[string]string{"TOKEN": "abc"},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildCodexConfigMissingEnv(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "github",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildCodexRules(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
		},
		CommandsAllow: []string{"git status"},
	}

	content := buildCodexRules(project)
	if !strings.Contains(content, "prefix_rule") {
		t.Fatalf("expected prefix_rule in output:\n%s", content)
	}

	project.Config.Approvals.Mode = "none"
	content = buildCodexRules(project)
	if strings.Contains(content, "prefix_rule") {
		t.Fatalf("expected no prefix_rule when commands disabled")
	}
}

func TestWriteCodexConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
		Env: map[string]string{},
	}
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "config.toml")); err != nil {
		t.Fatalf("expected config.toml: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config.toml mode 0600, got %o", got)
	}
}

func TestWriteCodexConfigError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := WriteCodexConfig(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCodexConfigWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir config.toml: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}
	if err := WriteCodexConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCodexRulesError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := WriteCodexRules(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCodexRulesWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rulesDir := filepath.Join(root, ".codex", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rulesDir, "default.rules"), 0o755); err != nil {
		t.Fatalf("mkdir default.rules: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}
	if err := WriteCodexRules(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildCodexConfigMultipleServers(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "server1",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "tool1",
					},
					{
						ID:        "server2",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "tool2",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	output, err := buildCodexConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have both servers with newline separator
	if !strings.Contains(output, "[mcp_servers.server1]") {
		t.Fatalf("missing server1 in output:\n%s", output)
	}
	if !strings.Contains(output, "[mcp_servers.server2]") {
		t.Fatalf("missing server2 in output:\n%s", output)
	}
}

func TestBuildCodexConfigUnsupportedTransport(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "bad",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "websocket", // unsupported
						Command:   "tool",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error for unsupported transport")
	}
	if !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildCodexConfigStdioMissingCommandEnv(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "local",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "${MISSING_CMD}",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error for missing command env var")
	}
	if !strings.Contains(err.Error(), "mcp server local") || !strings.Contains(err.Error(), "MISSING_CMD") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildCodexConfigStdioMissingArgEnv(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "local",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "tool",
						Args:      []string{"--token", "${MISSING_ARG}"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error for missing arg env var")
	}
	if !strings.Contains(err.Error(), "mcp server local") || !strings.Contains(err.Error(), "MISSING_ARG") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildCodexConfigStdioMissingEnvVarEnv(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "local",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "stdio",
						Command:   "tool",
						Env:       map[string]string{"TOKEN": "${MISSING_ENV}"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	_, err := buildCodexConfig(project)
	if err == nil {
		t.Fatalf("expected error for missing env var env")
	}
	if !strings.Contains(err.Error(), "missing environment variables: MISSING_ENV") {
		t.Fatalf("unexpected error: %v", err)
	}
}
