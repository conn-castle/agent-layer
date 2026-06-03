package config

import (
	"testing"
)

func TestIsAgentEnabled(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil pointer", nil, false},
		{"false pointer", &falseVal, false},
		{"true pointer", &trueVal, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAgentEnabled(tt.ptr); got != tt.want {
				t.Fatalf("IsAgentEnabled(%v) = %v, want %v", tt.ptr, got, tt.want)
			}
		})
	}
}

func TestClaudeStatuslineEnabled(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil defaults off", nil, false},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClaudeStatuslineEnabled(ClaudeConfig{Statusline: tt.ptr})
			if got != tt.want {
				t.Fatalf("ClaudeStatuslineEnabled(%v) = %v, want %v", tt.ptr, got, tt.want)
			}
		})
	}
}

func TestCodexStatuslineEnabled(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil defaults off", nil, false},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CodexStatuslineEnabled(CodexConfig{Statusline: tt.ptr})
			if got != tt.want {
				t.Fatalf("CodexStatuslineEnabled(%v) = %v, want %v", tt.ptr, got, tt.want)
			}
		})
	}
}

func TestSharedAgentSkillsEnabled(t *testing.T) {
	on := true
	tests := []struct {
		name   string
		agents AgentsConfig
		want   bool
	}{
		{"no agents enabled", AgentsConfig{}, false},
		{"codex enabled", AgentsConfig{Codex: CodexConfig{Enabled: &on}}, true},
		{"antigravity enabled", AgentsConfig{Antigravity: AntigravityConfig{Enabled: &on}}, true},
		{"vscode enabled", AgentsConfig{VSCode: EnableOnlyConfig{Enabled: &on}}, true},
		{"copilot_cli enabled", AgentsConfig{CopilotCLI: AgentConfig{Enabled: &on}}, true},
		// Claude (and Claude VS Code) do not consume the shared `.agents/skills/`
		// projection, so enabling only Claude must NOT report shared skills as in
		// use. This guards against accidentally adding a non-consumer to the set.
		{"only claude enabled", AgentsConfig{Claude: ClaudeConfig{Enabled: &on}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SharedAgentSkillsEnabled(tt.agents); got != tt.want {
				t.Fatalf("SharedAgentSkillsEnabled(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
