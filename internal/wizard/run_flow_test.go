package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRun_HappyPath(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error {
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(config.ApprovalModeAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"antigravity"}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	syncCalled := false
	mockSync := func(r string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)
	assert.True(t, syncCalled)

	// Reparse the produced config and assert the enablement is attached to
	// the antigravity block (F-C-17). The previous substring assertion would
	// have passed even if `enabled = true` appeared in another block, since
	// proximity to `[agents.antigravity]` was not verified.
	cfgPath := filepath.Join(configDir, "config.toml")
	data, readErr := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `mode = "all"`)
	parsed, err := config.ParseConfig(data, cfgPath)
	require.NoError(t, err)
	require.NotNil(t, parsed.Agents.Antigravity.Enabled, "antigravity enabled must be set")
	assert.True(t, *parsed.Agents.Antigravity.Enabled, "antigravity must be enabled")
}

func TestRun_SlimSeedDoesNotPromptToRestoreMissingCatalogDefaults(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
[mcp]
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			require.NotContains(t, title, "Default MCP server entries are missing")
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	err := Run(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)

	data, readErr := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "[[mcp.servers]]")
}

func TestRun_CLISkillCatalogSelectionCopiesAndRemovesSkill(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	validConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
[mcp]
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	runWithCLISkills := func(selectedSkills []string) {
		ui := &MockUI{
			NoteFunc:   func(title, body string) error { return nil },
			SelectFunc: func(title string, options []string, current *string) error { return nil },
			MultiSelectFunc: func(title string, options []string, selected *[]string) error {
				switch title {
				case messages.WizardEnableAgentsTitle:
					*selected = []string{}
				case messages.WizardEnableCLISkillsTitle:
					*selected = selectedSkills
				case messages.WizardEnableDefaultMCPServersTitle:
					*selected = []string{}
				}
				return nil
			},
			ConfirmFunc: func(title string, value *bool) error {
				if title == messages.WizardApplyChangesPrompt {
					*value = true
				}
				return nil
			},
		}
		err := Run(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
		require.NoError(t, err)
	}

	skillPath := filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md")
	runWithCLISkills([]string{"Tavily web search"})
	info, err := os.Stat(skillPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	runWithCLISkills([]string{})
	_, err = os.Stat(skillPath)
	assert.True(t, os.IsNotExist(err), "deselecting the catalog skill removes the installed directory")
}

func TestRun_ApplyCancel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	validConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = false
			}
			return nil
		},
	}

	syncCalled := false
	mockSync := func(r string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)
	assert.False(t, syncCalled)
}

func TestRun_SyncError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	validConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) {
		return nil, errors.New("sync failed")
	}

	err := Run(root, ui, mockSync, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync failed")
}

func TestRun_AddMissingDefaultViaMultiSelect(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// context7 exists but is disabled; the other defaults (including fetch) are
	// absent. The wizard no longer shows a blocking "restore missing defaults?"
	// prompt — missing defaults are simply unselected options the user can opt into.
	initialConfig := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false

[[mcp.servers]]
id = "context7"
enabled = false
transport = "stdio"
command = "npx"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	restorePromptShown := false
	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if strings.Contains(title, "missing from config.toml") {
				restorePromptShown = true
			}
			*value = true
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			// Select the absent default "fetch" (added from catalog) and leave
			// context7 unselected (kept, disabled in place — never deleted).
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"fetch"}
			}
			return nil
		},
		NoteFunc: func(title, body string) error { return nil },
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)
	assert.False(t, restorePromptShown, "wizard must not show a blocking restore-missing-defaults prompt")

	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	// Selected missing default is added from the catalog and enabled.
	assert.Contains(t, string(data), `id = "fetch"`)
	assert.True(t, mcpServerEnabled(t, string(data), "fetch"))
	// Existing unselected default is kept (not deleted) and disabled in place.
	assert.Contains(t, string(data), `id = "context7"`)
	assert.False(t, mcpServerEnabled(t, string(data), "context7"))
}

func TestRun_ClaudeLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = true
# local_config_dir = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(config.ApprovalModeAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"claude"}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardClaudeLocalConfigDirPrompt {
				*value = true
			}
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), "local_config_dir = true")
}

func TestRun_ClaudeVSCodeOnlyLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
# local_config_dir = false
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(config.ApprovalModeAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"claude_vscode"}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardClaudeLocalConfigDirPrompt {
				*value = true
			}
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), "local_config_dir = true")
}

func TestRun_CodexLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = true
# local_config_dir = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == messages.WizardApprovalModeTitle {
				label, ok := approvalModeLabelForValue(config.ApprovalModeAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableAgentsTitle {
				*selected = []string{AgentCodex}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardCodexLocalConfigDirPrompt {
				*value = true
			}
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), "local_config_dir = true")
}

// TestRun_CodexVSCodeOnlyLocalConfigDir drives the full wizard with only the
// Codex VS Code extension (agents.vscode) enabled and the Codex CLI disabled.
// Because `al vscode` sets CODEX_HOME from agents.codex.local_config_dir, the
// wizard must still present the Codex local_config_dir confirm and persist the
// choice. Against the old single-AgentCodex prompt gating the confirm never
// rendered and no local_config_dir line was written, so this fails.
func TestRun_CodexVSCodeOnlyLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
# local_config_dir = false
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	sawCodexLocalConfigDirPrompt := false
	var summaryBody string
	ui := &MockUI{
		NoteFunc: func(title, body string) error {
			if title == messages.WizardSummaryTitle {
				summaryBody = body
			}
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error {
			if title == messages.WizardApprovalModeTitle {
				label, ok := approvalModeLabelForValue(config.ApprovalModeAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableAgentsTitle {
				*selected = []string{AgentVSCode}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardCodexLocalConfigDirPrompt {
				sawCodexLocalConfigDirPrompt = true
				*value = true
			}
			if title == messages.WizardApplyChangesPrompt {
				*value = true
			}
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	assert.True(t, sawCodexLocalConfigDirPrompt, "enabling only the Codex VS Code extension must still prompt for local_config_dir")

	// The pre-apply confirmation summary the user reviews must report the enabled
	// setting. codexToggleVisible is CLI-only, so gating the summary line on it
	// would silently omit "Codex local home: enabled" for a VS Code-only repo even
	// though local_config_dir = true is written below.
	assert.Contains(t, summaryBody, messages.WizardSummaryCodexLocalConfigDir,
		"VS Code-only Codex local home must be shown in the confirmation summary")

	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), "local_config_dir = true")
}

// TestPromptModels_FeatureTogglesPreSelectAndRoundTrip proves the checkbox->
// disable inversion at the prompt boundary: a mixed enabled/disabled config
// pre-checks exactly the enabled features, and a no-edit re-run (the user leaves
// the pre-selection untouched) leaves every disable-sense field unchanged. It
// must fail if the inversion is wrong in either direction.
func TestPromptModels_FeatureTogglesPreSelectAndRoundTrip(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentClaude] = true
	choices.EnabledAgents[AgentCodex] = true

	// Mixed starting state: IDE reading and connectors disabled (so unchecked);
	// memory and AskUserQuestion enabled (so checked). Apps enabled (checked);
	// plugins enabled (checked); browser disabled (unchecked).
	choices.ClaudeDisableIDEReading = true
	choices.ClaudeDisableMemory = false
	choices.ClaudeDisableConnectors = true
	choices.ClaudeDisableQuestionTool = false
	choices.CodexApps = true
	choices.CodexPlugins = true
	choices.CodexDisableBrowser = true

	// Capture the labels pre-selected for each group, then echo them back
	// unchanged (a no-edit re-run).
	preSelected := map[string][]string{}
	ui := &MockUI{
		SelectFunc: func(string, []string, *string) error { return nil },
		MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
			if title == messages.WizardClaudeFeaturesTitle || title == messages.WizardCodexFeaturesTitle {
				captured := make([]string, len(*selected))
				copy(captured, *selected)
				preSelected[title] = captured
			}
			return nil // leave *selected untouched = no edits
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}

	// Pre-selection contains exactly the enabled features (inverted from the
	// disable-sense fields).
	assert.ElementsMatch(t,
		[]string{messages.WizardClaudeFeatureMemoryLabel, messages.WizardClaudeFeatureQuestionToolLabel},
		preSelected[messages.WizardClaudeFeaturesTitle],
		"only enabled Claude features should be pre-checked")
	assert.ElementsMatch(t,
		[]string{messages.WizardCodexFeatureAppsLabel, messages.WizardCodexFeaturePluginsLabel},
		preSelected[messages.WizardCodexFeaturesTitle],
		"only enabled Codex features should be pre-checked")

	// Round-trip: echoing the pre-selection back unchanged leaves every
	// disable-sense field at its original value.
	assert.True(t, choices.ClaudeDisableIDEReading)
	assert.False(t, choices.ClaudeDisableMemory)
	assert.True(t, choices.ClaudeDisableConnectors)
	assert.False(t, choices.ClaudeDisableQuestionTool)
	assert.True(t, choices.CodexApps)
	assert.True(t, choices.CodexPlugins)
	assert.True(t, choices.CodexDisableBrowser)

	// All toggles are marked touched after the prompt.
	assert.True(t, choices.ClaudeDisableIDEReadingTouched)
	assert.True(t, choices.ClaudeDisableMemoryTouched)
	assert.True(t, choices.ClaudeDisableConnectorsTouched)
	assert.True(t, choices.ClaudeDisableQuestionToolTouched)
	assert.True(t, choices.CodexAppsTouched)
	assert.True(t, choices.CodexPluginsTouched)
	assert.True(t, choices.CodexDisableBrowserTouched)
}

// TestPromptModels_CodexDisabledRendersNoCodexMultiSelect confirms that when
// Codex is not enabled, the Codex feature multi-select never renders and the
// Codex disable-sense fields stay untouched.
func TestPromptModels_CodexDisabledRendersNoCodexMultiSelect(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentClaude] = true // Claude on, Codex off

	var sawCodexFeatures bool
	ui := &MockUI{
		SelectFunc:  func(string, []string, *string) error { return nil },
		ConfirmFunc: func(string, *bool) error { return nil },
		MultiSelectFunc: func(title string, _ []string, _ *[]string) error {
			if title == messages.WizardCodexFeaturesTitle {
				sawCodexFeatures = true
			}
			return nil
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}

	assert.False(t, sawCodexFeatures, "Codex feature multi-select must not render when Codex is disabled")
	assert.False(t, choices.CodexAppsTouched)
	assert.False(t, choices.CodexDisableBrowserTouched)
}

// TestPromptModels_VSCodeOnlyPromptsCodexLocalConfigDir pins the prompt gating
// fix. With only the Codex VS Code extension (agents.vscode) enabled and the
// Codex CLI off, promptModels must present the local_config_dir confirm (it sets
// CODEX_HOME for the extension via `al vscode`) while the CLI-only Codex model,
// reasoning, and feature prompts stay hidden. Against the old single-AgentCodex
// gating the confirm never rendered, so CodexLocalConfigDirTouched stays false
// and this fails.
func TestPromptModels_VSCodeOnlyPromptsCodexLocalConfigDir(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentVSCode] = true // Codex VS Code extension only; CLI off

	var sawCodexLocalConfigDirPrompt bool
	var sawCodexModel, sawCodexReasoning, sawCodexFeatures bool
	ui := &MockUI{
		SelectFunc: func(title string, _ []string, _ *string) error {
			switch title {
			case messages.WizardCodexModelTitle:
				sawCodexModel = true
			case messages.WizardCodexReasoningEffortTitle:
				sawCodexReasoning = true
			}
			return nil
		},
		MultiSelectFunc: func(title string, _ []string, _ *[]string) error {
			if title == messages.WizardCodexFeaturesTitle {
				sawCodexFeatures = true
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardCodexLocalConfigDirPrompt {
				sawCodexLocalConfigDirPrompt = true
				*value = true
			}
			return nil
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}

	assert.True(t, sawCodexLocalConfigDirPrompt, "VS Code-only repos must be offered the Codex local_config_dir confirm")
	assert.True(t, choices.CodexLocalConfigDir, "confirming the prompt must record the choice")
	assert.True(t, choices.CodexLocalConfigDirTouched, "confirming the prompt must mark the choice touched")

	assert.False(t, sawCodexModel, "Codex model prompt is CLI-gated and must not render for VS Code-only repos")
	assert.False(t, sawCodexReasoning, "Codex reasoning prompt is CLI-gated and must not render for VS Code-only repos")
	assert.False(t, sawCodexFeatures, "Codex feature toggles are CLI-gated and must not render for VS Code-only repos")
	assert.False(t, choices.CodexModelTouched)
	assert.False(t, choices.CodexReasoningTouched)
	assert.False(t, choices.CodexAppsTouched)
	assert.False(t, choices.CodexDisableBrowserTouched)
}
