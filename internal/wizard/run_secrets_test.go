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
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRun_WithSecrets(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Config with no MCP servers, so 'tavily' is missing default
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
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == "Enable Agents" {
				*selected = []string{}
			}
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			if title == "Enter AL_TAVILY_API_KEY (leave blank to skip)" {
				*value = "my-token"
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			// Confirm every prompt (apply, warnings, etc.).
			*value = true
			return nil
		},
	}

	mockSync := func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	// Verify .env
	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=my-token`)
}

func TestRun_SecretsExisting(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
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
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0600))
	// Pre-existing secret
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_TAVILY_API_KEY=old-token"), 0600))

	// Case 1: Do NOT override
	t.Run("no override", func(t *testing.T) {
		ui := &MockUI{
			NoteFunc:   func(title, body string) error { return nil },
			SelectFunc: func(title string, options []string, current *string) error { return nil },
			MultiSelectFunc: func(title string, options []string, selected *[]string) error {
				if title == messages.WizardEnableDefaultMCPServersTitle {
					*selected = []string{"tavily"}
				}
				return nil
			},
			ConfirmFunc: func(title string, value *bool) error {
				switch title {
				case fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, "AL_TAVILY_API_KEY"):
					*value = false // No
				case messages.WizardApplyChangesPrompt:
					*value = true
				default:
					*value = true // restore
				}
				return nil
			},
		}

		err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
		require.NoError(t, err)

		envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
		assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=old-token`)
	})

	// Case 2: Override
	t.Run("override", func(t *testing.T) {
		ui := &MockUI{
			NoteFunc:   func(title, body string) error { return nil },
			SelectFunc: func(title string, options []string, current *string) error { return nil },
			MultiSelectFunc: func(title string, options []string, selected *[]string) error {
				if title == messages.WizardEnableDefaultMCPServersTitle {
					*selected = []string{"tavily"}
				}
				return nil
			},
			ConfirmFunc: func(title string, value *bool) error {
				switch title {
				case fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, "AL_TAVILY_API_KEY"):
					*value = true // Yes
				case messages.WizardApplyChangesPrompt:
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

		err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
		require.NoError(t, err)

		envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
		assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=new-token`)
	})
}

func TestRun_SecretFromEnv(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "env-token")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=env-token`)
}

func TestRun_SecretFromEnv_ConfirmError(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "env-token")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "env confirm error")
}

func TestRun_SecretFromEnv_Declined(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "env-token")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == fmt.Sprintf(messages.WizardEnvSecretFoundPromptFmt, "AL_TAVILY_API_KEY") {
				confirmCalls++
				*value = false
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=manual-token`)
}

func TestRun_SecretInputError(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "secret input error")
}

func TestRun_SecretBlank_DisableMCP(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	secretInputCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)
	assert.Equal(t, 1, secretInputCalls)
}

func TestRun_SecretBlank_DisableMCP_ConfirmError(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disable confirm error")
}

func TestRun_SecretBlank_Retry(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600))

	secretInputCalls := 0
	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == fmt.Sprintf(messages.WizardSecretMissingDisablePromptFmt, "AL_TAVILY_API_KEY", "tavily") {
				confirmCalls++
				*value = false
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)
	assert.Equal(t, 2, secretInputCalls)

	envData, _ := os.ReadFile(filepath.Join(configDir, ".env"))
	assert.Contains(t, string(envData), `AL_TAVILY_API_KEY=retry-token`)
}

func TestRun_ExistingSecret_OverrideConfirmError(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_TAVILY_API_KEY=existing"), 0600))

	confirmCalls := 0
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
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

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "override confirm error")
}

func TestRun_SecretInputSkip_DisablesServer(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			*value = "skip"
			return nil
		},
	}

	err := Run(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)

	configData, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	// tavily is an absent default that the user selected, but skipping its required
	// secret sets EnabledMCPServers[tavily] = false. A disabled *missing* default is
	// never added, so no tavily block is written. The user can re-enable it later by
	// re-running the wizard and supplying the secret.
	assert.NotContains(t, string(configData), `id = "tavily"`)

	// The skipped secret must not leak into .env: neither the key nor the "skip"
	// sentinel should be persisted.
	envData, err := os.ReadFile(filepath.Join(configDir, ".env"))
	require.NoError(t, err)
	assert.NotContains(t, string(envData), "AL_TAVILY_API_KEY")
	assert.NotContains(t, string(envData), "skip")
}

func TestRun_SecretInputCancel_StopsWithoutApply(t *testing.T) {
	t.Setenv("AL_TAVILY_API_KEY", "")
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	validConfig := basicAgentConfig()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	syncCalled := false
	ui := &MockUI{
		NoteFunc:   func(title, body string) error { return nil },
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{"tavily"}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			*value = "cancel"
			return nil
		},
	}

	originalConfig, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	err = Run(root, ui, func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}, "")
	require.NoError(t, err)
	assert.False(t, syncCalled)

	updatedConfig, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, string(originalConfig), string(updatedConfig))
}
