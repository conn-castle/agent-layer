package wizard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nicholasjconn/agent-layer/internal/templates"
)

func TestLoadWarningDefaults(t *testing.T) {
	defaults, err := loadWarningDefaults()
	require.NoError(t, err)
	require.Equal(t, 10000, defaults.InstructionTokenThreshold)
	require.Equal(t, 15, defaults.MCPServerThreshold)
	require.Equal(t, 60, defaults.MCPToolsTotalThreshold)
	require.Equal(t, 25, defaults.MCPServerToolsThreshold)
	require.Equal(t, 10000, defaults.MCPSchemaTokensTotalThreshold)
	require.Equal(t, 7500, defaults.MCPSchemaTokensServerThreshold)
}

func TestLoadWarningDefaultsReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadWarningDefaults()
	require.Error(t, err)
}

func TestLoadWarningDefaultsIncomplete(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		// Return valid TOML with required fields but incomplete warnings config
		return []byte(`[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[warnings]
instruction_token_threshold = 10000
`), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadWarningDefaults()
	require.Error(t, err)
	require.Contains(t, err.Error(), "incomplete")
}
