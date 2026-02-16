package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildClaudeSettings(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "example", Enabled: &enabled, Transport: "http", URL: "https://example.com", Clients: []string{"claude"}},
				},
			},
		},
		CommandsAllow: []string{"git status"},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if settings.Permissions == nil || len(settings.Permissions.Allow) < 2 {
		t.Fatalf("expected permissions allow list")
	}
}

func TestWriteClaudeSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}
	if err := WriteClaudeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteClaudeSettings error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err != nil {
		t.Fatalf("expected settings.json: %v", err)
	}
}

func TestWriteClaudeSettingsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := WriteClaudeSettings(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSettingsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(claudeDir, "settings.json"), 0o755); err != nil {
		t.Fatalf("mkdir settings.json: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}
	if err := WriteClaudeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildClaudeSettingsNone(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if settings.Permissions != nil {
		t.Fatalf("expected no permissions for none mode")
	}
}

func TestWriteClaudeSettingsMarshalError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}

	if err := WriteClaudeSettings(sys, root, project); err == nil {
		t.Fatal("expected marshal error")
	}
}
