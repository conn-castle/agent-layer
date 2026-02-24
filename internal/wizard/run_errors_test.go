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

func TestRun_ApprovalModeLabelUnknown(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				*current = "unknown-mode-label"
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown approval mode selection")
}

func TestRun_SelectError_Approvals(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Approval Mode" {
				return errors.New("select error")
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "select error")
}

func TestRun_MultiSelectError_Agents(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				return errors.New("multiselect error")
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiselect error")
}

func TestRun_NoteError_PreviewModelWarning(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error {
			if title == "Preview Model Warning" {
				return errors.New("preview note error")
			}
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"gemini"}
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "preview note error")
}

func TestRun_SelectError_GeminiModel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	callCount := 0
	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Gemini Model" {
				return errors.New("gemini model error")
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"gemini"}
			}
			return nil
		},
	}
	_ = callCount

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gemini model error")
}

func TestRun_SelectError_ClaudeModel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Claude Model" {
				return errors.New("claude model error")
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"claude"}
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claude model error")
}

func TestRun_SelectError_ClaudeReasoning(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			switch title {
			case messages.WizardClaudeModelTitle:
				*current = "opus" // must select opus to trigger reasoning prompt
			case messages.WizardClaudeReasoningEffortTitle:
				return errors.New("claude reasoning error")
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableAgentsTitle {
				*selected = []string{AgentClaude}
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claude reasoning error")
}

func TestRun_SelectError_CodexModel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Codex Model" {
				return errors.New("codex model error")
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"codex"}
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "codex model error")
}

func TestRun_SelectError_CodexReasoning(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error {
			if title == "Codex Reasoning Effort" {
				return errors.New("codex reasoning error")
			}
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{"codex"}
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "codex reasoning error")
}

func TestRun_ConfirmError_RestoreMissing(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	// Config without MCP servers so missing defaults trigger
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			if confirmCalls == 1 {
				// First confirm is restore missing
				return errors.New("restore confirm error")
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "restore confirm error")
}

func TestRun_MultiSelectError_MCPServers(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				return errors.New("mcp multiselect error")
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mcp multiselect error")
}

func TestRun_NoteError_Summary(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc: func(title, body string) error {
			if title == "Summary of Changes" {
				return errors.New("summary note error")
			}
			return nil
		},
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "summary note error")
}

func TestRun_ConfirmError_Apply(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				return errors.New("apply confirm error")
			}
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apply confirm error")
}

func TestRun_EnvFileReadError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	// Create .env as a directory so ReadFile fails with permission-like error
	require.NoError(t, os.Mkdir(filepath.Join(configDir, ".env"), 0755))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
}

func TestRun_EnvFileParseError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	// Invalid env file content
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("INVALID LINE WITHOUT EQUALS"), 0600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid env file")
}

func TestRun_LoadDefaultMCPServersError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{}

	orig := loadDefaultMCPServersFunc
	defer func() { loadDefaultMCPServersFunc = orig }()
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return nil, errors.New("mcp load error")
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load default MCP servers")
}

func TestRun_LoadWarningDefaultsError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{}

	orig := loadWarningDefaultsFunc
	defer func() { loadWarningDefaultsFunc = orig }()
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{}, errors.New("warnings load error")
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load warning defaults")
}
