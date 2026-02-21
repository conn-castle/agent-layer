package envfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_EmptyContent(t *testing.T) {
	result, err := Parse("")
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestPatch_EmptyValues(t *testing.T) {
	// Empty value should be skipped
	result := Patch("EXISTING=1", map[string]string{"NEW": ""})
	assert.NotContains(t, result, "NEW=")
	assert.Contains(t, result, "EXISTING=1")
}

func TestPatch_NoUpdates(t *testing.T) {
	// No updates should return original
	result := Patch("KEY=value", map[string]string{})
	assert.Equal(t, "KEY=value", result)
}

func TestPatch_AllEmptyValues(t *testing.T) {
	// All empty values = no updates
	result := Patch("KEY=value", map[string]string{"A": "", "B": ""})
	assert.Equal(t, "KEY=value", result)
}

func TestPatch_EmptyContentWithUpdate(t *testing.T) {
	result := Patch("", map[string]string{"NEW": "value"})
	assert.Equal(t, "NEW=value", result)
}

func TestParseLine_CommentLine(t *testing.T) {
	key, value, ok, err := parseLine("# this is a comment")
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, key)
	assert.Empty(t, value)
}

func TestParseLine_EmptyLine(t *testing.T) {
	key, value, ok, err := parseLine("")
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, key)
	assert.Empty(t, value)
}

func TestParseLine_WhitespaceLine(t *testing.T) {
	key, value, ok, err := parseLine("   \t   ")
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, key)
	assert.Empty(t, value)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "parses export and quoted values",
			input: `
# comment
export TOKEN=value
OTHER = "spaced value"
`,
			want: map[string]string{
				"TOKEN": "value",
				"OTHER": "spaced value",
			},
		},
		{
			name:    "invalid line",
			input:   "INVALID",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=value",
			wantErr: true,
		},
		{
			name:    "space key",
			input:   " =value",
			wantErr: true,
		},
		{
			name:  "single quoted value",
			input: "KEY='val'",
			want:  map[string]string{"KEY": "val"},
		},
		{
			name:  "double quoted escaped newline",
			input: `KEY="line1\nline2"`,
			want:  map[string]string{"KEY": "line1\nline2"},
		},
		{
			name:  "double quoted value with inline comment",
			input: `KEY="value" # keep this comment`,
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:  "single quoted value with inline comment",
			input: `KEY='value' # keep this comment`,
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:    "quoted value with invalid trailing content",
			input:   `KEY="value" trailing`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPatch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		updates  map[string]string
		contains []string
		absent   []string
	}{
		{
			name:     "add new secret",
			input:    `EXISTING=1`,
			updates:  map[string]string{"NEW": "secret"},
			contains: []string{`NEW=secret`},
		},
		{
			name:     "replace existing secret",
			input:    `KEY=old`,
			updates:  map[string]string{"KEY": "new"},
			contains: []string{`KEY=new`},
		},
		{
			name:    "replace export line",
			input:   `export KEY=old`,
			updates: map[string]string{"KEY": "new"},
			contains: []string{
				`KEY=new`,
			},
			absent: []string{
				`export KEY=old`,
			},
		},
		{
			name:    "replace spaced assignment",
			input:   `KEY = old`,
			updates: map[string]string{"KEY": "new"},
			contains: []string{
				`KEY=new`,
			},
			absent: []string{
				`KEY = old`,
			},
		},
		{
			name:    "dedupe existing key lines",
			input:   "KEY=old\nexport KEY=older\nOTHER=1",
			updates: map[string]string{"KEY": "new"},
			contains: []string{
				`KEY=new`,
				`OTHER=1`,
			},
			absent: []string{
				`export KEY=older`,
			},
		},
		{
			name:     "quote complex secret",
			input:    ``,
			updates:  map[string]string{"COMPLEX": "hash # check"},
			contains: []string{`COMPLEX="hash # check"`},
		},
		{
			name:     "escape quotes and backslashes",
			input:    ``,
			updates:  map[string]string{"COMPLEX": `C:\path\"file"`},
			contains: []string{`COMPLEX="C:\\path\\\"file\""`},
		},
		{
			name:     "preserves comments and skips invalid lines",
			input:    "# comment\nKEY=old\nINVALID_NO_EQUALS",
			updates:  map[string]string{"KEY": "new"},
			contains: []string{"# comment", "KEY=new", "INVALID_NO_EQUALS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Patch(tt.input, tt.updates)
			for _, c := range tt.contains {
				assert.Contains(t, got, c)
			}
			for _, c := range tt.absent {
				assert.NotContains(t, got, c)
			}
			if tt.name == "dedupe existing key lines" {
				assert.Equal(t, 1, strings.Count(got, "KEY="))
			}
		})
	}
}

func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no escaping needed",
			input: "simple",
			want:  "simple",
		},
		{
			name:  "space needs quotes",
			input: "with space",
			want:  `"with space"`,
		},
		{
			name:  "tab needs quotes",
			input: "with\ttab",
			want:  "\"with\ttab\"",
		},
		{
			name:  "hash needs quotes",
			input: "with#hash",
			want:  `"with#hash"`,
		},
		{
			name:  "quote needs escaping and quotes",
			input: `with"quote`,
			want:  `"with\"quote"`,
		},
		{
			name:  "backslash and quote",
			input: `C:\path\"file"`,
			want:  `"C:\\path\\\"file\""`,
		},
		{
			name:  "multiple backslashes and quotes",
			input: `\\\"`,
			want:  `"\\\\\\\""`,
		},
		{
			name:  "newline gets escaped",
			input: "line1\nline2",
			want:  `"line1\nline2"`,
		},
		{
			name:  "carriage return gets escaped",
			input: "line1\rline2",
			want:  `"line1\rline2"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, encodeValue(tt.input))
		})
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "plain", value: "simple"},
		{name: "spaces", value: "with space"},
		{name: "tab", value: "with\ttab"},
		{name: "hash", value: "with#hash"},
		{name: "equals", value: "with=value"},
		{name: "quotes", value: `with "quote"`},
		{name: "backslashes", value: `C:\path\to\dir`},
		{name: "backslash quote", value: `C:\path\"file"`},
		{name: "literal slash n", value: `\n`},
		{name: "actual newline", value: "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse("KEY=" + encodeValue(tt.value))
			require.NoError(t, err)
			require.Contains(t, parsed, "KEY")
			assert.Equal(t, tt.value, parsed["KEY"])
		})
	}
}

func TestPatchParseRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "plain", value: "simple"},
		{name: "spaces", value: "with space"},
		{name: "tab", value: "with\ttab"},
		{name: "hash", value: "with#hash"},
		{name: "equals", value: "with=value"},
		{name: "quotes", value: `with "quote"`},
		{name: "backslashes", value: `C:\path\to\dir`},
		{name: "backslash quote", value: `C:\path\"file"`},
		{name: "literal slash n", value: `\n`},
		{name: "actual newline", value: "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := Patch("", map[string]string{"KEY": tt.value})
			parsed, err := Parse(content)
			require.NoError(t, err)
			require.Contains(t, parsed, "KEY")
			assert.Equal(t, tt.value, parsed["KEY"])
		})
	}
}

func TestParse_UnterminatedQuotedValue(t *testing.T) {
	tests := []string{
		`KEY="unterminated`,
		`KEY='unterminated`,
		`KEY="trailing slash\\\"`,
	}
	for _, input := range tests {
		_, err := Parse(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unterminated quoted value")
	}
}

func TestParse_InvalidQuotedSuffix(t *testing.T) {
	tests := []string{
		`KEY="value" trailing`,
		`KEY='value' trailing`,
	}
	for _, input := range tests {
		_, err := Parse(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid trailing characters after quoted value")
	}
}

func FuzzEncodeParseRoundTrip(f *testing.F) {
	f.Add("")
	f.Add("simple")
	f.Add("with space")
	f.Add("with#hash")
	f.Add(`C:\path\"file"`)
	f.Add(`\n`)
	f.Add("line1\nline2")

	f.Fuzz(func(t *testing.T, value string) {
		parsed, err := Parse("KEY=" + encodeValue(value))
		require.NoError(t, err)
		require.Contains(t, parsed, "KEY")
		assert.Equal(t, value, parsed["KEY"])
	})
}

func FuzzPatchParseRoundTrip(f *testing.F) {
	f.Add("")
	f.Add("simple")
	f.Add("with space")
	f.Add("with#hash")
	f.Add(`C:\path\"file"`)
	f.Add(`\n`)
	f.Add("line1\nline2")

	f.Fuzz(func(t *testing.T, value string) {
		content := Patch("", map[string]string{"KEY": value})
		parsed, err := Parse(content)
		require.NoError(t, err)
		if value == "" {
			assert.NotContains(t, parsed, "KEY")
			return
		}
		require.Contains(t, parsed, "KEY")
		assert.Equal(t, value, parsed["KEY"])
	})
}
