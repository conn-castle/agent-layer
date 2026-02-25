package wizard

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
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
	ctrlCAbort bool // set by key filter during form.Run(); reset before each form
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

// wizardKeyMap returns a custom keymap for wizard forms.
// Esc triggers form abort (mapped to back navigation) and Ctrl+C triggers
// form abort (mapped to hard exit). The field-level Prev and Next bindings
// are repurposed as display-only hints — the form intercepts both keys at
// the Quit level before any field binding can fire.
func wizardKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()

	// Both Esc and Ctrl+C trigger form abort; runForm distinguishes them via ctrlCAbort flag.
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))

	// Display-only Prev: shows "esc • back" in hints bar.
	escBack := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	km.MultiSelect.Prev = escBack
	km.Select.Prev = escBack
	km.Confirm.Prev = escBack
	km.Input.Prev = escBack
	km.Note.Prev = escBack

	// Display-only Next: shows "ctrl+c • exit" in hints bar.
	ctrlCExit := key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "exit"))
	km.MultiSelect.Next = ctrlCExit
	km.Select.Next = ctrlCExit
	km.Confirm.Next = ctrlCExit
	km.Input.Next = ctrlCExit
	km.Note.Next = ctrlCExit

	// Disable filtering — wizard lists are small and filter mode would
	// conflict with Esc-to-back since the form intercepts Esc first.
	km.Select.Filter.SetEnabled(false)
	km.Select.SetFilter.SetEnabled(false)
	km.Select.ClearFilter.SetEnabled(false)

	return km
}

// hintField wraps a huh.Field so that the Prev ("esc"/"back") and Next
// ("ctrl+c"/"exit") hint bindings remain visible in the help bar.
//
// huh's UpdateFieldPositions (called on every KeyMsg and during NewForm)
// calls WithPosition on each field, which disables Prev for the first field
// and Next for the last field.  Since every wizard form has a single field,
// both are always disabled.  This wrapper intercepts WithPosition and
// immediately re-applies the wizard keymap to restore the hint bindings.
type hintField struct {
	huh.Field
	km *huh.KeyMap
}

// Update delegates to the inner field and re-wraps so the wrapper stays in
// the group's field list (group.Update stores the returned tea.Model).
func (f *hintField) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := f.Field.Update(msg)
	if field, ok := model.(huh.Field); ok {
		f.Field = field
	}
	return f, cmd
}

// WithPosition lets huh set positional state (which disables Prev/Next),
// then re-applies the wizard keymap to restore the hint bindings.
func (f *hintField) WithPosition(p huh.FieldPosition) huh.Field {
	f.Field.WithPosition(p)
	f.WithKeyMap(f.km)
	return f
}

// formFilter returns a tea.WithFilter callback that:
//   - Sets ctrlCAbort when Ctrl+C is pressed (key event).
//   - Converts InterruptMsg (from huh's CancelCmd = tea.Interrupt, or an
//     external SIGINT) to QuitMsg so bubbletea takes the graceful shutdown
//     path and the renderer properly clears the form output.
//
// In raw mode (normal bubbletea operation), keyboard Ctrl+C arrives as
// tea.KeyMsg{Type: KeyCtrlC} — NOT as an OS signal. The KeyMsg fires
// before InterruptMsg, so the flag is already set when the abort completes.
// Esc produces KeyEscape (no flag set), so the abort maps to back.
//
// Edge case: an external SIGINT (kill -2) produces only InterruptMsg with
// no preceding KeyMsg, so it maps to errWizardBack rather than
// errWizardCancelled. This is acceptable — externally signalling a wizard
// session is not a realistic scenario.
func (ui *HuhUI) formFilter() func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyCtrlC {
			ui.ctrlCAbort = true
		}
		if _, ok := msg.(tea.InterruptMsg); ok {
			return tea.QuitMsg{}
		}
		return msg
	}
}

// runForm validates terminal availability and runs the provided form.
// Esc returns errWizardBack (back navigation); Ctrl+C returns errWizardCancelled (hard exit).
func (ui *HuhUI) runForm(form *huh.Form) error {
	if err := ui.ensureInteractive(); err != nil {
		return err
	}

	ui.ctrlCAbort = false
	form.WithKeyMap(wizardKeyMap())
	form.WithProgramOptions(
		tea.WithOutput(os.Stderr),
		tea.WithReportFocus(),
		tea.WithFilter(ui.formFilter()),
	)

	err := runFormFunc(form)
	if errors.Is(err, huh.ErrUserAborted) {
		if ui.ctrlCAbort {
			return errWizardCancelled
		}
		return errWizardBack
	}
	return err
}

// newHintField wraps a huh.Field so that Prev/Next hint bindings survive
// huh's positional disabling in single-field forms.
func newHintField(field huh.Field) huh.Field {
	return &hintField{Field: field, km: wizardKeyMap()}
}

// Select renders a single-choice prompt.
func (ui *HuhUI) Select(title string, options []string, current *string) error {
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewSelect[string]().
				Title(title).
				Options(opts...).
				Value(current)),
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
			newHintField(huh.NewMultiSelect[string]().
				Title(title).
				Filterable(false).
				Options(opts...).
				Value(selected)),
		),
	))
}

// Confirm renders a yes/no prompt.
func (ui *HuhUI) Confirm(title string, value *bool) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewConfirm().
				Title(title).
				Value(value)),
		),
	))
}

// Input renders a plain text input prompt.
func (ui *HuhUI) Input(title string, value *string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewInput().
				Title(title).
				Value(value)),
		),
	))
}

// SecretInput renders a masked input prompt for secrets.
func (ui *HuhUI) SecretInput(title string, value *string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewInput().
				Title(title).
				Value(value).
				EchoMode(huh.EchoModePassword)),
		),
	))
}

// Note renders an informational note screen.
func (ui *HuhUI) Note(title string, body string) error {
	return ui.runForm(huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewNote().
				Title(title).
				Description(body)),
		),
	))
}
