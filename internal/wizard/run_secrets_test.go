package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

func TestRun_WithSecrets(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Config with no MCP servers, so 'github' is missing default
	initialConfig := `[approvals]
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
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{}
			}
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			if title == "Enter AL_GITHUB_PERSONAL_ACCESS_TOKEN (leave blank to skip)" {
				*value = "my-token"
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			// "Default MCP server entries are missing..." -> Yes
			if title == "Apply these changes?" {
				*value = true
			}
			// Confirm restore?
			// The prompt is formatted: fmt.Sprintf("Default MCP server entries are missing from config.toml: %s. Restore them before selection?", ...)
			// We can check contains
			*value = true // Default yes for restore and apply
			return nil
		},
	}

	mockSync := func(r string) ([]warnings.Warning, error) { return nil, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	// Verify .env
	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=my-token`)
}

func TestRun_SecretsExisting(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
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
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0644))
	// Pre-existing secret
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_GITHUB_PERSONAL_ACCESS_TOKEN=old-token"), 0600))

	// Case 1: Do NOT override
	t.Run("no override", func(t *testing.T) {
		ui := &MockUI{
			NoteFunc:   func(title, body string) error { return nil },
			SelectFunc: func(title string, options []string, current *string) error { return nil },
			MultiSelectFunc: func(title string, options []string, selected *[]string) error {
				if title == "Enable Default MCP Servers" {
					*selected = []string{"github"}
				}
				return nil
			},
			ConfirmFunc: func(title string, value *bool) error {
				switch title {
				case fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, "AL_GITHUB_PERSONAL_ACCESS_TOKEN"):
					*value = false // No
				case "Apply these changes?":
					*value = true
				default:
					*value = true // restore
				}
				return nil
			},
		}

		err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
		require.NoError(t, err)

		envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
		assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=old-token`)
	})

	// Case 2: Override
	t.Run("override", func(t *testing.T) {
		ui := &MockUI{
			NoteFunc:   func(title, body string) error { return nil },
			SelectFunc: func(title string, options []string, current *string) error { return nil },
			MultiSelectFunc: func(title string, options []string, selected *[]string) error {
				if title == "Enable Default MCP Servers" {
					*selected = []string{"github"}
				}
				return nil
			},
			ConfirmFunc: func(title string, value *bool) error {
				switch title {
				case fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, "AL_GITHUB_PERSONAL_ACCESS_TOKEN"):
					*value = true // Yes
				case "Apply these changes?":
					*value = true
				default:
					*value = true
				}
				return nil
			},
			SecretInputFunc: func(title string, value *string) error {
				*value = "new-token"
				return nil
			},
		}

		err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
		require.NoError(t, err)

		envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
		assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=new-token`)
	})
}

func TestRun_SecretFromEnv(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "env-token")
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
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=env-token`)
}

func TestRun_SecretFromEnv_ConfirmError(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "env-token")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			// First confirm is for restore missing, second is env variable
			if confirmCalls == 2 {
				return errors.New("env confirm error")
			}
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "env confirm error")
}

func TestRun_SecretFromEnv_Declined(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "env-token")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			// Decline env import, accept rest
			if confirmCalls == 2 {
				*value = false // Decline env import
				return nil
			}
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			*value = "manual-token"
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=manual-token`)
}

func TestRun_SecretInputError(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
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
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			return errors.New("secret input error")
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "secret input error")
}

func TestRun_SecretBlank_DisableMCP(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	secretInputCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true // Yes to all confirms including disable
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			secretInputCalls++
			*value = "" // Leave blank
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)
	assert.Equal(t, 1, secretInputCalls)
}

func TestRun_SecretBlank_DisableMCP_ConfirmError(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			// Third confirm is disable MCP (after restore missing and env check)
			if confirmCalls == 2 {
				return errors.New("disable confirm error")
			}
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			*value = "" // Leave blank
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disable confirm error")
}

func TestRun_SecretBlank_Retry(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	secretInputCalls := 0
	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			// Second confirm is disable prompt - decline to retry
			if confirmCalls == 2 {
				*value = false // Don't disable, retry
				return nil
			}
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			secretInputCalls++
			if secretInputCalls == 1 {
				*value = "" // First try blank
			} else {
				*value = "retry-token" // Second try with value
			}
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	require.NoError(t, err)
	assert.Equal(t, 2, secretInputCalls)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_GITHUB_PERSONAL_ACCESS_TOKEN=retry-token`)
}

func TestRun_ExistingSecret_OverrideConfirmError(t *testing.T) {
	t.Setenv("AL_GITHUB_PERSONAL_ACCESS_TOKEN", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_GITHUB_PERSONAL_ACCESS_TOKEN=existing"), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Default MCP Servers" {
				*selected = []string{"github"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			confirmCalls++
			// Second confirm is override prompt
			if confirmCalls == 2 {
				return errors.New("override confirm error")
			}
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) ([]warnings.Warning, error) { return nil, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "override confirm error")
}
