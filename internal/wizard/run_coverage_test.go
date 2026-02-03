package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

func TestRun_DefaultServersFromConfig(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	config := `[approvals]
mode = "all"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false

[mcp]
[[mcp.servers]]
id = "local"
enabled = true
transport = "stdio"
command = "tool"

[[mcp.servers]]
id = "empty-key"
enabled = true
transport = "stdio"
command = "tool"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	origDefaults := loadDefaultMCPServersFunc
	origWarnings := loadWarningDefaultsFunc
	t.Cleanup(func() {
		loadDefaultMCPServersFunc = origDefaults
		loadWarningDefaultsFunc = origWarnings
	})
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return []DefaultMCPServer{
			{ID: "local", RequiredEnv: []string{}},
			{ID: "empty-key", RequiredEnv: []string{""}},
		}, nil
	}
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{
			InstructionTokenThreshold:      100,
			MCPServerThreshold:             100,
			MCPToolsTotalThreshold:         100,
			MCPServerToolsThreshold:        100,
			MCPSchemaTokensTotalThreshold:  100,
			MCPSchemaTokensServerThreshold: 100,
		}, nil
	}

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = false
			}
			return nil
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)
}

func TestRun_InvalidEnvFile(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	config := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o644))
	envPath := filepath.Join(configDir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("AL_TOKEN=valid"), 0o600))

	origDefaults := loadDefaultMCPServersFunc
	origWarnings := loadWarningDefaultsFunc
	t.Cleanup(func() {
		loadDefaultMCPServersFunc = origDefaults
		loadWarningDefaultsFunc = origWarnings
	})
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return []DefaultMCPServer{{ID: "github", RequiredEnv: []string{"AL_TOKEN"}}}, nil
	}
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{InstructionTokenThreshold: 100, MCPServerThreshold: 100}, nil
	}

	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			return os.WriteFile(envPath, []byte("INVALID LINE"), 0o600)
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.Error(t, err)
}

func TestRun_EnvReadError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	config := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o644))
	envPath := filepath.Join(configDir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("AL_TOKEN=valid"), 0o600))

	origDefaults := loadDefaultMCPServersFunc
	origWarnings := loadWarningDefaultsFunc
	t.Cleanup(func() {
		loadDefaultMCPServersFunc = origDefaults
		loadWarningDefaultsFunc = origWarnings
	})
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return []DefaultMCPServer{{ID: "github", RequiredEnv: []string{"AL_TOKEN"}}}, nil
	}
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{InstructionTokenThreshold: 100, MCPServerThreshold: 100}, nil
	}

	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if err := os.Remove(envPath); err != nil {
				return err
			}
			return os.Mkdir(envPath, 0o755)
		},
	}

	err := Run(root, ui, func(string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.Error(t, err)
}
