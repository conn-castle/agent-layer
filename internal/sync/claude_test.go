package sync

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
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
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(claudeDir, "settings.json"), 0o700); err != nil {
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

func TestBuildClaudeSettingsAgentSpecificDenyPreservesManagedAllow(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"deny": []string{"AskUserQuestion"},
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
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map")
	}
	allow, ok := permissions["allow"].([]string)
	if !ok || len(allow) < 2 {
		t.Fatalf("expected managed permissions allow list, got %#v", permissions["allow"])
	}
	if !stringSliceContains(allow, "Bash(git status:*)") {
		t.Fatalf("expected generated command allow entry, got %#v", allow)
	}
	if !stringSliceContains(allow, "mcp__example__*") {
		t.Fatalf("expected generated MCP allow entry, got %#v", allow)
	}
	deny, ok := permissions["deny"].([]string)
	if !ok || !stringSliceContains(deny, "AskUserQuestion") {
		t.Fatalf("expected custom permissions deny list, got %#v", permissions["deny"])
	}
}

func TestBuildClaudeSettingsAgentSpecificAllowOverridesManagedAllow(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"allow": []string{"Bash(custom:*)"},
						},
					},
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
	if !ok {
		t.Fatalf("expected permissions allow list, got %#v", permissions["allow"])
	}
	if len(allow) != 1 || allow[0] != "Bash(custom:*)" {
		t.Fatalf("expected custom allow list to replace managed allow entries, got %#v", allow)
	}
}

func TestBuildClaudeSettingsAgentSpecificMergeDoesNotMutateConfig(t *testing.T) {
	t.Parallel()
	permissions := map[string]any{
		"deny": []string{"AskUserQuestion"},
		"nested": map[string]any{
			"custom": true,
		},
	}
	agentSpecific := map[string]any{"permissions": permissions}
	before := map[string]any{
		"permissions": map[string]any{
			"deny": []string{"AskUserQuestion"},
			"nested": map[string]any{
				"custom": true,
			},
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{AgentSpecific: agentSpecific},
			},
		},
		CommandsAllow: []string{"git status"},
	}

	settings, err := buildClaudeSettings(project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	mergedPermissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map")
	}
	if _, ok := mergedPermissions["allow"]; !ok {
		t.Fatalf("expected merged settings to include managed allow entries")
	}
	// Mutate the merged result and confirm the source agent-specific map is untouched
	// — this exercises the map- and slice-clone paths in cloneClaudeSettingValue, not
	// just non-mutation.
	mergedPermissions["injected"] = "from-test"
	mergedNested, ok := mergedPermissions["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map in merged permissions")
	}
	mergedNested["injected"] = true
	mergedDeny, ok := mergedPermissions["deny"].([]string)
	if !ok {
		t.Fatalf("expected deny slice in merged permissions, got %#v", mergedPermissions["deny"])
	}
	mergedDeny[0] = "Mutated"
	if !reflect.DeepEqual(agentSpecific, before) {
		t.Fatalf("expected agent-specific config to remain unchanged after mutating merged settings, got %#v", agentSpecific)
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

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildClaudeSettingsDisableTogglesAgentSpecific(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"env": map[string]any{
							"CLAUDE_CODE_AUTO_CONNECT_IDE": "false",
							"ENABLE_CLAUDEAI_MCP_SERVERS":  "false",
						},
						"autoMemoryEnabled": false,
						"permissions":       map[string]any{"deny": []any{"AskUserQuestion"}},
						"hooks": map[string]any{
							"PreToolUse": []any{
								map[string]any{"matcher": "AskUserQuestion"},
							},
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

	env, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %#v", settings["env"])
	}
	if env["CLAUDE_CODE_AUTO_CONNECT_IDE"] != "false" {
		t.Fatalf("expected CLAUDE_CODE_AUTO_CONNECT_IDE=\"false\", got %#v", env["CLAUDE_CODE_AUTO_CONNECT_IDE"])
	}
	if env["ENABLE_CLAUDEAI_MCP_SERVERS"] != "false" {
		t.Fatalf("expected ENABLE_CLAUDEAI_MCP_SERVERS=\"false\", got %#v", env["ENABLE_CLAUDEAI_MCP_SERVERS"])
	}

	if memory, ok := settings["autoMemoryEnabled"].(bool); !ok || memory {
		t.Fatalf("expected autoMemoryEnabled=false, got %#v", settings["autoMemoryEnabled"])
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks map, got %#v", settings["hooks"])
	}
	if _, ok := hooks["PreToolUse"].([]any); !ok {
		t.Fatalf("expected hooks.PreToolUse slice, got %#v", hooks["PreToolUse"])
	}

	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map, got %#v", settings["permissions"])
	}
	if !anyDenyContains(permissions["deny"], "AskUserQuestion") {
		t.Fatalf("expected permissions.deny to contain AskUserQuestion, got %#v", permissions["deny"])
	}
}

func anyDenyContains(value any, want string) bool {
	switch values := value.(type) {
	case []any:
		for _, v := range values {
			if s, ok := v.(string); ok && s == want {
				return true
			}
		}
	case []string:
		return stringSliceContains(values, want)
	}
	return false
}
