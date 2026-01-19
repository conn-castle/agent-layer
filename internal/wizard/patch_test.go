package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		choices  *Choices
		contains []string
	}{
		{
			name: "approvals mode change",
			input: `
[approvals]
mode = "mcp"
`,
			choices: &Choices{
				ApprovalMode:        "all",
				ApprovalModeTouched: true,
			},
			contains: []string{`mode = "all"`},
		},
		{
			name: "enable agent",
			input: `
[agents.gemini]
enabled = false
`,
			choices: &Choices{
				EnabledAgents:        map[string]bool{"gemini": true},
				EnabledAgentsTouched: true,
			},
			contains: []string{`enabled = true`},
		},
		{
			name: "set model preserves formatting",
			input: `
[agents.codex]
  model = "old" # comment
`,
			choices: &Choices{
				CodexModelTouched: true,
				CodexModel:        "new",
			},
			contains: []string{`  model = "new"`},
		},
		{
			name: "mcp server toggle",
			input: `
[[mcp.servers]]
id = "github"
enabled = false
`,
			choices: &Choices{
				EnabledMCPServers:        map[string]bool{"github": true},
				EnabledMCPServersTouched: true,
			},
			contains: []string{`enabled = true`},
		},
		{
			name: "insert missing table",
			input: `
[other]
foo = "bar"
`,
			choices: &Choices{
				ApprovalMode:        "all",
				ApprovalModeTouched: true,
			},
			contains: []string{`[approvals]`, `mode = "all"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PatchConfig(tt.input, tt.choices)
			require.NoError(t, err)
			for _, c := range tt.contains {
				assert.Contains(t, got, c)
			}
		})
	}
}
