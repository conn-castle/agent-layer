package wizard

import (
	"fmt"
	"sort"
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
			DefaultMCPServers:        []DefaultMCPServer{},
		}
		_, err := PatchConfig("[mcp]", choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default MCP servers are required")
	})

	t.Run("no default servers for restore", func(t *testing.T) {
		choices := &Choices{
			RestoreMissingMCPServers: true,
			DefaultMCPServers:        []DefaultMCPServer{},
		}
		_, err := PatchConfig("[mcp]", choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default MCP servers are required")
	})
}

func TestPatchConfig_CanonicalOrder(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[warnings]
instruction_token_threshold = 123

[approvals]
mode = "mcp"

[[mcp.servers]]
id = "custom"
enabled = true

[agents.gemini]
enabled = false
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)

	idxApprovals := strings.Index(out, "[approvals]")
	idxGemini := strings.Index(out, "[agents.gemini]")
	idxMCP := strings.Index(out, "[mcp]")
	idxWarnings := strings.Index(out, "[warnings]")

	require.NotEqual(t, -1, idxApprovals)
	require.NotEqual(t, -1, idxGemini)
	require.NotEqual(t, -1, idxMCP)
	require.NotEqual(t, -1, idxWarnings)

	assert.Less(t, idxApprovals, idxGemini)
	assert.Less(t, idxMCP, idxWarnings)
}

func TestPatchConfig_MCPServerOrdering(t *testing.T) {
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(defaults), 2)

	firstID := defaults[0].ID
	secondID := defaults[1].ID

	content := fmt.Sprintf(`
[mcp]
[[mcp.servers]]
id = "%s"
enabled = false
command = "custom"

[[mcp.servers]]
id = "custom"
enabled = true

[[mcp.servers]]
id = "%s"
enabled = true
`, secondID, firstID)

	choices := NewChoices()
	choices.DefaultMCPServers = defaults

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	idxFirst := strings.Index(out, fmt.Sprintf("id = \"%s\"", firstID))
	idxSecond := strings.Index(out, fmt.Sprintf("id = \"%s\"", secondID))
	idxCustom := strings.Index(out, "id = \"custom\"")

	require.NotEqual(t, -1, idxFirst)
	require.NotEqual(t, -1, idxSecond)
	require.NotEqual(t, -1, idxCustom)

	assert.Less(t, idxFirst, idxSecond)
	assert.Less(t, idxSecond, idxCustom)
	assert.Contains(t, out, "command = \"custom\"")
}

func TestPatchConfig_OptionalModelCleared(t *testing.T) {
	content := `
[agents.gemini]
enabled = true
model = "custom"
`
	choices := NewChoices()
	choices.GeminiModelTouched = true
	choices.GeminiModel = ""

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "model = \"custom\"")
	assert.Contains(t, out, "# model =")
}

func TestPatchConfig_WarningsDisabledRemovesSection(t *testing.T) {
	content := `
[warnings]
instruction_token_threshold = 100
`
	choices := NewChoices()
	choices.WarningsEnabledTouched = true
	choices.WarningsEnabled = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "[warnings]")
}

func TestPatchConfig_PreservesLeadingComments(t *testing.T) {
	content := `
[approvals]
# This comment should be preserved
mode = "mcp"
`
	choices := NewChoices()
	choices.ApprovalModeTouched = true
	choices.ApprovalMode = "all"

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "# This comment should be preserved")
	assert.Contains(t, out, `mode = "all"`)
}

func TestPatchConfig_InlineCommentsOnTemplateKeys(t *testing.T) {
	// Per README: "Inline comments on modified lines may be moved to leading comments or removed"
	// When a key exists in the template, the template formatting takes precedence.
	// This test verifies the value is updated correctly regardless of inline comment handling.
	content := `
[agents.gemini]
enabled = true # user comment
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents = map[string]bool{"gemini": false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Value should be updated
	lines := strings.Split(out, "\n")
	foundGemini := false
	for i, line := range lines {
		if strings.Contains(line, "[agents.gemini]") {
			foundGemini = true
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				if strings.Contains(lines[j], "enabled") && !strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
					assert.Contains(t, lines[j], "enabled = false", "enabled should be false")
					break
				}
			}
			break
		}
	}
	assert.True(t, foundGemini, "should find gemini section")
}

func TestPatchConfig_InlineCommentsOnCustomKeys(t *testing.T) {
	// For keys that don't exist in the template, the user's inline comment should be preserved
	content := `
[custom_section]
custom_key = "old_value" # important note
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Custom section preserved with inline comment
	assert.Contains(t, out, `custom_key = "old_value"`)
	assert.Contains(t, out, "# important note")
}

func TestPatchConfig_PreservesExtraSections(t *testing.T) {
	content := `
[approvals]
mode = "mcp"

[custom_section]
custom_key = "custom_value"

[another_custom]
foo = "bar"
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[custom_section]")
	assert.Contains(t, out, `custom_key = "custom_value"`)
	assert.Contains(t, out, "[another_custom]")
	assert.Contains(t, out, `foo = "bar"`)
}

func TestPatchConfig_ExtraSectionsSortedAlphabetically(t *testing.T) {
	content := `
[approvals]
mode = "mcp"

[zebra_section]
z = 1

[alpha_section]
a = 2
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	idxAlpha := strings.Index(out, "[alpha_section]")
	idxZebra := strings.Index(out, "[zebra_section]")

	require.NotEqual(t, -1, idxAlpha)
	require.NotEqual(t, -1, idxZebra)
	assert.Less(t, idxAlpha, idxZebra, "extra sections should be sorted alphabetically")
}

func TestPatchConfig_MCPServerWithoutID(t *testing.T) {
	content := `
[mcp]

[[mcp.servers]]
enabled = true
command = "no-id-server"

[[mcp.servers]]
id = "has-id"
enabled = false
`
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)

	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"has-id": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Server without ID should be preserved as-is
	assert.Contains(t, out, `command = "no-id-server"`)
	// Server with ID should be updated
	assert.Contains(t, out, `id = "has-id"`)
}

func TestPatchConfig_ApprovalModeChange(t *testing.T) {
	content := `
[approvals]
mode = "none"
`
	choices := NewChoices()
	choices.ApprovalModeTouched = true
	choices.ApprovalMode = "all"

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `mode = "all"`)
	assert.NotContains(t, out, `mode = "none"`)
}

func TestPatchConfig_EnableAgent(t *testing.T) {
	content := `
[agents.claude]
enabled = false
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents = map[string]bool{"claude": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Find the claude section and verify enabled is true
	lines := strings.Split(out, "\n")
	foundClaude := false
	for i, line := range lines {
		if strings.Contains(line, "[agents.claude]") {
			foundClaude = true
			// Check the next few lines for enabled
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				if strings.Contains(lines[j], "enabled") {
					assert.Contains(t, lines[j], "enabled = true", "claude should be enabled")
					break
				}
			}
			break
		}
	}
	assert.True(t, foundClaude, "should find claude section")
}

func TestPatchConfig_SetModel(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexModelTouched = true
	choices.CodexModel = "gpt-5"

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `model = "gpt-5"`)
}

func TestPatchConfig_EnableWarnings(t *testing.T) {
	content := ``

	choices := NewChoices()
	choices.WarningsEnabledTouched = true
	choices.WarningsEnabled = true
	choices.InstructionTokenThreshold = 10000
	choices.MCPServerThreshold = 15
	choices.MCPToolsTotalThreshold = 60
	choices.MCPServerToolsThreshold = 25
	choices.MCPSchemaTokensTotalThreshold = 30000
	choices.MCPSchemaTokensServerThreshold = 20000

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[warnings]")
	assert.Contains(t, out, "instruction_token_threshold = 10000")
	assert.Contains(t, out, "mcp_server_threshold = 15")
	assert.Contains(t, out, "mcp_tools_total_threshold = 60")
	assert.Contains(t, out, "mcp_server_tools_threshold = 25")
	assert.Contains(t, out, "mcp_schema_tokens_total_threshold = 30000")
	assert.Contains(t, out, "mcp_schema_tokens_server_threshold = 20000")
}

func TestPatchConfig_RestoreMissingMCPServer(t *testing.T) {
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	require.Greater(t, len(defaults), 0)

	serverID := defaults[0].ID

	content := `[mcp]`

	choices := NewChoices()
	choices.RestoreMissingMCPServers = true
	choices.MissingDefaultMCPServers = []string{serverID}
	choices.DefaultMCPServers = defaults

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, serverID))
}

func TestParseTomlDocument_EmptyContent(t *testing.T) {
	doc := parseTomlDocument("")

	assert.Empty(t, doc.sections)
	assert.Empty(t, doc.arrays)
	assert.Empty(t, doc.order)
}

func TestParseTomlDocument_PreambleOnly(t *testing.T) {
	content := `# This is a preamble comment
# Another line`

	doc := parseTomlDocument(content)

	assert.Len(t, doc.preamble, 2)
	assert.Contains(t, doc.preamble[0], "preamble comment")
	assert.Empty(t, doc.sections)
}

func TestParseTomlDocument_SingleSection(t *testing.T) {
	content := `[section]
key = "value"`

	doc := parseTomlDocument(content)

	require.Contains(t, doc.sections, "section")
	assert.Len(t, doc.sections["section"].lines, 2)
	assert.Equal(t, []string{"section"}, doc.order)
}

func TestParseTomlDocument_ArrayOfTables(t *testing.T) {
	content := `[[array]]
id = "first"

[[array]]
id = "second"`

	doc := parseTomlDocument(content)

	require.Contains(t, doc.arrays, "array")
	assert.Len(t, doc.arrays["array"], 2)
}

func TestParseTomlDocument_MixedContent(t *testing.T) {
	content := `# preamble
[section1]
a = 1

[[array]]
id = "item"

[section2]
b = 2`

	doc := parseTomlDocument(content)

	assert.Len(t, doc.preamble, 1)
	require.Contains(t, doc.sections, "section1")
	require.Contains(t, doc.sections, "section2")
	require.Contains(t, doc.arrays, "array")
	assert.Equal(t, []string{"section1", "section2"}, doc.order)
}

func TestParseTomlHeader_ValidHeaders(t *testing.T) {
	tests := []struct {
		line    string
		name    string
		isArray bool
		ok      bool
	}{
		{"[section]", "section", false, true},
		{"[[array]]", "array", true, true},
		{"[dotted.name]", "dotted.name", false, true},
		{"[[dotted.array]]", "dotted.array", true, true},
		{"  [indented]  ", "indented", false, true},
		{"# comment", "", false, false},
		{"", "", false, false},
		{"key = value", "", false, false},
		// Inline comments on headers
		{"[section] # comment", "section", false, true},
		{"[[array]] # inline comment", "array", true, true},
		{"[dotted.name] # with comment", "dotted.name", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			name, isArray, ok := parseTomlHeader(tt.line)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.isArray, isArray)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestExtractMCPServerID(t *testing.T) {
	t.Run("finds id", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`id = "github"`,
			"enabled = true",
		}
		id := extractMCPServerID(lines)
		assert.Equal(t, "github", id)
	})

	t.Run("skips comments", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`# id = "commented"`,
			`id = "actual"`,
		}
		id := extractMCPServerID(lines)
		assert.Equal(t, "actual", id)
	})

	t.Run("no id", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			"enabled = true",
		}
		id := extractMCPServerID(lines)
		assert.Equal(t, "", id)
	})
}

func TestFormatTomlValue(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{"hello", `"hello"`},
		{true, "true"},
		{false, "false"},
		{42, "42"},
		{3.14, "3.14"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.input), func(t *testing.T) {
			result := formatTomlValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReplaceOrInsertLine_RemovesDuplicates(t *testing.T) {
	block := &tomlBlock{
		name: "test",
		lines: []string{
			"[test]",
			"key = 1",
			"# key = commented",
			"key = 2",
		},
	}

	replaceOrInsertLine(block, "key", "key = 3", "")

	count := 0
	for _, line := range block.lines {
		if strings.Contains(line, "key") && !strings.HasPrefix(strings.TrimSpace(line), "[") {
			count++
		}
	}
	assert.Equal(t, 1, count, "should have only one key line after replacement")
	assert.Contains(t, block.lines, "key = 3")
}

func TestReplaceOrInsertLine_InsertsAfterKey(t *testing.T) {
	block := &tomlBlock{
		name: "test",
		lines: []string{
			"[test]",
			"first = 1",
			"third = 3",
		},
	}

	replaceOrInsertLine(block, "second", "second = 2", "first")

	firstIdx := -1
	secondIdx := -1
	for i, line := range block.lines {
		if strings.HasPrefix(line, "first") {
			firstIdx = i
		}
		if strings.HasPrefix(line, "second") {
			secondIdx = i
		}
	}

	require.NotEqual(t, -1, firstIdx)
	require.NotEqual(t, -1, secondIdx)
	assert.Equal(t, firstIdx+1, secondIdx, "second should be inserted right after first")
}

func TestPatchConfig_PreservesCustomArrayOfTables(t *testing.T) {
	content := `
[approvals]
mode = "mcp"

[[custom.items]]
name = "first"
value = 1

[[custom.items]]
name = "second"
value = 2

[[another.array]]
id = "item1"
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// All custom array-of-table blocks should be preserved
	assert.Contains(t, out, "[[custom.items]]")
	assert.Contains(t, out, `name = "first"`)
	assert.Contains(t, out, `name = "second"`)
	assert.Contains(t, out, "[[another.array]]")
	assert.Contains(t, out, `id = "item1"`)

	// Count occurrences to ensure both custom.items blocks are present
	count := strings.Count(out, "[[custom.items]]")
	assert.Equal(t, 2, count, "both custom.items blocks should be preserved")
}

func TestPatchConfig_HeaderWithInlineComment(t *testing.T) {
	content := `
[approvals] # this is the approvals section
mode = "mcp"
`
	choices := NewChoices()
	choices.ApprovalModeTouched = true
	choices.ApprovalMode = "all"

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Section should be recognized and updated
	assert.Contains(t, out, `mode = "all"`)
}

func TestExtraArrayBlocks(t *testing.T) {
	arrays := map[string][]*tomlBlock{
		"mcp.servers": {
			{name: "mcp.servers", lines: []string{"[[mcp.servers]]", `id = "test"`}},
		},
		"custom.items": {
			{name: "custom.items", lines: []string{"[[custom.items]]", "a = 1"}},
			{name: "custom.items", lines: []string{"[[custom.items]]", "b = 2"}},
		},
		"another": {
			{name: "another", lines: []string{"[[another]]", "x = 1"}},
		},
	}

	extra := extraArrayBlocks(arrays)

	// Should not include mcp.servers
	for _, block := range extra {
		assert.NotEqual(t, "mcp.servers", block.name)
	}

	// Should include custom.items (2 blocks) and another (1 block)
	assert.Len(t, extra, 3)

	// Should be sorted by name
	names := make([]string, len(extra))
	for i, block := range extra {
		names[i] = block.name
	}
	assert.True(t, sort.SliceIsSorted(names, func(i, j int) bool {
		return names[i] < names[j]
	}), "extra arrays should be sorted by name")
}
