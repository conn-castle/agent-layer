package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommentForLine_OutOfRange(t *testing.T) {
	lines := []string{"line1", "line2"}

	comment := commentForLine(lines, -1)
	assert.Empty(t, comment)

	comment = commentForLine(lines, 5)
	assert.Empty(t, comment)
}

func TestCommentForLine_LeadingComments(t *testing.T) {
	lines := []string{
		"# First comment",
		"# Second comment",
		"key = value",
	}
	comment := commentForLine(lines, 2)
	assert.Contains(t, comment, "First comment")
	assert.Contains(t, comment, "Second comment")
}

func TestCommentForLine_NoComments(t *testing.T) {
	lines := []string{
		"other = value",
		"key = value",
	}
	comment := commentForLine(lines, 1)
	assert.Empty(t, comment)
}

func TestCommentForLine_BlankLineBreaksComments(t *testing.T) {
	lines := []string{
		"# Detached comment",
		"",
		"key = value",
	}
	comment := commentForLine(lines, 2)
	assert.Empty(t, comment)
}

func TestCommentForLine_EdgeCases(t *testing.T) {
	lines := []string{"# comment", "key = value"}

	t.Run("negative lineIndex", func(t *testing.T) {
		got := commentForLine(lines, -1)
		assert.Equal(t, "", got)
	})

	t.Run("lineIndex out of bounds", func(t *testing.T) {
		got := commentForLine(lines, 100)
		assert.Equal(t, "", got)
	})

	t.Run("empty lines", func(t *testing.T) {
		got := commentForLine([]string{}, 0)
		assert.Equal(t, "", got)
	})
}

func TestScanTomlLineForComment_MultilineStrings(t *testing.T) {
	t.Run("multiline basic string", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = """start`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)

		commentPos, state = ScanTomlLineForComment(`middle # not a comment`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)

		commentPos, state = ScanTomlLineForComment(`end"""`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline literal string", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = '''start`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiLiteral, state)

		commentPos, state = ScanTomlLineForComment(`middle # not a comment`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiLiteral, state)

		commentPos, state = ScanTomlLineForComment(`end'''`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("basic string with escape", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = "value \" more"`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline basic string with escape", func(t *testing.T) {
		_, state := ScanTomlLineForComment(`key = """`, tomlStateNone)
		assert.Equal(t, tomlStateMultiBasic, state)

		commentPos, state := ScanTomlLineForComment(`escape \" here`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)
	})

	t.Run("literal string", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = 'value`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateLiteral, state)

		commentPos, state = ScanTomlLineForComment(`more'`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("basic string", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = "value`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateBasic, state)

		commentPos, state = ScanTomlLineForComment(`more"`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("comment after closed string", func(t *testing.T) {
		commentPos, state := ScanTomlLineForComment(`key = "value" # comment`, tomlStateNone)
		assert.Equal(t, 14, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})
}
