package sync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildCopilotMCPConfigStdio(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "ripgrep",
						Enabled:   &enabled,
						Clients:   []string{"copilot"},
						Transport: "stdio",
						Command:   "npx",
						Args:      []string{"-y", "mcp-ripgrep@0.4.0"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	cfg, err := buildCopilotMCPConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := cfg.Servers["ripgrep"]
	if !ok {
		t.Fatalf("expected ripgrep server entry")
	}
	if entry.Type != "stdio" {
		t.Fatalf("expected type stdio, got %q", entry.Type)
	}
	if entry.Command != "npx" {
		t.Fatalf("expected command npx, got %q", entry.Command)
	}
	if len(entry.Args) != 2 || entry.Args[0] != "-y" {
		t.Fatalf("unexpected args: %v", entry.Args)
	}
	if len(entry.Tools) != 1 || entry.Tools[0] != "*" {
		t.Fatalf("expected tools [\"*\"], got %v", entry.Tools)
	}
}

func TestBuildCopilotMCPConfigHTTP(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Clients:   []string{"copilot"},
						Transport: "http",
						URL:       "https://example.com/mcp",
						Headers:   map[string]string{"Authorization": "Bearer ${AL_TOKEN}"},
					},
				},
			},
		},
		Env: map[string]string{"AL_TOKEN": "secret"},
	}

	cfg, err := buildCopilotMCPConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := cfg.Servers["example"]
	if !ok {
		t.Fatalf("expected example server entry")
	}
	if entry.Type != "http" {
		t.Fatalf("expected type http, got %q", entry.Type)
	}
	if entry.URL != "https://example.com/mcp" {
		t.Fatalf("expected URL, got %q", entry.URL)
	}
	if entry.Headers["Authorization"] != "Bearer ${AL_TOKEN}" {
		t.Fatalf("expected raw placeholder in headers, got %q", entry.Headers["Authorization"])
	}
}

func TestBuildCopilotMCPConfigExcludesOtherClients(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "claude-only",
						Enabled:   &enabled,
						Clients:   []string{"claude"},
						Transport: "stdio",
						Command:   "tool",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	cfg, err := buildCopilotMCPConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Servers) != 0 {
		t.Fatalf("expected no servers for copilot, got %d", len(cfg.Servers))
	}
}

func TestWriteCopilotMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "test",
						Enabled:   &enabled,
						Transport: "stdio",
						Command:   "test-tool",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	sys := &RealSystem{}
	if err := WriteCopilotMCPConfig(sys, root, project); err != nil {
		t.Fatalf("WriteCopilotMCPConfig error: %v", err)
	}

	path := filepath.Join(root, ".copilot", "mcp-config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read generated file: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	servers, ok := result["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers object, got %T", result["mcpServers"])
	}
	if _, ok := servers["test"]; !ok {
		t.Fatalf("expected test server in mcpServers")
	}
}

func TestWriteCopilotMCPConfigMkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Place a file where the .copilot directory would be created.
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{Env: map[string]string{}}
	if err := WriteCopilotMCPConfig(&RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCopilotMCPConfigWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	copilotDir := filepath.Join(root, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a directory where the file would be written.
	if err := os.Mkdir(filepath.Join(copilotDir, "mcp-config.json"), 0o755); err != nil {
		t.Fatalf("mkdir mcp-config.json: %v", err)
	}
	project := &config.ProjectConfig{Env: map[string]string{}}
	if err := WriteCopilotMCPConfig(&RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCopilotMCPConfigMarshalError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal boom")
		},
	}
	project := &config.ProjectConfig{Env: map[string]string{}}
	err := WriteCopilotMCPConfig(sys, root, project)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestBuildCopilotSkill(t *testing.T) {
	t.Parallel()
	skill := config.Skill{
		Name:        "code-audit",
		Description: "Run a code quality audit.",
		Body:        "Audit the code.\n",
	}

	content, err := buildCopilotSkill(skill)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if content == "" {
		t.Fatalf("expected non-empty content")
	}
	if !strings.Contains(content, "name: code-audit") {
		t.Fatalf("expected name in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "Run a code quality audit.") {
		t.Fatalf("expected description in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "Audit the code.") {
		t.Fatalf("expected body in content, got:\n%s", content)
	}
}

func TestWriteCopilotSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skills := []config.Skill{
		{
			Name:        "test-skill",
			Description: "A test skill.",
			Body:        "Do the thing.\n",
		},
	}

	sys := &RealSystem{}
	if err := WriteCopilotSkills(sys, root, skills); err != nil {
		t.Fatalf("WriteCopilotSkills error: %v", err)
	}

	path := filepath.Join(root, ".github", "skills", "test-skill", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read generated skill: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "name: test-skill") {
		t.Fatalf("expected name in skill, got:\n%s", content)
	}
}

func TestWriteCopilotSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteCopilotSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCopilotSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillDir := filepath.Join(root, ".github", "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(skillDir, "SKILL.md"), 0o755); err != nil {
		t.Fatalf("mkdir SKILL.md: %v", err)
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteCopilotSkills(RealSystem{}, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCopilotSkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".github", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteCopilotSkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir creation failure")
	}
	if !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}
