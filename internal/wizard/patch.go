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
//  2. Deterministic output: The wizard rewrites config.toml in template-defined order
//     (Decision f7a3c9d). Custom parsing lets us control exact output ordering.
//
//  3. Key positioning: When clearing optional keys (like model=""), we convert them
//     to commented lines rather than deleting them, preserving the template structure.
//
// The go-toml library is still used for syntax validation before processing.
package wizard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	// toml is used for syntax validation only; actual manipulation uses custom
	// line-based parsing to preserve comments and formatting.
	toml "github.com/pelletier/go-toml"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
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

// PatchConfig applies wizard choices to TOML config content.
// content is the current config; choices holds selections; returns updated content or error.
func PatchConfig(content string, choices *Choices) (string, error) {
	if _, err := toml.LoadBytes([]byte(content)); err != nil {
		return "", fmt.Errorf(messages.WizardParseConfigFailedFmt, err)
	}

	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		return "", fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	templateContent := string(templateBytes)

	templateDoc := parseTomlDocument(templateContent)
	currentDoc := parseTomlDocument(content)

	if (choices.EnabledMCPServersTouched || choices.RestoreMissingMCPServers) && len(choices.DefaultMCPServers) == 0 {
		return "", fmt.Errorf(messages.WizardDefaultMCPServersRequired)
	}

	output, err := assembleCanonicalConfig(currentDoc, templateDoc, choices)
	if err != nil {
		return "", err
	}

	return strings.Join(output, "\n"), nil
}

// assembleCanonicalConfig renders updated config content in template order.
// currentDoc holds the existing config; templateDoc provides the canonical ordering; choices supplies wizard selections.
// Returns the ordered lines or an error when required template blocks are missing.
func assembleCanonicalConfig(currentDoc tomlDocument, templateDoc tomlDocument, choices *Choices) ([]string, error) {
	preamble := choosePreamble(currentDoc.preamble, templateDoc.preamble)
	output := make([]string, 0, len(preamble))
	output = append(output, preamble...)

	removeWarnings := choices.WarningsEnabledTouched && !choices.WarningsEnabled

	for _, name := range templateDoc.order {
		if name == "warnings" && removeWarnings {
			continue
		}
		block := selectSectionBlock(currentDoc.sections[name], templateDoc.sections[name])
		if block == nil {
			continue
		}
		updated := cloneBlock(block)
		applySectionUpdates(name, updated, templateDoc.sections[name], choices)
		appendBlock(&output, updated.lines)

		if name == "mcp" {
			serverBlocks, err := buildMCPServerBlocks(currentDoc, templateDoc, choices)
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
	case "approvals":
		if choices.ApprovalModeTouched {
			setKeyValue(block, templateBlock, "mode", formatTomlValue(choices.ApprovalMode), "")
		}
	case "agents.gemini":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentGemini]), "")
		}
		if choices.GeminiModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.GeminiModel, "enabled")
		}
	case "agents.claude":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentClaude]), "")
		}
		if choices.ClaudeModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.ClaudeModel, "enabled")
		}
	case "agents.claude-vscode":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentClaudeVSCode]), "")
		}
	case "agents.codex":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentCodex]), "")
		}
		if choices.CodexModelTouched {
			setOptionalKeyValue(block, templateBlock, "model", choices.CodexModel, "enabled")
		}
		if choices.CodexReasoningTouched {
			setOptionalKeyValue(block, templateBlock, "reasoning_effort", choices.CodexReasoning, "model")
		}
	case "agents.vscode":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentVSCode]), "")
		}
	case "agents.antigravity":
		if choices.EnabledAgentsTouched {
			setKeyValue(block, templateBlock, "enabled", formatTomlValue(choices.EnabledAgents[AgentAntigravity]), "")
		}
	case "warnings":
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
var httpIncompatibleKeys = []string{"command", "args", "env"}

// buildMCPServerBlocks returns ordered MCP server blocks using template order for defaults.
// currentDoc supplies existing blocks; templateDoc provides default blocks; choices controls restore and enabled toggles.
func buildMCPServerBlocks(currentDoc tomlDocument, templateDoc tomlDocument, choices *Choices) ([]tomlBlock, error) {
	currentBlocks := parseMCPBlocks(currentDoc.arrays["mcp.servers"])
	templateBlocks := parseMCPBlocks(templateDoc.arrays["mcp.servers"])

	currentByID := make(map[string]mcpBlock, len(currentBlocks))
	for _, block := range currentBlocks {
		if block.id != "" {
			currentByID[block.id] = block
		}
	}

	templateByID := make(map[string]mcpBlock, len(templateBlocks))
	for _, block := range templateBlocks {
		if block.id != "" {
			templateByID[block.id] = block
		}
	}

	defaultIDs := defaultServerIDs(choices, templateBlocks)
	defaultSet := make(map[string]struct{}, len(defaultIDs))
	for _, id := range defaultIDs {
		defaultSet[id] = struct{}{}
	}

	missingDefaults := make(map[string]struct{}, len(choices.MissingDefaultMCPServers))
	for _, id := range choices.MissingDefaultMCPServers {
		missingDefaults[id] = struct{}{}
	}

	var ordered []tomlBlock
	for _, id := range defaultIDs {
		block, ok := currentByID[id]
		switch {
		case ok:
			tb := updateMCPEnabled(block, templateByID[id], choices, id)
			sanitizeMCPServerBlock(&tb)
			ordered = append(ordered, tb)
		case choices.RestoreMissingMCPServers && containsKey(missingDefaults, id):
			tpl, exists := templateByID[id]
			if !exists {
				return nil, fmt.Errorf(messages.WizardMissingDefaultMCPServerTemplateFmt, id)
			}
			tb := updateMCPEnabled(tpl, tpl, choices, id)
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
		tb := tomlBlock{name: "mcp.servers", lines: cloneLines(block.lines)}
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

// extractMCPBlockKeyValue returns the unquoted value for a key in a TOML block.
// lines are the raw block lines; key is the key to search for.
// Tracks multiline string state to avoid parsing content inside multiline strings.
func extractMCPBlockKeyValue(lines []string, key string) string {
	state := tomlStateNone
	for _, line := range lines {
		if IsTomlStateInMultiline(state) {
			_, state = ScanTomlLineForComment(line, state)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			_, state = ScanTomlLineForComment(line, state)
			continue
		}
		k, value, ok := parseKeyValueWithState(trimmed, key, state)
		if ok && k == key {
			return strings.Trim(value, "\"'")
		}
		_, state = ScanTomlLineForComment(line, state)
	}
	return ""
}

// removeKeyFromBlock removes all uncommented lines for the given key from a block,
// including continuation lines of multiline arrays, inline tables, and triple-quoted strings.
// block is updated in place; commented-out lines for the key are preserved.
// Tracks multiline string state to avoid matching content inside multiline strings.
func removeKeyFromBlock(block *tomlBlock, key string) {
	type lineRange struct{ start, end int }
	state := tomlStateNone
	var ranges []lineRange
	for i := 0; i < len(block.lines); i++ {
		line := block.lines[i]
		if IsTomlStateInMultiline(state) {
			_, state = ScanTomlLineForComment(line, state)
			continue
		}
		parsed, ok := parseKeyLineWithState(line, key, state)
		_, state = ScanTomlLineForComment(line, state)
		if ok && !parsed.commented {
			endIdx := multilineValueEndIndex(block.lines, i)
			ranges = append(ranges, lineRange{i, endIdx})
			// Advance state through any skipped continuation lines.
			for j := i + 1; j <= endIdx && j < len(block.lines); j++ {
				_, state = ScanTomlLineForComment(block.lines[j], state)
			}
			i = endIdx
		}
	}
	// Remove in reverse order to avoid index shifting.
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		block.lines = append(block.lines[:r.start], block.lines[r.end+1:]...)
	}
}

// multilineValueEndIndex returns the index of the last line of a value that
// starts at startIdx. Returns startIdx when the value fits on a single line.
// Handles multiline arrays ([...]), inline tables ({...}), and triple-quoted
// strings ("""/”').
func multilineValueEndIndex(lines []string, startIdx int) int {
	if startIdx >= len(lines) {
		return startIdx
	}
	line := lines[startIdx]
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return startIdx
	}
	valuePart := strings.TrimSpace(line[eqIdx+1:])

	// Multiline basic string (""")
	// In basic strings, \" is an escaped quote; \""" must not be treated
	// as a closing delimiter (it is an escaped quote followed by two quotes).
	if strings.HasPrefix(valuePart, `"""`) {
		rest := valuePart[3:]
		if containsUnescapedTripleQuote(rest) {
			return startIdx // closed on same line
		}
		for i := startIdx + 1; i < len(lines); i++ {
			if containsUnescapedTripleQuote(lines[i]) {
				return i
			}
		}
		return startIdx // unclosed — don't remove extra lines
	}

	// Multiline literal string (''')
	// Literal strings have no escape sequences, so ''' always closes.
	if strings.HasPrefix(valuePart, `'''`) {
		rest := valuePart[3:]
		if strings.Contains(rest, `'''`) {
			return startIdx
		}
		for i := startIdx + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], `'''`) {
				return i
			}
		}
		return startIdx
	}

	// Array or inline table — track bracket depth, skipping brackets inside strings.
	var opener, closer byte
	switch {
	case strings.HasPrefix(valuePart, "["):
		opener, closer = '[', ']'
	case strings.HasPrefix(valuePart, "{"):
		opener, closer = '{', '}'
	default:
		return startIdx // simple scalar value
	}

	depth := 0
	for i := startIdx; i < len(lines); i++ {
		from := 0
		if i == startIdx {
			from = eqIdx + 1
		}
		depth += countBracketDepth(lines[i][from:], opener, closer)
		if depth <= 0 {
			return i
		}
	}
	return startIdx // unbalanced — don't remove extra lines
}

// countBracketDepth counts the net bracket depth change in a line, skipping
// brackets inside quoted strings and stopping at unquoted # comments.
func countBracketDepth(s string, opener, closer byte) int {
	depth := 0
	inDouble := false
	inSingle := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inDouble {
			if ch == '\\' {
				i++ // skip escaped character
				continue
			}
			if ch == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if ch == '\'' {
				inSingle = false
			}
			continue
		}
		switch ch {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case '#':
			return depth // rest of line is a comment
		case opener:
			depth++
		case closer:
			depth--
		}
	}
	return depth
}

// containsUnescapedTripleQuote reports whether s contains an unescaped """ delimiter.
// In TOML basic strings, \" is an escaped quote; an odd number of preceding
// backslashes means the first quote is escaped, so \""" is not a closing delimiter.
func containsUnescapedTripleQuote(s string) bool {
	for search := s; ; {
		idx := strings.Index(search, `"""`)
		if idx < 0 {
			return false
		}
		// Count backslashes immediately before the triple quote.
		backslashes := 0
		for i := idx - 1; i >= 0 && search[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return true // even (or zero) backslashes — unescaped delimiter
		}
		// Odd backslashes — first quote is escaped; advance past this match.
		search = search[idx+1:]
	}
}

// updateMCPEnabled applies the enabled toggle to a server block when requested.
// block holds the current server text; templateBlock provides canonical formatting; id identifies the server.
func updateMCPEnabled(block mcpBlock, templateBlock mcpBlock, choices *Choices, id string) tomlBlock {
	updated := tomlBlock{name: "mcp.servers", lines: cloneLines(block.lines)}
	if choices.EnabledMCPServersTouched {
		tpl := (*tomlBlock)(nil)
		if len(templateBlock.lines) > 0 {
			tpl = &tomlBlock{name: "mcp.servers", lines: cloneLines(templateBlock.lines)}
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
	commentPos, _ := ScanTomlLineForComment(line, state)
	clean := line
	if commentPos >= 0 {
		clean = strings.TrimSpace(line[:commentPos])
	}
	if !strings.HasPrefix(clean, key) {
		return "", "", false
	}
	rest := strings.TrimSpace(clean[len(key):])
	if !strings.HasPrefix(rest, "=") {
		return "", "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(rest, "="))
	return key, value, true
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
	if templateBlock != nil {
		if templateLine, ok := findKeyLine(templateBlock.lines, key); ok {
			commentedLine := ensureCommented(templateLine.raw)
			replaceOrInsertLine(block, key, commentedLine, afterKey)
			return
		}
	}
	if existingLine, ok := findKeyLine(block.lines, key); ok {
		commentedLine := ensureCommented(existingLine.raw)
		replaceOrInsertLine(block, key, commentedLine, afterKey)
	}
}

// setKeyValue updates or inserts a key/value line in a section block.
// block is updated in place; templateBlock provides canonical formatting; afterKey controls insertion order.
func setKeyValue(block *tomlBlock, templateBlock *tomlBlock, key string, value string, afterKey string) {
	var base keyLine
	if templateBlock != nil {
		if templateLine, ok := findKeyLine(templateBlock.lines, key); ok {
			base = templateLine
		}
	}
	if base.raw == "" {
		if existingLine, ok := findKeyLine(block.lines, key); ok {
			base = existingLine
		}
	}
	if base.raw == "" {
		newLine := buildKeyLine(keyLine{indent: ""}, key, value, false)
		replaceOrInsertLine(block, key, newLine, afterKey)
		return
	}

	newLine := buildKeyLine(base, key, value, false)
	replaceOrInsertLine(block, key, newLine, afterKey)
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
	state := tomlStateNone
	for _, line := range lines {
		// Skip lines inside multiline strings.
		if IsTomlStateInMultiline(state) {
			_, state = ScanTomlLineForComment(line, state)
			continue
		}
		parsed, ok := parseKeyLineWithState(line, key, state)
		if ok {
			return parsed, true
		}
		_, state = ScanTomlLineForComment(line, state)
	}
	return keyLine{}, false
}

// parseKeyLineWithState parses a key/value assignment line with explicit state tracking.
// Returns false when the line does not define the requested key.
func parseKeyLineWithState(line string, key string, state tomlStringState) (keyLine, bool) {
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	trimmed := strings.TrimLeft(line[indentLen:], " \t")
	commented := false
	if strings.HasPrefix(trimmed, "#") {
		commented = true
		trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, "#"), " \t")
	}
	if !strings.HasPrefix(trimmed, key) {
		return keyLine{}, false
	}
	rest := strings.TrimSpace(trimmed[len(key):])
	if !strings.HasPrefix(rest, "=") {
		return keyLine{}, false
	}
	inlineComment := extractInlineCommentWithState(trimmed, state)
	return keyLine{raw: line, indent: indent, commented: commented, inlineComment: inlineComment}, true
}

// extractInlineCommentWithState returns the inline comment portion with explicit state tracking.
func extractInlineCommentWithState(line string, state tomlStringState) string {
	commentPos, _ := ScanTomlLineForComment(line, state)
	if commentPos < 0 {
		return ""
	}
	return strings.TrimSpace(line[commentPos:])
}

// buildKeyLine renders a key/value line using indentation and inline comment from base.
func buildKeyLine(base keyLine, key string, value string, commented bool) string {
	indent := base.indent
	prefix := ""
	if commented {
		prefix = "# "
	}
	line := fmt.Sprintf("%s%s%s = %s", indent, prefix, key, value)
	if base.inlineComment != "" {
		line += " " + base.inlineComment
	}
	return line
}

// ensureCommented returns the line with a leading comment marker.
func ensureCommented(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return line
	}
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	return indent + "# " + strings.TrimLeft(line[indentLen:], " \t")
}

// replaceOrInsertLine replaces an existing key line or inserts a new line after afterKey.
// block is updated in place; duplicates are removed to keep a single key occurrence.
// Tracks multiline string state to avoid matching content inside multiline strings.
func replaceOrInsertLine(block *tomlBlock, key string, newLine string, afterKey string) {
	var matches []int
	uncommentedIndex := -1
	state := tomlStateNone
	for i, line := range block.lines {
		// Skip lines inside multiline strings.
		if IsTomlStateInMultiline(state) {
			_, state = ScanTomlLineForComment(line, state)
			continue
		}
		parsed, ok := parseKeyLineWithState(line, key, state)
		_, state = ScanTomlLineForComment(line, state)
		if !ok {
			continue
		}
		matches = append(matches, i)
		if !parsed.commented && uncommentedIndex == -1 {
			uncommentedIndex = i
		}
	}
	if len(matches) > 0 {
		replaceAt := matches[0]
		if uncommentedIndex >= 0 {
			replaceAt = uncommentedIndex
		}
		block.lines[replaceAt] = newLine
		// Remove duplicate key lines in reverse order to avoid index shifting.
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] == replaceAt {
				continue
			}
			block.lines = append(block.lines[:matches[i]], block.lines[matches[i]+1:]...)
		}
		return
	}
	insertAt := findInsertIndex(block.lines, afterKey)
	block.lines = append(block.lines[:insertAt], append([]string{newLine}, block.lines[insertAt:]...)...)
}

// findInsertIndex returns the line index to insert a new key line after afterKey.
// lines should include the section header as the first entry.
// Tracks multiline string state to avoid matching content inside multiline strings.
func findInsertIndex(lines []string, afterKey string) int {
	if len(lines) == 0 {
		return 0
	}
	if afterKey != "" {
		state := tomlStateNone
		for i, line := range lines {
			// Skip lines inside multiline strings.
			if IsTomlStateInMultiline(state) {
				_, state = ScanTomlLineForComment(line, state)
				continue
			}
			if _, ok := parseKeyLineWithState(line, afterKey, state); ok {
				return i + 1
			}
			_, state = ScanTomlLineForComment(line, state)
		}
	}
	if len(lines) > 0 {
		return 1
	}
	return 0
}

// formatTomlValue converts a scalar value into a TOML literal string.
func formatTomlValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseTomlDocument splits TOML content into preamble lines, section blocks, and array-of-table blocks.
// Returns the parsed document with section order based on appearance.
func parseTomlDocument(content string) tomlDocument {
	lines := strings.Split(content, "\n")
	sections := make(map[string]*tomlBlock)
	arrays := make(map[string][]*tomlBlock)
	var order []string
	var preamble []string
	var current *tomlBlock
	var currentIsArray bool

	flush := func() {
		if current == nil {
			return
		}
		if currentIsArray {
			arrays[current.name] = append(arrays[current.name], current)
		} else {
			if _, exists := sections[current.name]; !exists {
				sections[current.name] = current
				order = append(order, current.name)
			}
		}
		current = nil
		currentIsArray = false
	}

	for _, line := range lines {
		name, isArray, ok := parseTomlHeader(line)
		if ok {
			flush()
			current = &tomlBlock{name: name, lines: []string{line}}
			currentIsArray = isArray
			continue
		}
		if current == nil {
			preamble = append(preamble, line)
			continue
		}
		current.lines = append(current.lines, line)
	}
	flush()

	return tomlDocument{
		preamble: preamble,
		sections: sections,
		arrays:   arrays,
		order:    order,
	}
}

// parseTomlHeader detects a TOML table header line and extracts its name.
// Handles inline comments like `[section] # comment`.
// Returns the name, whether it's an array-of-table, and a match flag.
func parseTomlHeader(line string) (string, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false, false
	}
	// Strip inline comment before checking brackets.
	commentPos, _ := ScanTomlLineForComment(trimmed, tomlStateNone)
	if commentPos >= 0 {
		trimmed = strings.TrimSpace(trimmed[:commentPos])
	}
	if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "[["), "]]"))
		return name, true, name != ""
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
		return name, false, name != ""
	}
	return "", false, false
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
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

// appendBlock appends a block to the output, inserting a single blank line between blocks.
func appendBlock(output *[]string, block []string) {
	trimmed := trimEmptyLines(block)
	if len(trimmed) == 0 {
		return
	}
	if len(*output) > 0 && (*output)[len(*output)-1] != "" {
		*output = append(*output, "")
	}
	*output = append(*output, trimmed...)
}

// trimEmptyLines removes leading and trailing blank lines from a block.
func trimEmptyLines(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// trimTrailingEmptyLines removes trailing blank lines from the output.
func trimTrailingEmptyLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// extraSectionBlocks returns non-template section blocks sorted by name.
// sections are from the current config; templateSections defines known canonical sections.
func extraSectionBlocks(sections map[string]*tomlBlock, templateSections map[string]*tomlBlock) []*tomlBlock {
	extra := make([]*tomlBlock, 0)
	for name, block := range sections {
		if _, exists := templateSections[name]; exists {
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
		if name == "mcp.servers" {
			continue
		}
		for _, block := range blocks {
			extra = append(extra, cloneBlock(block))
		}
	}
	sort.Slice(extra, func(i, j int) bool {
		if extra[i].name != extra[j].name {
			return extra[i].name < extra[j].name
		}
		// Preserve original order within the same array name
		return false
	})
	return extra
}

// containsKey reports whether the key is present in the set.
func containsKey(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}
