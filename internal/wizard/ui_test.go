package wizard

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHuhUI_RunFormMapsCtrlCAbortToWizardCancelled(t *testing.T) {
	ui := &HuhUI{isTerminal: func() bool { return true }}
	origRunForm := runFormFunc
	t.Cleanup(func() {
		runFormFunc = origRunForm
	})

	runFormFunc = func(form *huh.Form) error {
		// Simulate the key filter detecting Ctrl+C before the form aborts.
		ui.ctrlCAbort = true
		return huh.ErrUserAborted
	}

	var res string
	err := ui.Input("Title", &res)
	assert.ErrorIs(t, err, errWizardCancelled)
}

func TestFormFilter_CtrlCKeySetsCancelFlag(t *testing.T) {
	ui := &HuhUI{}
	filter := ui.formFilter()

	msg := filter(nil, tea.KeyMsg{Type: tea.KeyCtrlC})

	assert.True(t, ui.ctrlCAbort, "Ctrl+C key should set ctrlCAbort flag")
	// KeyMsg should pass through unchanged.
	assert.IsType(t, tea.KeyMsg{}, msg)
}

func TestFormFilter_InterruptMsgConvertsToQuitMsg(t *testing.T) {
	ui := &HuhUI{}
	filter := ui.formFilter()

	msg := filter(nil, tea.InterruptMsg{})

	// InterruptMsg alone should not set ctrlCAbort — both Esc and Ctrl+C
	// produce InterruptMsg via huh's CancelCmd. The Esc/Ctrl+C distinction
	// relies on the earlier KeyMsg handler setting the flag only for Ctrl+C.
	assert.False(t, ui.ctrlCAbort, "InterruptMsg alone should not set ctrlCAbort")
	assert.IsType(t, tea.QuitMsg{}, msg, "InterruptMsg should be converted to QuitMsg")
}

func TestFormFilter_OtherMsgPassesThrough(t *testing.T) {
	ui := &HuhUI{}
	filter := ui.formFilter()

	msg := filter(nil, tea.WindowSizeMsg{Width: 80, Height: 24})

	assert.False(t, ui.ctrlCAbort, "Non-abort message should not set ctrlCAbort")
	assert.IsType(t, tea.WindowSizeMsg{}, msg)
}

func TestHuhUI_RunFormResetsCtrlCAbortBetweenForms(t *testing.T) {
	ui := &HuhUI{isTerminal: func() bool { return true }}
	origRunForm := runFormFunc
	t.Cleanup(func() {
		runFormFunc = origRunForm
	})

	// First form: Ctrl+C sets the flag.
	runFormFunc = func(form *huh.Form) error {
		ui.ctrlCAbort = true
		return huh.ErrUserAborted
	}
	var res string
	err := ui.Input("First", &res)
	require.ErrorIs(t, err, errWizardCancelled)

	// Second form: no Ctrl+C. The flag must be reset.
	runFormFunc = func(form *huh.Form) error {
		return huh.ErrUserAborted
	}
	err = ui.Input("Second", &res)
	assert.ErrorIs(t, err, errWizardBack)
}

func TestHintField_WithPositionRestoresBindings(t *testing.T) {
	// Create a hintField wrapping a real MultiSelect.
	inner := huh.NewMultiSelect[string]().
		Title("Test").
		Options(huh.NewOption("A", "a")).
		Filterable(false)
	wrapped := newHintField(inner)

	// Simulate what huh does: call WithPosition with first+last (single field form).
	wrapped.WithPosition(huh.FieldPosition{
		Group:      0,
		Field:      0,
		FirstField: 0,
		LastField:  0,
		FirstGroup: 0,
		LastGroup:  0,
	})

	// After WithPosition, the bindings should still be enabled
	// because the wrapper re-applies the keymap.
	binds := wrapped.KeyBinds()
	var foundPrev, foundNext bool
	for _, b := range binds {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		if h.Key == "esc" && h.Desc == "back" {
			foundPrev = true
		}
		if h.Key == "ctrl+c" && h.Desc == "exit" {
			foundNext = true
		}
	}
	assert.True(t, foundPrev, "Prev (esc/back) hint should be enabled after WithPosition")
	assert.True(t, foundNext, "Next (ctrl+c/exit) hint should be enabled after WithPosition")
}

func TestHintField_SurvivesFormConstruction(t *testing.T) {
	// Create a form exactly as the wizard does. NewForm calls
	// UpdateFieldPositions which calls WithPosition on each field.
	// Verify the hint bindings survive the full construction flow.
	form := huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewMultiSelect[string]().
				Title("Test").
				Filterable(false).
				Options(huh.NewOption("A", "a"), huh.NewOption("B", "b"))),
		),
	)
	form.WithKeyMap(wizardKeyMap())

	binds := form.KeyBinds()
	var hints []string
	for _, b := range binds {
		if b.Enabled() {
			hints = append(hints, b.Help().Key+" "+b.Help().Desc)
		}
	}
	assert.Contains(t, hints, "esc back", "Prev hint should be visible after full form construction")
	assert.Contains(t, hints, "ctrl+c exit", "Next hint should be visible after full form construction")
}

func TestHintField_WithoutWrapperBindingsDisabled(t *testing.T) {
	// Verify the problem: without the wrapper, Prev and Next are disabled.
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Test").
				Filterable(false).
				Options(huh.NewOption("A", "a"), huh.NewOption("B", "b")),
		),
	)
	form.WithKeyMap(wizardKeyMap())

	binds := form.KeyBinds()
	for _, b := range binds {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		if h.Key == "esc" && h.Desc == "back" {
			t.Fatal("Without wrapper, Prev (esc/back) should be disabled by WithPosition")
		}
		if h.Key == "ctrl+c" && h.Desc == "exit" {
			t.Fatal("Without wrapper, Next (ctrl+c/exit) should be disabled by WithPosition")
		}
	}
}

func TestHintField_UpdatePreservesWrapper(t *testing.T) {
	inner := huh.NewInput().Title("Test")
	wrapped := newHintField(inner)

	// Calling Update should return the wrapper, not the inner field.
	model, _ := wrapped.Update(nil)
	_, ok := model.(*hintField)
	assert.True(t, ok, "Update should return the hintField wrapper")
}

func TestWizardKeyMap(t *testing.T) {
	km := wizardKeyMap()

	t.Run("Quit binding includes esc and ctrl+c", func(t *testing.T) {
		keys := km.Quit.Keys()
		assert.Contains(t, keys, "ctrl+c")
		assert.Contains(t, keys, "esc")
	})

	t.Run("Prev bindings show esc/back hint", func(t *testing.T) {
		assert.Equal(t, []string{"esc"}, km.MultiSelect.Prev.Keys())
		assert.Equal(t, "esc", km.MultiSelect.Prev.Help().Key)
		assert.Equal(t, "back", km.MultiSelect.Prev.Help().Desc)

		assert.Equal(t, []string{"esc"}, km.Select.Prev.Keys())
		assert.Equal(t, []string{"esc"}, km.Confirm.Prev.Keys())
		assert.Equal(t, []string{"esc"}, km.Input.Prev.Keys())
		assert.Equal(t, []string{"esc"}, km.Note.Prev.Keys())
	})

	t.Run("Next bindings show ctrl+c/exit hint", func(t *testing.T) {
		assert.Equal(t, []string{"ctrl+c"}, km.MultiSelect.Next.Keys())
		assert.Equal(t, "ctrl+c", km.MultiSelect.Next.Help().Key)
		assert.Equal(t, "exit", km.MultiSelect.Next.Help().Desc)

		assert.Equal(t, []string{"ctrl+c"}, km.Select.Next.Keys())
		assert.Equal(t, []string{"ctrl+c"}, km.Confirm.Next.Keys())
		assert.Equal(t, []string{"ctrl+c"}, km.Input.Next.Keys())
		assert.Equal(t, []string{"ctrl+c"}, km.Note.Next.Keys())
	})

	t.Run("Select filtering disabled", func(t *testing.T) {
		assert.False(t, km.Select.Filter.Enabled())
		assert.False(t, km.Select.SetFilter.Enabled())
		assert.False(t, km.Select.ClearFilter.Enabled())
	})
}
