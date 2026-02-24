package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildGeminiSettingsCommandsOnly(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
		},
		CommandsAllow: []string{"git status"},
		Root:          t.TempDir(),
	}

	settings, err := buildGeminiSettings(sys, project)
	if err != nil {
		t.Fatalf("buildGeminiSettings error: %v", err)
	}
	if settings.Tools == nil || len(settings.Tools.Allowed) != 1 {
		t.Fatalf("expected allowed tools")
	}
	if settings.MCPServers["agent-layer"].Trust == nil || *settings.MCPServers["agent-layer"].Trust {
		t.Fatalf("expected trust to be false when approvals.mode=commands")
	}
}

func TestWriteGeminiSettings(t *testing.T) {
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
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
		Root: root,
	}

	if err := WriteGeminiSettings(sys, root, project); err != nil {
		t.Fatalf("WriteGeminiSettings error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gemini", "settings.json")); err != nil {
		t.Fatalf("expected settings.json: %v", err)
	}
}

func TestWriteGeminiSettingsError(t *testing.T) {
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
	if err := WriteGeminiSettings(sys, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGeminiSettingsWriteError(t *testing.T) {
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
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(geminiDir, "settings.json"), 0o755); err != nil {
		t.Fatalf("mkdir settings.json: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
		Root: root,
	}
	if err := WriteGeminiSettings(sys, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildGeminiSettingsMCPServers(t *testing.T) {
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
			Approvals: config.ApprovalsConfig{Mode: "all"},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "http",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
						Headers:   map[string]string{"X-Token": "${TOKEN}"},
					},
					{
						ID:            "http-streamable",
						Enabled:       &enabled,
						Transport:     "http",
						HTTPTransport: "streamable",
						URL:           "https://streamable.example.com?token=${TOKEN}",
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
		Env:           map[string]string{"TOKEN": "abc", "KEY": "123"},
		CommandsAllow: []string{"git status"},
		Root:          root,
	}

	settings, err := buildGeminiSettings(sys, project)
	if err != nil {
		t.Fatalf("buildGeminiSettings error: %v", err)
	}
	if settings.Tools == nil || len(settings.Tools.Allowed) != 1 {
		t.Fatalf("expected tool permissions")
	}
	// Gemini preserves ${VAR} placeholders - Gemini CLI resolves them at runtime.
	httpServer := settings.MCPServers["http"]
	if httpServer.URL != "https://example.com?token=${TOKEN}" {
		t.Fatalf("unexpected sse url: %s", httpServer.URL)
	}
	if httpServer.HTTPURL != "" {
		t.Fatalf("expected empty streamable url for sse server, got %q", httpServer.HTTPURL)
	}
	if httpServer.Headers["X-Token"] != "${TOKEN}" {
		t.Fatalf("unexpected header value: %s", httpServer.Headers["X-Token"])
	}
	streamableServer := settings.MCPServers["http-streamable"]
	if streamableServer.HTTPURL != "https://streamable.example.com?token=${TOKEN}" {
		t.Fatalf("unexpected streamable http url: %s", streamableServer.HTTPURL)
	}
	if streamableServer.URL != "" {
		t.Fatalf("expected empty sse url for streamable server, got %q", streamableServer.URL)
	}
	stdioServer := settings.MCPServers["stdio"]
	if stdioServer.Command != "tool-${TOKEN}" {
		t.Fatalf("unexpected command: %s", stdioServer.Command)
	}
	if len(stdioServer.Args) != 2 || stdioServer.Args[1] != "${KEY}" {
		t.Fatalf("unexpected args: %#v", stdioServer.Args)
	}
	if stdioServer.Env["API_KEY"] != "${KEY}" {
		t.Fatalf("unexpected env value: %s", stdioServer.Env["API_KEY"])
	}
	if settings.MCPServers["agent-layer"].Trust == nil || !*settings.MCPServers["agent-layer"].Trust {
		t.Fatalf("expected trust to be true when approvals.mode=all")
	}
	if settings.MCPServers["agent-layer"].Env[config.BuiltinRepoRootEnvVar] != root {
		t.Fatalf(
			"expected internal prompt env %s=%q, got %q",
			config.BuiltinRepoRootEnvVar,
			root,
			settings.MCPServers["agent-layer"].Env[config.BuiltinRepoRootEnvVar],
		)
	}
}

func TestBuildGeminiSettingsYOLO(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
		},
		CommandsAllow: []string{"git status"},
		Root:          t.TempDir(),
	}

	settings, err := buildGeminiSettings(sys, project)
	if err != nil {
		t.Fatalf("buildGeminiSettings error: %v", err)
	}
	if settings.Tools == nil || len(settings.Tools.Allowed) != 1 {
		t.Fatalf("expected allowed tools for yolo mode")
	}
	if settings.MCPServers["agent-layer"].Trust == nil || !*settings.MCPServers["agent-layer"].Trust {
		t.Fatalf("expected trust to be true when approvals.mode=yolo")
	}
}

func TestBuildGeminiSettingsMissingEnv(t *testing.T) {
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
			Approvals: config.ApprovalsConfig{Mode: "all"},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "example", Enabled: &enabled, Transport: "http", URL: "https://example.com?token=${TOKEN}"},
				},
			},
		},
		Env:  map[string]string{},
		Root: root,
	}

	_, err := buildGeminiSettings(sys, project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGeminiSettingsMarshalError(t *testing.T) {
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
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
		Root: root,
	}

	if err := WriteGeminiSettings(sys, root, project); err == nil {
		t.Fatal("expected marshal error")
	}
}
