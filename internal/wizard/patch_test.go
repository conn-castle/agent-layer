package wizard

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/tomlpatch"
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

	t.Run("missing default server selected without template block", func(t *testing.T) {
		choices := NewChoices()
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{"does-not-exist": true}
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

[agents.antigravity]
enabled = false
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)

	idxApprovals := strings.Index(out, "[approvals]")
	idxAntigravity := strings.Index(out, "[agents.antigravity]")
	idxMCP := strings.Index(out, "[mcp]")
	idxWarnings := strings.Index(out, "[warnings]")

	require.NotEqual(t, -1, idxApprovals)
	require.NotEqual(t, -1, idxAntigravity)
	require.NotEqual(t, -1, idxMCP)
	require.NotEqual(t, -1, idxWarnings)

	assert.Less(t, idxApprovals, idxAntigravity)
	assert.Less(t, idxMCP, idxWarnings)
}

func TestPatchConfig_UpdatesMuseSection(t *testing.T) {
	content := "[agents.muse]\nenabled = false\n# model = \"\"\n# reasoning_effort = \"\"\n"
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentMuse] = true
	choices.AgentModels[AgentMuse] = AgentModelChoice{ModelTouched: true, Model: "muse-spark", ReasoningTouched: true, Reasoning: "high"}
	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, "[agents.muse]")
	assert.Contains(t, out, "enabled = true")
	assert.Contains(t, out, `model = "muse-spark"`)
	assert.Contains(t, out, `reasoning_effort = "high"`)
}

func TestPatchConfig_MigratesLegacyClaudeVSCodeSection(t *testing.T) {
	content := `
[agents.claude-vscode] # legacy
enabled = true

[agents.vscode]
enabled = false
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)

	doc := tomlpatch.ParseDocument(out)
	if _, exists := doc.Sections["agents.claude-vscode"]; exists {
		t.Fatal("expected legacy section to be removed")
	}
	block, exists := doc.Sections["agents.claude_vscode"]
	require.True(t, exists, "expected canonical section to exist")
	require.NotEmpty(t, block.Lines)
	assert.Equal(t, "[agents.claude_vscode] # legacy", strings.TrimSpace(block.Lines[0]))
	assert.Contains(t, strings.Join(block.Lines, "\n"), "enabled = true")
}

func TestPatchConfig_DeduplicatesLegacyAndCanonicalClaudeVSCodeSections(t *testing.T) {
	content := "[agents.claude-vscode]\nenabled = false\n\n[agents.claude_vscode]\nenabled = true\n"
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)
	if strings.Contains(out, "[agents.claude-vscode]") || strings.Count(out, "[agents.claude_vscode]") != 1 {
		t.Fatalf("patched aliases were not reduced to one canonical section:\n%s", out)
	}
	if !strings.Contains(out, "[agents.claude_vscode]\nenabled = true") {
		t.Fatalf("canonical section value was not preserved:\n%s", out)
	}
}

func TestOrderedWizardSections_UsesPreferredOrderWithTemplateFallback(t *testing.T) {
	templateOrder := []string{
		"warnings",
		"mcp",
		"agents.antigravity",
		"approvals",
		"custom.section",
		"agents.codex",
	}
	got := orderedWizardSections(templateOrder)
	want := []string{
		"approvals",
		"agents.antigravity",
		"agents.codex",
		"mcp",
		"warnings",
		"custom.section",
	}
	require.Equal(t, want, got)
}

func TestPreferredWizardSectionOrder_CoversAllSupportedAgents(t *testing.T) {
	agents := SupportedAgents()
	sectionSet := make(map[string]struct{}, len(preferredWizardSectionOrder))
	for _, name := range preferredWizardSectionOrder {
		sectionSet[name] = struct{}{}
	}
	for _, agent := range agents {
		key := "agents." + agent
		if _, ok := sectionSet[key]; !ok {
			t.Errorf("SupportedAgents() includes %q but preferredWizardSectionOrder is missing %q", agent, key)
		}
	}
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
[agents.claude]
enabled = true
model = "custom"
`
	choices := NewChoices()
	choices.AgentModels[AgentClaude] = AgentModelChoice{ModelTouched: true, Model: ""}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "model = \"custom\"")
	assert.Contains(t, out, "# model =")
}

func TestPatchConfig_AntigravityModelUntouchedDoesNotWriteDefault(t *testing.T) {
	content := `
[agents.antigravity]
enabled = true
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)

	assert.NotContains(t, out, "\nmodel =")
	assert.NotContains(t, out, "[agents.antigravity.agent_specific]")
}

func TestPatchConfig_AntigravityModelWritesModel(t *testing.T) {
	content := `
[agents.antigravity]
enabled = true
`
	choices := NewChoices()
	choices.AgentModels[AgentAntigravity] = AgentModelChoice{ModelTouched: true, Model: "Gemini 3.5 Flash (High)"}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `model = "Gemini 3.5 Flash (High)"`)
	assert.NotContains(t, out, "agent_specific.model")
}

func TestPatchConfig_AntigravityModelPreservesUnrelatedAgentSpecific(t *testing.T) {
	content := `
[agents.antigravity]
enabled = true

[agents.antigravity.agent_specific]
other = true
`
	choices := NewChoices()
	choices.AgentModels[AgentAntigravity] = AgentModelChoice{ModelTouched: true, Model: "Gemini 3.1 Pro (High)"}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.antigravity.agent_specific]")
	assert.Contains(t, out, `model = "Gemini 3.1 Pro (High)"`)
	assert.Contains(t, out, "other = true")
}

func TestPatchConfig_AntigravityModelBlankCommentsExistingModel(t *testing.T) {
	content := `
[agents.antigravity]
enabled = true
model = "Gemini 3.1 Pro (High)"
`
	choices := NewChoices()
	choices.AgentModels[AgentAntigravity] = AgentModelChoice{ModelTouched: true, Model: ""}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	doc := tomlpatch.ParseDocument(out)
	antigravityBlock := doc.Sections["agents.antigravity"]
	require.NotNil(t, antigravityBlock)
	modelLine, ok := tomlpatch.FindKeyLine(antigravityBlock.Lines, "model")
	require.True(t, ok)
	assert.True(t, modelLine.Commented, "blank selection should comment the typed model line")
}

func TestPatchConfig_AntigravityModelSkippedWhenAntigravityDisabled(t *testing.T) {
	content := `
[agents.antigravity]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentAntigravity] = false
	choices.AgentModels[AgentAntigravity] = AgentModelChoice{ModelTouched: true, Model: "Gemini 3.5 Flash (High)"}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "enabled = false")
	assert.NotContains(t, out, "agent_specific.model")
	assert.NotContains(t, out, `model = "Gemini 3.5 Flash (High)"`)
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

func TestPatchConfig_WarningsDisabledKeepsNonThresholdKeys(t *testing.T) {
	content := `
[warnings]
version_update_on_sync = true
noise_mode = "quiet"
instruction_token_threshold = 100
mcp_server_threshold = 15
mcp_tools_total_threshold = 60
mcp_server_tools_threshold = 25
mcp_schema_tokens_total_threshold = 30000
mcp_schema_tokens_server_threshold = 20000
`
	choices := NewChoices()
	choices.WarningsEnabledTouched = true
	choices.WarningsEnabled = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	var parsed struct {
		Warnings map[string]any `toml:"warnings"`
	}
	require.NoError(t, toml.Unmarshal([]byte(out), &parsed))
	assert.Equal(t, map[string]any{"version_update_on_sync": true, "noise_mode": "quiet"}, parsed.Warnings)
}

func TestPatchConfig_WarningsDisabledWithoutSectionAddsNone(t *testing.T) {
	choices := NewChoices()
	choices.WarningsEnabledTouched = true
	choices.WarningsEnabled = false

	out, err := PatchConfig("[approvals]\nmode = \"all\"\n", choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "[warnings]")
	assert.NotContains(t, out, "version_update_on_sync")
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
[agents.antigravity]
enabled = true # user comment
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents = map[string]bool{"antigravity": false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// Value should be updated
	lines := strings.Split(out, "\n")
	foundAntigravity := false
	for i, line := range lines {
		if strings.Contains(line, "[agents.antigravity]") {
			foundAntigravity = true
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				if strings.Contains(lines[j], "enabled") && !strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
					assert.Contains(t, lines[j], "enabled = false", "enabled should be false")
					break
				}
			}
			break
		}
	}
	assert.True(t, foundAntigravity, "should find antigravity section")
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

func TestPatchConfig_PreservesSectionsAfterStringEndingInQuotes(t *testing.T) {
	content := `
[custom_section]
quote = """"This," she said, "is just a pointless statement.""""
notes = """
[fake_section]
"""

[approvals]
mode = "none"
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, toml.Unmarshal([]byte(out), &parsed))
	custom := parsed["custom_section"].(map[string]any)
	assert.Equal(t, `"This," she said, "is just a pointless statement."`, custom["quote"])
	assert.Equal(t, "[fake_section]\n", custom["notes"])
	assert.Equal(t, "none", parsed["approvals"].(map[string]any)["mode"])
	assert.NotContains(t, parsed, "fake_section")
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
	choices.AgentModels[AgentCodex] = AgentModelChoice{ModelTouched: true, Model: "gpt-5.3-codex", ReasoningTouched: true, Reasoning: "xhigh"}

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
	choices.AgentModels[AgentCodex] = AgentModelChoice{ModelTouched: true, Model: "gpt-5"}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `model = "gpt-5"`)
}

func TestPatchConfig_SetReasoningEffortAfterModel(t *testing.T) {
	for _, agent := range []string{AgentClaude, AgentCodex} {
		for _, modelLine := range []string{`model = "existing"`, `# model = "existing"`} {
			t.Run(agent+"/"+modelLine, func(t *testing.T) {
				content := fmt.Sprintf("[agents.%s]\nenabled = true\n%s\n", agent, modelLine)
				choices := NewChoices()
				choices.AgentModels[agent] = AgentModelChoice{ReasoningTouched: true, Reasoning: "high"}

				out, err := PatchConfig(content, choices)
				require.NoError(t, err)
				assert.Contains(t, out, modelLine+"\nreasoning_effort = \"high\"")
			})
		}
	}
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

// TestPatchConfig_Idempotent asserts that PatchConfig is idempotent across
// representative choice sets: re-applying the same Choices to its own output
// must produce byte-equal output. A regression in any code path that mutates
// based on the *current* document shape (rather than the touched-flag intent)
// will cause the second pass to diverge from the first.
func TestPatchConfig_Idempotent(t *testing.T) {
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	require.NotEmpty(t, defaults)

	mcpToggle := NewChoices()
	mcpToggle.EnabledMCPServersTouched = true
	mcpToggle.EnabledMCPServers = map[string]bool{defaults[0].ID: true}
	mcpToggle.DefaultMCPServers = defaults

	// A default the user disabled keeps its block with enabled = false; re-applying
	// must not drift (no prune, no re-enable).
	mcpDisable := NewChoices()
	mcpDisable.EnabledMCPServersTouched = true
	mcpDisable.EnabledMCPServers = map[string]bool{defaults[0].ID: false}
	mcpDisable.DefaultMCPServers = defaults

	cases := []struct {
		name    string
		content string
		choices *Choices
	}{
		{
			name:    "approval mode change",
			content: "[approvals]\nmode = \"none\"\n",
			choices: func() *Choices {
				c := NewChoices()
				c.ApprovalModeTouched = true
				c.ApprovalMode = "all"
				return c
			}(),
		},
		{
			name:    "enable agent",
			content: "[agents.claude]\nenabled = false\n",
			choices: func() *Choices {
				c := NewChoices()
				c.EnabledAgentsTouched = true
				c.EnabledAgents = map[string]bool{AgentClaude: true}
				return c
			}(),
		},
		{
			name:    "set codex model and reasoning",
			content: "[agents.codex]\nenabled = true\n",
			choices: func() *Choices {
				c := NewChoices()
				c.AgentModels[AgentCodex] = AgentModelChoice{ModelTouched: true, Model: "gpt-5", ReasoningTouched: true, Reasoning: "high"}
				return c
			}(),
		},
		{
			name:    "codex apps toggle on",
			content: "[agents.codex]\nenabled = true\n",
			choices: func() *Choices {
				c := NewChoices()
				c.EnabledAgentsTouched = true
				c.EnabledAgents = map[string]bool{AgentCodex: true}
				c.CodexAppsTouched = true
				c.CodexApps = true
				return c
			}(),
		},
		{
			name:    "warnings enabled with thresholds",
			content: "",
			choices: func() *Choices {
				c := NewChoices()
				c.WarningsEnabledTouched = true
				c.WarningsEnabled = true
				c.InstructionTokenThreshold = 10000
				c.MCPServerThreshold = 15
				c.MCPToolsTotalThreshold = 60
				c.MCPServerToolsThreshold = 25
				c.MCPSchemaTokensTotalThreshold = 30000
				c.MCPSchemaTokensServerThreshold = 20000
				return c
			}(),
		},
		{
			name:    "warnings disabled removes section",
			content: "[warnings]\ninstruction_token_threshold = 5000\n",
			choices: func() *Choices {
				c := NewChoices()
				c.WarningsEnabledTouched = true
				c.WarningsEnabled = false
				return c
			}(),
		},
		{
			name:    "mcp server toggle",
			content: "[mcp]\n",
			choices: mcpToggle,
		},
		{
			name:    "mcp server disable-in-place",
			content: fmt.Sprintf("[mcp]\n\n[[mcp.servers]]\nid = %q\nenabled = true\ntransport = \"stdio\"\ncommand = \"npx\"\n", defaults[0].ID),
			choices: mcpDisable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := PatchConfig(tc.content, tc.choices)
			require.NoError(t, err, "first pass must succeed")

			second, err := PatchConfig(first, tc.choices)
			require.NoError(t, err, "second pass must succeed")

			if first != second {
				t.Fatalf("PatchConfig is not idempotent for %q\n--- first pass ---\n%s\n--- second pass ---\n%s",
					tc.name, first, second)
			}
		})
	}
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
	arrays := map[string][]*tomlpatch.Block{
		"mcp.servers": {
			{Name: "mcp.servers", Lines: []string{"[[mcp.servers]]", `id = "test"`}},
		},
		"custom.items": {
			{Name: "custom.items", Lines: []string{"[[custom.items]]", "a = 1"}},
			{Name: "custom.items", Lines: []string{"[[custom.items]]", "b = 2"}},
		},
		"another": {
			{Name: "another", Lines: []string{"[[another]]", "x = 1"}},
		},
	}

	extra := extraArrayBlocks(arrays)

	// Should not include mcp.servers
	for _, block := range extra {
		assert.NotEqual(t, "mcp.servers", block.Name)
	}

	// Should include custom.items (2 blocks) and another (1 block)
	assert.Len(t, extra, 3)

	// Should be sorted by name
	names := make([]string, len(extra))
	for i, block := range extra {
		names[i] = block.Name
	}
	assert.True(t, sort.SliceIsSorted(names, func(i, j int) bool {
		return names[i] < names[j]
	}), "extra arrays should be sorted by name")
}

func TestAssembleCanonicalConfig_SkipsNilBlock(t *testing.T) {
	current := tomlpatch.Document{
		Preamble: []string{"# current preamble"},
		Sections: map[string]*tomlpatch.Block{},
		Arrays:   map[string][]*tomlpatch.Block{},
	}
	template := tomlpatch.Document{
		Preamble: []string{"# template preamble"},
		Sections: map[string]*tomlpatch.Block{"missing": nil},
		Arrays:   map[string][]*tomlpatch.Block{},
		Order:    []string{"missing"},
	}

	catalog := tomlpatch.Document{
		Sections: map[string]*tomlpatch.Block{},
		Arrays:   map[string][]*tomlpatch.Block{},
	}
	out, err := assembleCanonicalConfig(current, template, catalog, NewChoices())
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

func TestSanitizeMCPServerBlock_StdioRemovesHeaders(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
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

	joined := strings.Join(block.Lines, "\n")
	assert.NotContains(t, joined, "headers")
	assert.Contains(t, joined, `transport = "stdio"`)
	assert.Contains(t, joined, `command = "npx"`)
	assert.Contains(t, joined, `id = "myserver"`)
}

func TestSanitizeMCPServerBlock_StdioRemovesURLAndHTTPTransport(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
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

	joined := strings.Join(block.Lines, "\n")
	assert.NotContains(t, joined, "url =")
	assert.NotContains(t, joined, "http_transport")
	assert.Contains(t, joined, `command = "run"`)
}

func TestSanitizeMCPServerBlock_HTTPRemovesCommandArgsEnv(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
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

	joined := strings.Join(block.Lines, "\n")
	assert.NotContains(t, joined, "command =")
	assert.NotContains(t, joined, "args =")
	assert.NotContains(t, joined, "env =")
	assert.Contains(t, joined, `url = "https://api.example.com"`)
}

func TestSanitizeMCPServerBlock_PreservesCommentedLines(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
			"[[mcp.servers]]",
			`id = "myserver"`,
			`transport = "stdio"`,
			`command = "npx"`,
			`# headers = { old = "commented" }`,
			`headers = { Authorization = "Bearer ${TOKEN}" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.Lines, "\n")
	// Uncommented headers line should be removed.
	assert.NotContains(t, joined, `Authorization`)
	// Commented headers line should be preserved.
	assert.Contains(t, joined, `# headers = { old = "commented" }`)
}

func TestSanitizeMCPServerBlock_NoTransportDoesNothing(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
			"[[mcp.servers]]",
			`id = "notransport"`,
			`enabled = true`,
			`headers = { X = "kept" }`,
		},
	}

	sanitizeMCPServerBlock(block)

	joined := strings.Join(block.Lines, "\n")
	assert.Contains(t, joined, `headers = { X = "kept" }`)
}

func TestSanitizeMCPServerBlock_StdioRemovesDottedHeaders(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
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

	joined := strings.Join(block.Lines, "\n")
	assert.NotContains(t, joined, "headers")
	assert.NotContains(t, joined, "Authorization")
	assert.NotContains(t, joined, "X-Custom")
	assert.Contains(t, joined, `transport = "stdio"`)
	assert.Contains(t, joined, `command = "npx"`)
	assert.Contains(t, joined, `id = "myserver"`)
}

func TestSanitizeMCPServerBlock_HTTPRemovesDottedEnv(t *testing.T) {
	block := &tomlpatch.Block{
		Name: "mcp.servers",
		Lines: []string{
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

	joined := strings.Join(block.Lines, "\n")
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
	parseErr := toml.Unmarshal([]byte(out), &map[string]any{})
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
	// to tomlpatch.RemoveKeyFromBlock because tomlpatch.ParseKeyLineWithState only matched
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
	parseErr := toml.Unmarshal([]byte(out), &map[string]any{})
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

	parseErr := toml.Unmarshal([]byte(out), &map[string]any{})
	require.NoError(t, parseErr, "patched output must be valid TOML")
}

func TestPatchConfig_ArraySubTablesStayWithOwningElement(t *testing.T) {
	twoHTTPServers := `
[[mcp.servers]]
id = "alpha"
enabled = true
transport = "http"
url = "https://a.example"
[mcp.servers.headers]
Authorization = "Bearer ${AL_ALPHA}"

[[mcp.servers]]
id = "beta"
enabled = true
transport = "http"
url = "https://b.example"
[mcp.servers.headers]
Authorization = "Bearer ${AL_BETA}"
`
	multilineHeaders := func(quote string, extraQuotes int) string {
		triple := strings.Repeat(quote, 3)
		headers := "Authorization = " + triple + "Bearer ${AL_ALPHA}" + strings.Repeat(quote, extraQuotes) + triple + "\n" +
			"Notes = " + triple + "\n[[mcp.servers]]\n[mcp.servers.headers]\n" + triple
		return strings.Replace(twoHTTPServers, `Authorization = "Bearer ${AL_ALPHA}"`, headers, 1)
	}
	tests := []struct {
		name    string
		content string
		choices func() *Choices
		// expect adjusts the decoded input into the expected decoded output.
		expect          func(decoded map[string]any)
		wantServerOrder []string
	}{
		{name: "http headers", content: twoHTTPServers},
		{name: "basic header string closes with four quotes", content: multilineHeaders(`"`, 1)},
		{name: "basic header string closes with five quotes", content: multilineHeaders(`"`, 2)},
		{name: "literal header string closes with four quotes", content: multilineHeaders(`'`, 1)},
		{name: "literal header string closes with five quotes", content: multilineHeaders(`'`, 2)},
		{
			name: "stdio env followed by another server",
			content: `
[[mcp.servers]]
id = "alpha"
transport = "stdio"
command = "alpha"
[mcp.servers.env]
ALPHA_TOKEN = "${AL_ALPHA}"

[[mcp.servers]]
id = "beta"
transport = "stdio"
command = "beta"
`,
		},
		{
			name: "sub-table after an unrelated table",
			content: `
[[mcp.servers]]
id = "alpha"
transport = "stdio"
command = "alpha"

[warnings]
instruction_token_threshold = 50000

[mcp.servers.env]
ALPHA_TOKEN = "${AL_ALPHA}"

[[mcp.servers]]
id = "beta"
transport = "stdio"
command = "beta"
`,
		},
		{
			name: "warnings disabled keeps other settings and intervening server sub-table",
			content: `
[[mcp.servers]]
id = "alpha"
transport = "stdio"
command = "alpha"

[warnings]
instruction_token_threshold = 50000
noise_mode = "quiet"
version_update_on_sync = true

[mcp.servers.env]
ALPHA_TOKEN = "${AL_ALPHA}"

[[mcp.servers]]
id = "beta"
transport = "stdio"
command = "beta"
`,
			choices: func() *Choices {
				choices := NewChoices()
				choices.WarningsEnabledTouched = true
				choices.WarningsEnabled = false
				return choices
			},
			expect: func(decoded map[string]any) {
				delete(decoded["warnings"].(map[string]any), "instruction_token_threshold")
			},
		},
		{
			name: "custom server reordered after catalog default",
			content: `
[[mcp.servers]]
id = "beta"
enabled = true
transport = "http"
url = "https://b.example"
[mcp.servers.headers]
Authorization = "Bearer ${AL_BETA}"

[[mcp.servers]]
id = "tavily"
enabled = true
transport = "http"
url = "https://mcp.tavily.com/mcp/"
[mcp.servers.headers]
Authorization = "Bearer ${AL_TAVILY_API_KEY}"
`,
			wantServerOrder: []string{"tavily", "beta"},
		},
		{
			name: "catalog default disabled",
			content: `
[[mcp.servers]]
id = "tavily"
enabled = true
transport = "http"
url = "https://mcp.tavily.com/mcp/"
[mcp.servers.headers]
Authorization = "Bearer ${AL_TAVILY_API_KEY}"

[[mcp.servers]]
id = "beta"
enabled = true
transport = "http"
url = "https://b.example"
[mcp.servers.headers]
Authorization = "Bearer ${AL_BETA}"
`,
			choices: func() *Choices {
				choices := NewChoices()
				choices.DefaultMCPServers = []DefaultMCPServer{{ID: "tavily"}}
				choices.EnabledMCPServersTouched = true
				choices.EnabledMCPServers = map[string]bool{"tavily": false}
				return choices
			},
			expect: func(decoded map[string]any) {
				servers := decoded["mcp"].(map[string]any)["servers"].([]any)
				servers[0].(map[string]any)["enabled"] = false
			},
		},
		{
			name: "stdio drops incompatible headers sub-table",
			content: `
[[mcp.servers]]
id = "alpha"
transport = "stdio"
command = "alpha"
[mcp.servers.headers]
Authorization = "Bearer ${AL_ALPHA}"
[mcp.servers.env]
ALPHA_TOKEN = "${AL_ALPHA}"
`,
			expect: func(decoded map[string]any) {
				servers := decoded["mcp"].(map[string]any)["servers"].([]any)
				delete(servers[0].(map[string]any), "headers")
			},
		},
		{
			name: "non-MCP array",
			content: `
[[extra.items]]
name = "one"
[extra.items.meta]
owner = "one"

[[extra.items]]
name = "two"
[extra.items.meta]
owner = "two"
`,
		},
		{
			name:    "custom server disabled",
			content: twoHTTPServers,
			choices: func() *Choices {
				choices := NewChoices()
				choices.CustomMCPServersTouched = true
				choices.CustomMCPServersEnabled = map[string]bool{"alpha": false}
				return choices
			},
			expect: func(decoded map[string]any) {
				servers := decoded["mcp"].(map[string]any)["servers"].([]any)
				servers[0].(map[string]any)["enabled"] = false
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			choices := NewChoices()
			if tt.choices != nil {
				choices = tt.choices()
			}
			out, err := PatchConfig(tt.content, choices)
			require.NoError(t, err)

			var want, got map[string]any
			require.NoError(t, toml.Unmarshal([]byte(tt.content), &want))
			require.NoError(t, toml.Unmarshal([]byte(out), &got))
			if tt.expect != nil {
				tt.expect(want)
			}
			if mcp, ok := want["mcp"].(map[string]any); ok {
				serversByID := func(servers any) map[string]any {
					byID := make(map[string]any)
					for _, server := range servers.([]any) {
						id := server.(map[string]any)["id"].(string)
						require.NotContains(t, byID, id)
						byID[id] = server
					}
					return byID
				}
				gotServers := got["mcp"].(map[string]any)["servers"]
				assert.Equal(t, serversByID(mcp["servers"]), serversByID(gotServers))
				if tt.wantServerOrder != nil {
					var ids []string
					for _, server := range gotServers.([]any) {
						ids = append(ids, server.(map[string]any)["id"].(string))
					}
					assert.Equal(t, tt.wantServerOrder, ids)
				}
			}
			if warnings, ok := want["warnings"]; ok {
				assert.Equal(t, warnings, got["warnings"])
			}
			assert.Equal(t, want["extra"], got["extra"])
		})
	}
}

func TestPatchConfig_MCPTableAfterServersKeepsUserKey(t *testing.T) {
	content := `[[mcp.servers]]
id = "alpha"
transport = "http"
url = "https://a.example"
[mcp.servers.headers]
Authorization = "Bearer ${AL_ALPHA}"

[mcp]
user_key = "keep"
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, `user_key = "keep"`))
	var decoded map[string]any
	require.NoError(t, toml.Unmarshal([]byte(out), &decoded))
	mcp := decoded["mcp"].(map[string]any)
	assert.Equal(t, "keep", mcp["user_key"])
	for _, server := range mcp["servers"].([]any) {
		assert.NotContains(t, server.(map[string]any), "user_key")
	}
}

func TestPatchConfig_ArraySubTablesKeepOriginalSpacing(t *testing.T) {
	server := `[[mcp.servers]]
id = "alpha"
transport = "http"
url = "https://a.example"

# auth for alpha
[mcp.servers.headers]
Authorization = "Bearer ${AL_ALPHA}"
X-Team = "alpha"`

	out, err := PatchConfig(server+"\n", NewChoices())
	require.NoError(t, err)
	assert.Contains(t, out, server)
}

func TestPatchConfig_DroppedSubTableKeepsNextServerComment(t *testing.T) {
	content := `[[mcp.servers]]
id = "alpha"
transport = "stdio"
command = "alpha"
[mcp.servers.headers]
X = "leftover"

# beta: internal tools
[[mcp.servers]]
id = "beta"
transport = "stdio"
command = "beta"
`
	out, err := PatchConfig(content, NewChoices())
	require.NoError(t, err)
	assert.NotContains(t, out, "leftover")
	assert.Contains(t, out, "command = \"alpha\"\n\n# beta: internal tools\n")
}

func TestSanitizeMCPServerBlock_SectionStyleSubTables(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantKept []string
		wantGone []string
	}{
		{
			name: "stdio drops headers sub-tables and keeps env",
			content: `
[[mcp.servers]]
id = "local"
transport = "stdio"
command = "tool"
url = "https://leftover.example"
[mcp.servers.headers]
Authorization = "leftover"
[mcp.servers.env]
TOKEN = "keep"
[mcp.servers.headers.extra]
X = "leftover"
`,
			wantKept: []string{"mcp.servers.env"},
			wantGone: []string{"url ="},
		},
		{
			name: "http drops env sub-table and keeps headers",
			content: `
[[mcp.servers]]
id = "remote"
transport = "http"
url = "https://api.example.com"
command = "leftover"
[mcp.servers.env]
KEY = "leftover"
[mcp.servers.headers]
Authorization = "keep"
`,
			wantKept: []string{"mcp.servers.headers"},
			wantGone: []string{"command ="},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := tomlpatch.ParseDocument(tt.content)
			require.Len(t, doc.Arrays[mcpServersSection], 1)
			block := cloneBlock(doc.Arrays[mcpServersSection][0])

			sanitizeMCPServerBlock(block)

			var kept []string
			for _, subTable := range block.SubTables {
				kept = append(kept, subTable.Name)
			}
			assert.Equal(t, tt.wantKept, kept)
			joined := strings.Join(block.Lines, "\n")
			for _, gone := range tt.wantGone {
				assert.NotContains(t, joined, gone)
			}
		})
	}
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

	doc := tomlpatch.ParseDocument(out)
	section := strings.Join(doc.Sections["agents.claude"].Lines, "\n")
	assert.Contains(t, section, "local_config_dir = true")
	assert.NotContains(t, section, "# local_config_dir")
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

	doc := tomlpatch.ParseDocument(out)
	section := strings.Join(doc.Sections["agents.claude"].Lines, "\n")
	assert.Contains(t, section, "# local_config_dir")
	assert.NotContains(t, section, "local_config_dir = true")
}

func TestPatchConfig_CodexLocalConfigDirEnabled(t *testing.T) {
	content := `
[agents.codex]
enabled = true
# local_config_dir = false
`
	choices := NewChoices()
	choices.CodexLocalConfigDirTouched = true
	choices.CodexLocalConfigDir = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	doc := tomlpatch.ParseDocument(out)
	section := strings.Join(doc.Sections["agents.codex"].Lines, "\n")
	assert.Contains(t, section, "local_config_dir = true")
	assert.NotContains(t, section, "# local_config_dir")
}

func TestPatchConfig_CodexLocalConfigDirDisabled(t *testing.T) {
	content := `
[agents.codex]
enabled = true
local_config_dir = true
`
	choices := NewChoices()
	choices.CodexLocalConfigDirTouched = true
	choices.CodexLocalConfigDir = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	doc := tomlpatch.ParseDocument(out)
	section := strings.Join(doc.Sections["agents.codex"].Lines, "\n")
	assert.Contains(t, section, "# local_config_dir")
	assert.NotContains(t, section, "local_config_dir = true")
}

// TestPatchConfig_CodexStatuslineFallsBackToReasoningAnchor guards against a
// reordering regression: when an existing [agents.codex] block has no
// local_config_dir line and only the statusline toggle is touched, statusline
// must stay in place (after reasoning_effort) rather than being inserted at the
// top of the section because the local_config_dir anchor is missing.
func TestPatchConfig_CodexStatuslineFallsBackToReasoningAnchor(t *testing.T) {
	content := `
[agents.codex]
enabled = true
reasoning_effort = "high"
`
	choices := NewChoices()
	choices.CodexStatuslineTouched = true
	choices.CodexStatusline = true
	// CodexLocalConfigDirTouched intentionally left false: no local_config_dir
	// line is inserted, so the statusline anchor must fall back.

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	lines := tomlpatch.ParseDocument(out).Sections["agents.codex"].Lines
	enabledIdx, reasoningIdx, statuslineIdx := -1, -1, -1
	for i, l := range lines {
		switch trimmed := strings.TrimSpace(l); {
		case strings.HasPrefix(trimmed, "enabled"):
			enabledIdx = i
		case strings.HasPrefix(trimmed, "reasoning_effort"):
			reasoningIdx = i
		case strings.HasPrefix(trimmed, "statusline"):
			statuslineIdx = i
		}
	}
	require.NotEqual(t, -1, enabledIdx, "enabled line missing:\n%s", strings.Join(lines, "\n"))
	require.NotEqual(t, -1, reasoningIdx, "reasoning_effort line missing:\n%s", strings.Join(lines, "\n"))
	require.NotEqual(t, -1, statuslineIdx, "statusline line missing:\n%s", strings.Join(lines, "\n"))
	assert.Greater(t, statuslineIdx, enabledIdx, "statusline must not be reordered above enabled")
	assert.Greater(t, statuslineIdx, reasoningIdx, "statusline should follow reasoning_effort")
}

func TestPatchConfig_CodexAppsDisabledOnFreshConfig(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "apps = false")
}

func TestPatchConfig_CodexAppsSectionStaysWithCodexConfig(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	codexIndex := strings.Index(out, "[agents.codex]\n")
	featuresIndex := strings.Index(out, "[agents.codex.agent_specific.features]\n")
	vscodeIndex := strings.Index(out, "[agents.vscode]\n")
	require.NotEqual(t, -1, codexIndex)
	require.NotEqual(t, -1, featuresIndex)
	require.NotEqual(t, -1, vscodeIndex)
	assert.Less(t, codexIndex, featuresIndex)
	assert.Less(t, featuresIndex, vscodeIndex)
}

func TestPatchConfig_CodexAppsAddsToExistingFeatures(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific.features]
multi_agent = true
prevent_idle_sleep = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "apps = false")
	assert.Contains(t, out, "multi_agent = true")
	assert.Contains(t, out, "prevent_idle_sleep = true")
}

func TestPatchConfig_CodexAppsUpdatesExistingValue(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific.features]
apps = true
multi_agent = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "apps = false")
	assert.NotContains(t, out, "apps = true")
	assert.Contains(t, out, "multi_agent = true")
}

func TestPatchConfig_CodexAppsUpdatesExistingDottedValue(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features.apps = true
features.multi_agent = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific]")
	assert.Contains(t, out, "features.apps = false")
	assert.NotContains(t, out, "features.apps = true")
	assert.Contains(t, out, "features.multi_agent = true")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestPatchConfig_CodexAppsUsesDottedValueWhenOtherFeatureDottedKeysExist(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features.multi_agent = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific]")
	assert.Contains(t, out, "features.apps = false")
	assert.Contains(t, out, "features.multi_agent = true")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestPatchConfig_CodexAppsInlineFeaturesErrors(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { apps = true, multi_agent = true }
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	_, err := PatchConfig(content, choices)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inline table syntax")
}

func TestPatchConfig_CodexAppsInlineFeaturesSameValuePreserved(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { apps = false, multi_agent = true }
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "features = { apps = false, multi_agent = true }")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestPatchConfig_CodexAppsInlineFeaturesWithoutAppsDefaultFalsePreserved(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { multi_agent = true }
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "features = { multi_agent = true }")
	assert.NotContains(t, out, "apps = false")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestPatchConfig_CodexAppsCommentedInlineFeaturesIgnored(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
# features = { apps = true, multi_agent = true }
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "# features = { apps = true, multi_agent = true }")
	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "apps = false")
}

func TestPatchConfig_CodexAppsSkippedWhenCodexDisabled(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentCodex] = false
	choices.CodexAppsTouched = true
	choices.CodexApps = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex]")
	assert.Contains(t, out, "enabled = false")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
	assert.NotContains(t, out, "apps = false")
}

func TestPatchConfig_CodexRuntimeTogglesApplyUnderVSCodeOnly(t *testing.T) {
	content := `
[agents.codex]
enabled = false

[agents.vscode]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentVSCode] = true
	choices.CodexAppsTouched = true
	choices.CodexApps = true
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = false
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "apps = true")
	assert.Contains(t, out, "plugins = false")
	assert.Contains(t, out, "browser_use = false")
}

func TestPatchConfig_CodexStatuslineSkippedUnderVSCodeOnly(t *testing.T) {
	content := `
[agents.codex]
enabled = false

[agents.vscode]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentVSCode] = true
	choices.CodexStatuslineTouched = true
	choices.CodexStatusline = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	block, exists := tomlpatch.ParseDocument(out).Sections[codexSection]
	require.True(t, exists)
	assert.False(t, hasUncommentedKeyLine(block.Lines, "statusline"))
}

func TestPatchConfig_CodexAppsUntouchedPreservesExistingValue(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific.features]
apps = true
multi_agent = true
`
	choices := NewChoices()

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "apps = true")
	assert.NotContains(t, out, "apps = false")
	assert.Contains(t, out, "multi_agent = true")
}

func TestPatchConfig_CodexAppsEnabledExplicitlyWrittenTrue(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "apps = true")
}

func TestPatchConfig_CodexPluginsDisabledOnFreshConfig(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "plugins = false")
}

func TestPatchConfig_CodexPluginsUpdatesExistingDottedValue(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features.plugins = true
features.multi_agent = true
`
	choices := NewChoices()
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific]")
	assert.Contains(t, out, "features.plugins = false")
	assert.NotContains(t, out, "features.plugins = true")
	assert.Contains(t, out, "features.multi_agent = true")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestPatchConfig_CodexPluginsInlineFeaturesErrors(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { plugins = true, multi_agent = true }
`
	choices := NewChoices()
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = false

	_, err := PatchConfig(content, choices)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inline table syntax")
}

// Plugins default to enabled, so an inline features table that omits plugins
// still leaves them on; a requested disable therefore requires editing the
// inline table and must surface the unsupported error rather than be silently
// dropped.
func TestPatchConfig_CodexPluginsInlineFeaturesWithoutPluginsDisableErrors(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { multi_agent = true }
`
	choices := NewChoices()
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = false

	_, err := PatchConfig(content, choices)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inline table syntax")
}

// Keeping plugins enabled already matches the native default, so an inline
// features table that omits plugins needs no change and must be preserved
// without a spurious unsupported error.
func TestPatchConfig_CodexPluginsInlineFeaturesWithoutPluginsKeepEnabledPreserved(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { multi_agent = true }
`
	choices := NewChoices()
	choices.CodexPluginsTouched = true
	choices.CodexPlugins = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "features = { multi_agent = true }")
	assert.NotContains(t, out, "features.plugins = true")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestReadCodexAppsEnabled(t *testing.T) {
	t.Run("missing agent_specific returns false", func(t *testing.T) {
		assert.False(t, readCodexAppsEnabled(nil))
	})
	t.Run("missing features returns false", func(t *testing.T) {
		assert.False(t, readCodexAppsEnabled(map[string]any{"other": true}))
	})
	t.Run("features wrong type returns false", func(t *testing.T) {
		assert.False(t, readCodexAppsEnabled(map[string]any{"features": "not-a-map"}))
	})
	t.Run("apps wrong type returns false", func(t *testing.T) {
		agentSpecific := map[string]any{"features": map[string]any{"apps": "yes"}}
		assert.False(t, readCodexAppsEnabled(agentSpecific))
	})
	t.Run("apps true returns true", func(t *testing.T) {
		agentSpecific := map[string]any{"features": map[string]any{"apps": true}}
		assert.True(t, readCodexAppsEnabled(agentSpecific))
	})
	t.Run("apps false returns false", func(t *testing.T) {
		agentSpecific := map[string]any{"features": map[string]any{"apps": false}}
		assert.False(t, readCodexAppsEnabled(agentSpecific))
	})
}

func TestReadCodexFeatureReaders(t *testing.T) {
	agentSpecific := map[string]any{"features": map[string]any{"plugins": true}}

	assert.True(t, readCodexPluginsEnabled(agentSpecific))
	assert.False(t, readCodexAppsEnabled(agentSpecific))
}

func TestReadCodexPluginsEnabledDefaultsToNativeEnabled(t *testing.T) {
	assert.True(t, readCodexPluginsEnabled(nil))
	assert.True(t, readCodexPluginsEnabled(map[string]any{"features": map[string]any{"plugins": true}}))
	assert.False(t, readCodexPluginsEnabled(map[string]any{"features": map[string]any{"plugins": false}}))
}

func TestPatchConfig_CodexBrowserDisabledOnFreshConfig(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "browser_use = false")
	assert.Contains(t, out, "in_app_browser = false")
	assert.Contains(t, out, "computer_use = false")
}

func TestPatchConfig_CodexBrowserDisabledAlongsideApps(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific.features]
apps = false
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "apps = false")
	assert.Contains(t, out, "browser_use = false")
	assert.Contains(t, out, "in_app_browser = false")
	assert.Contains(t, out, "computer_use = false")
}

func TestPatchConfig_CodexBrowserAndAppsBothDisabledFreshConfig(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexAppsTouched = true
	choices.CodexApps = false
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.codex.agent_specific.features]")
	assert.Contains(t, out, "apps = false")
	assert.Contains(t, out, "browser_use = false")
}

func TestPatchConfig_CodexBrowserInlineFeaturesErrors(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { multi_agent = true }
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	_, err := PatchConfig(content, choices)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inline table syntax")
}

func TestPatchConfig_CodexBrowserInlineFeaturesWithBrowserKeyOffErrors(t *testing.T) {
	// Toggle off (not disabling) but the inline table already pins a browser key:
	// clearing it would require editing the inline table, so surface the limitation
	// instead of silently leaving the pin in place.
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { browser_use = false, multi_agent = true }
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = false

	_, err := PatchConfig(content, choices)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inline table syntax")
}

func TestPatchConfig_CodexBrowserInlineFeaturesWithoutBrowserKeyOffPreserved(t *testing.T) {
	// Toggle off and the inline table pins no browser key: nothing to change, so
	// leave the inline features untouched without erroring.
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features = { multi_agent = true }
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, "features = { multi_agent = true }")
	assert.NotContains(t, out, "browser_use")
}

func TestPatchConfig_CodexBrowserNotDisabledAddsNoFeaturesSection(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = false

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
	assert.NotContains(t, out, "browser_use")
}

func TestPatchConfig_CodexBrowserDisabledSkippedWhenCodexDisabled(t *testing.T) {
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentCodex] = false
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "browser_use")
}

func TestPatchConfig_ClaudeDisableIDEReading(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableIDEReading = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `agent_specific.env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false"`)
}

func TestPatchConfig_ClaudeDisableConnectors(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableConnectorsTouched = true
	choices.ClaudeDisableConnectors = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, `agent_specific.env.ENABLE_CLAUDEAI_MCP_SERVERS = "false"`)
}

func TestPatchConfig_ClaudeDisableMemory(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "agent_specific.autoMemoryEnabled = false")
}

func TestPatchConfig_GrokDisableMemory(t *testing.T) {
	content := `
[agents.grok]
enabled = true
model = "grok-4.6"
`
	choices := NewChoices()
	choices.GrokDisableMemoryTouched = true
	choices.GrokDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, "disable_memory = true")
	assert.Less(t, strings.Index(out, `model = "grok-4.6"`), strings.Index(out, "disable_memory = true"))

	choices.GrokDisableMemory = false
	out, err = PatchConfig(out, choices)
	require.NoError(t, err)
	assert.NotContains(t, out, "\ndisable_memory = true")
	assert.Contains(t, out, "# disable_memory = true")
}

func TestPatchConfig_ClaudeDisableQuestionToolWritesTypedFlag(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableQuestionToolTouched = true
	choices.ClaudeDisableQuestionTool = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	// The toggle writes a typed [agents.claude] scalar; sync injects the deny +
	// PreToolUse hook so the wizard never edits agent_specific arrays.
	assert.Contains(t, out, "disable_question_tool = true")
	assert.NotContains(t, out, "agent_specific.permissions.deny")
	assert.NotContains(t, out, "agent_specific.hooks.PreToolUse")
}

func TestPatchConfig_ClaudeQuestionToolPreservesUserDenyAndHooks(t *testing.T) {
	// Enabling or disabling the toggle must never touch a user's co-listed
	// permissions.deny / hooks.PreToolUse entries — that was the clobber bug.
	content := `
[agents.claude]
enabled = true
disable_question_tool = true
agent_specific.permissions.deny = ["Bash(rm:*)"]
agent_specific.hooks.PreToolUse = [{ matcher = "Write" }]
`
	for _, disable := range []bool{true, false} {
		choices := NewChoices()
		choices.ClaudeDisableQuestionToolTouched = true
		choices.ClaudeDisableQuestionTool = disable

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)

		// User arrays survive verbatim and uncommented regardless of toggle state.
		assert.Contains(t, out, `agent_specific.permissions.deny = ["Bash(rm:*)"]`)
		assert.Contains(t, out, `agent_specific.hooks.PreToolUse = [{ matcher = "Write" }]`)
		for _, line := range strings.Split(out, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# agent_specific.permissions.deny") ||
				strings.HasPrefix(trimmed, "# agent_specific.hooks.PreToolUse") {
				t.Fatalf("user agent_specific entry was clobbered (commented): %q", line)
			}
		}
		if disable {
			assert.Contains(t, out, "disable_question_tool = true")
		} else {
			assert.Contains(t, out, "# disable_question_tool")
		}
	}
}

func TestPatchConfig_ClaudeDisableIDEReadingWritesIntoExpandedEnvSection(t *testing.T) {
	content := `
[agents.claude]
enabled = true

[agents.claude.agent_specific.env]
SOME_OTHER = "value"
`
	choices := NewChoices()
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableIDEReading = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.claude.agent_specific.env]")
	assert.Contains(t, out, `SOME_OTHER = "value"`)
	assert.Contains(t, out, `CLAUDE_CODE_AUTO_CONNECT_IDE = "false"`)
	// The key must land in the expanded section as a bare leaf, not as a dotted
	// key in [agents.claude] (which would redefine the table — a TOML error).
	assert.NotContains(t, out, "agent_specific.env.CLAUDE_CODE_AUTO_CONNECT_IDE")
}

func TestPatchConfig_ClaudeDisableWritesIntoExpandedParentSection(t *testing.T) {
	content := `
[agents.claude]
enabled = true

[agents.claude.agent_specific]
autoMemoryEnabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.claude.agent_specific]")
	assert.Contains(t, out, "autoMemoryEnabled = false")
	assert.NotContains(t, out, "autoMemoryEnabled = true")
}

func TestPatchConfig_ClaudeDisableTogglesIdempotent(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableIDEReading = true
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true
	choices.ClaudeDisableQuestionToolTouched = true
	choices.ClaudeDisableQuestionTool = true

	first, err := PatchConfig(content, choices)
	require.NoError(t, err)
	second, err := PatchConfig(first, choices)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestPatchConfig_ClaudeDisableTogglesSkippedWhenClaudeDisabled(t *testing.T) {
	content := `
[agents.claude]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentClaude] = false
	choices.EnabledAgents[AgentClaudeVSCode] = false
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.NotContains(t, out, "autoMemoryEnabled")
}

func TestPatchConfig_ClaudeDisableToggleAppliesUnderClaudeVSCodeOnly(t *testing.T) {
	content := `
[agents.claude]
enabled = false

[agents.claude_vscode]
enabled = true
`
	choices := NewChoices()
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentClaudeVSCode] = true
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "agent_specific.autoMemoryEnabled = false")
}

func TestPatchConfig_ClaudeDisableCreatesClaudeSectionWhenAbsent(t *testing.T) {
	// No [agents.claude] block at all — the writer's defensive create path.
	content := `
[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.claude]")
	assert.Contains(t, out, "agent_specific.autoMemoryEnabled = false")
}

func TestPatchConfig_ClaudeDisableEnvWritesDottedIntoParentSection(t *testing.T) {
	// [agents.claude.agent_specific] exists but no expanded .env sub-table: the
	// env key is written as a dotted `env.*` key into the parent section.
	content := `
[agents.claude]
enabled = true

[agents.claude.agent_specific]
foo = "bar"
`
	choices := NewChoices()
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableIDEReading = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "[agents.claude.agent_specific]")
	assert.Contains(t, out, `env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false"`)
	assert.NotContains(t, out, "agent_specific.env.CLAUDE_CODE_AUTO_CONNECT_IDE")
}

func TestPatchConfig_CodexBrowserWritesDottedFeaturesIntoParentSection(t *testing.T) {
	content := `
[agents.codex]
enabled = true

[agents.codex.agent_specific]
features.multi_agent = true
`
	choices := NewChoices()
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)

	assert.Contains(t, out, "features.browser_use = false")
	assert.Contains(t, out, "features.multi_agent = true")
	assert.NotContains(t, out, "[agents.codex.agent_specific.features]")
}

func TestCloneBlock_Nil(t *testing.T) {
	assert.Nil(t, cloneBlock(nil))
}

// Assembly must leave source documents intact when mutating selected sections,
// catalog defaults, custom servers, and the nested tables of those servers.
func TestAssembleCanonicalConfig_DoesNotMutateSourceDocuments(t *testing.T) {
	currentContent := `[approvals]
mode = "mcp"
[warnings]
instruction_token_threshold = 1
noise_mode = "keep"
[mcp]
[[mcp.servers]]
id = "default"
enabled = true
transport = "stdio"
headers = { old = "remove" }
[mcp.servers.env]
KEEP = "value"
[mcp.servers.headers]
Old = "remove"
# keep default comment
[[mcp.servers]]
id = "custom"
enabled = true
transport = "stdio"
command = "run"
[mcp.servers.headers]
Old = "remove"
# keep custom comment
`
	templateContent := `[approvals]
mode = "all"
[agents.claude]
enabled = false
model = "template"
[warnings]
instruction_token_threshold = 10
[mcp]
`
	catalogContent := `[[mcp.servers]]
id = "default"
enabled = true
[[mcp.servers]]
id = "missing"
enabled = false
transport = "stdio"
headers = { old = "remove" }
[mcp.servers.env]
KEEP = "value"
[mcp.servers.headers]
Old = "remove"
# keep catalog comment
`
	current := tomlpatch.ParseDocument(currentContent)
	template := tomlpatch.ParseDocument(templateContent)
	catalog := tomlpatch.ParseDocument(catalogContent)
	choices := NewChoices()
	choices.ApprovalModeTouched, choices.ApprovalMode = true, "all"
	choices.EnabledAgentsTouched = true
	choices.EnabledAgents[AgentClaude] = true
	choices.AgentModels[AgentClaude] = AgentModelChoice{ModelTouched: true, Model: "updated"}
	choices.WarningsEnabledTouched, choices.WarningsEnabled = true, false
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "default"}, {ID: "missing"}}
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{"default": false, "missing": true}
	choices.CustomMCPServersTouched = true
	choices.CustomMCPServersEnabled = map[string]bool{"custom": false}

	output, err := assembleCanonicalConfig(current, template, catalog, choices)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(output, "\n"), `model = "updated"`)
	assert.Equal(t, tomlpatch.ParseDocument(currentContent), current)
	assert.Equal(t, tomlpatch.ParseDocument(templateContent), template)
	assert.Equal(t, tomlpatch.ParseDocument(catalogContent), catalog)
}
