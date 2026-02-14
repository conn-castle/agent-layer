package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/warnings"
)

func TestRun_WarningsConfirmError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			if strings.Contains(title, "Enable warnings") {
				return errors.New("warnings confirm error")
			}
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "warnings confirm error")
}

func TestRun_WarningsEnabled_UsesDefaultsWithoutThresholdPrompts(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	inputCalls := 0
	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		InputFunc: func(title string, value *string) error {
			inputCalls++
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)
	assert.Equal(t, 0, inputCalls, "wizard should not prompt for warning threshold values")

	data, readErr := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, readErr)
	output := string(data)
	assert.Contains(t, output, "[warnings]")
	assert.Contains(t, output, "instruction_token_threshold = 10000")
	assert.Contains(t, output, "mcp_server_threshold = 15")
	assert.Contains(t, output, "mcp_tools_total_threshold = 60")
	assert.Contains(t, output, "mcp_server_tools_threshold = 25")
	assert.Contains(t, output, "mcp_schema_tokens_total_threshold = 30000")
	assert.Contains(t, output, "mcp_schema_tokens_server_threshold = 20000")
}

func TestRun_WarningsDisabled_RemovesWarningsSection(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	ui := &MockUI{
		NoteFunc:        func(title, body string) error { return nil },
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			if strings.Contains(title, "Enable warnings") {
				*value = false
				return nil
			}
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)

	data, readErr := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "[warnings]")
}
