package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatchEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		secrets  map[string]string
		contains []string
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
		})
	}
}
