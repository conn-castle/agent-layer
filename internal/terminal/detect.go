// Package terminal provides terminal detection utilities.
package terminal

import (
	"os"

	"golang.org/x/term"
)

// IsInteractive reports whether stdin and stdout are both interactive terminals.
// This is the canonical implementation for terminal detection across the codebase.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // Unix file descriptors are small non-negative ints; cast is safe on all supported platforms
}
