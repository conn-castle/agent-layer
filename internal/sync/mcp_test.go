package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildMCPConfig(t *testing.T) {
	t.Parallel()
	enabled := true
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	root := t.TempDir()
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

	cfg, err := buildMCPConfig(sys, project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	if cfg.Servers["agent-layer"].Command == "" {
		t.Fatalf("expected internal prompt server")
	}
	if cfg.Servers["agent-layer"].Type != "stdio" {
		t.Fatalf("unexpected internal prompt server type: %s", cfg.Servers["agent-layer"].Type)
	}
	if cfg.Servers["example"].Type != "http" {
		t.Fatalf("unexpected server type: %s", cfg.Servers["example"].Type)
	}
	if cfg.Servers["example"].URL != "https://example.com?token=${TOKEN}" {
		t.Fatalf("unexpected url: %s", cfg.Servers["example"].URL)
	}
	if cfg.Servers["example"].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("unexpected header: %s", cfg.Servers["example"].Headers["Authorization"])
	}
}

func TestWriteMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
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

	if err := WriteMCPConfig(sys, root, project); err != nil {
		t.Fatalf("WriteMCPConfig error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatalf("expected mcp.json: %v", err)
	}
}

func TestWriteMCPConfigError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{Root: root}
	if err := WriteMCPConfig(sys, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteMCPConfigWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	if err := os.Mkdir(filepath.Join(root, ".mcp.json"), 0o755); err != nil {
		t.Fatalf("mkdir .mcp.json: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: nil},
		},
		Root: root,
	}
	if err := WriteMCPConfig(sys, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigMissingEnv(t *testing.T) {
	t.Parallel()
	enabled := true
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	root := t.TempDir()
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

	_, err := buildMCPConfig(sys, project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigStdioServer(t *testing.T) {
	t.Parallel()
	enabled := true
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	root := t.TempDir()
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

	cfg, err := buildMCPConfig(sys, project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	server, ok := cfg.Servers["stdio"]
	if !ok {
		t.Fatalf("expected stdio server")
	}
	if server.Type != "stdio" {
		t.Fatalf("unexpected type: %s", server.Type)
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

func TestWriteMCPConfigMarshalError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	if err := WriteMCPConfig(sys, root, project); err == nil {
		t.Fatal("expected marshal error")
	}
}
