package wizard

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestPromptWizardFlow_BackFromAgentsReturnsToApprovalStep(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
	require.True(t, ok)
	noneLabel, ok := approvalModeLabelForValue(config.ApprovalModeNone)
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 2, approvalCalls, "expected to revisit approval step after back from agents")
	require.Equal(t, 2, agentCalls)
	require.Equal(t, config.ApprovalModeNone, choices.ApprovalMode)
	require.True(t, choices.ApprovalModeTouched)
	require.True(t, choices.EnabledAgentsTouched)
	require.Empty(t, choices.EnabledAgents)
}

func TestPromptWizardFlow_FirstStepEscapeCancelsWhenConfirmed(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.ErrorIs(t, err, errWizardCancelled)
	require.Equal(t, 1, exitConfirmCalls)
}

func TestPromptWizardFlow_FirstStepEscapeContinuesWhenDeclined(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 2, approvalCalls)
	require.Equal(t, 1, exitConfirmCalls)
}

func TestPromptWizardFlow_ClaudeReasoningPromptedForNonOpusModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 1, claudeReasoningCalls, "reasoning effort prompt should be shown regardless of model")
}

func TestPromptWizardFlow_ClaudeReasoningPreservedWhenSwitchingToNonOpusModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.ClaudeReasoning = "high" // existing value from previous wizard run

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
	require.True(t, ok)

	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardClaudeModelTitle:
				*current = "sonnet" // switching away from opus
				// Reasoning prompt is left unhandled so selectOptionalValue keeps
				// the existing "high" value: the wizard must not clear it.
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, "high", choices.ClaudeReasoning, "reasoning should be preserved when switching to a non-opus model")
	require.True(t, choices.ClaudeReasoningTouched, "reasoning touched flag should be set after the prompt")
}

func TestPromptWizardFlow_ClaudeReasoningPromptedForOpusModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 1, claudeReasoningCalls, "reasoning effort prompt should be shown for opus model")
}

func TestPromptWizardFlow_BackFromModelsRollsBackPartialModelState(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
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

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 2, agentCalls, "expected back from models to revisit agent selection")
	require.Equal(t, "", choices.CodexModel)
	require.False(t, choices.CodexModelTouched)
	require.Equal(t, "", choices.CodexReasoning)
	require.False(t, choices.CodexReasoningTouched)
}

func TestPromptWizardFlow_DisablingCodexClearsAppsChoiceAfterBackNavigation(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{{ID: "tavily-web", Name: "Tavily"}}

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
	require.True(t, ok)

	var agentCalls, cliSkillCalls, mcpCalls, secondModelCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardApprovalModeTitle:
				*current = allLabel
			case messages.WizardCodexModelTitle:
				if mcpCalls > 0 {
					secondModelCalls++
					return errWizardBack
				}
				*current = messages.WizardLeaveBlankOption
			case messages.WizardCodexReasoningEffortTitle:
				*current = messages.WizardLeaveBlankOption
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
				mcpCalls++
				if mcpCalls == 1 {
					return errWizardBack
				}
				*selected = []string{}
			case messages.WizardEnableCLISkillsTitle:
				cliSkillCalls++
				if mcpCalls > 0 && secondModelCalls == 0 {
					return errWizardBack
				}
			case messages.WizardCodexFeaturesTitle:
				// Uncheck every Codex feature (including apps) = disable them.
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardEnableAgentLayerPrompt:
				// During the back-navigation pass (mcpCalls > 0 and we have
				// not yet reached Models again), propagate the back signal so
				// we reach Models. Once the second Models pass has occurred,
				// the forward sweep should not loop on EnableLayer.
				if mcpCalls > 0 && secondModelCalls == 0 {
					return errWizardBack
				}
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 2, agentCalls, "expected back navigation to revisit agent selection")
	require.Equal(t, 1, secondModelCalls, "expected second model step to go back to agent selection")
	require.False(t, choices.EnabledAgents[AgentCodex])
	require.False(t, choices.CodexApps)
	require.False(t, choices.CodexAppsTouched)
}

// TestPromptWizardFlow_BackFromWarningsSkipsNoOpSecretsStep guards against the
// regression where pressing Esc on the final (warnings) step appeared to do
// nothing. The secrets step renders no prompt when no enabled MCP server has an
// unsatisfied required-env key (the common case). Back navigation from warnings
// lands on secrets, which returns nil (no-op) and is then treated as forward
// success, bouncing the user straight back to warnings. Going back must instead
// skip the no-op secrets step and reach the previous visible step (MCP defaults).
func TestPromptWizardFlow_BackFromWarningsSkipsNoOpSecretsStep(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	// No CLI skills catalog, no default/custom MCP servers: the secrets step has
	// nothing to prompt for and must not trap back navigation from warnings.

	allLabel, ok := approvalModeLabelForValue(config.ApprovalModeAll)
	require.True(t, ok)

	var mcpDefaultsCalls, warningsCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title == messages.WizardApprovalModeTitle {
				*current = allLabel
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				*selected = []string{}
			case messages.WizardEnableDefaultMCPServersTitle:
				mcpDefaultsCalls++
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardEnableWarningsPrompt {
				warningsCalls++
				if warningsCalls == 1 {
					return errWizardBack
				}
				*value = false
			}
			return nil
		},
	}

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.NoError(t, err)
	require.Equal(t, 2, warningsCalls, "expected warnings to be revisited after going forward from the previous step")
	require.Equal(t, 2, mcpDefaultsCalls, "back from warnings should reach the MCP defaults step, not bounce off the no-op secrets step")
}

func TestPromptWizardFlow_CtrlCExitsImmediatelyWithoutConfirmation(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	var exitConfirmCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title == messages.WizardApprovalModeTitle {
				// Ctrl+C returns errWizardCancelled, not errWizardBack.
				return errWizardCancelled
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardFirstStepEscapeExitPrompt {
				exitConfirmCalls++
			}
			return nil
		},
	}

	err := promptWizardFlow(t.TempDir(), ui, choices, false)
	require.ErrorIs(t, err, errWizardCancelled)
	require.Equal(t, 0, exitConfirmCalls, "Ctrl+C should exit immediately without asking for confirmation")
}
