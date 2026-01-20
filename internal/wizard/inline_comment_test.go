package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInlineComment(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		lineIndex int
		expected  string
	}{
		{
			name:      "no comment",
			lines:     []string{`key = "value"`},
			lineIndex: 0,
			expected:  "",
		},
		{
			name:      "simple comment",
			lines:     []string{`key = "value" # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "comment with spaces",
			lines:     []string{`key = "value"   #    spaced   `},
			lineIndex: 0,
			expected:  "spaced",
		},
		{
			name:      "hash in string",
			lines:     []string{`key = "val#ue" # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "hash in single quoted string",
			lines:     []string{`key = 'val#ue' # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "escaped quote",
			lines:     []string{`key = "val\"ue" # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "hash in triple double quotes",
			lines:     []string{`key = """val#ue""" # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "hash in triple single quotes",
			lines:     []string{`key = '''val#ue''' # comment`},
			lineIndex: 0,
			expected:  "comment",
		},
		{
			name:      "triple quote with inner quote and hash",
			lines:     []string{`key = """ " # """ # real comment`},
			lineIndex: 0,
			expected:  "real comment",
		},
		{
			name:      "multiline basic string ignores hash",
			lines:     []string{`key = """line1`, `line#2`, `line3""" # comment`},
			lineIndex: 1,
			expected:  "",
		},
		{
			name:      "multiline basic string comment after close",
			lines:     []string{`key = """line1`, `line2`, `line3""" # comment`},
			lineIndex: 2,
			expected:  "comment",
		},
		{
			name:      "multiline literal string ignores hash",
			lines:     []string{`key = '''line1`, `line#2`, `line3''' # comment`},
			lineIndex: 1,
			expected:  "",
		},
		{
			name:      "multiline literal string comment after close",
			lines:     []string{`key = '''line1`, `line2`, `line3''' # comment`},
			lineIndex: 2,
			expected:  "comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inlineCommentForLine(tt.lines, tt.lineIndex)
			assert.Equal(t, tt.expected, got)
		})
	}
}
