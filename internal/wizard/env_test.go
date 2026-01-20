package wizard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnv(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnv(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPatchEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		secrets  map[string]string
		contains []string
		absent   []string
	}{
		{
			name:     "add new secret",
			input:    `EXISTING=1`,
			secrets:  map[string]string{"NEW": "secret"},
			contains: []string{`NEW=secret`},
		},
		{
			name:     "replace existing secret",
			input:    `KEY=old`,
			secrets:  map[string]string{"KEY": "new"},
			contains: []string{`KEY=new`},
		},
		{
			name:    "replace export line",
			input:   `export KEY=old`,
			secrets: map[string]string{"KEY": "new"},
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
			secrets: map[string]string{"KEY": "new"},
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
			secrets: map[string]string{"KEY": "new"},
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
			secrets:  map[string]string{"COMPLEX": "hash # check"},
			contains: []string{`COMPLEX="hash # check"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PatchEnv(tt.input, tt.secrets)
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
