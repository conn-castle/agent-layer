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
