package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

func TestBuildCodexConfig_UnsupportedTransport(t *testing.T) {
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
	if s := tomlStringArray([]string{}); s != "[]" {
		t.Fatalf("expected [], got %q", s)
	}
	if s := tomlInlineTable(map[string]string{}); s != "{}" {
		t.Fatalf("expected {}, got %q", s)
	}
}

func TestExtractBearerEnvVar_Empty(t *testing.T) {
	// If no Authorization header, return empty string, nil error
	val, err := extractBearerEnvVar(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty, got %q", val)
	}
}

func TestBuildCodexConfig_ModelSettings(t *testing.T) {
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
	root := t.TempDir()
	// Create .codex as a file to force MkdirAll to fail
	if err := os.WriteFile(filepath.Join(root, ".codex"), []byte("file"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{}
	if err := WriteCodexConfig(root, project); err == nil {
		t.Fatalf("expected error from MkdirAll")
	}
}

func TestBuildCodexRules_EmptyCommand(t *testing.T) {
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
