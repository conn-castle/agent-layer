package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRun_NotInstalled_UserCancels(t *testing.T) {
	root := t.TempDir()

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardInstallPrompt {
				*value = false
				return nil
			}
			return nil
		},
	}

	mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)
	// Should return nil (Exit without changes)

	// Config should not exist
	_, err = os.Stat(filepath.Join(root, ".agent-layer", "config.toml"))
	assert.True(t, os.IsNotExist(err))
}

func TestRun_Install(t *testing.T) {
	root := t.TempDir()

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardInstallPrompt {
				*value = true
				return nil
			}
			// Fallback for apply
			if title == messages.WizardApplyChangesPrompt {
				*value = false // Stop after install for this test
				return nil
			}
			return nil
		},
		SelectFunc:      func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error { return nil },
		NoteFunc:        func(title, body string) error { return nil },
	}

	mockSync := func(root string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	// Verify install ran (config exists)
	_, err = os.Stat(filepath.Join(root, ".agent-layer", "config.toml"))
	assert.NoError(t, err)
}

func TestRun_ConfirmError_Install(t *testing.T) {
	root := t.TempDir()

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			return errors.New("confirm error")
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "confirm error")
}

func TestRun_InstallFailure(t *testing.T) {
	root := t.TempDir()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	// Create .agent-layer as an empty dir (no config.toml yet)
	require.NoError(t, os.MkdirAll(agentLayerDir, 0755))
	// Create a file where install expects to create the instructions directory
	// This will cause install to fail when it tries to mkdir
	require.NoError(t, os.WriteFile(filepath.Join(agentLayerDir, "instructions"), []byte("blocker"), 0644))

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			*value = true // Confirm install
			return nil
		},
	}

	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "install failed")
}

func TestRun_ConfigLoadFailure(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Write invalid TOML that will fail to parse
	invalidConfig := `[approvals
mode = "none"`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(invalidConfig), 0644))

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
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRun_ConfigLoadFailureAfterInstall(t *testing.T) {
	root := t.TempDir()
	// Do NOT call setupRepo - let install run

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			*value = true // Confirm install and apply
			return nil
		},
	}

	// Stub loadProjectConfigFunc to return a validation error after install succeeds
	// (simulates a config with missing required fields from a newer version).
	// Must wrap ErrConfigValidation so the narrowed lenient fallback triggers.
	orig := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: injected config load error", config.ErrConfigValidation)
	}
	t.Cleanup(func() { loadProjectConfigFunc = orig })

	// With lenient fallback, the wizard should proceed through the flow
	// (lenient loading reads the installed config without validation).
	err := Run(root, ui, func(r string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	assert.NoError(t, err)
}
