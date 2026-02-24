package wizard

import (
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
)

func TestNewHuhUI(t *testing.T) {
	ui := NewHuhUI()
	assert.NotNil(t, ui)
	assert.NotNil(t, ui.isTerminal)
}

func TestHuhUI_EnsureInteractive_NilChecker(t *testing.T) {
	// Test with nil isTerminal - should use default
	ui := &HuhUI{isTerminal: nil}
	// This will fail because we're not in a TTY during tests,
	// but it exercises the nil fallback code path
	err := ui.ensureInteractive()
	// In test environment, defaultIsTerminal() returns false
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "interactive terminal")
}

// TestHuhUI_NoTTY verifies that we can call the methods (covering the code)
// even if they fail due to missing TTY.
func TestHuhUI_NoTTY(t *testing.T) {
	ui := &HuhUI{isTerminal: func() bool { return false }}

	t.Run("Select", func(t *testing.T) {
		var res string
		// Expect error because no TTY
		err := ui.Select("Title", []string{"A", "B"}, &res)
		assert.Error(t, err)
	})

	t.Run("MultiSelect", func(t *testing.T) {
		var res []string
		err := ui.MultiSelect("Title", []string{"A", "B"}, &res)
		assert.Error(t, err)
	})

	t.Run("Confirm", func(t *testing.T) {
		var res bool
		err := ui.Confirm("Title", &res)
		assert.Error(t, err)
	})

	t.Run("Input", func(t *testing.T) {
		var res string
		err := ui.Input("Title", &res)
		assert.Error(t, err)
	})

	t.Run("SecretInput", func(t *testing.T) {
		var res string
		err := ui.SecretInput("Title", &res)
		assert.Error(t, err)
	})

	t.Run("Note", func(t *testing.T) {
		err := ui.Note("Title", "Body")
		assert.Error(t, err)
	})
}

func TestHuhUI_RunFormSuccess(t *testing.T) {
	ui := &HuhUI{isTerminal: func() bool { return true }}
	origRunForm := runFormFunc
	t.Cleanup(func() {
		runFormFunc = origRunForm
	})

	called := false
	runFormFunc = func(form *huh.Form) error {
		assert.NotNil(t, form)
		called = true
		return nil
	}

	var res string
	err := ui.Input("Title", &res)
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestHuhUI_RunFormMapsUserAbortToWizardBack(t *testing.T) {
	ui := &HuhUI{isTerminal: func() bool { return true }}
	origRunForm := runFormFunc
	t.Cleanup(func() {
		runFormFunc = origRunForm
	})

	runFormFunc = func(form *huh.Form) error {
		assert.NotNil(t, form)
		return huh.ErrUserAborted
	}

	var res string
	err := ui.Input("Title", &res)
	assert.ErrorIs(t, err, errWizardBack)
}
