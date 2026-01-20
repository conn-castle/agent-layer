package wizard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchConfig_Errors(t *testing.T) {
	t.Run("invalid TOML", func(t *testing.T) {
		_, err := PatchConfig("[broken", &Choices{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parse config")
	})

	t.Run("no default servers for mcp toggle", func(t *testing.T) {
		choices := &Choices{
			EnabledMCPServersTouched: true,
			DefaultMCPServers:        []DefaultMCPServer{}, // Empty!
		}
		_, err := PatchConfig("[mcp]", choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default MCP servers are required")
	})

	t.Run("no default servers for restore", func(t *testing.T) {
		choices := &Choices{
			RestoreMissingMCPServers: true,
			DefaultMCPServers:        []DefaultMCPServer{}, // Empty!
		}
		_, err := PatchConfig("[mcp]", choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default MCP servers are required")
	})

	t.Run("mcp servers unexpected type", func(t *testing.T) {
		choices := &Choices{
			EnabledMCPServersTouched: true,
			DefaultMCPServers:        []DefaultMCPServer{{ID: "github"}},
		}
		_, err := PatchConfig(`[mcp]
servers = "not-an-array"
`, choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected type")
	})
}

func TestSetMCPServerEnabled_EmptyID(t *testing.T) {
	// Test that servers without ID are skipped
	content := `[[mcp.servers]]
enabled = false

[[mcp.servers]]
id = "github"
enabled = false
`
	choices := &Choices{
		EnabledMCPServersTouched: true,
		EnabledMCPServers:        map[string]bool{"github": true},
		DefaultMCPServers:        []DefaultMCPServer{{ID: "github"}},
	}
	result, err := PatchConfig(content, choices)
	assert.NoError(t, err)
	assert.Contains(t, result, `enabled = true`)
}

func TestCommentForLine_OutOfRange(t *testing.T) {
	lines := []string{"line1", "line2"}

	// Negative index
	comment := commentForLine(lines, -1)
	assert.Empty(t, comment)

	// Index >= len
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
	// Blank line breaks the comment chain
	assert.Empty(t, comment)
}

func TestPatchConfig_MovesInlineCommentToLeading(t *testing.T) {
	input := `
[agents.codex]
model = "old" # comment
`
	choices := &Choices{
		CodexModelTouched: true,
		CodexModel:        "new",
	}
	got, err := PatchConfig(input, choices)
	require.NoError(t, err)

	assert.NotContains(t, got, `model = "new" # comment`)

	commentIdx := strings.Index(got, "# comment")
	modelIdx := strings.Index(got, `model = "new"`)
	assert.NotEqual(t, -1, commentIdx)
	assert.NotEqual(t, -1, modelIdx)
	assert.Less(t, commentIdx, modelIdx)
}

func TestPatchConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		choices  *Choices
		contains []string
		absent   []string
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
			name: "set model preserves comment",
			input: `
[agents.codex]
  model = "old" # comment
`,
			choices: &Choices{
				CodexModelTouched: true,
				CodexModel:        "new",
			},
			contains: []string{`model = "new"`, "comment"},
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
				DefaultMCPServers:        []DefaultMCPServer{{ID: "github"}},
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
		{
			name: "clear model",
			input: `
[agents.gemini]
model = "old"
`,
			choices: &Choices{
				GeminiModelTouched: true,
				GeminiModel:        "",
			},
			absent: []string{`model = "old"`, `model = ""`},
		},
		{
			name: "clear reasoning effort",
			input: `
[agents.codex]
reasoning_effort = "high"
`,
			choices: &Choices{
				CodexReasoningTouched: true,
				CodexReasoning:        "",
			},
			absent: []string{`reasoning_effort = "high"`, `reasoning_effort = ""`},
		},
		{
			name:  "restore missing default mcp server",
			input: "[mcp]\n",
			choices: &Choices{
				RestoreMissingMCPServers: true,
				MissingDefaultMCPServers: []string{"github"},
				DefaultMCPServers:        []DefaultMCPServer{{ID: "github"}},
			},
			contains: []string{`id = "github"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PatchConfig(tt.input, tt.choices)
			require.NoError(t, err)
			for _, c := range tt.contains {
				assert.Contains(t, got, c)
			}
			for _, c := range tt.absent {
				assert.NotContains(t, got, c)
			}
		})
	}
}
