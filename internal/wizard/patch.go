// Package wizard implements the interactive setup wizard for Agent Layer.
//
// # TOML Parsing Strategy
//
// This package uses custom line-based TOML parsing instead of the go-toml library's
// tree manipulation for config updates. This is intentional for several reasons:
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

type tomlBlock struct {
	name  string
	lines []string
}

type tomlDocument struct {
	preamble []string
	sections map[string]*tomlBlock
	arrays   map[string][]*tomlBlock
	order    []string
}

func toSharedBlock(block *tomlBlock) *tomlpatch.Block {
	if block == nil {
		return nil
	}
	return &tomlpatch.Block{Name: block.name, Lines: cloneLines(block.lines)}
}

func applySharedBlock(dst *tomlBlock, src *tomlpatch.Block) {
	if dst == nil || src == nil {
		return
	}
	dst.name = src.Name
	dst.lines = cloneLines(src.Lines)
}

func fromSharedKeyLine(line tomlpatch.KeyLine) keyLine {
	return keyLine{
		raw:           line.Raw,
		indent:        line.Indent,
		commented:     line.Commented,
		inlineComment: line.InlineComment,
	}
}

func fromSharedDocument(doc tomlpatch.Document) tomlDocument {
	sections := make(map[string]*tomlBlock, len(doc.Sections))
	for name, block := range doc.Sections {
		sections[name] = &tomlBlock{name: block.Name, lines: cloneLines(block.Lines)}
	}
	arrays := make(map[string][]*tomlBlock, len(doc.Arrays))
	for name, blocks := range doc.Arrays {
		arrays[name] = make([]*tomlBlock, 0, len(blocks))
		for _, block := range blocks {
			arrays[name] = append(arrays[name], &tomlBlock{name: block.Name, lines: cloneLines(block.Lines)})
		}
	}
	return tomlDocument{
		preamble: cloneLines(doc.Preamble),
		sections: sections,
		arrays:   arrays,
		order:    cloneLines(doc.Order),
	}
}

var preferredWizardSectionOrder = []string{
	approvalsSection,
	antigravitySection,
	claudeSection,
	claudeVSCodeSection,
	codexSection,
	"agents.vscode",
	"agents.copilot_cli",
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

	templateDoc := parseTomlDocument(templateContent)
	currentDoc := parseTomlDocument(content)
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
func assembleCanonicalConfig(currentDoc tomlDocument, templateDoc tomlDocument, catalogDoc tomlDocument, choices *Choices) ([]string, error) {
	preamble := choosePreamble(currentDoc.preamble, templateDoc.preamble)
	output := make([]string, 0, len(preamble))
	output = append(output, preamble...)

	removeWarnings := choices.WarningsEnabledTouched && !choices.WarningsEnabled

	for _, name := range orderedWizardSections(templateDoc.order) {
		if name == warningsSection && removeWarnings {
			continue
		}
		block := selectSectionBlock(currentDoc.sections[name], templateDoc.sections[name])
		if block == nil {
			continue
		}
		updated := cloneBlock(block)
		applySectionUpdates(name, updated, templateDoc.sections[name], choices)
		appendBlock(&output, updated.lines)

		if name == codexSection {
			for _, block := range codexAgentSpecificSectionBlocks(currentDoc.sections, templateDoc.sections) {
				appendBlock(&output, block.lines)
			}
		}

		if name == mcpSection {
			serverBlocks, err := buildMCPServerBlocks(currentDoc, catalogDoc, choices)
			if err != nil {
				return nil, err
			}
			for _, serverBlock := range serverBlocks {
				appendBlock(&output, serverBlock.lines)
			}
		}
	}

	extraSections := extraSectionBlocks(currentDoc.sections, templateDoc.sections)
	for _, block := range extraSections {
		appendBlock(&output, block.lines)
	}

	// Preserve non-mcp.servers array-of-table blocks.
	extraArrays := extraArrayBlocks(currentDoc.arrays)
	for _, block := range extraArrays {
		appendBlock(&output, block.lines)
	}

	return trimTrailingEmptyLines(output), nil
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
func selectSectionBlock(current *tomlBlock, template *tomlBlock) *tomlBlock {
	if current != nil {
		return current
	}
	return template
}

// applySectionUpdates mutates the block in place based on wizard choices.
// name identifies the section; templateBlock provides canonical formatting for inserted keys.
func applySectionUpdates(name string, block *tomlBlock, templateBlock *tomlBlock, choices *Choices) {
	switch name {
	case approvalsSection:
		if choices.ApprovalModeTouched {
			setKeyValue(block, templateBlock, "mode", formatTomlValue(choices.ApprovalMode), "")
		}
	case antigravitySection:
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentAntigravity]), "")
		}
		if choices.AntigravityModelTouched && (!choices.EnabledAgentsTouched || choices.EnabledAgents[AgentAntigravity]) {
			setOptionalKeyValue(block, templateBlock, "model", choices.AntigravityModel, "enabled")
		}
	case claudeSection:
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentClaude]), "")
		}
		if choices.ClaudeModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.ClaudeModel, "enabled")
		}
		if choices.ClaudeReasoningTouched {
			setOptionalKeyValue(block, templateBlock, "reasoning_effort", choices.ClaudeReasoning, "model")
		}
		if choices.ClaudeLocalConfigDirTouched {
			if choices.ClaudeLocalConfigDir {
				setKeyValue(block, templateBlock, "local_config_dir", formatTomlValue(true), "model")
			} else {
				setCommentedKeyLine(block, templateBlock, "local_config_dir", "model")
			}
		}
		if choices.ClaudeDisableQuestionToolTouched {
			if choices.ClaudeDisableQuestionTool {
				setKeyValue(block, templateBlock, "disable_question_tool", formatTomlValue(true), "local_config_dir")
			} else {
				setCommentedKeyLine(block, templateBlock, "disable_question_tool", "local_config_dir")
			}
		}
		if choices.ClaudeStatuslineTouched {
			setKeyValue(block, templateBlock, "statusline", formatTomlValue(choices.ClaudeStatusline), "disable_question_tool")
		}
	case claudeVSCodeSection:
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentClaudeVSCode]), "")
		}
	case codexSection:
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentCodex]), "")
		}
		if choices.CodexModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.CodexModel, "enabled")
		}
		if choices.CodexReasoningTouched {
			setOptionalKeyValue(block, templateBlock, "reasoning_effort", choices.CodexReasoning, "model")
		}
		if choices.CodexLocalConfigDirTouched {
			if choices.CodexLocalConfigDir {
				setKeyValue(block, templateBlock, "local_config_dir", formatTomlValue(true), "reasoning_effort")
			} else {
				setCommentedKeyLine(block, templateBlock, "local_config_dir", "reasoning_effort")
			}
		}
		if choices.CodexStatuslineTouched {
			// Anchor statusline after local_config_dir when that line exists,
			// otherwise fall back to reasoning_effort (the pre-local_config_dir
			// anchor). This keeps statusline in place instead of reordering it to
			// the top of a block that has no local_config_dir line.
			statuslineAnchor := "local_config_dir"
			if _, ok := findKeyLine(block.lines, "local_config_dir"); !ok {
				statuslineAnchor = "reasoning_effort"
			}
			setKeyValue(block, templateBlock, "statusline", formatTomlValue(choices.CodexStatusline), statuslineAnchor)
		}
	case "agents.vscode":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentVSCode]), "")
		}
	case "agents.copilot_cli":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentCopilotCLI]), "")
		}
		if choices.CopilotCLIModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.CopilotCLIModel, "enabled")
		}
	case warningsSection:
		if choices.WarningsEnabledTouched && choices.WarningsEnabled {
			setKeyValue(block, templateBlock, "instruction_token_threshold", formatTomlValue(choices.InstructionTokenThreshold), "")
			setKeyValue(block, templateBlock, "mcp_server_threshold", formatTomlValue(choices.MCPServerThreshold), "instruction_token_threshold")
			setKeyValue(block, templateBlock, "mcp_tools_total_threshold", formatTomlValue(choices.MCPToolsTotalThreshold), "mcp_server_threshold")
			setKeyValue(block, templateBlock, "mcp_server_tools_threshold", formatTomlValue(choices.MCPServerToolsThreshold), "mcp_tools_total_threshold")
			setKeyValue(block, templateBlock, "mcp_schema_tokens_total_threshold", formatTomlValue(choices.MCPSchemaTokensTotalThreshold), "mcp_server_tools_threshold")
			setKeyValue(block, templateBlock, "mcp_schema_tokens_server_threshold", formatTomlValue(choices.MCPSchemaTokensServerThreshold), "mcp_schema_tokens_total_threshold")
		}
	}
}

type mcpBlock struct {
	id    string
	lines []string
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
func buildMCPServerBlocks(currentDoc tomlDocument, catalogDoc tomlDocument, choices *Choices) ([]tomlBlock, error) {
	currentBlocks := parseMCPBlocks(currentDoc.arrays[mcpServersSection])
	catalogBlocks := parseMCPBlocks(catalogDoc.arrays[mcpServersSection])

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

	var ordered []tomlBlock
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
		tb := tomlBlock{name: mcpServersSection, lines: cloneLines(block.lines)}
		// Honor the custom-server keep/disable decision. Unlike catalog defaults,
		// a custom server has no template to restore from, so disabling sets
		// enabled = false rather than pruning the block. Untouched configs pass
		// through unchanged, preserving the original enabled state. A block with no
		// recorded decision (absent from the map) is also left as-is rather than
		// forced to false, so we never impose a decision the user did not make.
		if choices.CustomMCPServersTouched && block.id != "" {
			if enabled, ok := choices.CustomMCPServersEnabled[block.id]; ok {
				setKeyValue(&tb, nil, "enabled", formatTomlValue(enabled), "id")
			}
		}
		sanitizeMCPServerBlock(&tb)
		ordered = append(ordered, tb)
	}

	return ordered, nil
}

// sanitizeMCPServerBlock removes transport-incompatible fields from a server block.
// This allows the wizard to repair configs where, for example, a stdio server
// has leftover headers from a previous configuration.
func sanitizeMCPServerBlock(block *tomlBlock) {
	transport := extractMCPBlockKeyValue(block.lines, "transport")
	switch transport {
	case "stdio":
		for _, key := range stdioIncompatibleKeys {
			removeKeyFromBlock(block, key)
		}
	case "http":
		for _, key := range httpIncompatibleKeys {
			removeKeyFromBlock(block, key)
		}
	}
}

type tomlLineWalkResult struct {
	advanceTo int
	stop      bool
}

func walkTomlLinesOutsideMultiline(lines []string, fn func(i int, line string, state tomlStringState) tomlLineWalkResult) {
	tomlpatch.WalkLinesOutsideMultiline(lines, func(i int, line string, state tomlpatch.StringState) tomlpatch.LineWalkResult {
		result := fn(i, line, tomlStringState(state))
		return tomlpatch.LineWalkResult{AdvanceTo: result.advanceTo, Stop: result.stop}
	})
}

// extractMCPBlockKeyValue returns the unquoted value for a key in a TOML block.
// lines are the raw block lines; key is the key to search for.
// Tracks multiline string state to avoid parsing content inside multiline strings.
func extractMCPBlockKeyValue(lines []string, key string) string {
	return tomlpatch.ExtractBlockKeyValue(lines, key)
}

// removeKeyFromBlock removes all uncommented lines for the given key from a block,
// including continuation lines of multiline arrays, inline tables, and triple-quoted strings.
// Also removes dotted sub-key lines (e.g., "headers.Authorization = val" when key is "headers").
// block is updated in place; commented-out lines for the key are preserved.
// Tracks multiline string state to avoid matching content inside multiline strings.
func removeKeyFromBlock(block *tomlBlock, key string) {
	shared := toSharedBlock(block)
	tomlpatch.RemoveKeyFromBlock(shared, key)
	applySharedBlock(block, shared)
}

// updateMCPEnabled applies the enabled toggle to a server block when requested.
// block holds the current server text; templateBlock provides canonical formatting; id identifies the server.
func updateMCPEnabled(block mcpBlock, templateBlock mcpBlock, choices *Choices, id string) tomlBlock {
	updated := tomlBlock{name: mcpServersSection, lines: cloneLines(block.lines)}
	if choices.EnabledMCPServersTouched {
		tpl := (*tomlBlock)(nil)
		if len(templateBlock.lines) > 0 {
			tpl = &tomlBlock{name: mcpServersSection, lines: cloneLines(templateBlock.lines)}
		}
		setKeyValue(&updated, tpl, "enabled", formatTomlValue(choices.EnabledMCPServers[id]), "id")
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
func parseMCPBlocks(blocks []*tomlBlock) []mcpBlock {
	result := make([]mcpBlock, 0, len(blocks))
	for _, block := range blocks {
		id := extractMCPServerID(block.lines)
		result = append(result, mcpBlock{id: id, lines: cloneLines(block.lines)})
	}
	return result
}

// extractMCPServerID returns the first non-commented id value in a server block.
// lines are the raw block lines; returns empty string when no id is found.
func extractMCPServerID(lines []string) string {
	return extractMCPBlockKeyValue(lines, "id")
}

// parseKeyValueWithState extracts a simple key/value pair from a TOML line with explicit state.
// line is the raw line; key is the expected key name; state tracks multiline strings.
func parseKeyValueWithState(line string, key string, state tomlStringState) (string, string, bool) {
	return tomlpatch.ParseKeyValueWithState(line, key, tomlpatch.StringState(state))
}

// setOptionalKeyValue updates or comments out an optional key based on the provided value.
// block is updated in place; templateBlock provides a canonical commented line when clearing; afterKey controls insertion order.
func setOptionalKeyValue(block *tomlBlock, templateBlock *tomlBlock, key string, value string, afterKey string) {
	if value == "" {
		setCommentedKeyLine(block, templateBlock, key, afterKey)
		return
	}
	setKeyValue(block, templateBlock, key, formatTomlValue(value), afterKey)
}

// setCommentedKeyLine ensures the key line is commented, inserting a template line when available.
// block is updated in place; templateBlock provides canonical formatting; afterKey controls insertion order.
func setCommentedKeyLine(block *tomlBlock, templateBlock *tomlBlock, key string, afterKey string) {
	shared := toSharedBlock(block)
	tomlpatch.SetCommentedKeyLine(shared, toSharedBlock(templateBlock), key, afterKey)
	applySharedBlock(block, shared)
}

// setKeyValue updates or inserts a key/value line in a section block.
// block is updated in place; templateBlock provides canonical formatting; afterKey controls insertion order.
func setKeyValue(block *tomlBlock, templateBlock *tomlBlock, key string, value string, afterKey string) {
	shared := toSharedBlock(block)
	tomlpatch.SetKeyValue(shared, toSharedBlock(templateBlock), key, value, afterKey)
	applySharedBlock(block, shared)
}

// keyLine holds a parsed key/value line with comment metadata.
type keyLine struct {
	raw           string
	indent        string
	commented     bool
	inlineComment string
}

// findKeyLine searches lines for a key/value assignment and returns the parsed line.
// Returns false if the key is not present.
// Tracks multiline string state to avoid parsing content inside multiline strings.
func findKeyLine(lines []string, key string) (keyLine, bool) {
	parsed, ok := tomlpatch.FindKeyLine(lines, key)
	if !ok {
		return keyLine{}, false
	}
	return fromSharedKeyLine(parsed), true
}

// parseKeyLineWithState parses a key/value assignment line with explicit state tracking.
// Returns false when the line does not define the requested key.
func parseKeyLineWithState(line string, key string, state tomlStringState) (keyLine, bool) {
	parsed, ok := tomlpatch.ParseKeyLineWithState(line, key, tomlpatch.StringState(state))
	if !ok {
		return keyLine{}, false
	}
	return fromSharedKeyLine(parsed), true
}

// buildKeyLine renders a key/value line using indentation and inline comment from base.
func buildKeyLine(base keyLine, key string, value string, commented bool) string {
	return tomlpatch.BuildKeyLine(tomlpatch.KeyLine{
		Raw:           base.raw,
		Indent:        base.indent,
		Commented:     base.commented,
		InlineComment: base.inlineComment,
	}, key, value, commented)
}

// ensureCommented returns the line with a leading comment marker.
func ensureCommented(line string) string {
	return tomlpatch.EnsureCommented(line)
}

// replaceOrInsertLine replaces an existing key line or inserts a new line after afterKey.
// block is updated in place; duplicates are removed to keep a single key occurrence.
// Tracks multiline string state to avoid matching content inside multiline strings.
func replaceOrInsertLine(block *tomlBlock, key string, newLine string, afterKey string) {
	shared := toSharedBlock(block)
	tomlpatch.ReplaceOrInsertLine(shared, key, newLine, afterKey)
	applySharedBlock(block, shared)
}

// findInsertIndex returns the line index to insert a new key line after afterKey.
// lines should include the section header as the first entry.
// Tracks multiline string state to avoid matching content inside multiline strings.
func findInsertIndex(lines []string, afterKey string) int {
	return tomlpatch.FindInsertIndex(lines, afterKey)
}

// formatTomlValue converts a scalar value into a TOML literal string.
func formatTomlValue(value interface{}) string {
	return tomlpatch.FormatValue(value)
}

// parseTomlDocument splits TOML content into preamble lines, section blocks, and array-of-table blocks.
// Returns the parsed document with section order based on appearance.
func parseTomlDocument(content string) tomlDocument {
	return fromSharedDocument(tomlpatch.ParseDocument(content))
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

func applyCodexAppsUpdate(doc *tomlDocument, choices *Choices) error {
	if !choices.CodexAppsTouched {
		return nil
	}
	return applyCodexBooleanFeatureUpdate(doc, choices, config.CodexFeatureAppsKey, choices.CodexApps, codexFeatureDefaultApps)
}

// applyCodexPluginsUpdate writes choices.CodexPlugins into the
// [agents.codex.agent_specific.features] section of doc when CodexPluginsTouched.
func applyCodexPluginsUpdate(doc *tomlDocument, choices *Choices) error {
	if !choices.CodexPluginsTouched {
		return nil
	}
	return applyCodexBooleanFeatureUpdate(doc, choices, config.CodexFeaturePluginsKey, choices.CodexPlugins, codexFeatureDefaultPlugins)
}

func applyCodexBooleanFeatureUpdate(doc *tomlDocument, choices *Choices, key string, enabled, defaultEnabled bool) error {
	if choices.EnabledAgentsTouched && !choices.EnabledAgents[AgentCodex] {
		return nil
	}
	if block, exists := doc.sections[codexFeaturesSection]; exists {
		setKeyValue(block, nil, key, formatTomlValue(enabled), "")
		return nil
	}
	if parentBlock, exists := doc.sections[codexAgentSpecificSectionPrefix]; exists {
		dottedKey := "features." + key
		if hasUncommentedKeyLine(parentBlock.lines, dottedKey) {
			setKeyValue(parentBlock, nil, dottedKey, formatTomlValue(enabled), "")
			return nil
		}
		if hasUncommentedKeyLine(parentBlock.lines, "features") {
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
		if hasUncommentedKeyWithPrefix(parentBlock.lines, "features.") {
			setKeyValue(parentBlock, nil, dottedKey, formatTomlValue(enabled), "")
			return nil
		}
	}
	block := &tomlBlock{
		name:  codexFeaturesSection,
		lines: []string{"[" + codexFeaturesSection + "]"},
	}
	doc.sections[codexFeaturesSection] = block
	doc.order = append(doc.order, codexFeaturesSection)
	setKeyValue(block, nil, key, formatTomlValue(enabled), "")
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
func applyCodexBrowserUpdate(doc *tomlDocument, choices *Choices) error {
	if !choices.CodexDisableBrowserTouched {
		return nil
	}
	if choices.EnabledAgentsTouched && !choices.EnabledAgents[AgentCodex] {
		return nil
	}
	if block, exists := doc.sections[codexFeaturesSection]; exists {
		applyCodexBrowserKeys(block, "", choices.CodexDisableBrowser)
		return nil
	}
	if parentBlock, exists := doc.sections[codexAgentSpecificSectionPrefix]; exists {
		if hasUncommentedKeyLine(parentBlock.lines, "features") {
			// An inline `features = {...}` table cannot be edited line-by-line.
			// Surface the limitation whenever a change is required: when disabling
			// (we would need to set the keys) or when the inline table already pins
			// a browser key the toggle would otherwise clear. Matches the apps path.
			if choices.CodexDisableBrowser || inlineFeaturesHasAnyCodexBrowserKey(parentBlock) {
				return fmt.Errorf(messages.WizardCodexInlineFeaturesUnsupported)
			}
			return nil
		}
		if choices.CodexDisableBrowser || hasUncommentedKeyWithPrefix(parentBlock.lines, "features.") {
			applyCodexBrowserKeys(parentBlock, "features.", choices.CodexDisableBrowser)
		}
		return nil
	}
	if !choices.CodexDisableBrowser {
		return nil
	}
	block := &tomlBlock{
		name:  codexFeaturesSection,
		lines: []string{"[" + codexFeaturesSection + "]"},
	}
	doc.sections[codexFeaturesSection] = block
	doc.order = append(doc.order, codexFeaturesSection)
	applyCodexBrowserKeys(block, "", true)
	return nil
}

// inlineFeaturesHasAnyCodexBrowserKey reports whether an inline
// `features = {...}` table in block defines any browser/computer-use key. Used
// to surface WizardCodexInlineFeaturesUnsupported when the line-based patcher
// cannot edit those pins inside an inline table. Parallels the generic Codex
// feature reader used for apps/plugins.
func inlineFeaturesHasAnyCodexBrowserKey(block *tomlBlock) bool {
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
	if err := toml.Unmarshal([]byte(strings.Join(block.lines, "\n")), &cfg); err != nil {
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
func applyCodexBrowserKeys(block *tomlBlock, prefix string, disable bool) {
	for _, key := range codexBrowserFeatureKeys {
		if disable {
			setKeyValue(block, nil, prefix+key, formatTomlValue(false), "")
		} else {
			setCommentedKeyLine(block, nil, prefix+key, "")
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
		value:           formatTomlValue(falseValue),
	}
}

// applyClaudeAgentSpecificUpdate writes the touched Claude disable toggles into
// doc. It mirrors applyCodexAppsUpdate but operates across the Claude
// agent_specific sections so an expanded layout is handled without producing a
// duplicate-table error. Mutates doc in place.
func applyClaudeAgentSpecificUpdate(doc *tomlDocument, choices *Choices) {
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
			value:           formatTomlValue(false),
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
func writeClaudeAgentSpecificKey(doc *tomlDocument, key claudeAgentSpecificKey, disable bool) {
	if block, exists := doc.sections[key.expandedSection]; exists {
		writeOrCommentKey(block, key.leafKey, key.value, disable)
		return
	}
	if block, exists := doc.sections[claudeAgentSpecificSection]; exists && key.expandedSection != claudeAgentSpecificSection {
		writeOrCommentKey(block, key.parentDotted, key.value, disable)
		return
	}
	writeOrCommentKey(ensureClaudeSectionBlock(doc), key.claudeDotted, key.value, disable)
}

// writeOrCommentKey sets key = value when disable is true, otherwise comments
// any existing uncommented line for key (inserting nothing when absent). Keys
// in [agents.claude] are placed after "enabled"; in sub-tables the afterKey is
// not found and the key lands just after the section header.
func writeOrCommentKey(block *tomlBlock, key string, value string, disable bool) {
	if disable {
		setKeyValue(block, nil, key, value, "enabled")
		return
	}
	setCommentedKeyLine(block, nil, key, "enabled")
}

// ensureClaudeSectionBlock returns the [agents.claude] block, creating an empty
// one when absent. The section is present in every config derived from the
// template; the create path is a defensive fallback for hand-edited configs.
func ensureClaudeSectionBlock(doc *tomlDocument) *tomlBlock {
	if block, exists := doc.sections[claudeSection]; exists {
		return block
	}
	block := &tomlBlock{name: claudeSection, lines: []string{"[" + claudeSection + "]"}}
	doc.sections[claudeSection] = block
	doc.order = append(doc.order, claudeSection)
	return block
}

func codexFeatureValueFromAgentSpecificBlock(block *tomlBlock, key string) (bool, bool) {
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
	if err := toml.Unmarshal([]byte(strings.Join(block.lines, "\n")), &cfg); err != nil {
		return false, false
	}
	return readCodexFeatureValue(cfg.Agents.Codex.AgentSpecific, key)
}

func isCodexAgentSpecificSection(name string) bool {
	return name == codexAgentSpecificSectionPrefix || strings.HasPrefix(name, codexAgentSpecificSectionPrefix+".")
}

func hasUncommentedKeyWithPrefix(lines []string, prefix string) bool {
	found := false
	walkTomlLinesOutsideMultiline(lines, func(_ int, line string, state tomlStringState) tomlLineWalkResult {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			return tomlLineWalkResult{}
		}
		commentPos, _ := ScanTomlLineForComment(trimmed, state)
		if commentPos >= 0 {
			trimmed = strings.TrimSpace(trimmed[:commentPos])
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			return tomlLineWalkResult{}
		}
		if strings.HasPrefix(strings.TrimSpace(key), prefix) {
			found = true
			return tomlLineWalkResult{stop: true}
		}
		return tomlLineWalkResult{}
	})
	return found
}

func hasUncommentedKeyLine(lines []string, key string) bool {
	found := false
	walkTomlLinesOutsideMultiline(lines, func(_ int, line string, state tomlStringState) tomlLineWalkResult {
		parsed, ok := parseKeyLineWithState(line, key, state)
		if ok && !parsed.commented {
			found = true
			return tomlLineWalkResult{stop: true}
		}
		return tomlLineWalkResult{}
	})
	return found
}

func normalizeLegacySectionAliases(doc *tomlDocument) {
	for legacyName, canonicalName := range legacySectionAliases {
		legacyBlock, hasLegacy := doc.sections[legacyName]
		if !hasLegacy {
			continue
		}

		if _, hasCanonical := doc.sections[canonicalName]; !hasCanonical {
			migrated := cloneBlock(legacyBlock)
			migrated.name = canonicalName
			if len(migrated.lines) > 0 {
				migrated.lines[0] = rewriteSectionHeaderLine(migrated.lines[0], canonicalName)
			}
			doc.sections[canonicalName] = migrated
		}

		delete(doc.sections, legacyName)
		doc.order = applyLegacyAliasToOrder(doc.order, legacyName, canonicalName)
	}
}

func rewriteSectionHeaderLine(line string, sectionName string) string {
	leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	trimmed := strings.TrimSpace(line)
	commentPos, _ := ScanTomlLineForComment(trimmed, tomlStateNone)
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

// parseTomlHeader detects a TOML table header line and extracts its name.
// Handles inline comments like `[section] # comment`.
// Returns the name, whether it's an array-of-table, and a match flag.
func parseTomlHeader(line string) (string, bool, bool) {
	return tomlpatch.ParseHeader(line)
}

// cloneBlock returns a deep copy of a block, including its lines.
func cloneBlock(block *tomlBlock) *tomlBlock {
	if block == nil {
		return nil
	}
	return &tomlBlock{name: block.name, lines: cloneLines(block.lines)}
}

// cloneLines returns a copy of the provided line slice.
func cloneLines(lines []string) []string {
	return tomlpatch.CloneLines(lines)
}

// appendBlock appends a block to the output, inserting a single blank line between blocks.
func appendBlock(output *[]string, block []string) {
	tomlpatch.AppendBlock(output, block)
}

// trimEmptyLines removes leading and trailing blank lines from a block.
func trimEmptyLines(lines []string) []string {
	return tomlpatch.TrimEmptyLines(lines)
}

// trimTrailingEmptyLines removes trailing blank lines from the output.
func trimTrailingEmptyLines(lines []string) []string {
	return tomlpatch.TrimTrailingEmptyLines(lines)
}

// extraSectionBlocks returns non-template section blocks sorted by name.
// sections are from the current config; templateSections defines known canonical sections.
func extraSectionBlocks(sections map[string]*tomlBlock, templateSections map[string]*tomlBlock) []*tomlBlock {
	extra := make([]*tomlBlock, 0)
	for name, block := range sections {
		if _, exists := templateSections[name]; exists {
			continue
		}
		if isCodexAgentSpecificSection(name) {
			continue
		}
		extra = append(extra, cloneBlock(block))
	}
	sort.Slice(extra, func(i, j int) bool {
		return extra[i].name < extra[j].name
	})
	return extra
}

func codexAgentSpecificSectionBlocks(sections map[string]*tomlBlock, templateSections map[string]*tomlBlock) []*tomlBlock {
	extra := make([]*tomlBlock, 0)
	for name, block := range sections {
		if _, exists := templateSections[name]; exists {
			continue
		}
		if !isCodexAgentSpecificSection(name) {
			continue
		}
		extra = append(extra, cloneBlock(block))
	}
	sort.Slice(extra, func(i, j int) bool {
		return extra[i].name < extra[j].name
	})
	return extra
}

// extraArrayBlocks returns non-mcp.servers array-of-table blocks sorted by name.
// arrays are from the current config; returns cloned blocks for arrays not handled by MCP logic.
func extraArrayBlocks(arrays map[string][]*tomlBlock) []*tomlBlock {
	extra := make([]*tomlBlock, 0)
	for name, blocks := range arrays {
		if name == mcpServersSection {
			continue
		}
		for _, block := range blocks {
			extra = append(extra, cloneBlock(block))
		}
	}
	sort.SliceStable(extra, func(i, j int) bool {
		return extra[i].name < extra[j].name
	})
	return extra
}
