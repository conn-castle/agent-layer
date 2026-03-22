package terminal

import "testing"

func TestIsInteractive(t *testing.T) {
	// go test always runs with piped stdin/stdout, so IsInteractive
	// must return false in any test environment.
	if IsInteractive() {
		t.Error("expected false: stdin/stdout are not terminals under go test")
	}
}
