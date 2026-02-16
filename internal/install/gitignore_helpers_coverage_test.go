package install

import (
	"strings"
	"testing"
)

func TestUpdateGitignoreContent_AdditionalBranches(t *testing.T) {
	t.Run("append without existing trailing newline", func(t *testing.T) {
		content := "node_modules/"
		block := wrapGitignoreBlock("dist/")
		updated := updateGitignoreContent(content, block)
		if !strings.HasPrefix(updated, "node_modules/\n") {
			t.Fatalf("expected inserted separator newline, got %q", updated)
		}
		if !strings.Contains(updated, gitignoreStart) || !strings.Contains(updated, gitignoreEnd) {
			t.Fatalf("expected managed block markers in updated content, got %q", updated)
		}
	})

	t.Run("dangling start marker appends new block", func(t *testing.T) {
		content := gitignoreStart + "\nold block line\n"
		block := wrapGitignoreBlock("new-line")
		updated := updateGitignoreContent(content, block)
		if strings.Count(updated, gitignoreStart) < 2 {
			t.Fatalf("expected dangling block to remain and new block to append, got %q", updated)
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
		updated := updateGitignoreContent(content, block)
		if strings.Contains(updated, "\n\n\nafter") {
			t.Fatalf("expected collapsed blank lines before post section, got %q", updated)
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
