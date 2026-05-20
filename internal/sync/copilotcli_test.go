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
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
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
	if err := os.MkdirAll(copilotDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a directory where the file would be written.
	if err := os.Mkdir(filepath.Join(copilotDir, "mcp-config.json"), 0o700); err != nil {
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

func TestCleanCopilotOutputsRemovesMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := RealSystem{}

	// Set up artifacts as if Copilot were previously enabled.
	copilotDir := filepath.Join(root, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o700); err != nil {
		t.Fatalf("mkdir .copilot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(copilotDir, "mcp-config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write mcp-config: %v", err)
	}

	if err := CleanCopilotOutputs(sys, root); err != nil {
		t.Fatalf("CleanCopilotOutputs error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(copilotDir, "mcp-config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected mcp-config.json to be removed")
	}
}

func TestCleanCopilotOutputsNoArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// No .copilot/ exists — should not error.
	if err := CleanCopilotOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("CleanCopilotOutputs error on clean dir: %v", err)
	}
}
