package wizard

import (
	"fmt"
	"sort"
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

	t.Run("restore missing server without template block", func(t *testing.T) {
		choices := NewChoices()
		choices.RestoreMissingMCPServers = true
		choices.MissingDefaultMCPServers = []string{"does-not-exist"}
		choices.DefaultMCPServers = []DefaultMCPServer{{ID: "does-not-exist"}}

		_, err := PatchConfig("", choices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing default MCP server template")
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

func TestPatchConfig_PreservesCodexAgentSpecificFeatures(t *testing.T) {
	content := `
[approvals]
mode = "none"

[agents.codex]
enabled = false
model = "gpt-5"
reasoning_effort = "medium"

[agents.codex.agent_specific]
note = "keep this"

[agents.codex.agent_specific.features]
multi_agent = true
prevent_idle_sleep = true
`
	choices := NewChoices()
	choices.ApprovalModeTouched = true
	choices.ApprovalMode = "all"
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents = map[string]bool{AgentCodex: true}
	choices.CodexModelTouched = true
	choices.CodexModel = "gpt-5.3-codex"
	choices.CodexReasoningTouched = true
	choices.CodexReasoning = "xhigh"

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `[agents.codex.agent_specific]`)
	assert.Contains(t, out, `note = "keep this"`)
	assert.Contains(t, out, `[agents.codex.agent_specific.features]`)
	assert.Contains(t, out, `multi_agent = true`)
	assert.Contains(t, out, `prevent_idle_sleep = true`)
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

	t.Run("ignores id inside multiline string", func(t *testing.T) {
		// This test verifies that content inside multiline strings is not
		// incorrectly parsed as a key-value pair.
		lines := []string{
			"[[mcp.servers]]",
			`description = """`,
			`id = "fake-id"`,
			`"""`,
			`id = "real-id"`,
		}
		id := extractMCPServerID(lines)
		assert.Equal(t, "real-id", id)
	})

	t.Run("ignores id inside multiline literal string", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`description = '''`,
			`id = "fake-id"`,
			`'''`,
			`id = "real-id"`,
		}
		id := extractMCPServerID(lines)
		assert.Equal(t, "real-id", id)
	})
}

func TestFindKeyLine_IgnoresMultilineContent(t *testing.T) {
	lines := []string{
		"[section]",
		`description = """`,
		`key = "fake"`,
		`"""`,
		`key = "real"`,
	}

	result, ok := findKeyLine(lines, "key")
	require.True(t, ok)
	assert.Contains(t, result.raw, `key = "real"`)
	assert.NotContains(t, result.raw, "fake")
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

func TestAssembleCanonicalConfig_SkipsNilBlock(t *testing.T) {
	current := tomlDocument{
		preamble: []string{"# current preamble"},
		sections: map[string]*tomlBlock{},
		arrays:   map[string][]*tomlBlock{},
	}
	template := tomlDocument{
		preamble: []string{"# template preamble"},
		sections: map[string]*tomlBlock{"missing": nil},
		arrays:   map[string][]*tomlBlock{},
		order:    []string{"missing"},
	}

	out, err := assembleCanonicalConfig(current, template, NewChoices())
	require.NoError(t, err)
	assert.Equal(t, []string{"# current preamble"}, out)
}

func TestDefaultServerIDs_SkipsEmptyIDs(t *testing.T) {
	choices := &Choices{
		DefaultMCPServers: []DefaultMCPServer{
			{ID: ""},
			{ID: "github"},
		},
	}

	ids := defaultServerIDs(choices, nil)
	assert.Equal(t, []string{"github"}, ids)
}

func TestParseKeyValueWithState_EdgeCases(t *testing.T) {
	key, value, ok := parseKeyValueWithState(`id = "x" # trailing comment`, "id", tomlStateNone)
	require.True(t, ok)
	assert.Equal(t, "id", key)
	assert.Equal(t, `"x"`, value)

	_, _, ok = parseKeyValueWithState(`id "x"`, "id", tomlStateNone)
	assert.False(t, ok)
}

func TestSetCommentedKeyLine_CommentsExistingLineWhenTemplateMissing(t *testing.T) {
	block := &tomlBlock{
		name:  "agents.gemini",
		lines: []string{"[agents.gemini]", `model = "custom"`},
	}
	setCommentedKeyLine(block, nil, "model", "enabled")
	assert.Contains(t, strings.Join(block.lines, "\n"), "# model =")
}

func TestSetKeyValue_UsesExistingLineAndInsertsWhenMissing(t *testing.T) {
	t.Run("updates existing line", func(t *testing.T) {
		block := &tomlBlock{
			name:  "agents.claude",
			lines: []string{"[agents.claude]", "enabled = false"},
		}
		setKeyValue(block, nil, "enabled", "true", "")
		assert.Contains(t, strings.Join(block.lines, "\n"), "enabled = true")
	})

	t.Run("inserts when missing", func(t *testing.T) {
		block := &tomlBlock{
			name:  "agents.claude",
			lines: []string{"[agents.claude]"},
		}
		setKeyValue(block, nil, "enabled", "true", "")
		assert.Contains(t, strings.Join(block.lines, "\n"), "enabled = true")
	})
}

func TestFindKeyLine_KeyMissingReturnsFalse(t *testing.T) {
	_, ok := findKeyLine([]string{"[section]", `present = "yes"`}, "missing")
	assert.False(t, ok)
}

func TestBuildKeyLine_CommentAndInlineComment(t *testing.T) {
	line := buildKeyLine(keyLine{indent: "  ", inlineComment: "# note"}, "model", `"x"`, true)
	assert.Equal(t, `  # model = "x" # note`, line)
}

func TestEnsureCommented_AddsCommentAfterIndent(t *testing.T) {
	line := ensureCommented("\tmodel = \"x\"")
	assert.Equal(t, "\t# model = \"x\"", line)
}

func TestReplaceOrInsertLine_SkipsMultilineStringContent(t *testing.T) {
	block := &tomlBlock{
		name: "test",
		lines: []string{
			"[test]",
			`description = """`,
			`key = "fake"`,
			`"""`,
			`key = "real"`,
		},
	}

	replaceOrInsertLine(block, "key", `key = "new"`, "")

	joined := strings.Join(block.lines, "\n")
	assert.Contains(t, joined, `key = "fake"`) // inside multiline string content
	assert.Contains(t, joined, `key = "new"`)
	assert.NotContains(t, joined, `key = "real"`)
}

func TestFindInsertIndex_EdgeCases(t *testing.T) {
	assert.Equal(t, 0, findInsertIndex(nil, "after"))

	lines := []string{
		"[test]",
		`description = """`,
		`after = "fake"`,
		`"""`,
		"other = 1",
	}
	assert.Equal(t, 1, findInsertIndex(lines, "after"))
	assert.Equal(t, 1, findInsertIndex([]string{"[test]", "x = 1"}, ""))
}

func TestSanitizeMCPServerBlock_StdioRemovesHeaders(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "myserver"`,
			`enabled = true`,
			`transport = "stdio"`,
			`command = "npx"`,
			`args = ["-y", "some-package"]`,
			`headers = { Authorization = "Bearer ${TOKEN}" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "headers")
	assert.Contains(t, joined, `transport = "stdio"`)
	assert.Contains(t, joined, `command = "npx"`)
	assert.Contains(t, joined, `id = "myserver"`)
}

func TestSanitizeMCPServerBlock_StdioRemovesURLAndHTTPTransport(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "broken"`,
			`enabled = true`,
			`transport = "stdio"`,
			`command = "run"`,
			`url = "https://leftover.example.com"`,
			`http_transport = "streamable"`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "url =")
	assert.NotContains(t, joined, "http_transport")
	assert.Contains(t, joined, `command = "run"`)
}

func TestSanitizeMCPServerBlock_HTTPRemovesCommandArgsEnv(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "httpserver"`,
			`enabled = true`,
			`transport = "http"`,
			`url = "https://api.example.com"`,
			`command = "leftover"`,
			`args = ["--stale"]`,
			`env = { KEY = "value" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "command =")
	assert.NotContains(t, joined, "args =")
	assert.NotContains(t, joined, "env =")
	assert.Contains(t, joined, `url = "https://api.example.com"`)
}

func TestSanitizeMCPServerBlock_PreservesCommentedLines(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "myserver"`,
			`transport = "stdio"`,
			`command = "npx"`,
			`# headers = { old = "commented" }`,
			`headers = { Authorization = "Bearer ${TOKEN}" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	// Uncommented headers line should be removed.
	assert.NotContains(t, joined, `Authorization`)
	// Commented headers line should be preserved.
	assert.Contains(t, joined, `# headers = { old = "commented" }`)
}

func TestSanitizeMCPServerBlock_NoTransportDoesNothing(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "notransport"`,
			`enabled = true`,
			`headers = { X = "kept" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.Contains(t, joined, `headers = { X = "kept" }`)
}

func TestSanitizeMCPServerBlock_StdioRemovesDottedHeaders(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "myserver"`,
			`enabled = true`,
			`transport = "stdio"`,
			`command = "npx"`,
			`headers.Authorization = "Bearer ${TOKEN}"`,
			`headers."X-Custom" = "value"`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "headers")
	assert.NotContains(t, joined, "Authorization")
	assert.NotContains(t, joined, "X-Custom")
	assert.Contains(t, joined, `transport = "stdio"`)
	assert.Contains(t, joined, `command = "npx"`)
	assert.Contains(t, joined, `id = "myserver"`)
}

func TestSanitizeMCPServerBlock_HTTPRemovesDottedEnv(t *testing.T) {
	block := &tomlBlock{
		name: "mcp.servers",
		lines: []string{
			"[[mcp.servers]]",
			`id = "myhttp"`,
			`enabled = true`,
			`transport = "http"`,
			`url = "https://api.example.com"`,
			`env.TOKEN = "secret"`,
			`env.PATH = "/usr/bin"`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "env")
	assert.NotContains(t, joined, "TOKEN")
	assert.NotContains(t, joined, "PATH")
	assert.Contains(t, joined, `transport = "http"`)
	assert.Contains(t, joined, `url = "https://api.example.com"`)
}

func TestPatchConfig_SanitizesHTTPMultilineArgs(t *testing.T) {
	// Simulate an HTTP server that has leftover multiline args from a transport
	// change. The wizard must remove all continuation lines, not just the key line.
	content := `
[mcp]

[[mcp.servers]]
id = "myhttp"
enabled = true
transport = "http"
url = "https://api.example.com"
args = [
    "--one",
    "--two",
]
`
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "myhttp"}}
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"myhttp": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "args")
	assert.NotContains(t, out, "--one")
	assert.NotContains(t, out, "--two")
	assert.Contains(t, out, `url = "https://api.example.com"`)

	// Verify the output is valid TOML by parsing it.
	_, parseErr := toml.LoadBytes([]byte(out))
	require.NoError(t, parseErr, "patched output must be valid TOML")
}

func TestPatchConfig_SanitizesStdioHeadersDuringPatch(t *testing.T) {
	// Simulate a config that has headers on a stdio server — the exact
	// scenario that caused the user's "headers are not allowed for stdio
	// transport" validation error after upgrading.
	content := `
[mcp]

[[mcp.servers]]
id = "context7"
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
headers = { Authorization = "Bearer ${TOKEN}" }
`
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "context7"}}
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"context7": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "headers")
	assert.Contains(t, out, `transport = "stdio"`)
	assert.Contains(t, out, `command = "npx"`)
}

func TestPatchConfig_SanitizesDottedHeadersOnStdioServer(t *testing.T) {
	// Regression: dotted-key headers (headers.Foo = "bar") were invisible
	// to removeKeyFromBlock because parseKeyLineWithState only matched
	// "key = value" format, not "key.subkey = value".
	content := `
[mcp]

[[mcp.servers]]
id = "context7"
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
headers.Authorization = "Bearer ${TOKEN}"
headers."X-Tools" = "action_list"
`
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "context7"}}
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"context7": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "headers")
	assert.NotContains(t, out, "Authorization")
	assert.NotContains(t, out, "X-Tools")
	assert.Contains(t, out, `transport = "stdio"`)
	assert.Contains(t, out, `command = "npx"`)

	// Verify the output is valid TOML.
	_, parseErr := toml.LoadBytes([]byte(out))
	require.NoError(t, parseErr, "patched output must be valid TOML")
}

func TestPatchConfig_SanitizesDottedEnvOnHTTPServer(t *testing.T) {
	// Regression: dotted-key env (env.TOKEN = "val") was invisible to sanitization.
	content := `
[mcp]

[[mcp.servers]]
id = "myhttp"
enabled = true
transport = "http"
url = "https://api.example.com"
env.TOKEN = "secret"
env.PATH = "/usr/bin"
`
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "myhttp"}}
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"myhttp": true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "env.TOKEN")
	assert.NotContains(t, out, "env.PATH")
	assert.NotContains(t, out, `"secret"`)
	assert.Contains(t, out, `transport = "http"`)
	assert.Contains(t, out, `url = "https://api.example.com"`)

	_, parseErr := toml.LoadBytes([]byte(out))
	require.NoError(t, parseErr, "patched output must be valid TOML")
}

func TestExtractMCPBlockKeyValue(t *testing.T) {
	t.Run("extracts transport value", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`id = "test"`,
			`transport = "stdio"`,
		}
		assert.Equal(t, "stdio", extractMCPBlockKeyValue(lines, "transport"))
	})

	t.Run("returns empty for missing key", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`id = "test"`,
		}
		assert.Equal(t, "", extractMCPBlockKeyValue(lines, "transport"))
	})

	t.Run("skips commented lines", func(t *testing.T) {
		lines := []string{
			"[[mcp.servers]]",
			`# transport = "http"`,
			`transport = "stdio"`,
		}
		assert.Equal(t, "stdio", extractMCPBlockKeyValue(lines, "transport"))
	})
}

func TestRemoveKeyFromBlock(t *testing.T) {
	t.Run("removes uncommented key", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`headers = { X = "remove" }`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "headers")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "headers")
		assert.Contains(t, joined, "command")
	})

	t.Run("preserves commented key", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`# headers = { old = "commented" }`,
				`headers = { new = "active" }`,
			},
		}
		removeKeyFromBlock(block, "headers")
		joined := strings.Join(block.lines, "\n")
		assert.Contains(t, joined, `# headers = { old = "commented" }`)
		assert.NotContains(t, joined, `new = "active"`)
	})

	t.Run("noop when key absent", func(t *testing.T) {
		block := &tomlBlock{
			name:  "test",
			lines: []string{"[[mcp.servers]]", `id = "test"`},
		}
		before := len(block.lines)
		removeKeyFromBlock(block, "headers")
		assert.Equal(t, before, len(block.lines))
	})

	t.Run("removes multiline array", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`args = [`,
				`    "--flag1",`,
				`    "--flag2",`,
				`]`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "args")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "args")
		assert.NotContains(t, joined, "--flag1")
		assert.NotContains(t, joined, "--flag2")
		assert.Contains(t, joined, `command = "keep"`)
		assert.Contains(t, joined, `id = "test"`)
	})

	t.Run("removes multiline array with brackets in strings", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`args = [`,
				`    "value with ] bracket",`,
				`    "--other",`,
				`]`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "args")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "args")
		assert.NotContains(t, joined, "bracket")
		assert.NotContains(t, joined, "--other")
		assert.Contains(t, joined, `command = "keep"`)
	})

	t.Run("removes multiline inline table", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`env = {`,
				`    KEY1 = "val1",`,
				`    KEY2 = "val2",`,
				`}`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "env")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "env")
		assert.NotContains(t, joined, "KEY1")
		assert.NotContains(t, joined, "KEY2")
		assert.Contains(t, joined, `command = "keep"`)
	})

	t.Run("removes multiline triple-quoted string", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`command = """`,
				`/usr/local/bin/`,
				`my-server`,
				`"""`,
				`enabled = true`,
			},
		}
		removeKeyFromBlock(block, "command")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "command")
		assert.NotContains(t, joined, "my-server")
		assert.Contains(t, joined, `enabled = true`)
	})

	t.Run("handles single-line array correctly", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`args = ["-y", "pkg@1.0"]`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "args")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "args")
		assert.Contains(t, joined, `command = "keep"`)
	})

	t.Run("removes dotted sub-keys", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`headers.Authorization = "Bearer token"`,
				`headers."X-Custom" = "value"`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "headers")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "headers")
		assert.NotContains(t, joined, "Authorization")
		assert.NotContains(t, joined, "X-Custom")
		assert.Contains(t, joined, `command = "keep"`)
	})

	t.Run("preserves commented dotted sub-keys", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`# headers.Authorization = "Bearer old"`,
				`headers.Authorization = "Bearer active"`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "headers")
		joined := strings.Join(block.lines, "\n")
		assert.Contains(t, joined, `# headers.Authorization = "Bearer old"`)
		assert.NotContains(t, joined, `Bearer active`)
		assert.Contains(t, joined, `command = "keep"`)
	})

	t.Run("removes mix of inline table and dotted keys", func(t *testing.T) {
		block := &tomlBlock{
			name: "test",
			lines: []string{
				"[[mcp.servers]]",
				`id = "test"`,
				`env.TOKEN = "secret"`,
				`env.PATH = "/usr/bin"`,
				`command = "keep"`,
			},
		}
		removeKeyFromBlock(block, "env")
		joined := strings.Join(block.lines, "\n")
		assert.NotContains(t, joined, "env")
		assert.NotContains(t, joined, "TOKEN")
		assert.NotContains(t, joined, "PATH")
		assert.Contains(t, joined, `command = "keep"`)
	})
}

func TestMultilineValueEndIndex(t *testing.T) {
	t.Run("single line scalar", func(t *testing.T) {
		lines := []string{`command = "npx"`}
		assert.Equal(t, 0, multilineValueEndIndex(lines, 0))
	})

	t.Run("single line array", func(t *testing.T) {
		lines := []string{`args = ["-y", "pkg"]`}
		assert.Equal(t, 0, multilineValueEndIndex(lines, 0))
	})

	t.Run("multiline array", func(t *testing.T) {
		lines := []string{
			`args = [`,
			`    "--one",`,
			`    "--two",`,
			`]`,
		}
		assert.Equal(t, 3, multilineValueEndIndex(lines, 0))
	})

	t.Run("nested brackets in strings", func(t *testing.T) {
		lines := []string{
			`args = [`,
			`    "value with ] bracket",`,
			`    "other",`,
			`]`,
		}
		assert.Equal(t, 3, multilineValueEndIndex(lines, 0))
	})

	t.Run("multiline inline table", func(t *testing.T) {
		lines := []string{
			`env = {`,
			`    KEY = "val",`,
			`}`,
		}
		assert.Equal(t, 2, multilineValueEndIndex(lines, 0))
	})

	t.Run("triple-quoted string", func(t *testing.T) {
		lines := []string{
			`command = """`,
			`multi`,
			`line`,
			`"""`,
		}
		assert.Equal(t, 3, multilineValueEndIndex(lines, 0))
	})

	t.Run("triple-quoted string closed same line", func(t *testing.T) {
		lines := []string{`command = """inline"""`}
		assert.Equal(t, 0, multilineValueEndIndex(lines, 0))
	})

	t.Run("triple-quoted literal string", func(t *testing.T) {
		lines := []string{
			`command = '''`,
			`multi`,
			`'''`,
		}
		assert.Equal(t, 2, multilineValueEndIndex(lines, 0))
	})

	t.Run("no equals sign", func(t *testing.T) {
		lines := []string{`no-equals`}
		assert.Equal(t, 0, multilineValueEndIndex(lines, 0))
	})

	t.Run("unbalanced brackets returns start", func(t *testing.T) {
		lines := []string{`args = [`}
		// Only one line, bracket never closed — should fall back to startIdx.
		assert.Equal(t, 0, multilineValueEndIndex(lines, 0))
	})

	t.Run("startIdx beyond range", func(t *testing.T) {
		lines := []string{`x = 1`}
		assert.Equal(t, 5, multilineValueEndIndex(lines, 5))
	})

	t.Run("multiline string inside array does not break bracket tracking", func(t *testing.T) {
		// A triple-quoted string inside an array that contains ] on an interior
		// line. Without cross-line quote state, the ] would incorrectly close
		// the array.
		lines := []string{
			`args = ["""`,
			`value with ] bracket`,
			`""",`,
			`"other",`,
			`]`,
		}
		assert.Equal(t, 4, multilineValueEndIndex(lines, 0))
	})
}

func TestCountBracketDepth(t *testing.T) {
	noState := quoteState{}

	t.Run("simple open bracket", func(t *testing.T) {
		depth, _ := countBracketDepth("[", '[', ']', noState)
		assert.Equal(t, 1, depth)
	})

	t.Run("balanced", func(t *testing.T) {
		depth, _ := countBracketDepth(`["a", "b"]`, '[', ']', noState)
		assert.Equal(t, 0, depth)
	})

	t.Run("bracket inside double quotes ignored", func(t *testing.T) {
		depth, _ := countBracketDepth(`["value with ] bracket"`, '[', ']', noState)
		assert.Equal(t, 1, depth)
	})

	t.Run("bracket inside single quotes ignored", func(t *testing.T) {
		depth, _ := countBracketDepth(`['value with ] bracket'`, '[', ']', noState)
		assert.Equal(t, 1, depth)
	})

	t.Run("escaped quote in string does not break tracking", func(t *testing.T) {
		// The \" is an escaped quote inside the string; the ] correctly closes the array.
		// Without escape handling, the \" would prematurely end the string.
		depth, _ := countBracketDepth(`["escaped \" quote"]`, '[', ']', noState)
		assert.Equal(t, 0, depth)
	})

	t.Run("comment stops counting", func(t *testing.T) {
		depth, _ := countBracketDepth(`[ # comment with ]`, '[', ']', noState)
		assert.Equal(t, 1, depth)
	})

	t.Run("no brackets", func(t *testing.T) {
		depth, _ := countBracketDepth(`"just a string"`, '[', ']', noState)
		assert.Equal(t, 0, depth)
	})

	t.Run("curly braces", func(t *testing.T) {
		depth, _ := countBracketDepth(`{ KEY = "val" }`, '{', '}', noState)
		assert.Equal(t, 0, depth)
	})

	t.Run("quote state persists across lines", func(t *testing.T) {
		// Line 1 opens a double-quoted string that doesn't close on this line.
		// Line 2 has a ] inside the still-open string — should be ignored.
		// Line 3 closes the string and closes the array.
		qs := noState
		var depth int
		depth, qs = countBracketDepth(`["""`, '[', ']', qs)
		assert.Equal(t, 1, depth, "opening bracket counted, triple-quote opens string")
		assert.True(t, qs.inDouble, "should be inside double-quoted string after line 1")

		depth, qs = countBracketDepth(`value with ] bracket`, '[', ']', qs)
		assert.Equal(t, 0, depth, "] inside string should be ignored")
		assert.True(t, qs.inDouble, "still inside double-quoted string")

		depth, qs = countBracketDepth(`""",`, '[', ']', qs)
		assert.Equal(t, 0, depth, "no brackets on closing line")
		assert.False(t, qs.inDouble, "string should be closed")

		depth, _ = countBracketDepth(`]`, '[', ']', qs)
		assert.Equal(t, -1, depth, "closing bracket counted")
	})
}

func TestContainsUnescapedTripleQuote(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"plain triple quote", `hello """`, true},
		{"just triple quote", `"""`, true},
		{"escaped first quote", `hello \"""`, false},
		{"double-escaped backslash then triple quote", `hello \\"""`, true},
		{"triple-escaped then triple quote", `hello \\\"""`, false},
		{"no triple quote at all", `hello world`, false},
		{"two quotes only", `hello ""`, false},
		{"escaped then later unescaped", `\""" then """`, true},
		{"empty string", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, containsUnescapedTripleQuote(tt.input))
		})
	}
}

func TestMultilineValueEndIndex_EscapedTripleQuote(t *testing.T) {
	t.Run("escaped triple quote does not close multiline string", func(t *testing.T) {
		// In TOML basic strings, \" is an escaped quote.
		// \""" is NOT a closing delimiter — the first quote is escaped.
		// The actual closing delimiter is the plain """ on the next line.
		lines := []string{
			`command = """`,
			`line with \""" not a close`,
			`still open`,
			`"""`,
		}
		assert.Equal(t, 3, multilineValueEndIndex(lines, 0))
	})

	t.Run("double-escaped backslash before triple quote does close", func(t *testing.T) {
		// \\ is an escaped backslash; the """ after it is unescaped.
		lines := []string{
			`command = """`,
			`line with \\"""`,
		}
		assert.Equal(t, 1, multilineValueEndIndex(lines, 0))
	})
}

func TestRemoveKeyFromBlock_EscapedTripleQuoteMultiline(t *testing.T) {
	block := &tomlBlock{
		name: "test",
		lines: []string{
			"[[mcp.servers]]",
			`id = "test"`,
			`command = """`,
			`path with \""" embedded`,
			`still going`,
			`"""`,
			`enabled = true`,
		},
	}
	removeKeyFromBlock(block, "command")
	joined := strings.Join(block.lines, "\n")
	assert.NotContains(t, joined, "command")
	assert.NotContains(t, joined, "path with")
	assert.NotContains(t, joined, "still going")
	assert.Contains(t, joined, `id = "test"`)
	assert.Contains(t, joined, `enabled = true`)
}

func TestSanitizeMCPServerBlock_SectionStyleSubTableNotInBlock(t *testing.T) {
	// Section-style sub-tables like [mcp.servers.env] are parsed as separate
	// sections by the line-based parser — they are NOT part of the [[mcp.servers]]
	// block. This means sanitizeMCPServerBlock cannot reach them.
	//
	// This is a known limitation of the line-based parser. In practice, agent-layer
	// templates use inline tables (env = { KEY = "val" }), and go-toml validation
	// will still reject the config if section-style sub-tables contain transport-
	// incompatible fields. The wizard just can't auto-remove them.
	//
	// This test verifies that sanitization works correctly on the server block
	// itself when a section-style sub-table exists — no crash, no corruption.
	content := `
[mcp]

[[mcp.servers]]
id = "myserver"
enabled = true
transport = "http"
url = "https://api.example.com"
command = "leftover"

[mcp.servers.env]
KEY = "val"
`
	doc := parseTomlDocument(content)

	// The section-style sub-table should be parsed as a separate section.
	require.Contains(t, doc.sections, "mcp.servers.env",
		"section-style sub-table should be a separate section in the line-based parser")

	// The [[mcp.servers]] block should NOT contain the env key-value.
	require.Contains(t, doc.arrays, "mcp.servers")
	require.Len(t, doc.arrays["mcp.servers"], 1)
	serverBlock := doc.arrays["mcp.servers"][0]
	joined := strings.Join(serverBlock.lines, "\n")
	assert.NotContains(t, joined, "KEY =",
		"section-style env sub-table should not be inside the server block")

	// Sanitize the server block — it should remove "command" (http-incompatible).
	tb := tomlBlock{name: serverBlock.name, lines: cloneLines(serverBlock.lines)}
	sanitizeMCPServerBlock(&tb)
	sanitized := strings.Join(tb.lines, "\n")
	assert.NotContains(t, sanitized, "command")
	assert.Contains(t, sanitized, `url = "https://api.example.com"`)
}

func TestPatchConfig_ClaudeLocalConfigDirEnabled(t *testing.T) {
	content := `
[agents.claude]
enabled = true
# local_config_dir = false
`
	choices := NewChoices()
	choices.ClaudeLocalConfigDirTouched = true
	choices.ClaudeLocalConfigDir = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "local_config_dir = true")
	assert.NotContains(t, out, "# local_config_dir")
}

func TestPatchConfig_ClaudeLocalConfigDirDisabled(t *testing.T) {
	content := `
[agents.claude]
enabled = true
local_config_dir = true
`
	choices := NewChoices()
	choices.ClaudeLocalConfigDirTouched = true
	choices.ClaudeLocalConfigDir = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "# local_config_dir")
	assert.NotContains(t, out, "local_config_dir = true")
}

func TestPatchHelpers_EdgeCases(t *testing.T) {
	assert.Nil(t, cloneBlock(nil))
	assert.Nil(t, cloneLines(nil))

	output := []string{"[section]"}
	appendBlock(&output, []string{"", "  ", ""})
	assert.Equal(t, []string{"[section]"}, output)

	assert.Equal(t, []string{"a"}, trimEmptyLines([]string{"", "a", ""}))
	assert.Equal(t, []string{"a"}, trimTrailingEmptyLines([]string{"a", "", "  "}))
}
