package sync

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map")
	}
	allow, ok := permissions["allow"].([]string)
	if !ok {
		t.Fatalf("expected permissions allow []string, got %T", permissions["allow"])
	}
	// Under ApprovalModeAll the managed allow list must render the configured
	// command (Claude Bash form) and the enabled MCP server (mcp__<id>__* form),
	// commands first then sorted MCP IDs. Asserting the exact entries catches a
	// defect in the renderer, the client filter, or the approval gating that a
	// bare length check (>= 2) would miss.
	want := []string{"Bash(git status:*)", "mcp__example__*"}
	if !reflect.DeepEqual(allow, want) {
		t.Fatalf("unexpected allow list: got %v want %v", allow, want)
	}
}

func TestWriteClaudeSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
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
	if err := writeClaudeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeClaudeSettings error: %v", err)
	}
	// The writer must persist the built settings, not an empty object. Read the
	// file back and assert the managed allow entries are present so a regression
	// that writes "{}" (or drops permissions) actually fails.
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("expected settings.json: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"permissions"`, `"allow"`, `Bash(git status:*)`, `mcp__example__*`} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected settings.json to contain %q, got:\n%s", want, content)
		}
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
	if err := writeClaudeSettings(RealSystem{}, file, project); err == nil {
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
	if err := writeClaudeSettings(RealSystem{}, root, project); err == nil {
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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
	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	settings, err := buildClaudeSettings("/repo", project)
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

	if err := writeClaudeSettings(sys, root, project); err == nil {
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

	settings, err := buildClaudeSettings("/repo", project)
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

func claudeWithQuestionToolFlag(disable *bool, agentSpecific map[string]any) *config.ProjectConfig {
	return &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					DisableQuestionTool: disable,
					AgentSpecific:       agentSpecific,
				},
			},
		},
	}
}

func denyOccurrences(value any, want string) int {
	count := 0
	switch values := value.(type) {
	case []any:
		for _, v := range values {
			if s, ok := v.(string); ok && s == want {
				count++
			}
		}
	case []string:
		for _, s := range values {
			if s == want {
				count++
			}
		}
	}
	return count
}

func preToolUseMatcherCount(settings map[string]any, matcher string) int {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	entries, ok := hooks["PreToolUse"].([]any)
	if !ok {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if m, ok := entry.(map[string]any); ok && m["matcher"] == matcher {
			count++
		}
	}
	return count
}

func claudeStopChimeHookCount(settings map[string]any) int {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	entries, ok := hooks["Stop"].([]any)
	if !ok {
		return 0
	}
	count := 0
	for _, entry := range entries {
		group, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, handler := range handlers {
			if chimeHandlerMatchesAny(handler, map[string]struct{}{agentLayerClaudeChimeCommand: {}}) {
				count++
			}
		}
	}
	return count
}

func TestBuildClaudeSettings_InjectsDenyAndHookWhenFlagTrue(t *testing.T) {
	t.Parallel()
	disable := true
	settings, err := buildClaudeSettings("/repo", claudeWithQuestionToolFlag(&disable, nil))
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map to be created for the injected deny, got %#v", settings["permissions"])
	}
	if !anyDenyContains(permissions["deny"], "AskUserQuestion") {
		t.Fatalf("expected injected deny to contain AskUserQuestion, got %#v", permissions["deny"])
	}
	if got := preToolUseMatcherCount(settings, "AskUserQuestion"); got != 1 {
		t.Fatalf("expected exactly one AskUserQuestion PreToolUse hook, got %d (%#v)", got, settings["hooks"])
	}
}

func TestBuildClaudeSettings_QuestionToolMalformedOverrideFailsLoud(t *testing.T) {
	t.Parallel()
	// When disable_question_tool is set, a non-table agent_specific override for
	// permissions/hooks must fail loud rather than be silently discarded: silently
	// overwriting it would drop a user-supplied setting with no diagnostic and
	// (worse) hide that the AskUserQuestion control's enforcement assumptions about
	// the user's own override were ignored.
	disable := true
	for name, key := range map[string]string{"permissions": "permissions", "hooks": "hooks"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			project := claudeWithQuestionToolFlag(&disable, map[string]any{key: "not-a-table"})
			_, err := buildClaudeSettings("/repo", project)
			if err == nil {
				t.Fatalf("expected error for malformed agent_specific.%s override, got nil", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("expected error to name agent_specific.%s, got %v", key, err)
			}
		})
	}
}

func TestBuildClaudeSettings_QuestionToolMalformedNestedListFailsLoud(t *testing.T) {
	t.Parallel()
	// A scalar (non-list) permissions.deny or hooks.PreToolUse override would be
	// silently discarded by unionStringIntoList / appendAskUserQuestionHook (their
	// type switches fall through on a string), replacing the user's value with only
	// the managed entry. That violates the union guarantee, so it must fail loud.
	disable := true
	cases := map[string]struct {
		agentSpecific map[string]any
		wantInError   string
	}{
		"deny is a scalar": {
			agentSpecific: map[string]any{"permissions": map[string]any{"deny": "Bash(rm:*)"}},
			wantInError:   "permissions.deny",
		},
		"PreToolUse is a scalar": {
			agentSpecific: map[string]any{"hooks": map[string]any{"PreToolUse": "not-a-list"}},
			wantInError:   "hooks.PreToolUse",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := buildClaudeSettings("/repo", claudeWithQuestionToolFlag(&disable, tc.agentSpecific))
			if err == nil {
				t.Fatalf("expected error for malformed nested list %q, got nil", tc.wantInError)
			}
			if !strings.Contains(err.Error(), tc.wantInError) {
				t.Fatalf("expected error to name %q, got %v", tc.wantInError, err)
			}
		})
	}
}

func TestBuildClaudeSettings_NoInjectionWhenFlagNilOrFalse(t *testing.T) {
	t.Parallel()
	disable := false
	for name, project := range map[string]*config.ProjectConfig{
		"nil":   claudeWithQuestionToolFlag(nil, nil),
		"false": claudeWithQuestionToolFlag(&disable, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			settings, err := buildClaudeSettings("/repo", project)
			if err != nil {
				t.Fatalf("buildClaudeSettings error: %v", err)
			}
			if permissions, ok := settings["permissions"].(map[string]any); ok {
				if anyDenyContains(permissions["deny"], "AskUserQuestion") {
					t.Fatalf("expected no injected deny when flag %s, got %#v", name, permissions["deny"])
				}
			}
			if got := preToolUseMatcherCount(settings, "AskUserQuestion"); got != 0 {
				t.Fatalf("expected no injected hook when flag %s, got %d", name, got)
			}
		})
	}
}

func TestBuildClaudeSettings_InjectionUnionsWithUserDenyAndHooks(t *testing.T) {
	t.Parallel()
	disable := true
	agentSpecific := map[string]any{
		"permissions": map[string]any{"deny": []any{"Bash(rm:*)"}},
		"hooks":       map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Write"}}},
	}
	settings, err := buildClaudeSettings("/repo", claudeWithQuestionToolFlag(&disable, agentSpecific))
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions := settings["permissions"].(map[string]any)
	if !anyDenyContains(permissions["deny"], "Bash(rm:*)") {
		t.Fatalf("expected user deny entry preserved, got %#v", permissions["deny"])
	}
	if !anyDenyContains(permissions["deny"], "AskUserQuestion") {
		t.Fatalf("expected injected deny entry, got %#v", permissions["deny"])
	}
	if got := preToolUseMatcherCount(settings, "Write"); got != 1 {
		t.Fatalf("expected user PreToolUse hook preserved, got %d", got)
	}
	if got := preToolUseMatcherCount(settings, "AskUserQuestion"); got != 1 {
		t.Fatalf("expected injected PreToolUse hook, got %d", got)
	}
}

func TestBuildClaudeSettings_InjectionUnionsStringSliceDeny(t *testing.T) {
	t.Parallel()
	disable := true
	// A []string deny (cloneAgentSpecificValue preserves this type) must be unioned,
	// not dropped, when the injected entry is added.
	agentSpecific := map[string]any{
		"permissions": map[string]any{"deny": []string{"Bash(rm:*)"}},
	}
	settings, err := buildClaudeSettings("/repo", claudeWithQuestionToolFlag(&disable, agentSpecific))
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions := settings["permissions"].(map[string]any)
	if !anyDenyContains(permissions["deny"], "Bash(rm:*)") {
		t.Fatalf("expected user []string deny entry preserved, got %#v", permissions["deny"])
	}
	if !anyDenyContains(permissions["deny"], "AskUserQuestion") {
		t.Fatalf("expected injected deny entry, got %#v", permissions["deny"])
	}
}

func TestBuildClaudeSettings_InjectionIsIdempotentAgainstUserEntries(t *testing.T) {
	t.Parallel()
	disable := true
	// User already blocks AskUserQuestion by hand; injection must not duplicate.
	agentSpecific := map[string]any{
		"permissions": map[string]any{"deny": []any{"AskUserQuestion"}},
		"hooks":       map[string]any{"PreToolUse": []any{map[string]any{"matcher": "AskUserQuestion"}}},
	}
	settings, err := buildClaudeSettings("/repo", claudeWithQuestionToolFlag(&disable, agentSpecific))
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions := settings["permissions"].(map[string]any)
	if got := denyOccurrences(permissions["deny"], "AskUserQuestion"); got != 1 {
		t.Fatalf("expected AskUserQuestion deny deduped to 1, got %d (%#v)", got, permissions["deny"])
	}
	if got := preToolUseMatcherCount(settings, "AskUserQuestion"); got != 1 {
		t.Fatalf("expected AskUserQuestion hook deduped to 1, got %d", got)
	}
}

func TestBuildClaudeSettings_InjectionUnderYOLOStillEmitsHook(t *testing.T) {
	t.Parallel()
	disable := true
	project := claudeWithQuestionToolFlag(&disable, nil)
	project.Config.Approvals.Mode = config.ApprovalModeYOLO
	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	// permissions.deny is ignored under YOLO/bypassPermissions, so the hook is the
	// real enforcement and must always be present.
	if got := preToolUseMatcherCount(settings, "AskUserQuestion"); got != 1 {
		t.Fatalf("expected AskUserQuestion hook under YOLO, got %d", got)
	}
}

func TestBuildClaudeSettings_InjectionPreservesManagedAllow(t *testing.T) {
	t.Parallel()
	disable := true
	enabled := true
	project := claudeWithQuestionToolFlag(&disable, nil)
	project.Config.Approvals.Mode = config.ApprovalModeAll
	project.Config.MCP.Servers = []config.MCPServer{
		{ID: "example", Enabled: &enabled, Transport: "http", URL: "https://example.com", Clients: []string{"claude"}},
	}
	project.CommandsAllow = []string{"git status"}

	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	permissions := settings["permissions"].(map[string]any)
	allow, ok := permissions["allow"].([]string)
	if !ok || !stringSliceContains(allow, "Bash(git status:*)") || !stringSliceContains(allow, "mcp__example__*") {
		t.Fatalf("expected managed allow entries preserved, got %#v", permissions["allow"])
	}
	if !anyDenyContains(permissions["deny"], "AskUserQuestion") {
		t.Fatalf("expected injected deny alongside managed allow, got %#v", permissions["deny"])
	}
}

func TestBuildClaudeSettings_ChimeInjectsStopHookWhenEnabled(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	if got := claudeStopChimeHookCount(settings); got != 1 {
		t.Fatalf("expected one managed chime Stop hook, got %d (%#v)", got, settings["hooks"])
	}
}

func TestBuildClaudeSettings_ChimePreservesUserStopHooks(t *testing.T) {
	t.Parallel()
	enabled := true
	agentSpecific := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo user", "timeout": int64(3)}}},
			},
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{AgentSpecific: agentSpecific},
			},
		},
	}
	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("expected user Stop hook and managed chime hook, got %#v", stop)
	}
	if got := claudeStopChimeHookCount(settings); got != 1 {
		t.Fatalf("expected one chime hook, got %d (%#v)", got, stop)
	}
}

func TestAppendClaudeChimeStopHookDedupesExactHandler(t *testing.T) {
	t.Parallel()
	existing := []any{map[string]any{"hooks": []any{chimeHandler(agentLayerClaudeChimeCommand)}}}
	settings := map[string]any{"hooks": map[string]any{"Stop": existing}}
	if err := injectClaudeChimeHook(settings); err != nil {
		t.Fatalf("injectClaudeChimeHook: %v", err)
	}
	if got := claudeStopChimeHookCount(settings); got != 1 {
		t.Fatalf("expected exact chime handler deduped, got %d (%#v)", got, settings)
	}
}

func TestAppendClaudeChimeStopHookMigratesLegacyHandler(t *testing.T) {
	t.Parallel()
	existing := []any{
		map[string]any{"hooks": []any{chimeHandler(legacyAgentLayerClaudeChimeCommand)}},
		map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo user", "timeout": 3}}},
	}
	settings := map[string]any{"hooks": map[string]any{"Stop": existing}}
	if err := injectClaudeChimeHook(settings); err != nil {
		t.Fatalf("injectClaudeChimeHook: %v", err)
	}
	if got := claudeStopChimeHookCount(settings); got != 1 {
		t.Fatalf("expected one current chime handler, got %d (%#v)", got, settings)
	}
	if containsExactChimeCommand(settings, legacyChimeCommandVariants(legacyAgentLayerClaudeChimeCommand)) {
		t.Fatalf("legacy direct-sound handler survived migration: %#v", settings)
	}
	if !containsExactChimeCommand(settings, map[string]struct{}{"echo user": {}}) {
		t.Fatalf("user handler was not preserved: %#v", settings)
	}
}

func TestBuildClaudeSettings_ChimeMalformedOverrideFailsLoud(t *testing.T) {
	t.Parallel()
	enabled := true
	cases := map[string]map[string]any{
		"hooks scalar":      {"hooks": "not-a-table"},
		"hooks.Stop scalar": {"hooks": map[string]any{"Stop": "not-a-list"}},
	}
	for name, agentSpecific := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			project := &config.ProjectConfig{
				Config: config.Config{
					Notifications: config.NotificationsConfig{Chime: &enabled},
					Agents: config.AgentsConfig{
						Claude: config.ClaudeConfig{AgentSpecific: agentSpecific},
					},
				},
			}
			_, err := buildClaudeSettings("/repo", project)
			if err == nil {
				t.Fatal("expected malformed hooks override to fail")
			}
			if !strings.Contains(err.Error(), "notifications.chime") {
				t.Fatalf("expected actionable chime error, got %v", err)
			}
		})
	}
}

func TestBuildClaudeSettings_ChimeRejectsLegacyAgentSpecificHook(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{AgentSpecific: map[string]any{
					"hooks": map[string]any{
						"Stop": []any{map[string]any{"hooks": []any{chimeHandler(agentLayerClaudeChimeCommand)}}},
					},
				}},
			},
		},
	}
	_, err := buildClaudeSettings("/repo", project)
	if err == nil || !strings.Contains(err.Error(), "agents.claude.agent_specific.hooks") {
		t.Fatalf("expected legacy agent_specific chime error, got %v", err)
	}
}

func TestBuildClaudeSettings_ChimeAppendsAfterMalformedStopEntries(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{AgentSpecific: map[string]any{
					"hooks": map[string]any{
						"Stop": []any{
							"keep scalar entry",
							map[string]any{"hooks": "keep malformed hooks value"},
						},
					},
				}},
			},
		},
	}

	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings error: %v", err)
	}
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 3 {
		t.Fatalf("expected two user entries plus managed chime hook, got %#v", stop)
	}
	if stop[0] != "keep scalar entry" {
		t.Fatalf("expected scalar user Stop entry preserved, got %#v", stop[0])
	}
	second, ok := stop[1].(map[string]any)
	if !ok || second["hooks"] != "keep malformed hooks value" {
		t.Fatalf("expected malformed user Stop entry preserved, got %#v", stop[1])
	}
	if got := claudeStopChimeHookCount(settings); got != 1 {
		t.Fatalf("expected exactly one managed chime handler appended, got %d (%#v)", got, stop)
	}
}

func TestCleanClaudeChimeHookRemovesOnlyManagedHandler(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime",
            "timeout": 5
          }
        ]
      },
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo user",
            "timeout": 5
          }
        ]
      }
    ]
  },
  "theme": "keep"
}
`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := cleanClaudeChimeHook(RealSystem{}, root); err != nil {
		t.Fatalf("cleanClaudeChimeHook: %v", err)
	}
	updated := readFileForTest(t, settingsPath)
	if strings.Contains(updated, "agent-layer-chime") {
		t.Fatalf("expected managed chime removed, got:\n%s", updated)
	}
	for _, want := range []string{`"command": "echo user"`, `"theme": "keep"`} {
		if !strings.Contains(updated, want) {
			t.Fatalf("expected %q preserved, got:\n%s", want, updated)
		}
	}
}

func TestCleanClaudeChimeHookRejectsSymlinkSettingsDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	outsideSettings := filepath.Join(outside, "settings.json")
	if err := os.WriteFile(outsideSettings, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+agentLayerClaudeChimeCommand+`","timeout":5}]}]}}`), 0o600); err != nil {
		t.Fatalf("write outside settings: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".claude")); err != nil {
		t.Fatalf("seed .claude symlink: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "must be a real file") {
		t.Fatalf("expected symlink cleanup error, got %v", err)
	}
	if got := readFileForTest(t, outsideSettings); !strings.Contains(got, agentLayerChimeMarker) {
		t.Fatalf("outside settings must not be rewritten, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookRejectsSymlinkSettingsFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	outsideSettings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(outsideSettings, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+agentLayerClaudeChimeCommand+`","timeout":5}]}]}}`), 0o600); err != nil {
		t.Fatalf("write outside settings: %v", err)
	}
	if err := os.Symlink(outsideSettings, filepath.Join(settingsDir, "settings.json")); err != nil {
		t.Fatalf("seed settings symlink: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "must be a real file") {
		t.Fatalf("expected symlink cleanup error, got %v", err)
	}
	if got := readFileForTest(t, outsideSettings); !strings.Contains(got, agentLayerChimeMarker) {
		t.Fatalf("outside settings must not be rewritten, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookIgnoresMalformedSettingsWithoutChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := cleanClaudeChimeHook(RealSystem{}, root); err != nil {
		t.Fatalf("cleanClaudeChimeHook should ignore malformed no-chime settings: %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected malformed no-chime settings untouched, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookReadErrorFailsLoud(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"note":"`+agentLayerClaudeChimeCommand+`"}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	readErr := errors.New("read denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == settingsPath {
				return nil, readErr
			}
			return RealSystem{}.ReadFile(name)
		},
	}

	err := cleanClaudeChimeHook(sys, root)
	if err == nil || !strings.Contains(err.Error(), "read denied") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestCleanClaudeChimeHookRejectsMalformedSettingsWithChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"note":"` + agentLayerClaudeChimeCommand + `","hooks":`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "invalid Claude settings") {
		t.Fatalf("expected malformed chime-bearing settings to fail, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected malformed settings preserved after cleanup failure, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookRejectsMalformedHooksWithChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":"bad","note":"` + agentLayerClaudeChimeCommand + `"}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "hooks must be a table") {
		t.Fatalf("expected malformed hooks cleanup error, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected malformed hooks preserved after cleanup failure, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookRejectsMalformedStopWithChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":{"Stop":"bad"},"note":"` + agentLayerClaudeChimeCommand + `"}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "hooks.Stop must be a list") {
		t.Fatalf("expected malformed Stop cleanup error, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected malformed Stop preserved after cleanup failure, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookRejectsMalformedHandlerListWithChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":{"Stop":[{"hooks":"bad"}]},"note":"` + agentLayerClaudeChimeCommand + `"}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := cleanClaudeChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "hooks.Stop.hooks must be a list") {
		t.Fatalf("expected malformed handler-list cleanup error, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected malformed handler list preserved after cleanup failure, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookNoopWhenChimeTextIsOutsideHooks(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no hooks":          `{"note":"` + agentLayerClaudeChimeCommand + `","theme":"keep"}`,
		"no Stop":           `{"hooks":{},"note":"` + agentLayerClaudeChimeCommand + `"}`,
		"scalar Stop entry": `{"hooks":{"Stop":["keep scalar entry"]},"note":"` + agentLayerClaudeChimeCommand + `"}`,
		"group without hooks": `{"hooks":{"Stop":[{"matcher":"keep group"}]},"note":"` +
			agentLayerClaudeChimeCommand + `"}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			settingsPath := filepath.Join(root, ".claude", "settings.json")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatalf("mkdir .claude: %v", err)
			}
			if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write settings: %v", err)
			}

			if err := cleanClaudeChimeHook(RealSystem{}, root); err != nil {
				t.Fatalf("cleanClaudeChimeHook should preserve no-op %q: %v", name, err)
			}
			if got := readFileForTest(t, settingsPath); got != content {
				t.Fatalf("expected no-op settings preserved for %q, got:\n%s", name, got)
			}
		})
	}
}

func TestCleanClaudeChimeHookMarshalErrorPreservesSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + agentLayerClaudeChimeCommand + `","timeout":5}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal denied")
		},
	}

	err := cleanClaudeChimeHook(sys, root)
	if err == nil || !strings.Contains(err.Error(), "marshal denied") {
		t.Fatalf("expected marshal error, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected settings preserved after marshal error, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookWriteErrorPreservesSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + agentLayerClaudeChimeCommand + `","timeout":5}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if filename == settingsPath {
				return errors.New("write denied")
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := cleanClaudeChimeHook(sys, root)
	if err == nil || !strings.Contains(err.Error(), "write denied") {
		t.Fatalf("expected write error, got %v", err)
	}
	if got := readFileForTest(t, settingsPath); got != content {
		t.Fatalf("expected settings preserved after write error, got:\n%s", got)
	}
}

func TestCleanClaudeChimeHookPreservesAugmentedMatchingHandler(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime",
            "timeout": 5,
            "description": "user-owned"
          },
          {
            "type": "command",
            "command": "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := cleanClaudeChimeHook(RealSystem{}, root); err != nil {
		t.Fatalf("cleanClaudeChimeHook: %v", err)
	}
	updated := readFileForTest(t, settingsPath)
	if !strings.Contains(updated, `"description": "user-owned"`) {
		t.Fatalf("expected augmented matching handler preserved, got:\n%s", updated)
	}
	if strings.Count(updated, agentLayerChimeMarker) != 1 {
		t.Fatalf("expected only the augmented matching handler to remain, got:\n%s", updated)
	}
}

func TestChimeHandlerMatchesAnyRequiresExactManagedHandler(t *testing.T) {
	t.Parallel()
	commands := map[string]struct{}{agentLayerClaudeChimeCommand: {}}
	for name, handler := range map[string]any{
		"non-map":         "not a hook",
		"extra key":       map[string]any{"type": "command", "command": agentLayerClaudeChimeCommand, "timeout": agentLayerChimeTimeout, "description": "user-owned"},
		"missing command": map[string]any{"type": "command", "timeout": agentLayerChimeTimeout, "note": "not generated"},
		"wrong type":      map[string]any{"type": "prompt", "command": agentLayerClaudeChimeCommand, "timeout": agentLayerChimeTimeout},
		"wrong command":   map[string]any{"type": "command", "command": "echo user", "timeout": agentLayerChimeTimeout},
		"string timeout":  map[string]any{"type": "command", "command": agentLayerClaudeChimeCommand, "timeout": "5"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if chimeHandlerMatchesAny(handler, commands) {
				t.Fatalf("handler %q must not be treated as Agent Layer-owned: %#v", name, handler)
			}
		})
	}
	for name, timeout := range map[string]any{"int": 5, "int64": int64(5), "float64": float64(5)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := map[string]any{"type": "command", "command": agentLayerClaudeChimeCommand, "timeout": timeout}
			if !chimeHandlerMatchesAny(handler, commands) {
				t.Fatalf("generated handler with %s timeout should match ownership detector: %#v", name, handler)
			}
		})
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
