package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

func TestBuildCodexConfig_UnsupportedTransport(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "unknown",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "pigeon",
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
	if !strings.Contains(err.Error(), "unsupported transport pigeon") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTomlHelpers_Empty(t *testing.T) {
	t.Parallel()
	if s := tomlStringArray([]string{}); s != "[]" {
		t.Fatalf("expected [], got %q", s)
	}
	if s := tomlInlineTable(map[string]string{}); s != "{}" {
		t.Fatalf("expected {}, got %q", s)
	}
}

func TestSplitCodexHeaders_Empty(t *testing.T) {
	t.Parallel()
	spec, err := splitCodexHeaders(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.BearerTokenEnvVar != "" {
		t.Fatalf("expected empty bearer token env var, got %q", spec.BearerTokenEnvVar)
	}
	if spec.EnvHeaders != nil {
		t.Fatalf("expected nil env headers, got %v", spec.EnvHeaders)
	}
	if spec.HTTPHeaders != nil {
		t.Fatalf("expected nil http headers, got %v", spec.HTTPHeaders)
	}
}

func TestBuildCodexConfig_ModelSettings(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled:         &enabled,
					Model:           "claude-3-5-sonnet",
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
	if !strings.Contains(output, "model = \"claude-3-5-sonnet\"") {
		t.Fatalf("missing model setting")
	}
	if !strings.Contains(output, "model_reasoning_effort = \"high\"") {
		t.Fatalf("missing reasoning setting")
	}
}

func TestWriteCodexConfig_MkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Create .codex as a file to force MkdirAll to fail
	if err := os.WriteFile(filepath.Join(root, ".codex"), []byte("file"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{}
	if err := WriteCodexConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error from MkdirAll")
	}
}

func TestBuildCodexRules_EmptyCommand(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
		},
		CommandsAllow: []string{"   ", "git status"}, // One empty/whitespace command
	}

	content := buildCodexRules(project)
	if !strings.Contains(content, "\"git\", \"status\"") {
		t.Fatalf("expected git status in rules:\n%s", content)
	}
	// The empty command should be skipped, so no empty pattern
	if strings.Contains(content, "pattern=[]") {
		t.Fatalf("unexpected empty pattern")
	}
}

func TestWriteCodexConfig_BuildError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "http",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com?token=${MISSING}",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error from buildCodexConfig")
	}
}

func TestWriteCodexHTTPServer_MissingEnv(t *testing.T) {
	t.Parallel()
	var builder strings.Builder
	server := projection.ResolvedMCPServer{
		ID:        "http",
		Transport: config.TransportHTTP,
		URL:       "https://example.com?token=${MISSING}",
	}
	err := writeCodexHTTPServer(&builder, server, map[string]string{})
	if err == nil {
		t.Fatalf("expected error for missing URL env")
	}
}

func TestWriteCodexStdioServer_SubstitutionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		server projection.ResolvedMCPServer
	}{
		{
			name: "command env missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "${MISSING}",
			},
		},
		{
			name: "arg env missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "tool",
				Args:      []string{"--token", "${MISSING}"},
			},
		},
		{
			name: "env var missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "tool",
				Env:       map[string]string{"TOKEN": "${MISSING}"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			if err := writeCodexStdioServer(&builder, tt.server, map[string]string{}); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestSplitCodexHeaders_AuthorizationStatic(t *testing.T) {
	t.Parallel()
	headers := map[string]string{
		"Authorization": "Bearer abc",
	}
	spec, err := splitCodexHeaders(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.HTTPHeaders["Authorization"] != "Bearer abc" {
		t.Fatalf("expected Authorization in HTTP headers, got %v", spec.HTTPHeaders)
	}
}

func TestExtractBearerEnvPlaceholder_EdgeCases(t *testing.T) {
	t.Parallel()
	if _, ok := extractBearerEnvPlaceholder("Bearer"); ok {
		t.Fatalf("expected false for short bearer value")
	}
	if _, ok := extractBearerEnvPlaceholder("Token ${TOKEN}"); ok {
		t.Fatalf("expected false for non-bearer prefix")
	}
}
