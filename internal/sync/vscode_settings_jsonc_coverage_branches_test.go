package sync

import (
	"strings"
	"testing"
)

func TestRenderVSCodeSettingsContent_ExistingManagedBlockAddsTrailingNewline(t *testing.T) {
	existing := "{\n  // >>> agent-layer\n  // <<< agent-layer\n}"
	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{})
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent: %v", err)
	}
	if !strings.HasSuffix(updated, "\n") {
		t.Fatalf("expected trailing newline after managed block replacement, got %q", updated)
	}
}

func TestHasJSONCContentBetween_AdditionalBranches(t *testing.T) {
	// startLine < 0 and endLine >= len(lines) normalization branches.
	if hasJSONCContentBetween([]string{"{", "}"}, -10, 1, 100, 100) {
		t.Fatal("expected false for normalized empty-object scan")
	}

	// lineStart >= lineEnd branch when start/end are on same line but inverted by bounds.
	if hasJSONCContentBetween([]string{"abcdef"}, 0, 4, 0, 1) {
		t.Fatal("expected false when bounded range is empty after clamping")
	}

	// In-string escape handling branches: backslash marks escape, next byte consumes escaped state.
	line := `{"k":"a\\\"b"}`
	if !hasJSONCContentBetween([]string{line}, 0, 1, 0, len(line)-1) {
		t.Fatal("expected content to be detected while exercising in-string escape transitions")
	}
}
