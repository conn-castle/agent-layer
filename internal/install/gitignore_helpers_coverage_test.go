package install

import (
	"strings"
	"testing"
)

func TestUpdateGitignoreContent_AdditionalBranches(t *testing.T) {
	t.Run("append without existing trailing newline", func(t *testing.T) {
		content := "node_modules/"
		block := wrapGitignoreBlock("dist/")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(updated, "node_modules/\n") {
			t.Fatalf("expected inserted separator newline, got %q", updated)
		}
		if !strings.Contains(updated, gitignoreStart) || !strings.Contains(updated, gitignoreEnd) {
			t.Fatalf("expected managed block markers in updated content, got %q", updated)
		}
	})

	t.Run("dangling start marker fails loud instead of appending", func(t *testing.T) {
		// An orphaned start marker (start present, end missing) must NOT append a
		// second managed block: doing so would, on the next sync, cause the user's
		// content between the orphaned start and the appended block to be silently
		// deleted. The correct behavior is to surface the corruption.
		content := gitignoreStart + "\nold block line\n"
		block := wrapGitignoreBlock("new-line")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err == nil {
			t.Fatalf("expected error for unterminated managed block, got updated content %q", updated)
		}
		if !strings.Contains(err.Error(), gitignoreStart) || !strings.Contains(err.Error(), gitignoreEnd) {
			t.Fatalf("expected error to name both markers, got %v", err)
		}
	})

	t.Run("dangling end marker fails loud", func(t *testing.T) {
		// Symmetric to the orphaned-start case: an end marker with no preceding
		// start marker is also a corrupt managed block and must fail loud rather
		// than silently appending a second block. Mirrors the VS Code sibling,
		// which rejects either incomplete marker.
		content := "user-line\n" + gitignoreEnd + "\nmore\n"
		block := wrapGitignoreBlock("new-line")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err == nil {
			t.Fatalf("expected error for orphaned end marker, got updated content %q", updated)
		}
	})

	t.Run("end marker before start marker fails loud", func(t *testing.T) {
		content := gitignoreEnd + "\nuser-line\n" + gitignoreStart + "\n"
		block := wrapGitignoreBlock("new-line")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err == nil {
			t.Fatalf("expected error for inverted markers, got updated content %q", updated)
		}
	})

	t.Run("duplicate start marker fails loud", func(t *testing.T) {
		// start ... start ... USER ... end: pairing the first start with the end
		// would silently delete the nested USER content. Must fail loud, matching
		// the VS Code sibling's duplicate-marker rejection.
		content := gitignoreStart + "\nold\n" + gitignoreStart + "\nuser-precious\n" + gitignoreEnd + "\n"
		block := wrapGitignoreBlock("new-line")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err == nil {
			t.Fatalf("expected error for duplicate start marker, got updated content %q", updated)
		}
	})

	t.Run("duplicate end marker fails loud", func(t *testing.T) {
		content := gitignoreStart + "\nold\n" + gitignoreEnd + "\nuser\n" + gitignoreEnd + "\n"
		block := wrapGitignoreBlock("new-line")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err == nil {
			t.Fatalf("expected error for duplicate end marker, got updated content %q", updated)
		}
	})

	t.Run("collapse multiple leading post blank lines to one", func(t *testing.T) {
		content := strings.Join([]string{
			"before",
			gitignoreStart,
			"old",
			gitignoreEnd,
			"",
			"",
			"after",
			"",
		}, "\n")
		block := wrapGitignoreBlock("new")
		updated, err := updateGitignoreContent(content, block, ".gitignore")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(updated, "\n\n\n") {
			t.Fatalf("expected no run of blank lines anywhere in output, got %q", updated)
		}
		if !strings.Contains(updated, "\n\nafter") {
			t.Fatalf("expected exactly one blank line before post section, got %q", updated)
		}
	})
}

func TestSplitLines_AdditionalBranches(t *testing.T) {
	if lines := splitLines("\n\n"); len(lines) != 1 || lines[0] != "" {
		t.Fatalf("expected one trailing-blank sentinel line, got %#v", lines)
	}
	if lines := splitLines("a\n\n"); len(lines) != 2 || lines[1] != "" {
		t.Fatalf("expected preserved trailing blank line, got %#v", lines)
	}
}

func TestContainsManagedGitignoreMarkers_HashAndTrimmedMarkers(t *testing.T) {
	cases := []string{
		"   " + gitignoreStart + "   \nvalue\n",
		"line\n" + gitignoreHashPrefix + "abc123\n",
		"value\n   " + gitignoreEnd + "\n",
	}
	for _, input := range cases {
		if !containsManagedGitignoreMarkers(input) {
			t.Fatalf("expected managed marker/hash detection for %q", input)
		}
	}
	if containsManagedGitignoreMarkers("just-user-content\n") {
		t.Fatal("did not expect marker detection for plain content")
	}
}
