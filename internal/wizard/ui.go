package wizard

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/terminal"
)

// UI defines the interaction methods.
type UI interface {
	Select(title string, options []string, current *string) error
	MultiSelect(title string, options []string, selected *[]string) error
	Confirm(title string, value *bool) error
	Input(title string, value *string) error
	SecretInput(title string, value *string) error
	Note(title string, body string) error
}

// HuhUI implements UI using charmbracelet/huh.
type HuhUI struct {
	isTerminal func() bool
}

var runFormFunc = func(form *huh.Form) error { return form.Run() }

// NewHuhUI creates a new HuhUI using the default terminal check.
// The default implementation uses terminal.IsInteractive().
func NewHuhUI() *HuhUI {
	return &HuhUI{isTerminal: terminal.IsInteractive}
}

// ensureInteractive returns an error when the UI is invoked without a terminal.
func (ui *HuhUI) ensureInteractive() error {
	checker := ui.isTerminal
	if checker == nil {
		checker = terminal.IsInteractive
	}
	if checker() {
		return nil
	}
	return fmt.Errorf(messages.WizardRequiresTerminal)
}

// runForm validates terminal availability and runs the provided form.
func (ui *HuhUI) runForm(form *huh.Form) error {
	if err := ui.ensureInteractive(); err != nil {
		return err
	}
	return runFormFunc(form)
}

// Select renders a single-choice prompt.
func (ui *HuhUI) Select(title string, options []string, current *string) error {
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Options(opts...).
				Value(current),
		),
	))
}

// MultiSelect renders a multi-choice prompt.
func (ui *HuhUI) MultiSelect(title string, options []string, selected *[]string) error {
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(title).
				Description(messages.WizardMultiSelectDescription).
				Options(opts...).
				Value(selected),
		),
	))
}

// Confirm renders a yes/no prompt.
func (ui *HuhUI) Confirm(title string, value *bool) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Value(value),
		),
	))
}

// Input renders a plain text input prompt.
func (ui *HuhUI) Input(title string, value *string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Value(value),
		),
	))
}

// SecretInput renders a masked input prompt for secrets.
func (ui *HuhUI) SecretInput(title string, value *string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Value(value).
				EchoMode(huh.EchoModePassword),
		),
	))
}

// Note renders an informational note screen.
func (ui *HuhUI) Note(title string, body string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(body),
		),
	))
}
