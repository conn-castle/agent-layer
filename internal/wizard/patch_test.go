package wizard

import (
	"fmt"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml"
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

func TestPatchConfig_RestoredServerSkipsCommentPreservation(t *testing.T) {
	blocks, err := defaultMCPServerTrees()
	require.NoError(t, err)
	github, ok := blocks["github"]
	require.True(t, ok)

	pos := github.GetPositionPath([]string{"enabled"})
	require.False(t, pos.Invalid())
	require.Greater(t, pos.Line, 1)

	lines := make([]string, pos.Line)
	for i := range lines {
		lines[i] = fmt.Sprintf("key_%d = \"value\"", i)
	}
	lines[pos.Line-2] = "# should-not-attach"
	lines[pos.Line-1] = fmt.Sprintf("key_%d = \"value\"", pos.Line-1)
	content := strings.Join(lines, "\n") + "\n[mcp]\n"

	choices := &Choices{
		RestoreMissingMCPServers: true,
		MissingDefaultMCPServers: []string{"github"},
		DefaultMCPServers:        []DefaultMCPServer{{ID: "github"}},
		EnabledMCPServersTouched: true,
		EnabledMCPServers:        map[string]bool{"github": true},
	}
	got, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, got, `id = "github"`)
	assert.Contains(t, got, `enabled = true`)
	assert.NotContains(t, got, "# should-not-attach\nenabled = true")
	assert.NotContains(t, got, `enabled = true # should-not-attach`)
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
		{
			name:  "enable warnings",
			input: "",
			choices: &Choices{
				WarningsEnabledTouched:         true,
				WarningsEnabled:                true,
				InstructionTokenThreshold:      10000,
				MCPServerThreshold:             15,
				MCPToolsTotalThreshold:         60,
				MCPServerToolsThreshold:        25,
				MCPSchemaTokensTotalThreshold:  10000,
				MCPSchemaTokensServerThreshold: 7500,
			},
			contains: []string{
				"[warnings]",
				"instruction_token_threshold = 10000",
				"mcp_server_threshold = 15",
				"mcp_tools_total_threshold = 60",
				"mcp_server_tools_threshold = 25",
				"mcp_schema_tokens_total_threshold = 10000",
				"mcp_schema_tokens_server_threshold = 7500",
			},
		},
		{
			name: "disable warnings",
			input: `
[warnings]
instruction_token_threshold = 10000
`,
			choices: &Choices{
				WarningsEnabledTouched: true,
				WarningsEnabled:        false,
			},
			absent: []string{
				"[warnings]",
				"instruction_token_threshold = 10000",
			},
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

func TestCommentForPath(t *testing.T) {
	tomlContent := `# comment for key
key = "value"
[section]
# section comment
nested = true
`
	tree, err := toml.Load(tomlContent)
	require.NoError(t, err)
	lines := strings.Split(tomlContent, "\n")

	t.Run("path not in tree", func(t *testing.T) {
		got := commentForPath(tree, lines, []string{"nonexistent"})
		assert.Equal(t, "", got)
	})

	t.Run("path exists", func(t *testing.T) {
		got := commentForPath(tree, lines, []string{"key"})
		assert.Contains(t, got, "comment for key")
	})
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
		// Start multiline basic string with """
		commentPos, state := ScanTomlLineForComment(`key = """start`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)

		// Continue in multiline basic string - # is not a comment
		commentPos, state = ScanTomlLineForComment(`middle # not a comment`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)

		// End multiline basic string with """
		commentPos, state = ScanTomlLineForComment(`end"""`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline literal string", func(t *testing.T) {
		// Start multiline literal string with '''
		commentPos, state := ScanTomlLineForComment(`key = '''start`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiLiteral, state)

		// Continue in multiline literal string - # is not a comment
		commentPos, state = ScanTomlLineForComment(`middle # not a comment`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiLiteral, state)

		// End multiline literal string with '''
		commentPos, state = ScanTomlLineForComment(`end'''`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("basic string with escape", func(t *testing.T) {
		// String with escaped quote
		commentPos, state := ScanTomlLineForComment(`key = "value \" more"`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("multiline basic string with escape", func(t *testing.T) {
		// Start multiline basic string
		_, state := ScanTomlLineForComment(`key = """`, tomlStateNone)
		assert.Equal(t, tomlStateMultiBasic, state)

		// Line with escaped quote
		commentPos, state := ScanTomlLineForComment(`escape \" here`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateMultiBasic, state)
	})

	t.Run("literal string", func(t *testing.T) {
		// Start literal string with '
		commentPos, state := ScanTomlLineForComment(`key = 'value`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateLiteral, state)

		// End literal string with '
		commentPos, state = ScanTomlLineForComment(`more'`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("basic string", func(t *testing.T) {
		// Start basic string with "
		commentPos, state := ScanTomlLineForComment(`key = "value`, tomlStateNone)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateBasic, state)

		// End basic string with "
		commentPos, state = ScanTomlLineForComment(`more"`, state)
		assert.Equal(t, -1, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})

	t.Run("comment after closed string", func(t *testing.T) {
		// Comment after string is closed
		commentPos, state := ScanTomlLineForComment(`key = "value" # comment`, tomlStateNone)
		assert.Equal(t, 14, commentPos)
		assert.Equal(t, tomlStateNone, state)
	})
}
