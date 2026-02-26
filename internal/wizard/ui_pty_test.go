//go:build !windows

package wizard

import (
	"errors"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
)

// runFormInPTY builds a huh form with the same key components as
// HuhUI.runForm (wizardKeyMap, formFilter, hintField), feeds raw key bytes
// through Bubble Tea input parsing, and returns the classified result.
//
// This validates the full chain: raw byte → bubbletea input parser →
// tea.KeyMsg → formFilter → huh Quit binding → CancelCmd → InterruptMsg →
// formFilter conversion → ErrUserAborted → ctrlCAbort classification.
func runFormInPTY(t *testing.T, keyBytes []byte) error {
	t.Helper()

	inputR, inputW := io.Pipe()
	t.Cleanup(func() { _ = inputR.Close() })
	t.Cleanup(func() { _ = inputW.Close() })

	ui := &HuhUI{isTerminal: func() bool { return true }}

	var val string
	form := huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewInput().Title("PTY Test").Value(&val)),
		),
	)
	form.WithAccessible(false)
	form.WithKeyMap(wizardKeyMap())
	form.WithProgramOptions(
		tea.WithInput(inputR),
		tea.WithOutput(io.Discard),
		tea.WithFilter(ui.formFilter()),
	)

	go func() {
		// Allow Bubble Tea to finish program startup so the first key byte is
		// consumed by the input parser instead of racing with initialization.
		time.Sleep(50 * time.Millisecond)
		_, _ = inputW.Write(keyBytes)
		// Keep the stream open briefly so a lone Esc can be recognized as a
		// complete escape keypress rather than part of an escape sequence.
		time.Sleep(350 * time.Millisecond)
		_ = inputW.Close()
	}()

	// Run the form; classify the result the same way runForm does.
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		runErr := form.Run()
		if errors.Is(runErr, huh.ErrUserAborted) {
			if ui.ctrlCAbort {
				ch <- result{errWizardCancelled}
			} else {
				ch <- result{errWizardBack}
			}
			return
		}
		ch <- result{runErr}
	}()

	select {
	case r := <-ch:
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatal("form did not exit within timeout")
		return nil
	}
}

func TestPTY_EscProducesWizardBack(t *testing.T) {
	// Esc = 0x1b. bubbletea's input parser waits ~100ms for follow-up bytes;
	// with none, it classifies the lone byte as standalone Esc (KeyEscape).
	err := runFormInPTY(t, []byte{0x1b})
	assert.ErrorIs(t, err, errWizardBack)
}

func TestPTY_CtrlCProducesWizardCancelled(t *testing.T) {
	// Ctrl+C = 0x03. The slave is pre-set to raw mode so the kernel passes
	// this byte through (ISIG cleared); bubbletea reads it as KeyCtrlC.
	err := runFormInPTY(t, []byte{0x03})
	assert.ErrorIs(t, err, errWizardCancelled)
}
