package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanTomlLineForComment_BasicCases(t *testing.T) {
	t.Run("simple comment", func(t *testing.T) {
		pos, state := ScanTomlLineForComment("key = value # comment", tomlStateNone)
		assert.Equal(t, 12, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("no comment", func(t *testing.T) {
		pos, state := ScanTomlLineForComment("key = value", tomlStateNone)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("comment only line", func(t *testing.T) {
		pos, state := ScanTomlLineForComment("# this is a comment", tomlStateNone)
		assert.Equal(t, 0, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("hash in basic string", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = "value with # hash"`, tomlStateNone)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("hash in literal string", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = 'value with # hash'`, tomlStateNone)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("comment after basic string with hash", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = "value with # hash" # real comment`, tomlStateNone)
		assert.Equal(t, 26, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("escaped quote in basic string", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = "value \" more" # comment`, tomlStateNone)
		assert.Equal(t, 22, pos)
		assert.Equal(t, tomlStateNone, state)
	})
}

func TestScanTomlLineForComment_MultilineStateTransitions(t *testing.T) {
	t.Run("multiline basic opening", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = """`, tomlStateNone)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateMultiBasic, state)
	})

	t.Run("multiline literal opening", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = '''`, tomlStateNone)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateMultiLiteral, state)
	})

	t.Run("inside multiline basic", func(t *testing.T) {
		pos, state := ScanTomlLineForComment("content with # hash", tomlStateMultiBasic)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateMultiBasic, state)
	})

	t.Run("inside multiline literal", func(t *testing.T) {
		pos, state := ScanTomlLineForComment("content with # hash", tomlStateMultiLiteral)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateMultiLiteral, state)
	})

	t.Run("multiline basic closing", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`content"""`, tomlStateMultiBasic)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline literal closing", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`content'''`, tomlStateMultiLiteral)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline basic closing with trailing comment", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`content""" # comment`, tomlStateMultiBasic)
		assert.Equal(t, 11, pos)
		assert.Equal(t, tomlStateNone, state)
	})
}

func TestScanTomlLineForComment_EscapeSequences(t *testing.T) {
	t.Run("escaped backslash before quote", func(t *testing.T) {
		pos, state := ScanTomlLineForComment(`key = "value \\" # comment`, tomlStateNone)
		assert.Equal(t, 17, pos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("escape at end of multiline basic line", func(t *testing.T) {
		// Escape at end should not cause issues
		pos, state := ScanTomlLineForComment(`content \`, tomlStateMultiBasic)
		assert.Equal(t, -1, pos)
		assert.Equal(t, tomlStateMultiBasic, state)
	})
}

func TestIsTomlStateInMultiline(t *testing.T) {
	assert.False(t, IsTomlStateInMultiline(tomlStateNone))
	assert.False(t, IsTomlStateInMultiline(tomlStateBasic))
	assert.False(t, IsTomlStateInMultiline(tomlStateLiteral))
	assert.True(t, IsTomlStateInMultiline(tomlStateMultiBasic))
	assert.True(t, IsTomlStateInMultiline(tomlStateMultiLiteral))
}

func TestInlineCommentForLine(t *testing.T) {
	t.Run("simple inline comment", func(t *testing.T) {
		lines := []string{"key = value # comment"}
		got := inlineCommentForLine(lines, 0)
		assert.Equal(t, "comment", got)
	})

	t.Run("no comment", func(t *testing.T) {
		lines := []string{"key = value"}
		got := inlineCommentForLine(lines, 0)
		assert.Equal(t, "", got)
	})

	t.Run("hash in string not treated as comment", func(t *testing.T) {
		lines := []string{`key = "value with # hash"`}
		got := inlineCommentForLine(lines, 0)
		assert.Equal(t, "", got)
	})

	t.Run("comment after string with hash", func(t *testing.T) {
		lines := []string{`key = "value # hash" # real comment`}
		got := inlineCommentForLine(lines, 0)
		assert.Equal(t, "real comment", got)
	})

	t.Run("multiline string context", func(t *testing.T) {
		lines := []string{
			`key = """`,
			`content # not a comment`,
			`"""`,
		}
		got := inlineCommentForLine(lines, 1)
		assert.Equal(t, "", got)
	})

	t.Run("negative lineIndex", func(t *testing.T) {
		got := inlineCommentForLine([]string{"test"}, -1)
		assert.Equal(t, "", got)
	})

	t.Run("lineIndex out of bounds", func(t *testing.T) {
		got := inlineCommentForLine([]string{"test"}, 100)
		assert.Equal(t, "", got)
	})
}

func TestCommentForLine(t *testing.T) {
	t.Run("leading comments only", func(t *testing.T) {
		lines := []string{
			"# comment one",
			"# comment two",
			"key = value",
		}
		got := commentForLine(lines, 2)
		assert.Equal(t, "comment one\ncomment two", got)
	})

	t.Run("inline comment only", func(t *testing.T) {
		lines := []string{
			"key = value # inline",
		}
		got := commentForLine(lines, 0)
		assert.Equal(t, "inline", got)
	})

	t.Run("leading and inline comments", func(t *testing.T) {
		lines := []string{
			"# leading",
			"key = value # inline",
		}
		got := commentForLine(lines, 1)
		assert.Equal(t, "leading\ninline", got)
	})

	t.Run("blank line breaks leading comments", func(t *testing.T) {
		lines := []string{
			"# not included",
			"",
			"# included",
			"key = value",
		}
		got := commentForLine(lines, 3)
		assert.Equal(t, "included", got)
	})

	t.Run("non-comment line breaks leading comments", func(t *testing.T) {
		lines := []string{
			"# not included",
			"other = 1",
			"# included",
			"key = value",
		}
		got := commentForLine(lines, 3)
		assert.Equal(t, "included", got)
	})

	t.Run("no comments", func(t *testing.T) {
		lines := []string{"key = value"}
		got := commentForLine(lines, 0)
		assert.Equal(t, "", got)
	})

	t.Run("negative lineIndex", func(t *testing.T) {
		got := commentForLine([]string{"test"}, -1)
		assert.Equal(t, "", got)
	})

	t.Run("lineIndex out of bounds", func(t *testing.T) {
		got := commentForLine([]string{"test"}, 100)
		assert.Equal(t, "", got)
	})
}

func TestFormatTomlNoIndent_RemovesIndentation(t *testing.T) {
	input := "[agents]\n  [agents.codex]\n    enabled = true\n"
	want := "[agents]\n[agents.codex]\nenabled = true\n"

	got, err := formatTomlNoIndent(input)
	if err != nil {
		t.Fatalf("formatTomlNoIndent error: %v", err)
	}
	if got != want {
		t.Fatalf("formatTomlNoIndent output mismatch:\n%s\n---\n%s", got, want)
	}
}

func TestFormatTomlNoIndent_PreservesMultilineContent(t *testing.T) {
	input := "[section]\n  key = \"\"\"\n  line one\n  line two\n  \"\"\"\n  other = 1\n"
	want := "[section]\nkey = \"\"\"\n  line one\n  line two\n  \"\"\"\nother = 1\n"

	got, err := formatTomlNoIndent(input)
	if err != nil {
		t.Fatalf("formatTomlNoIndent error: %v", err)
	}
	if got != want {
		t.Fatalf("formatTomlNoIndent output mismatch:\n%s\n---\n%s", got, want)
	}
}

func TestFormatTomlNoIndent_UnterminatedMultiline(t *testing.T) {
	input := "[section]\nkey = \"\"\"\nunterminated\n"

	_, err := formatTomlNoIndent(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated multiline string")
}
