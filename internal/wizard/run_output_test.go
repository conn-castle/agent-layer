package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRunWithWriter_InstallCancelledPrintsToWriter(t *testing.T) {
	root := t.TempDir()
	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardInstallPrompt {
				*value = false
			}
			return nil
		},
	}

	var out bytes.Buffer
	err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), messages.WizardExitWithoutChanges)
}

func TestRunWithWriter_InstallEscapeBackPrintsToWriter(t *testing.T) {
	root := t.TempDir()
	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardInstallPrompt {
				return errWizardBack
			}
			return nil
		},
	}

	var out bytes.Buffer
	err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), messages.WizardExitWithoutChanges)
}

func TestRunWithWriter_ApplyCancelledPrintsToWriter(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	ui := &MockUI{
		NoteFunc:        func(string, string) error { return nil },
		SelectFunc:      func(string, []string, *string) error { return nil },
		MultiSelectFunc: func(string, []string, *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = false
			}
			return nil
		},
	}

	var out bytes.Buffer
	err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), messages.WizardExitWithoutChanges)
}

func TestRunWithWriter_FreshInstallPromptCancelledDoesNotClaimNoChanges(t *testing.T) {
	root := t.TempDir()
	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardInstallPrompt, messages.WizardEnableAgentLayerInstallPrompt:
				*value = true
			}
			return nil
		},
		SelectFunc: func(string, []string, *string) error {
			return errWizardCancelled
		},
	}

	var out bytes.Buffer
	err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "0.0.0", &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), messages.WizardInstallComplete)
	require.NotContains(t, out.String(), messages.WizardExitWithoutChanges)
}

func TestRunWithWriter_FreshInstallApplyCancelledDoesNotClaimNoChanges(t *testing.T) {
	root := t.TempDir()
	ui := &MockUI{
		NoteFunc:        func(string, string) error { return nil },
		SelectFunc:      func(string, []string, *string) error { return nil },
		MultiSelectFunc: func(string, []string, *[]string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardInstallPrompt, messages.WizardEnableAgentLayerInstallPrompt:
				*value = true
			case messages.WizardApplyChangesPrompt:
				*value = false
			}
			return nil
		},
	}

	var out bytes.Buffer
	err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "0.0.0", &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), messages.WizardInstallComplete)
	require.NotContains(t, out.String(), messages.WizardExitWithoutChanges)
}
