package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildVSCodeMCPConfig(t *testing.T) {
	t.Parallel()
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
		Env: map[string]string{"TOKEN": "abc"},
	}

	cfg, err := buildVSCodeMCPConfig(project)
	if err != nil {
		t.Fatalf("buildVSCodeMCPConfig error: %v", err)
	}
	server, ok := cfg.Servers["example"]
	if !ok {
		t.Fatalf("expected server entry")
	}
	if server.Type != "http" {
		t.Fatalf("unexpected server type: %s", server.Type)
	}
	// VS Code uses ${env:VAR} syntax - VS Code resolves at runtime.
	if server.URL != "https://example.com?token=${env:TOKEN}" {
		t.Fatalf("unexpected url: %s", server.URL)
	}
}

func TestBuildVSCodeMCPConfigHeadersAndEnv(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "http",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com",
						Headers:   map[string]string{"X-Token": "${TOKEN}"},
					},
					{
						ID:        "stdio",
						Enabled:   &enabled,
						Transport: "stdio",
						Command:   "tool-${TOKEN}",
						Args:      []string{"--flag", "${KEY}"},
						Env:       map[string]string{"API_KEY": "${KEY}"},
					},
				},
			},
		},
		Env: map[string]string{"TOKEN": "abc", "KEY": "123"},
	}

	cfg, err := buildVSCodeMCPConfig(project)
	if err != nil {
		t.Fatalf("buildVSCodeMCPConfig error: %v", err)
	}
	// VS Code uses ${env:VAR} syntax - VS Code resolves at runtime.
	httpServer, ok := cfg.Servers["http"]
	if !ok {
		t.Fatalf("expected http server entry")
	}
	if httpServer.Headers["X-Token"] != "${env:TOKEN}" {
		t.Fatalf("unexpected header value: %s", httpServer.Headers["X-Token"])
	}

	server, ok := cfg.Servers["stdio"]
	if !ok {
		t.Fatalf("expected stdio server entry")
	}
	if server.Type != "stdio" {
		t.Fatalf("unexpected server type: %s", server.Type)
	}
	if server.Command != "tool-${env:TOKEN}" {
		t.Fatalf("unexpected command: %s", server.Command)
	}
	if len(server.Args) != 2 || server.Args[1] != "${env:KEY}" {
		t.Fatalf("unexpected args: %#v", server.Args)
	}
	if server.Env["API_KEY"] != "${env:KEY}" {
		t.Fatalf("unexpected env value: %s", server.Env["API_KEY"])
	}
}

func TestWriteVSCodeMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
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
		Env: map[string]string{"TOKEN": "abc"},
	}

	if err := WriteVSCodeMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeMCPConfig error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "mcp.json")); err != nil {
		t.Fatalf("expected mcp.json: %v", err)
	}
}

func TestWriteVSCodeMCPConfigError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := WriteVSCodeMCPConfig(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeMCPConfigMissingEnv(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
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
		Env: map[string]string{},
	}

	if err := WriteVSCodeMCPConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildVSCodeMCPConfigMissingEnv(t *testing.T) {
	t.Parallel()
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
		Env: map[string]string{},
	}

	_, err := buildVSCodeMCPConfig(project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeMCPConfigMarshalError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal fail")
		},
	}
	if err := WriteVSCodeMCPConfig(sys, t.TempDir(), &config.ProjectConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeMCPConfigWriteError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return []byte("{}"), nil
		},
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "mcp.json" {
				return errors.New("write fail")
			}
			return nil
		},
	}
	if err := WriteVSCodeMCPConfig(sys, t.TempDir(), &config.ProjectConfig{}); err == nil {
		t.Fatal("expected error")
	}
}
