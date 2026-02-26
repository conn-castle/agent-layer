package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRun_HappyPath(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "none"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error {
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(ApprovalAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"gemini"}
			}
			if title == "Enable Default MCP Servers" {
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

	// Verify config updated
	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), `mode = "all"`)
	assert.Contains(t, string(data), `[agents.gemini]`)
	assert.Contains(t, string(data), `enabled = true`)
}

func TestRun_ApplyCancel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	validConfig := `[approvals]
mode = "none"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
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
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
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

func TestRun_RestoreDefaults(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Config with empty [mcp] or missing [mcp]
	initialConfig := `[approvals]
mode = "all"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			// "Default MCP server entries are missing..." -> Yes
			*value = true
			return nil
		},
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		NoteFunc:        func(title, body string) error { return nil },
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	// Verify restored in config
	data, _ := os.ReadFile(filepath.Join(configDir, "config.toml"))
	assert.Contains(t, string(data), `id = "tavily"`)
}

func TestRun_ClaudeLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "all"
[agents.gemini]
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
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(ApprovalAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"claude"}
			}
			if title == "Enable Default MCP Servers" {
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
[agents.gemini]
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
[agents.antigravity]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				label, ok := approvalModeLabelForValue(ApprovalAll)
				require.True(t, ok)
				*current = label
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"claude_vscode"}
			}
			if title == "Enable Default MCP Servers" {
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
