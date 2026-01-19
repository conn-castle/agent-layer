package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

func TestBuildMCPConfig(t *testing.T) {
	enabled := true
	root := t.TempDir()
	writePromptServerBinary(t, root)
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
						Headers: map[string]string{
							"Authorization": "Bearer ${TOKEN}",
						},
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	if cfg.Servers["agent-layer"].Command == "" {
		t.Fatalf("expected internal prompt server")
	}
	if cfg.Servers["example"].URL != "https://example.com?token=${TOKEN}" {
		t.Fatalf("unexpected url: %s", cfg.Servers["example"].URL)
	}
	if cfg.Servers["example"].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("unexpected header: %s", cfg.Servers["example"].Headers["Authorization"])
	}
}

func TestWriteMCPConfig(t *testing.T) {
	root := t.TempDir()
	writePromptServerBinary(t, root)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	if err := WriteMCPConfig(root, project); err != nil {
		t.Fatalf("WriteMCPConfig error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatalf("expected mcp.json: %v", err)
	}
}

func TestWriteMCPConfigError(t *testing.T) {
	root := t.TempDir()
	writePromptServerBinary(t, root)
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{Root: root}
	if err := WriteMCPConfig(file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteMCPConfigWriteError(t *testing.T) {
	root := t.TempDir()
	writePromptServerBinary(t, root)
	if err := os.Mkdir(filepath.Join(root, ".mcp.json"), 0o755); err != nil {
		t.Fatalf("mkdir .mcp.json: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: nil},
		},
		Root: root,
	}
	if err := WriteMCPConfig(root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigMissingEnv(t *testing.T) {
	enabled := true
	root := t.TempDir()
	writePromptServerBinary(t, root)
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
					},
				},
			},
		},
		Env:  map[string]string{},
		Root: root,
	}

	_, err := buildMCPConfig(project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigStdioServer(t *testing.T) {
	enabled := true
	root := t.TempDir()
	writePromptServerBinary(t, root)
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "stdio",
						Enabled:   &enabled,
						Transport: "stdio",
						Command:   "tool",
						Args:      []string{"--flag", "${TOKEN}"},
						Env: map[string]string{
							"TOKEN": "${TOKEN}",
						},
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	server, ok := cfg.Servers["stdio"]
	if !ok {
		t.Fatalf("expected stdio server")
	}
	if server.Command != "tool" {
		t.Fatalf("unexpected command: %s", server.Command)
	}
	if len(server.Args) != 2 || server.Args[1] != "${TOKEN}" {
		t.Fatalf("unexpected args: %#v", server.Args)
	}
	if server.Env["TOKEN"] != "${TOKEN}" {
		t.Fatalf("unexpected env: %s", server.Env["TOKEN"])
	}
}
