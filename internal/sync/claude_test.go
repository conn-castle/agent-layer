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
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
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
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map")
	}
	allow, ok := permissions["allow"].([]string)
	if !ok || len(allow) < 2 {
		t.Fatalf("expected permissions allow list")
	}
}

func TestWriteClaudeSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
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
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
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
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if _, ok := settings["permissions"]; ok {
		t.Fatalf("expected no permissions for none mode")
	}
}

func TestBuildClaudeSettingsYOLO(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
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
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map for yolo mode")
	}
	allow, ok := permissions["allow"].([]string)
	if !ok || len(allow) < 2 {
		t.Fatalf("expected permissions allow list for yolo mode")
	}
}

func TestBuildClaudeSettingsAgentSpecific(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"features": map[string]any{
							"example_feature": true,
						},
					},
				},
			},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	features, ok := settings["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features map")
	}
	if value, ok := features["example_feature"].(bool); !ok || !value {
		t.Fatalf("expected example_feature=true, got %v", features["example_feature"])
	}
}

func TestBuildClaudeSettingsIncludesReasoningEffort(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					ReasoningEffort: "high",
				},
			},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if got, ok := settings["effortLevel"].(string); !ok || got != "high" {
		t.Fatalf("expected effortLevel=high, got %#v", settings["effortLevel"])
	}
}

func TestBuildClaudeSettingsMaxEffortExcludedFromSettings(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					ReasoningEffort: "max",
				},
			},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if _, ok := settings["effortLevel"]; ok {
		t.Fatal("expected effortLevel to be excluded for max (session-only via --effort CLI flag)")
	}
}

func TestBuildClaudeSettingsTrimsReasoningEffort(t *testing.T) {
	t.Parallel()
	// Regression: " max " must be treated as "max" so the session-only level is
	// not persisted to settings.json.
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents:    config.AgentsConfig{Claude: config.ClaudeConfig{ReasoningEffort: " max "}},
		},
	}
	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if _, ok := settings["effortLevel"]; ok {
		t.Fatal("expected effortLevel to be excluded for whitespace-padded max")
	}
}

func TestBuildClaudeSettingsMaxEffortWithAgentSpecificOverride(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					ReasoningEffort: "max",
					AgentSpecific: map[string]any{
						"effortLevel": "low",
					},
				},
			},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	// When effort is "max", the managed effortLevel write is skipped (max is session-only).
	// The agentSpecific override should still take effect via the generic merge loop.
	if got, ok := settings["effortLevel"].(string); !ok || got != "low" {
		t.Fatalf("expected agentSpecific effortLevel override=low with max effort, got %#v", settings["effortLevel"])
	}
}

func TestBuildClaudeSettingsAgentSpecificEffortLevelOverride(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					ReasoningEffort: "high",
					AgentSpecific: map[string]any{
						"effortLevel": "low",
					},
				},
			},
		},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if got, ok := settings["effortLevel"].(string); !ok || got != "low" {
		t.Fatalf("expected effortLevel override=low, got %#v", settings["effortLevel"])
	}
}

func TestBuildClaudeSettingsAgentSpecificWithApprovals(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"features": map[string]any{
							"example_feature": true,
						},
					},
				},
			},
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
	// Managed permissions must be present alongside agent-specific keys.
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map when approvals are active")
	}
	allow, ok := permissions["allow"].([]string)
	if !ok || len(allow) < 2 {
		t.Fatalf("expected permissions allow list, got %v", permissions["allow"])
	}
	// Agent-specific keys must also be present.
	features, ok := settings["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features map from agent-specific config")
	}
	if value, ok := features["example_feature"].(bool); !ok || !value {
		t.Fatalf("expected example_feature=true, got %v", features["example_feature"])
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
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
	}

	if err := WriteClaudeSettings(sys, root, project); err == nil {
		t.Fatal("expected marshal error")
	}
}
