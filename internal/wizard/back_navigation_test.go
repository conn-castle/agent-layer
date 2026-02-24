package wizard

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestPromptWizardFlow_BackFromAgentsReturnsToApprovalStep(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)
	noneLabel, ok := approvalModeLabelForValue(ApprovalNone)
	require.True(t, ok)

	var approvalCalls, agentCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardApprovalModeTitle {
				return nil
			}
			approvalCalls++
			if approvalCalls == 1 {
				*current = allLabel
			} else {
				*current = noneLabel
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				agentCalls++
				if agentCalls == 1 {
					return errWizardBack
				}
				*selected = []string{}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardEnableWarningsPrompt {
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, 2, approvalCalls, "expected to revisit approval step after back from agents")
	require.Equal(t, 2, agentCalls)
	require.Equal(t, ApprovalNone, choices.ApprovalMode)
	require.True(t, choices.ApprovalModeTouched)
	require.True(t, choices.EnabledAgentsTouched)
	require.Empty(t, choices.EnabledAgents)
}

func TestPromptWizardFlow_FirstStepEscapeCancelsWhenConfirmed(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	var exitConfirmCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title == messages.WizardApprovalModeTitle {
				return errWizardBack
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardFirstStepEscapeExitPrompt {
				exitConfirmCalls++
				*value = true
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.ErrorIs(t, err, errWizardCancelled)
	require.Equal(t, 1, exitConfirmCalls)
}

func TestPromptWizardFlow_FirstStepEscapeContinuesWhenDeclined(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)

	var approvalCalls, exitConfirmCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardApprovalModeTitle {
				return nil
			}
			approvalCalls++
			if approvalCalls == 1 {
				return errWizardBack
			}
			*current = allLabel
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardFirstStepEscapeExitPrompt:
				exitConfirmCalls++
				*value = false
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, 2, approvalCalls)
	require.Equal(t, 1, exitConfirmCalls)
}

func TestPromptWizardFlow_ClaudeReasoningSkippedForNonOpusModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)

	var claudeReasoningCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardClaudeModelTitle:
				*current = "sonnet"
			case messages.WizardClaudeReasoningEffortTitle:
				claudeReasoningCalls++
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				*selected = []string{AgentClaude}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardClaudeLocalConfigDirPrompt:
				*value = false
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, 0, claudeReasoningCalls, "reasoning effort prompt should be skipped for non-opus model")
	require.Equal(t, "", choices.ClaudeReasoning)
}

func TestPromptWizardFlow_ClaudeReasoningClearedWhenSwitchingFromOpus(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll
	choices.ClaudeReasoning = "high" // existing value from previous wizard run

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)

	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardClaudeModelTitle:
				*current = "sonnet" // switching away from opus
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				*selected = []string{AgentClaude}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardClaudeLocalConfigDirPrompt:
				*value = false
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, "", choices.ClaudeReasoning, "reasoning should be cleared when switching to non-opus model")
	require.True(t, choices.ClaudeReasoningTouched, "reasoning touched flag should be set after clearing")
}

func TestPromptWizardFlow_ClaudeReasoningPromptedForOpusModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)

	var claudeReasoningCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardClaudeModelTitle:
				*current = "opus"
			case messages.WizardClaudeReasoningEffortTitle:
				claudeReasoningCalls++
				*current = messages.WizardLeaveBlankOption
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				*selected = []string{AgentClaude}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardClaudeLocalConfigDirPrompt:
				*value = false
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, 1, claudeReasoningCalls, "reasoning effort prompt should be shown for opus model")
}

func TestPromptWizardFlow_BackFromModelsRollsBackPartialModelState(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll

	allLabel, ok := approvalModeLabelForValue(ApprovalAll)
	require.True(t, ok)

	var agentCalls, codexReasoningCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardCodexModelTitle:
				*current = "gpt-5"
			case messages.WizardCodexReasoningEffortTitle:
				codexReasoningCalls++
				if codexReasoningCalls == 1 {
					return errWizardBack
				}
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				agentCalls++
				if agentCalls == 1 {
					*selected = []string{AgentCodex}
				} else {
					*selected = []string{}
				}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardEnableWarningsPrompt {
				*value = false
			}
			return nil
		},
	}

	cfg := &config.ProjectConfig{Config: config.Config{}}
	err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
	require.NoError(t, err)
	require.Equal(t, 2, agentCalls, "expected back from models to revisit agent selection")
	require.Equal(t, "", choices.CodexModel)
	require.False(t, choices.CodexModelTouched)
	require.Equal(t, "", choices.CodexReasoning)
	require.False(t, choices.CodexReasoningTouched)
}
