package wizard

import (
	"github.com/charmbracelet/huh"
)

// UI defines the interaction methods.
type UI interface {
	Select(title string, options []string, current *string) error
	MultiSelect(title string, options []string, selected *[]string) error
	Confirm(title string, value *bool) error
	SecretInput(title string, value *string) error
	Note(title string, body string) error
}

// HuhUI implements UI using charmbracelet/huh.
type HuhUI struct{}

// NewHuhUI creates a new HuhUI.
func NewHuhUI() *HuhUI {
	return &HuhUI{}
}

// Select renders a single-choice prompt.
func (ui *HuhUI) Select(title string, options []string, current *string) error {
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Options(opts...).
				Value(current),
		),
	).Run()
}

// MultiSelect renders a multi-choice prompt.
func (ui *HuhUI) MultiSelect(title string, options []string, selected *[]string) error {
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(title).
				Description("Space to toggle, Enter to continue").
				Options(opts...).
				Value(selected),
		),
	).Run()
}

// Confirm renders a yes/no prompt.
func (ui *HuhUI) Confirm(title string, value *bool) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Value(value),
		),
	).Run()
}

// SecretInput renders a masked input prompt for secrets.
func (ui *HuhUI) SecretInput(title string, value *string) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Value(value).
				EchoMode(huh.EchoModePassword),
		),
	).Run()
}

// Note renders an informational note screen.
func (ui *HuhUI) Note(title string, body string) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(body),
		),
	).Run()
}
