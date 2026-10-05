package wizard

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestAgentHelpers(t *testing.T) {
	t.Run("agentIDSet", func(t *testing.T) {
		input := []string{"a", "b"}
		got := agentIDSet(input)
		assert.True(t, got["a"])
		assert.True(t, got["b"])
		assert.False(t, got["c"])
	})

	t.Run("enabledAgentIDs", func(t *testing.T) {
		input := map[string]bool{
			"a": true,
			"b": false,
			"c": true,
		}
		got := enabledAgentIDs(input)
		sort.Strings(got)
		assert.Equal(t, []string{"a", "c"}, got)
	})

	t.Run("setEnabledAgentsFromConfig", func(t *testing.T) {
		dest := make(map[string]bool)
		tBool := true
		fBool := false
		configs := []agentEnabledConfig{
			{id: "a", enabled: &tBool},
			{id: "b", enabled: &fBool},
			{id: "c", enabled: nil},
		}
		setEnabledAgentsFromConfig(dest, configs)
		assert.True(t, dest["a"])
		assert.False(t, dest["b"]) // Not set to true
		assert.False(t, dest["c"])
	})
}

func TestAgentModelSummary(t *testing.T) {
	tests := []struct {
		name     string
		choice   AgentModelChoice
		expected string
	}{
		{"model and reasoning", AgentModelChoice{Model: "codex-max", Reasoning: "high"}, "codex-max (high)"},
		{"model only", AgentModelChoice{Model: "claude-3"}, "claude-3"},
		{"reasoning only", AgentModelChoice{Reasoning: "high"}, "reasoning: high"},
		{"neither", AgentModelChoice{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, agentModelSummary(tt.choice))
		})
	}
}

func TestGrokHelpers(t *testing.T) {
	assert.True(t, grokToggleVisible(&Choices{}))
	assert.True(t, grokToggleVisible(&Choices{EnabledAgentsTouched: true, EnabledAgents: map[string]bool{AgentGrok: true}}))
	assert.False(t, grokToggleVisible(&Choices{EnabledAgentsTouched: true, EnabledAgents: map[string]bool{}}))
}

func TestSelectOptionalValue_Custom(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			*current = messages.WizardCustomOption
			return nil
		},
		InputFunc: func(title string, value *string) error {
			*value = "custom-model"
			return nil
		},
	}

	value := ""
	err := selectOptionalValue(ui, "Test Model", []string{"test-preview"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "custom-model", value)
}

func TestSelectOptionalValue_CustomBlank(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			*current = messages.WizardCustomOption
			return nil
		},
		InputFunc: func(title string, value *string) error {
			*value = "   "
			return nil
		},
	}

	value := ""
	err := selectOptionalValue(ui, "Test Model", []string{"test-model"}, &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "custom value required")
}

func TestSelectOptionalValue_CustomPrefill(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			assert.Equal(t, messages.WizardCustomOption, *current)
			return nil
		},
		InputFunc: func(title string, value *string) error {
			assert.Equal(t, "custom-model", *value)
			return nil
		},
	}

	value := "custom-model"
	err := selectOptionalValue(ui, "Test Model", []string{"test-model"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "custom-model", value)
}

func TestSelectOptionalValue_ValueInOptions(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			// Current should be the predefined value from options
			assert.Equal(t, "test-model", *current)
			return nil
		},
	}

	value := "test-model"
	err := selectOptionalValue(ui, "Test Model", []string{"test-model"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "test-model", value)
}

func TestSelectOptionalValue_SelectError(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			return errors.New("select error")
		},
	}

	value := ""
	err := selectOptionalValue(ui, "Test Model", []string{"test-model"}, &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "select error")
}

func TestSelectOptionalValue_InputError(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			*current = messages.WizardCustomOption
			return nil
		},
		InputFunc: func(title string, value *string) error {
			return errors.New("input error")
		},
	}

	value := ""
	err := selectOptionalValue(ui, "Test Model", []string{"test-model"}, &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "input error")
}

func TestSelectOptionalValue_LeaveBlank(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			*current = messages.WizardLeaveBlankOption
			return nil
		},
	}

	value := "some-value"
	err := selectOptionalValue(ui, "Test Model", []string{"test-preview"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestBuildSummary(t *testing.T) {
	t.Run("with MCP servers enabled", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.EnabledAgents["antigravity"] = true
		c.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}, {ID: "tavily"}}
		c.EnabledMCPServers["github"] = true

		summary := buildSummary(c)
		assert.Contains(t, summary, "Approval mode: all")
		assert.Contains(t, summary, "Enabled Agents:")
		assert.Contains(t, summary, "Enabled MCP Servers:")
		assert.Contains(t, summary, "- github")
	})

	t.Run("no MCP servers loaded", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "none"
		c.DefaultMCPServers = nil

		summary := buildSummary(c)
		assert.Contains(t, summary, "(none loaded)")
	})

	t.Run("no MCP servers enabled", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "mcp"
		c.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}
		c.EnabledMCPServers["github"] = false

		summary := buildSummary(c)
		assert.Contains(t, summary, "(none)")
	})

	t.Run("with secrets to update", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}
		c.Secrets["GITHUB_TOKEN"] = "secret"
		c.Secrets["OTHER_TOKEN"] = "other"

		summary := buildSummary(c)
		assert.Contains(t, summary, "Secrets to Update:")
		assert.Contains(t, summary, "- GITHUB_TOKEN")
		assert.Contains(t, summary, "- OTHER_TOKEN")
	})
}

// TestBuildSummaryReportsEveryClaudeHardeningChoice covers the confirmation
// step the user actually reads before the wizard writes anything: a choice that
// turns off a Claude capability must appear in the summary, and a capability
// left at its client default must not — otherwise the user confirms a change
// they were never shown, or is warned about one they did not make.
func TestBuildSummaryReportsEveryClaudeHardeningChoice(t *testing.T) {
	disabled := NewChoices()
	disabled.ApprovalMode = "none"
	disabled.EnabledAgentsTouched = true
	disabled.EnabledAgents[AgentClaude] = true
	disabled.AgentModels[AgentClaude] = AgentModelChoice{Model: "opus", Reasoning: "high"}
	for _, touched := range []*bool{
		&disabled.ClaudeDisableIDEReadingTouched, &disabled.ClaudeDisableMemoryTouched,
		&disabled.ClaudeDisableConnectorsTouched, &disabled.ClaudeDisableQuestionToolTouched,
	} {
		*touched = true
	}
	for _, value := range []*bool{
		&disabled.ClaudeDisableIDEReading, &disabled.ClaudeDisableMemory,
		&disabled.ClaudeDisableConnectors, &disabled.ClaudeDisableQuestionTool,
	} {
		*value = true
	}

	summary := buildSummary(disabled)
	for _, line := range []string{
		messages.WizardSummaryClaudeIDEReadingDisabled,
		messages.WizardSummaryClaudeMemoryDisabled,
		messages.WizardSummaryClaudeConnectorsDisabled,
		messages.WizardSummaryClaudeQuestionToolDisabled,
	} {
		assert.Contains(t, summary, strings.TrimSpace(line))
	}
	assert.Contains(t, summary, "opus (high)")

	// The same choices left at the client default report nothing.
	kept := NewChoices()
	kept.ApprovalMode = "none"
	kept.EnabledAgentsTouched = true
	kept.EnabledAgents[AgentClaude] = true
	kept.ClaudeDisableIDEReadingTouched = true
	kept.ClaudeDisableMemoryTouched = true
	keptSummary := buildSummary(kept)
	assert.NotContains(t, keptSummary, strings.TrimSpace(messages.WizardSummaryClaudeIDEReadingDisabled))
	assert.NotContains(t, keptSummary, strings.TrimSpace(messages.WizardSummaryClaudeMemoryDisabled))

	// A reasoning effort chosen without a model still has to be reported.
	reasoningOnly := NewChoices()
	reasoningOnly.ApprovalMode = "none"
	reasoningOnly.EnabledAgents[AgentClaude] = true
	reasoningOnly.AgentModels[AgentClaude] = AgentModelChoice{Reasoning: "high"}
	assert.Contains(t, buildSummary(reasoningOnly), "reasoning: high")
}
