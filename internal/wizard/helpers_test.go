package wizard

import (
	"errors"
	"sort"
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
	c := &Choices{
		GeminiModel:    "gemini-pro",
		ClaudeModel:    "claude-3",
		CodexModel:     "codex-max",
		CodexReasoning: "high",
	}

	assert.Equal(t, "gemini-pro", agentModelSummary(AgentGemini, c))
	assert.Equal(t, "claude-3", agentModelSummary(AgentClaude, c))
	assert.Equal(t, "codex-max (high)", agentModelSummary(AgentCodex, c))
	assert.Equal(t, "", agentModelSummary(AgentVSCode, c))
	assert.Equal(t, "", agentModelSummary("unknown", c))
}

func TestCodexModelSummary(t *testing.T) {
	tests := []struct {
		name     string
		choices  *Choices
		expected string
	}{
		{
			name: "model and reasoning",
			choices: &Choices{
				CodexModel:     "mod",
				CodexReasoning: "reas",
			},
			expected: "mod (reas)",
		},
		{
			name: "model only",
			choices: &Choices{
				CodexModel: "mod",
			},
			expected: "mod",
		},
		{
			name: "reasoning only",
			choices: &Choices{
				CodexReasoning: "reas",
			},
			expected: "reasoning: reas",
		},
		{
			name:     "neither",
			choices:  &Choices{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, codexModelSummary(tt.choices))
		})
	}
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
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
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
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Custom value required")
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
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "custom-model", value)
}

func TestSelectOptionalValue_ValueInOptions(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			// Current should be the predefined value from options
			assert.Equal(t, "gemini-2.5-pro", *current)
			return nil
		},
	}

	value := "gemini-2.5-pro"
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "gemini-2.5-pro", value)
}

func TestSelectOptionalValue_SelectError(t *testing.T) {
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			return errors.New("select error")
		},
	}

	value := ""
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
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
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
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
	err := selectOptionalValue(ui, "Gemini Model", []string{"gemini-2.5-pro"}, &value)
	assert.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestPromptPositiveInt_Default(t *testing.T) {
	ui := &MockUI{}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.NoError(t, err)
	assert.Equal(t, 10, value)
}

func TestPromptPositiveInt_Override(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			*value = "42"
			return nil
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.NoError(t, err)
	assert.Equal(t, 42, value)
}

func TestPromptPositiveInt_Invalid(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			*value = "0"
			return nil
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
}

func TestPromptPositiveInt_NegativeNumber(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			*value = "-5"
			return nil
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
}

func TestPromptPositiveInt_NotANumber(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			*value = "abc"
			return nil
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
}

func TestPromptPositiveInt_InputError(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			return errors.New("input error")
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "input error")
}

func TestPromptPositiveInt_EmptyInput(t *testing.T) {
	ui := &MockUI{
		InputFunc: func(title string, value *string) error {
			*value = "   " // whitespace only
			return nil
		},
	}
	value := 10
	err := promptPositiveInt(ui, "Threshold", &value)
	assert.NoError(t, err)
	assert.Equal(t, 10, value) // keeps original value
}

func TestBuildSummary(t *testing.T) {
	t.Run("with MCP servers enabled", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.EnabledAgents["gemini"] = true
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

	t.Run("with restored MCP servers", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}
		c.EnabledMCPServers["github"] = true
		c.RestoreMissingMCPServers = true
		c.MissingDefaultMCPServers = []string{"context7"}

		summary := buildSummary(c)
		assert.Contains(t, summary, "Restored Default MCP Servers:")
		assert.Contains(t, summary, "- context7")
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
