// Package wizard implements the interactive setup wizard for Agent Layer.
//
// # TOML Parsing Strategy
//
// This package uses the line-based TOML parser and patcher in internal/tomlpatch
// instead of the go-toml library's tree manipulation for config updates. This is
// intentional for several reasons:
//
//  1. Comment preservation: go-toml's ToTomlString() loses inline comments and
//     rearranges leading comments. Users expect their config formatting to be preserved.
//
//  2. Deterministic output: The wizard rewrites config.toml in preferred section order
//     (Decision wizard-order-policy). Custom parsing lets us control exact output ordering.
//
//  3. Key positioning: When clearing optional keys (like model=""), we convert them
//     to commented lines rather than deleting them, preserving the template structure.
//
// The go-toml/v2 library is still used for syntax validation before processing.
package wizard

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

var preferredWizardSectionOrder = []string{
	approvalsSection,
	antigravitySection,
	claudeSection,
	claudeVSCodeSection,
	codexSection,
	"agents.vscode",
	"agents.copilot_cli",
	"agents.grok",
	museSection,
	mcpSection,
	warningsSection,
}

var legacySectionAliases = map[string]string{
	"agents.claude-vscode": claudeVSCodeSection,
}

// PatchConfig applies wizard choices to TOML config content.
// content is the current config; choices holds selections; returns updated content or error.
func PatchConfig(content string, choices *Choices) (string, error) {
	var parseCheck map[string]any
	if err := toml.Unmarshal([]byte(content), &parseCheck); err != nil {
		return "", fmt.Errorf(messages.WizardParseConfigFailedFmt, err)
	}

	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		return "", fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	templateContent := string(templateBytes)

	catalogDoc, err := loadCatalogDocument()
	if err != nil {
		return "", err
	}

	templateDoc := tomlpatch.ParseDocument(templateContent)
	currentDoc := tomlpatch.ParseDocument(content)
	normalizeLegacySectionAliases(&currentDoc)
	if err := applyCodexAppsUpdate(&currentDoc, choices); err != nil {
		return "", err
	}
	if err := applyCodexPluginsUpdate(&currentDoc, choices); err != nil {
		return "", err
	}
	if err := applyCodexBrowserUpdate(&currentDoc, choices); err != nil {
		return "", err
	}
	applyClaudeAgentSpecificUpdate(&currentDoc, choices)

	if choices.EnabledMCPServersTouched && len(choices.DefaultMCPServers) == 0 {
		return "", fmt.Errorf(messages.WizardDefaultMCPServersRequired)
	}

	output, err := assembleCanonicalConfig(currentDoc, templateDoc, catalogDoc, choices)
	if err != nil {
		return "", err
	}

	rendered := strings.Join(output, "\n")
	var renderCheck map[string]any
	if err := toml.Unmarshal([]byte(rendered), &renderCheck); err != nil {
		return "", fmt.Errorf(messages.WizardRenderConfigFailedFmt, err)
	}

	return rendered, nil
}

// assembleCanonicalConfig renders updated config content in template order.
// currentDoc holds the existing config; templateDoc provides the canonical ordering and section formatting;
// catalogDoc provides default-shaped [[mcp.servers]] blocks; choices supplies wizard selections.
// Returns the ordered lines or an error when required template blocks are missing.
func assembleCanonicalConfig(currentDoc tomlpatch.Document, templateDoc tomlpatch.Document, catalogDoc tomlpatch.Document, choices *Choices) ([]string, error) {
	preamble := choosePreamble(currentDoc.Preamble, templateDoc.Preamble)
	output := make([]string, 0, len(preamble))
	output = append(output, preamble...)

	removeWarnings := choices.WarningsEnabledTouched && !choices.WarningsEnabled

	for _, name := range orderedWizardSections(templateDoc.Order) {
		if name == warningsSection && removeWarnings {
			if block := disabledWarningsBlock(currentDoc.Sections[name]); block != nil {
				tomlpatch.AppendBlock(&output, block.Lines)
			}
			continue
		}
		block := selectSectionBlock(currentDoc.Sections[name], templateDoc.Sections[name])
		if block == nil {
			continue
		}
		updated := cloneBlock(block)
		applySectionUpdates(name, updated, templateDoc.Sections[name], choices)
		tomlpatch.AppendBlock(&output, updated.Lines)

		if name == codexSection {
			for _, block := range extraSectionBlocks(currentDoc.Sections, templateDoc.Sections, true) {
				tomlpatch.AppendBlock(&output, block.Lines)
			}
		}

		if name == mcpSection {
			serverBlocks, err := buildMCPServerBlocks(currentDoc, catalogDoc, choices)
			if err != nil {
				return nil, err
			}
			for _, serverBlock := range serverBlocks {
				tomlpatch.AppendBlock(&output, renderedBlockLines(&serverBlock))
			}
		}
	}

	extraSections := extraSectionBlocks(currentDoc.Sections, templateDoc.Sections, false)
	for _, block := range extraSections {
		tomlpatch.AppendBlock(&output, block.Lines)
	}

	// Preserve non-mcp.servers array-of-table blocks.
	extraArrays := extraArrayBlocks(currentDoc.Arrays)
	for _, block := range extraArrays {
		tomlpatch.AppendBlock(&output, renderedBlockLines(block))
	}

	return tomlpatch.TrimTrailingEmptyLines(output), nil
}

// warningThresholdKeys are the [warnings] keys controlled by the wizard's warnings prompt.
var warningThresholdKeys = []string{
	"instruction_token_threshold",
	"mcp_server_threshold",
	"mcp_tools_total_threshold",
	"mcp_server_tools_threshold",
	"mcp_schema_tokens_total_threshold",
	"mcp_schema_tokens_server_threshold",
}

// disabledWarningsBlock returns current with the warning thresholds removed, or nil when current is
// absent or keeps no other keys. Declining warnings must not drop unrelated settings such as
// noise_mode and version_update_on_sync, nor seed them from the template.
func disabledWarningsBlock(current *tomlpatch.Block) *tomlpatch.Block {
	if current == nil {
		return nil
	}
	updated := cloneBlock(current)
	for _, key := range warningThresholdKeys {
		tomlpatch.RemoveKeyFromBlock(updated, key)
	}
	if !hasUncommentedKeyWithPrefix(updated.Lines, "") {
		return nil
	}
	return updated
}

// choosePreamble returns the preamble lines to keep before the first table.
// current is the existing preamble; template is the default preamble; returns the preferred set.
func choosePreamble(current []string, template []string) []string {
	for _, line := range current {
		if strings.TrimSpace(line) != "" {
			return current
		}
	}
	return template
}

func orderedWizardSections(templateOrder []string) []string {
	seen := make(map[string]struct{}, len(templateOrder))
	ordered := make([]string, 0, len(templateOrder))

	for _, name := range preferredWizardSectionOrder {
		if slices.Contains(templateOrder, name) {
			ordered = append(ordered, name)
			seen[name] = struct{}{}
		}
	}
	for _, name := range templateOrder {
		if _, exists := seen[name]; exists {
			continue
		}
		ordered = append(ordered, name)
		seen[name] = struct{}{}
	}
	return ordered
}

// selectSectionBlock picks the current block when present, otherwise the template block.
func selectSectionBlock(current *tomlpatch.Block, template *tomlpatch.Block) *tomlpatch.Block {
	if current != nil {
		return current
	}
	return template
}

// applyAgentModelUpdates writes the touched model and reasoning_effort keys for
// one agent section. reasoning_effort is anchored after model, or after enabled
// when the block has no model line.
func applyAgentModelUpdates(block, templateBlock *tomlpatch.Block, choice AgentModelChoice) {
	if choice.ModelTouched {
		setOptionalKeyValue(block, templateBlock, modelKey, choice.Model, enabledKey)
	}
	if choice.ReasoningTouched {
		anchor := modelKey
		if _, ok := tomlpatch.FindKeyLine(block.Lines, anchor); !ok {
			anchor = enabledKey
		}
		setOptionalKeyValue(block, templateBlock, "reasoning_effort", choice.Reasoning, anchor)
	}
}

// applySectionUpdates mutates the block in place based on wizard choices.
// name identifies the section; templateBlock provides canonical formatting for inserted keys.
func applySectionUpdates(name string, block *tomlpatch.Block, templateBlock *tomlpatch.Block, choices *Choices) {
	switch name {
	case approvalsSection:
		if choices.ApprovalModeTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "mode", tomlpatch.FormatValue(choices.ApprovalMode), "")
		}
	case antigravitySection:
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentAntigravity]), "")
		}
		if !choices.EnabledAgentsTouched || choices.EnabledAgents[AgentAntigravity] {
			applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentAntigravity])
		}
	case claudeSection:
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentClaude]), "")
		}
		applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentClaude])
		if choices.ClaudeLocalConfigDirTouched {
			if choices.ClaudeLocalConfigDir {
				tomlpatch.SetKeyValue(block, templateBlock, "local_config_dir", tomlpatch.FormatValue(true), "model")
			} else {
				tomlpatch.SetCommentedKeyLine(block, templateBlock, "local_config_dir", "model")
			}
		}
		if choices.ClaudeDisableQuestionToolTouched {
			if choices.ClaudeDisableQuestionTool {
				tomlpatch.SetKeyValue(block, templateBlock, "disable_question_tool", tomlpatch.FormatValue(true), "local_config_dir")
			} else {
				tomlpatch.SetCommentedKeyLine(block, templateBlock, "disable_question_tool", "local_config_dir")
			}
		}
		if choices.ClaudeStatuslineTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "statusline", tomlpatch.FormatValue(choices.ClaudeStatusline), "disable_question_tool")
		}
	case claudeVSCodeSection:
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentClaudeVSCode]), "")
		}
	case codexSection:
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentCodex]), "")
		}
		applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentCodex])
		if choices.CodexLocalConfigDirTouched {
			if choices.CodexLocalConfigDir {
				tomlpatch.SetKeyValue(block, templateBlock, "local_config_dir", tomlpatch.FormatValue(true), "reasoning_effort")
			} else {
				tomlpatch.SetCommentedKeyLine(block, templateBlock, "local_config_dir", "reasoning_effort")
			}
		}
		if choices.CodexStatuslineTouched && codexStatuslineToggleVisible(choices) {
			// Anchor statusline after local_config_dir when that line exists,
			// otherwise fall back to reasoning_effort (the pre-local_config_dir
			// anchor). This keeps statusline in place instead of reordering it to
			// the top of a block that has no local_config_dir line.
			statuslineAnchor := "local_config_dir"
			if _, ok := tomlpatch.FindKeyLine(block.Lines, "local_config_dir"); !ok {
				statuslineAnchor = "reasoning_effort"
			}
			tomlpatch.SetKeyValue(block, templateBlock, "statusline", tomlpatch.FormatValue(choices.CodexStatusline), statuslineAnchor)
		}
	case "agents.vscode":
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentVSCode]), "")
		}
	case "agents.copilot_cli":
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentCopilotCLI]), "")
		}
		applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentCopilotCLI])
	case "agents.grok":
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, "enabled", tomlpatch.FormatValue(choices.EnabledAgents[AgentGrok]), "")
		}
		applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentGrok])
		if choices.GrokDisableMemoryTouched {
			anchor := "reasoning_effort"
			if _, ok := tomlpatch.FindKeyLine(block.Lines, anchor); !ok {
				anchor = "model"
				if _, ok := tomlpatch.FindKeyLine(block.Lines, anchor); !ok {
					anchor = "enabled"
				}
			}
			if choices.GrokDisableMemory {
				tomlpatch.SetKeyValue(block, templateBlock, "disable_memory", tomlpatch.FormatValue(true), anchor)
			} else {
				tomlpatch.SetCommentedKeyLine(block, templateBlock, "disable_memory", anchor)
			}
		}
	case museSection:
		if choices.EnabledAgentsTouched {
			tomlpatch.SetKeyValue(block, templateBlock, enabledKey, tomlpatch.FormatValue(choices.EnabledAgents[AgentMuse]), "")
		}
		applyAgentModelUpdates(block, templateBlock, choices.AgentModels[AgentMuse])
	case warningsSection:
		if choices.WarningsEnabledTouched && choices.WarningsEnabled {
			tomlpatch.SetKeyValue(block, templateBlock, "instruction_token_threshold", tomlpatch.FormatValue(choices.InstructionTokenThreshold), "")
			tomlpatch.SetKeyValue(block, templateBlock, "mcp_server_threshold", tomlpatch.FormatValue(choices.MCPServerThreshold), "instruction_token_threshold")
			tomlpatch.SetKeyValue(block, templateBlock, "mcp_tools_total_threshold", tomlpatch.FormatValue(choices.MCPToolsTotalThreshold), "mcp_server_threshold")
			tomlpatch.SetKeyValue(block, templateBlock, "mcp_server_tools_threshold", tomlpatch.FormatValue(choices.MCPServerToolsThreshold), "mcp_tools_total_threshold")
			tomlpatch.SetKeyValue(block, templateBlock, "mcp_schema_tokens_total_threshold", tomlpatch.FormatValue(choices.MCPSchemaTokensTotalThreshold), "mcp_server_tools_threshold")
			tomlpatch.SetKeyValue(block, templateBlock, "mcp_schema_tokens_server_threshold", tomlpatch.FormatValue(choices.MCPSchemaTokensServerThreshold), "mcp_schema_tokens_total_threshold")
		}
	}
}

type mcpBlock struct {
	id        string
	lines     []string
	subTables []*tomlpatch.Block
}

// stdioIncompatibleKeys are TOML keys that are not valid for stdio transport MCP servers.
var stdioIncompatibleKeys = []string{"headers", "url", "http_transport"}

// httpIncompatibleKeys are TOML keys that are not valid for http transport MCP servers.
var httpIncompatibleKeys = []string{"command", "args", envKey}

// buildMCPServerBlocks returns ordered MCP server blocks using catalog order for defaults.
// currentDoc supplies existing blocks; catalogDoc provides default-shaped catalog blocks;
// choices controls enabled toggles and which missing defaults to insert.
//
// Disable-in-place: when choices.EnabledMCPServersTouched is true and choices.EnabledMCPServers[id]
// is false for a default-catalog id that already exists in config, the block is kept with
// enabled = false rather than deleted. A default that is absent from config is inserted from the
// catalog only when the user selected (enabled) it; an unselected missing default stays absent.
// User-defined non-catalog blocks follow the same never-delete rule (see the trailing loop).
func buildMCPServerBlocks(currentDoc tomlpatch.Document, catalogDoc tomlpatch.Document, choices *Choices) ([]tomlpatch.Block, error) {
	currentBlocks := parseMCPBlocks(currentDoc.Arrays[mcpServersSection])
	catalogBlocks := parseMCPBlocks(catalogDoc.Arrays[mcpServersSection])

	currentByID := make(map[string]mcpBlock, len(currentBlocks))
	for _, block := range currentBlocks {
		if block.id != "" {
			currentByID[block.id] = block
		}
	}

	catalogByID := make(map[string]mcpBlock, len(catalogBlocks))
	for _, block := range catalogBlocks {
		if block.id != "" {
			catalogByID[block.id] = block
		}
	}

	defaultIDs := defaultServerIDs(choices, catalogBlocks)
	defaultSet := make(map[string]struct{}, len(defaultIDs))
	for _, id := range defaultIDs {
		defaultSet[id] = struct{}{}
	}

	var ordered []tomlpatch.Block
	for _, id := range defaultIDs {
		if choices.EnabledMCPServersTouched {
			block, ok := currentByID[id]
			if !ok {
				// Missing default: insert from the catalog only when the user
				// selected (enabled) it. An unselected missing default stays
				// absent — the wizard never adds an entry the user did not ask for.
				if !choices.EnabledMCPServers[id] {
					continue
				}
				tpl, exists := catalogByID[id]
				if !exists {
					return nil, fmt.Errorf(messages.WizardMissingDefaultMCPServerTemplateFmt, id)
				}
				block = tpl
			}
			// Existing default: keep the block and set enabled to the user's choice.
			// Disabling sets enabled = false rather than deleting the entry.
			tb := updateMCPEnabled(block, catalogByID[id], choices, id)
			sanitizeMCPServerBlock(&tb)
			ordered = append(ordered, tb)
			continue
		}
		// MCP step not touched: preserve existing state unchanged.
		if block, ok := currentByID[id]; ok {
			tb := updateMCPEnabled(block, catalogByID[id], choices, id)
			sanitizeMCPServerBlock(&tb)
			ordered = append(ordered, tb)
		}
	}

	for _, block := range currentBlocks {
		if block.id != "" {
			if _, isDefault := defaultSet[block.id]; isDefault {
				continue
			}
		}
		tb := tomlpatch.Block{Name: mcpServersSection, Lines: tomlpatch.CloneLines(block.lines), SubTables: cloneBlocks(block.subTables)}
		// Honor the custom-server keep/disable decision. Unlike catalog defaults,
		// a custom server has no template to restore from, so disabling sets
		// enabled = false rather than pruning the block. Untouched configs pass
		// through unchanged, preserving the original enabled state. A block with no
		// recorded decision (absent from the map) is also left as-is rather than
		// forced to false, so we never impose a decision the user did not make.
		if choices.CustomMCPServersTouched && block.id != "" {
			if enabled, ok := choices.CustomMCPServersEnabled[block.id]; ok {
				tomlpatch.SetKeyValue(&tb, nil, "enabled", tomlpatch.FormatValue(enabled), "id")
			}
		}
		sanitizeMCPServerBlock(&tb)
		ordered = append(ordered, tb)
	}

	return ordered, nil
}

// sanitizeMCPServerBlock removes transport-incompatible fields from a server block,
// including section-style sub-tables such as [mcp.servers.headers].
// This allows the wizard to repair configs where, for example, a stdio server
// has leftover headers from a previous configuration.
func sanitizeMCPServerBlock(block *tomlpatch.Block) {
	var incompatibleKeys []string
	switch tomlpatch.ExtractBlockKeyValue(block.Lines, "transport") {
	case "stdio":
		incompatibleKeys = stdioIncompatibleKeys
	case "http":
		incompatibleKeys = httpIncompatibleKeys
	}
	for _, key := range incompatibleKeys {
		tomlpatch.RemoveKeyFromBlock(block, key)
	}
	kept := make([]*tomlpatch.Block, 0, len(block.SubTables))
	for _, subTable := range block.SubTables {
		path, ok := tomlpatch.ParseKeyPath(subTable.Name)
		if !ok || len(path) <= 2 || path[0] != mcpSection || path[1] != "servers" || !slices.Contains(incompatibleKeys, path[2]) {
			kept = append(kept, subTable)
			continue
		}
		// The parser assigns comments and blank lines preceding the next header
		// to this sub-table; keep them so dropping it does not drop the next
		// block's leading comment.
		trailing := trailingCommentLines(subTable.Lines)
		if len(kept) > 0 {
			kept[len(kept)-1].Lines = append(kept[len(kept)-1].Lines, trailing...)
		} else {
			block.Lines = append(block.Lines, trailing...)
		}
	}
	block.SubTables = kept
}

// trailingCommentLines returns the comment and blank lines after the last
// content line of a block.
func trailingCommentLines(lines []string) []string {
	end := len(lines)
	for end > 0 {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		end--
	}
	return lines[end:]
}

// updateMCPEnabled applies the enabled toggle to a server block when requested.
// block holds the current server text; templateBlock provides canonical formatting; id identifies the server.
func updateMCPEnabled(block mcpBlock, templateBlock mcpBlock, choices *Choices, id string) tomlpatch.Block {
	updated := tomlpatch.Block{Name: mcpServersSection, Lines: tomlpatch.CloneLines(block.lines), SubTables: cloneBlocks(block.subTables)}
	if choices.EnabledMCPServersTouched {
		tpl := (*tomlpatch.Block)(nil)
		if len(templateBlock.lines) > 0 {
			tpl = &tomlpatch.Block{Name: mcpServersSection, Lines: tomlpatch.CloneLines(templateBlock.lines)}
		}
		tomlpatch.SetKeyValue(&updated, tpl, "enabled", tomlpatch.FormatValue(choices.EnabledMCPServers[id]), "id")
	}
	return updated
}

// defaultServerIDs returns default MCP server IDs in template order.
// choices provides explicit defaults; templateBlocks are used as a fallback.
func defaultServerIDs(choices *Choices, templateBlocks []mcpBlock) []string {
	if len(choices.DefaultMCPServers) > 0 {
		ids := make([]string, 0, len(choices.DefaultMCPServers))
		for _, server := range choices.DefaultMCPServers {
			if server.ID == "" {
				continue
			}
			ids = append(ids, server.ID)
		}
		return ids
	}
	ids := make([]string, 0, len(templateBlocks))
	for _, block := range templateBlocks {
		if block.id != "" {
			ids = append(ids, block.id)
		}
	}
	return ids
}

// parseMCPBlocks extracts MCP server IDs and block lines from parsed array blocks.
func parseMCPBlocks(blocks []*tomlpatch.Block) []mcpBlock {
	result := make([]mcpBlock, 0, len(blocks))
	for _, block := range blocks {
		id := tomlpatch.ExtractBlockKeyValue(block.Lines, "id")
		result = append(result, mcpBlock{id: id, lines: tomlpatch.CloneLines(block.Lines), subTables: cloneBlocks(block.SubTables)})
	}
	return result
}

// setOptionalKeyValue updates or comments out an optional key based on the provided value.
// block is updated in place; templateBlock provides a canonical commented line when clearing; afterKey controls insertion order.
func setOptionalKeyValue(block *tomlpatch.Block, templateBlock *tomlpatch.Block, key string, value string, afterKey string) {
	if value == "" {
		tomlpatch.SetCommentedKeyLine(block, templateBlock, key, afterKey)
		return
	}
	tomlpatch.SetKeyValue(block, templateBlock, key, tomlpatch.FormatValue(value), afterKey)
}

// codexFeaturesSection is the dotted TOML path for the Codex
// agent_specific.features table where Codex feature toggles live.
const codexFeaturesSection = "agents.codex.agent_specific.features"

const codexAgentSpecificSectionPrefix = "agents.codex.agent_specific"

// applyCodexAppsUpdate writes choices.CodexApps into the
// [agents.codex.agent_specific.features] section of doc when CodexAppsTouched.
// Creates the section when missing so the extra-section preservation flow in
// assembleCanonicalConfig renders it. Mutates doc in place.
// Codex's native defaults when a features key is absent: apps is off (Agent
// Layer also always writes it explicitly), plugins is on. These drive whether a
// requested state is already satisfied by an inline features table that omits
// the key.
const (
	codexFeatureDefaultApps    = false
	codexFeatureDefaultPlugins = true
)

func applyCodexAppsUpdate(doc *tomlpatch.Document, choices *Choices) error {
	if !choices.CodexAppsTouched {
		return nil
	}
	return applyCodexBooleanFeatureUpdate(doc, choices, config.CodexFeatureAppsKey, choices.CodexApps, codexFeatureDefaultApps)
}

// applyCodexPluginsUpdate writes choices.CodexPlugins into the
// [agents.codex.agent_specific.features] section of doc when CodexPluginsTouched.
func applyCodexPluginsUpdate(doc *tomlpatch.Document, choices *Choices) error {
	if !choices.CodexPluginsTouched {
		return nil
	}
	return applyCodexBooleanFeatureUpdate(doc, choices, config.PluginsKey, choices.CodexPlugins, codexFeatureDefaultPlugins)
}

func applyCodexBooleanFeatureUpdate(doc *tomlpatch.Document, choices *Choices, key string, enabled, defaultEnabled bool) error {
	if !codexRuntimeToggleVisible(choices) {
		return nil
	}
	if block, exists := doc.Sections[codexFeaturesSection]; exists {
		tomlpatch.SetKeyValue(block, nil, key, tomlpatch.FormatValue(enabled), "")
		return nil
	}
	if parentBlock, exists := doc.Sections[codexAgentSpecificSectionPrefix]; exists {
		dottedKey := "features." + key
		if hasUncommentedKeyLine(parentBlock.Lines, dottedKey) {
			tomlpatch.SetKeyValue(parentBlock, nil, dottedKey, tomlpatch.FormatValue(enabled), "")
			return nil
		}
		if hasUncommentedKeyLine(parentBlock.Lines, "features") {
			if current, exists := codexFeatureValueFromAgentSpecificBlock(parentBlock, key); exists {
				if current != enabled {
					return fmt.Errorf(messages.WizardCodexInlineFeaturesUnsupported)
				}
				return nil
			}
			// The key is absent from the inline table, so Codex applies its native
			// default. A no-op is correct only when the desired state already matches
			// that default; otherwise the inline table would need editing, which the
			// line patcher cannot do, so surface the limitation.
			if enabled == defaultEnabled {
				return nil
			}
			return fmt.Errorf(messages.WizardCodexInlineFeaturesUnsupported)
		}
		if hasUncommentedKeyWithPrefix(parentBlock.Lines, "features.") {
			tomlpatch.SetKeyValue(parentBlock, nil, dottedKey, tomlpatch.FormatValue(enabled), "")
			return nil
		}
	}
	block := &tomlpatch.Block{
		Name:  codexFeaturesSection,
		Lines: []string{"[" + codexFeaturesSection + "]"},
	}
	doc.Sections[codexFeaturesSection] = block
	doc.Order = append(doc.Order, codexFeaturesSection)
	tomlpatch.SetKeyValue(block, nil, key, tomlpatch.FormatValue(enabled), "")
	return nil
}

// codexBrowserFeatureKeys are the [features] keys the browser/computer-use
// disable toggle controls. All three are set to false together when disabling.
var codexBrowserFeatureKeys = config.CodexBrowserFeatureKeys()

// applyCodexBrowserUpdate writes the browser/computer-use feature keys into the
// [agents.codex.agent_specific.features] table, parallel to applyCodexAppsUpdate
// (both target the same table). Disabling sets each key false; leaving the
// toggle off comments any existing keys and adds none. Inline `features = {...}`
// surfaces a clear error when a change is required. Mutates doc in place.
func applyCodexBrowserUpdate(doc *tomlpatch.Document, choices *Choices) error {
	if !choices.CodexDisableBrowserTouched {
		return nil
	}
	if !codexRuntimeToggleVisible(choices) {
		return nil
	}
	if block, exists := doc.Sections[codexFeaturesSection]; exists {
		applyCodexBrowserKeys(block, "", choices.CodexDisableBrowser)
		return nil
	}
	if parentBlock, exists := doc.Sections[codexAgentSpecificSectionPrefix]; exists {
		if hasUncommentedKeyLine(parentBlock.Lines, "features") {
			// An inline `features = {...}` table cannot be edited line-by-line.
			// Surface the limitation whenever a change is required: when disabling
			// (we would need to set the keys) or when the inline table already pins
			// a browser key the toggle would otherwise clear. Matches the apps path.
			if choices.CodexDisableBrowser || inlineFeaturesHasAnyCodexBrowserKey(parentBlock) {
				return fmt.Errorf(messages.WizardCodexInlineFeaturesUnsupported)
			}
			return nil
		}
		if choices.CodexDisableBrowser || hasUncommentedKeyWithPrefix(parentBlock.Lines, "features.") {
			applyCodexBrowserKeys(parentBlock, "features.", choices.CodexDisableBrowser)
		}
		return nil
	}
	if !choices.CodexDisableBrowser {
		return nil
	}
	block := &tomlpatch.Block{
		Name:  codexFeaturesSection,
		Lines: []string{"[" + codexFeaturesSection + "]"},
	}
	doc.Sections[codexFeaturesSection] = block
	doc.Order = append(doc.Order, codexFeaturesSection)
	applyCodexBrowserKeys(block, "", true)
	return nil
}

// inlineFeaturesHasAnyCodexBrowserKey reports whether an inline
// `features = {...}` table in block defines any browser/computer-use key. Used
// to surface WizardCodexInlineFeaturesUnsupported when the line-based patcher
// cannot edit those pins inside an inline table. Parallels the generic Codex
// feature reader used for apps/plugins.
func inlineFeaturesHasAnyCodexBrowserKey(block *tomlpatch.Block) bool {
	if block == nil {
		return false
	}
	var cfg struct {
		Agents struct {
			Codex struct {
				AgentSpecific map[string]any `toml:"agent_specific"`
			} `toml:"codex"`
		} `toml:"agents"`
	}
	if err := toml.Unmarshal([]byte(strings.Join(block.Lines, "\n")), &cfg); err != nil {
		return false
	}
	features, ok := cfg.Agents.Codex.AgentSpecific["features"].(map[string]any)
	if !ok {
		return false
	}
	for _, key := range codexBrowserFeatureKeys {
		if _, exists := features[key]; exists {
			return true
		}
	}
	return false
}

// applyCodexBrowserKeys sets (when disabling) or comments (when not) each
// browser feature key in block. prefix is "" for a dedicated [features] table
// or "features." for dotted keys under [agents.codex.agent_specific].
func applyCodexBrowserKeys(block *tomlpatch.Block, prefix string, disable bool) {
	for _, key := range codexBrowserFeatureKeys {
		if disable {
			tomlpatch.SetKeyValue(block, nil, prefix+key, tomlpatch.FormatValue(false), "")
		} else {
			tomlpatch.SetCommentedKeyLine(block, nil, prefix+key, "")
		}
	}
}

// Claude agent_specific sections and the dotted keys the wizard's Claude disable
// toggles write. Keys go into the [agents.claude] block as dotted
// `agent_specific.*` keys (the template's form) unless the user expanded
// agent_specific into explicit sub-tables, in which case the leaf is written
// into the matching section to avoid a TOML duplicate-table error.
const (
	claudeSection                 = "agents.claude"
	claudeAgentSpecificSection    = "agents.claude.agent_specific"
	claudeAgentSpecificEnvSection = "agents.claude.agent_specific.env"
)

// claudeAgentSpecificKey describes one agent_specific value the wizard writes
// into the Claude config and the three forms it can take depending on how the
// user has laid out agent_specific.
type claudeAgentSpecificKey struct {
	expandedSection string // section holding the leaf when agent_specific is expanded
	leafKey         string // key name inside expandedSection
	parentDotted    string // dotted path under [agents.claude.agent_specific]
	claudeDotted    string // dotted path under [agents.claude]
	value           string // TOML literal written when disabling
}

func claudeEnvKey(envKey string) claudeAgentSpecificKey {
	return claudeAgentSpecificKey{
		expandedSection: claudeAgentSpecificEnvSection,
		leafKey:         envKey,
		parentDotted:    "env." + envKey,
		claudeDotted:    "agent_specific.env." + envKey,
		value:           tomlpatch.FormatValue(falseValue),
	}
}

// applyClaudeAgentSpecificUpdate writes the touched Claude disable toggles into
// doc. It mirrors applyCodexAppsUpdate but operates across the Claude
// agent_specific sections so an expanded layout is handled without producing a
// duplicate-table error. Mutates doc in place.
func applyClaudeAgentSpecificUpdate(doc *tomlpatch.Document, choices *Choices) {
	if !claudeDisableTogglesTouched(choices) {
		return
	}
	if choices.EnabledAgentsTouched &&
		!choices.EnabledAgents[AgentClaude] && !choices.EnabledAgents[AgentClaudeVSCode] {
		return
	}
	if choices.ClaudeDisableIDEReadingTouched {
		writeClaudeAgentSpecificKey(doc, claudeEnvKey(claudeIDEReadingEnvKey), choices.ClaudeDisableIDEReading)
	}
	if choices.ClaudeDisableConnectorsTouched {
		writeClaudeAgentSpecificKey(doc, claudeEnvKey(claudeConnectorsEnvKey), choices.ClaudeDisableConnectors)
	}
	if choices.ClaudeDisableMemoryTouched {
		writeClaudeAgentSpecificKey(doc, claudeAgentSpecificKey{
			expandedSection: claudeAgentSpecificSection,
			leafKey:         autoMemoryEnabledKey,
			parentDotted:    autoMemoryEnabledKey,
			claudeDotted:    "agent_specific.autoMemoryEnabled",
			value:           tomlpatch.FormatValue(false),
		}, choices.ClaudeDisableMemory)
	}
	// The AskUserQuestion toggle is written as a typed agents.claude
	// disable_question_tool scalar in applySectionUpdates, not here — sync injects
	// the deny + PreToolUse hook so user agent_specific entries are never clobbered.
}

// claudeDisableTogglesTouched gates the agent_specific writes in
// applyClaudeAgentSpecificUpdate. The AskUserQuestion toggle is intentionally
// excluded — it writes a typed agents.claude scalar, not an agent_specific key.
func claudeDisableTogglesTouched(choices *Choices) bool {
	return choices.ClaudeDisableIDEReadingTouched ||
		choices.ClaudeDisableMemoryTouched ||
		choices.ClaudeDisableConnectorsTouched
}

// writeClaudeAgentSpecificKey writes key into the most specific existing target
// so the rendered config stays valid TOML: the expanded sub-table section if
// present, else the [agents.claude.agent_specific] parent section, else the
// [agents.claude] block as a fully-dotted key. Disabling writes the value;
// leaving the toggle off comments any existing line and inserts nothing.
func writeClaudeAgentSpecificKey(doc *tomlpatch.Document, key claudeAgentSpecificKey, disable bool) {
	if block, exists := doc.Sections[key.expandedSection]; exists {
		writeOrCommentKey(block, key.leafKey, key.value, disable)
		return
	}
	if block, exists := doc.Sections[claudeAgentSpecificSection]; exists && key.expandedSection != claudeAgentSpecificSection {
		writeOrCommentKey(block, key.parentDotted, key.value, disable)
		return
	}
	writeOrCommentKey(ensureClaudeSectionBlock(doc), key.claudeDotted, key.value, disable)
}

// writeOrCommentKey sets key = value when disable is true, otherwise comments
// any existing uncommented line for key (inserting nothing when absent). Keys
// in [agents.claude] are placed after "enabled"; in sub-tables the afterKey is
// not found and the key lands just after the section header.
func writeOrCommentKey(block *tomlpatch.Block, key string, value string, disable bool) {
	if disable {
		tomlpatch.SetKeyValue(block, nil, key, value, "enabled")
		return
	}
	tomlpatch.SetCommentedKeyLine(block, nil, key, "enabled")
}

// ensureClaudeSectionBlock returns the [agents.claude] block, creating an empty
// one when absent. The section is present in every config derived from the
// template; the create path is a defensive fallback for hand-edited configs.
func ensureClaudeSectionBlock(doc *tomlpatch.Document) *tomlpatch.Block {
	if block, exists := doc.Sections[claudeSection]; exists {
		return block
	}
	block := &tomlpatch.Block{Name: claudeSection, Lines: []string{"[" + claudeSection + "]"}}
	doc.Sections[claudeSection] = block
	doc.Order = append(doc.Order, claudeSection)
	return block
}

func codexFeatureValueFromAgentSpecificBlock(block *tomlpatch.Block, key string) (bool, bool) {
	if block == nil {
		return false, false
	}
	var cfg struct {
		Agents struct {
			Codex struct {
				AgentSpecific map[string]any `toml:"agent_specific"`
			} `toml:"codex"`
		} `toml:"agents"`
	}
	if err := toml.Unmarshal([]byte(strings.Join(block.Lines, "\n")), &cfg); err != nil {
		return false, false
	}
	return readCodexFeatureValue(cfg.Agents.Codex.AgentSpecific, key)
}

func isCodexAgentSpecificSection(name string) bool {
	return name == codexAgentSpecificSectionPrefix || strings.HasPrefix(name, codexAgentSpecificSectionPrefix+".")
}

func hasUncommentedKeyWithPrefix(lines []string, prefix string) bool {
	found := false
	tomlpatch.WalkLinesOutsideMultiline(lines, func(_ int, line string, state tomlpatch.StringState) tomlpatch.LineWalkResult {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			return tomlpatch.LineWalkResult{}
		}
		commentPos, _ := tomlpatch.ScanLineForComment(trimmed, state)
		if commentPos >= 0 {
			trimmed = strings.TrimSpace(trimmed[:commentPos])
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			return tomlpatch.LineWalkResult{}
		}
		if strings.HasPrefix(strings.TrimSpace(key), prefix) {
			found = true
			return tomlpatch.LineWalkResult{Stop: true}
		}
		return tomlpatch.LineWalkResult{}
	})
	return found
}

func hasUncommentedKeyLine(lines []string, key string) bool {
	found := false
	tomlpatch.WalkLinesOutsideMultiline(lines, func(_ int, line string, state tomlpatch.StringState) tomlpatch.LineWalkResult {
		parsed, ok := tomlpatch.ParseKeyLineWithState(line, key, state)
		if ok && !parsed.Commented {
			found = true
			return tomlpatch.LineWalkResult{Stop: true}
		}
		return tomlpatch.LineWalkResult{}
	})
	return found
}

func normalizeLegacySectionAliases(doc *tomlpatch.Document) {
	for legacyName, canonicalName := range legacySectionAliases {
		legacyBlock, hasLegacy := doc.Sections[legacyName]
		if !hasLegacy {
			continue
		}

		if _, hasCanonical := doc.Sections[canonicalName]; !hasCanonical {
			migrated := cloneBlock(legacyBlock)
			migrated.Name = canonicalName
			if len(migrated.Lines) > 0 {
				migrated.Lines[0] = rewriteSectionHeaderLine(migrated.Lines[0], canonicalName)
			}
			doc.Sections[canonicalName] = migrated
		}

		delete(doc.Sections, legacyName)
		doc.Order = applyLegacyAliasToOrder(doc.Order, legacyName, canonicalName)
	}
}

func rewriteSectionHeaderLine(line string, sectionName string) string {
	leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	trimmed := strings.TrimSpace(line)
	commentPos, _ := tomlpatch.ScanLineForComment(trimmed, tomlpatch.StateNone)
	comment := ""
	if commentPos >= 0 {
		comment = strings.TrimSpace(trimmed[commentPos:])
	}
	rewritten := "[" + sectionName + "]"
	if comment != "" {
		rewritten += " " + comment
	}
	return leading + rewritten
}

func applyLegacyAliasToOrder(order []string, legacyName string, canonicalName string) []string {
	normalized := make([]string, 0, len(order))
	canonicalSeen := false
	for _, name := range order {
		switch name {
		case legacyName:
			if canonicalSeen {
				continue
			}
			normalized = append(normalized, canonicalName)
			canonicalSeen = true
		case canonicalName:
			if canonicalSeen {
				continue
			}
			normalized = append(normalized, canonicalName)
			canonicalSeen = true
		default:
			normalized = append(normalized, name)
		}
	}
	return normalized
}

// cloneBlock returns a deep copy of a block, including its lines and sub-tables.
func cloneBlock(block *tomlpatch.Block) *tomlpatch.Block {
	if block == nil {
		return nil
	}
	return &tomlpatch.Block{Name: block.Name, Lines: tomlpatch.CloneLines(block.Lines), SubTables: cloneBlocks(block.SubTables)}
}

// cloneBlocks returns deep copies of blocks.
func cloneBlocks(blocks []*tomlpatch.Block) []*tomlpatch.Block {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]*tomlpatch.Block, 0, len(blocks))
	for _, block := range blocks {
		cloned = append(cloned, cloneBlock(block))
	}
	return cloned
}

// renderedBlockLines returns a block's lines followed by its sub-tables' lines,
// keeping each array element and its nested tables together as one unit.
func renderedBlockLines(block *tomlpatch.Block) []string {
	if len(block.SubTables) == 0 {
		return block.Lines
	}
	lines := tomlpatch.CloneLines(block.Lines)
	for _, subTable := range block.SubTables {
		lines = append(lines, subTable.Lines...)
	}
	return lines
}

// extraSectionBlocks returns non-template section blocks sorted by name.
// sections are from the current config; templateSections defines known canonical sections.
// codexAgentSpecific selects whether to return only Codex agent_specific sections or the other extras.
func extraSectionBlocks(sections map[string]*tomlpatch.Block, templateSections map[string]*tomlpatch.Block, codexAgentSpecific bool) []*tomlpatch.Block {
	extra := make([]*tomlpatch.Block, 0)
	for name, block := range sections {
		if _, exists := templateSections[name]; exists {
			continue
		}
		if isCodexAgentSpecificSection(name) != codexAgentSpecific {
			continue
		}
		extra = append(extra, cloneBlock(block))
	}
	sort.Slice(extra, func(i, j int) bool {
		return extra[i].Name < extra[j].Name
	})
	return extra
}

// extraArrayBlocks returns non-mcp.servers array-of-table blocks sorted by name.
// arrays are from the current config; returns cloned blocks for arrays not handled by MCP logic.
func extraArrayBlocks(arrays map[string][]*tomlpatch.Block) []*tomlpatch.Block {
	extra := make([]*tomlpatch.Block, 0)
	for name, blocks := range arrays {
		if name == mcpServersSection {
			continue
		}
		for _, block := range blocks {
			extra = append(extra, cloneBlock(block))
		}
	}
	sort.SliceStable(extra, func(i, j int) bool {
		return extra[i].Name < extra[j].Name
	})
	return extra
}
