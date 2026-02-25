//go:build !windows

package wizard

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

// runFormInPTY creates a real pseudo-terminal, builds a huh form with the
// same key components as HuhUI.runForm (wizardKeyMap, formFilter, hintField),
// and returns the classified error after the form processes the pre-buffered
// keystroke.
//
// This validates the full chain: raw byte → bubbletea input parser →
// tea.KeyMsg → formFilter → huh Quit binding → CancelCmd → InterruptMsg →
// formFilter conversion → ErrUserAborted → ctrlCAbort classification.
//
// Note: the production path (runForm) also sets tea.WithOutput(os.Stderr) and
// tea.WithReportFocus(); here we redirect I/O to the PTY and omit focus
// reporting since neither affects keystroke classification.
func runFormInPTY(t *testing.T, keyBytes []byte) error {
	t.Helper()

	ptmx, tty, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tty.Close()
		_ = ptmx.Close()
	})
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}))

	// Put the slave in raw mode before bubbletea starts. This clears ISIG
	// so that 0x03 (Ctrl+C) is treated as a data byte rather than generating
	// SIGINT. bubbletea will call MakeRaw again when it initialises, which
	// is effectively a no-op since the terminal is already raw.
	_, err = term.MakeRaw(int(tty.Fd()))
	require.NoError(t, err)

	// Buffer the keystroke in the PTY before starting the form. The byte(s)
	// sit in the kernel buffer until bubbletea's input reader consumes them.
	// This avoids any dependency on startup-event timing (e.g. WindowSizeMsg)
	// which varies across environments and test runners.
	_, err = ptmx.Write(keyBytes)
	require.NoError(t, err)

	ui := &HuhUI{isTerminal: func() bool { return true }}

	var val string
	form := huh.NewForm(
		huh.NewGroup(
			newHintField(huh.NewInput().Title("PTY Test").Value(&val)),
		),
	)
	form.WithKeyMap(wizardKeyMap())
	form.WithProgramOptions(
		tea.WithInput(tty),
		tea.WithOutput(tty),
		tea.WithFilter(ui.formFilter()),
	)

	// Drain PTY master output to prevent the form from blocking on writes.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := ptmx.Read(buf); err != nil {
				return
			}
		}
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
