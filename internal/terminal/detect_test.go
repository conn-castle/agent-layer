package terminal

import "testing"

func TestIsInteractive(t *testing.T) {
	// IsInteractive returns false in test environments (no TTY).
	// This test verifies the function runs without panic.
	result := IsInteractive()
	// In CI/test environments, this is typically false.
	// We don't assert the value since it depends on the environment.
	_ = result
}
