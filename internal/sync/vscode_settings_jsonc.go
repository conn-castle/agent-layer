package sync

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// renderVSCodeSettingsContent merges the managed settings block into existing JSONC content.
// Args: sys marshals settings, existing is the current file contents, settings is the managed config.
// Returns: updated content with a trailing newline, or an error if the managed block is malformed
// or the root object is invalid when the block is missing.
func renderVSCodeSettingsContent(sys System, existing string, settings *vscodeSettings) (string, error) {
	newline := detectNewline(existing)
	normalized := normalizeNewlines(existing)
	bom, stripped := stripUTF8BOM(normalized)
	normalized = stripped

	if strings.TrimSpace(normalized) == "" {
		blockLines, err := buildVSCodeManagedBlock(sys, settings, "  ", "  ", false)
		if err != nil {
			return "", err
		}
		lines := append([]string{"{"}, blockLines...)
		lines = append(lines, "}")
		return applyNewlineStyle(bom+strings.Join(lines, "\n")+"\n", newline), nil
	}

	lines := strings.Split(normalized, "\n")
	blockStart, blockEnd, blockIndent, found, err := findVSCodeManagedBlock(lines, 0, len(lines)-1)
	if err != nil {
		return "", invalidVSCodeSettingsError(err.Error())
	}

	if found {
		indentBase := blockIndent
		if indentBase == "" {
			indentBase = detectVSCodeIndent(lines, 0, len(lines)-1)
		}
		if indentBase == "" {
			indentBase = "  "
		}
		indentUnit := indentBase
		needsTrailingComma := hasJSONCContentBetween(lines, blockEnd+1, 0, len(lines)-1, len(lines[len(lines)-1])-1)
		// Reuse a separator after the marker for the last moved property, or for the
		// regenerated managed block when no user properties move.
		following := strings.Join(lines[blockEnd+1:], "\n")
		firstToken, triviaErr := skipJSONCTrivia(following, 0)
		if triviaErr == nil && firstToken < len(following) && following[firstToken] == ',' {
			needsTrailingComma = false
		}

		// VS Code appends new settings after the last property, which lands them inside a
		// trailing managed block; keep them by moving them to just after the block.
		userEntries, movesProperty, err := extractVSCodeUserEntries(strings.Join(lines[blockStart+1:blockEnd], "\n"))
		if err != nil {
			return "", invalidVSCodeSettingsError("managed block: " + err.Error())
		}

		blockLines, err := buildVSCodeManagedBlock(sys, settings, indentBase, indentUnit, needsTrailingComma || movesProperty)
		if err != nil {
			return "", err
		}
		blockLines = append(blockLines, renderVSCodeUserEntries(userEntries, indentBase, needsTrailingComma)...)
		lines = replaceVSCodeManagedBlock(lines, blockStart, blockEnd, blockLines)
		updated := bom + strings.Join(lines, "\n")
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		return applyNewlineStyle(updated, newline), nil
	}

	startIdx, endIdx, err := findJSONCRootBounds(normalized)
	if err != nil {
		return "", invalidVSCodeSettingsError(err.Error())
	}
	if hasJSONCNonTrivia(normalized, 0, startIdx) {
		return "", invalidVSCodeSettingsError("unexpected content before root object")
	}
	if hasJSONCNonTrivia(normalized, endIdx+1, len(normalized)) {
		return "", invalidVSCodeSettingsError("unexpected content after root object")
	}
	startLine, startCol := indexToLineCol(normalized, startIdx)
	endLine, endCol := indexToLineCol(normalized, endIdx)

	indentBase := detectVSCodeIndent(lines, startLine, endLine)
	if indentBase == "" {
		indentBase = "  "
	}
	indentUnit := indentBase
	needsTrailingComma := hasJSONCContentBetween(lines, startLine, startCol+1, endLine, endCol)

	blockLines, err := buildVSCodeManagedBlock(sys, settings, indentBase, indentUnit, needsTrailingComma)
	if err != nil {
		return "", err
	}

	lines = insertVSCodeManagedBlock(lines, startLine, startCol, blockLines)
	updated := bom + strings.Join(lines, "\n")
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	return applyNewlineStyle(updated, newline), nil
}

// detectNewline returns the newline sequence used in the content.
// Args: content is the original file content.
// Returns: "\r\n", "\r", or "\n".
func detectNewline(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	if strings.Contains(content, "\r") {
		return "\r"
	}
	return "\n"
}

// normalizeNewlines converts all line endings to "\n".
// Args: content is the original file content.
// Returns: content with normalized line endings.
func normalizeNewlines(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}

// applyNewlineStyle replaces "\n" line endings with the target newline sequence.
// Args: content is the normalized content, newline is the target line ending.
// Returns: content using the desired newline sequence.
func applyNewlineStyle(content, newline string) string {
	if newline == "\n" {
		return content
	}
	return strings.ReplaceAll(content, "\n", newline)
}

// stripUTF8BOM removes a UTF-8 BOM if present.
// Args: content is the normalized content.
// Returns: the BOM (if any) and the content without it.
func stripUTF8BOM(content string) (string, string) {
	if strings.HasPrefix(content, "\ufeff") {
		return "\ufeff", strings.TrimPrefix(content, "\ufeff")
	}
	return "", content
}

// findJSONCRootBounds locates the root object bounds in JSONC content.
// Args: content is normalized JSONC text.
// Returns: start and end indices for the root object, or an error if invalid.
func findJSONCRootBounds(content string) (int, int, error) {
	start := -1
	depth := 0
	inString := false
	inLineComment := false
	inBlockComment := false
	escaped := false

	for i := 0; i < len(content); i++ {
		ch := content[i]
		next := byte(0)
		if i+1 < len(content) {
			next = content[i+1]
		}

		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}

		if ch == '{' {
			if start == -1 {
				start = i
				depth = 1
				continue
			}
			depth++
			continue
		}
		if ch == '}' {
			if start == -1 {
				continue
			}
			depth--
			if depth == 0 {
				return start, i, nil
			}
			if depth < 0 {
				return -1, -1, fmt.Errorf("unexpected closing brace")
			}
		}
	}

	if start == -1 {
		return -1, -1, fmt.Errorf("missing root object")
	}
	return -1, -1, fmt.Errorf("unterminated root object")
}

// hasJSONCNonTrivia reports whether non-whitespace, non-comment tokens exist in the range.
// Args: content is normalized JSONC text; start/end bound the scan range (end is exclusive).
// Returns: true if any non-comment, non-whitespace character is found.
func hasJSONCNonTrivia(content string, start, end int) bool {
	if start < 0 {
		start = 0
	}
	if end > len(content) {
		end = len(content)
	}
	if end <= start {
		return false
	}

	inLineComment := false
	inBlockComment := false
	for i := start; i < end; i++ {
		ch := content[i]
		next := byte(0)
		if i+1 < end {
			next = content[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}

		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			continue
		}

		return true
	}

	return false
}

// indexToLineCol converts a byte index to line and column positions.
// Args: content is normalized text, idx is a byte index into content.
// Returns: zero-based line and column numbers.
func indexToLineCol(content string, idx int) (int, int) {
	if idx <= 0 {
		return 0, 0
	}
	prefix := content[:idx]
	line := strings.Count(prefix, "\n")
	lastNewline := strings.LastIndex(prefix, "\n")
	if lastNewline == -1 {
		return line, idx
	}
	return line, idx - lastNewline - 1
}

// findVSCodeManagedBlock locates the managed block markers within the provided bounds.
// Args: lines are normalized content lines, startLine/endLine bound the scan range.
// Returns: block start/end line indices, indent, found flag, or error if malformed.
func findVSCodeManagedBlock(lines []string, startLine, endLine int) (int, int, string, bool, error) {
	start := -1
	end := -1
	indent := ""

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == vscodeSettingsManagedStart {
			if start != -1 {
				return -1, -1, "", false, fmt.Errorf("duplicate managed block start")
			}
			start = i
			indent = leadingWhitespace(line)
		}
		if trimmed == vscodeSettingsManagedEnd {
			if end != -1 {
				return -1, -1, "", false, fmt.Errorf("duplicate managed block end")
			}
			end = i
		}
	}

	if start == -1 && end == -1 {
		return -1, -1, "", false, nil
	}
	if start == -1 || end == -1 {
		return -1, -1, "", false, fmt.Errorf("managed block markers are incomplete")
	}
	if end < start {
		return -1, -1, "", false, fmt.Errorf("managed block end appears before start")
	}
	if start < startLine || end > endLine {
		return -1, -1, "", false, fmt.Errorf("managed block is outside scan range")
	}

	return start, end, indent, true, nil
}

// detectVSCodeIndent finds the indentation used for root-level properties.
// Args: lines are normalized content lines, startLine/endLine bound the root object.
// Returns: the leading whitespace used for root-level properties, or empty if unknown.
func detectVSCodeIndent(lines []string, startLine, endLine int) string {
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	for i := startLine + 1; i <= endLine && i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		return leadingWhitespace(lines[i])
	}
	return ""
}

// hasJSONCContentBetween checks for non-comment content between two positions.
// Args: lines are normalized content lines; startLine/startCol and endLine/endCol bound the scan region.
// Returns: true if a non-comment, non-whitespace token other than ',' or '}' appears before a '}' token.
func hasJSONCContentBetween(lines []string, startLine, startCol, endLine, endCol int) bool {
	if len(lines) == 0 {
		return false
	}
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	if endLine < startLine {
		return false
	}

	inBlockComment := false
	inString := false
	escaped := false
	for lineIdx := startLine; lineIdx <= endLine; lineIdx++ {
		line := lines[lineIdx]
		lineStart := 0
		lineEnd := len(line)
		if lineIdx == startLine && startCol > lineStart {
			if startCol >= lineEnd {
				continue
			}
			lineStart = startCol
		}
		if lineIdx == endLine && endCol >= 0 && endCol < lineEnd {
			lineEnd = endCol + 1
		}
		if lineStart >= lineEnd {
			continue
		}
		for i := lineStart; i < lineEnd; i++ {
			ch := line[i]
			next := byte(0)
			if i+1 < lineEnd {
				next = line[i+1]
			}

			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}

			if inBlockComment {
				if ch == '*' && next == '/' {
					inBlockComment = false
					i++
				}
				continue
			}

			if ch == '/' && next == '*' {
				inBlockComment = true
				i++
				continue
			}
			if ch == '/' && next == '/' {
				break
			}
			if ch == '"' {
				inString = true
				continue
			}
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				continue
			}
			if ch == ',' {
				continue
			}
			if ch == '}' {
				return false
			}
			return true
		}
	}

	return false
}

// leadingWhitespace returns the leading spaces or tabs from a line.
// Args: line is a single content line.
// Returns: the leading whitespace substring.
func leadingWhitespace(line string) string {
	i := 0
	for i < len(line) {
		if line[i] != ' ' && line[i] != '\t' {
			break
		}
		i++
	}
	return line[:i]
}

// buildVSCodeManagedBlock builds the managed block lines for VS Code settings JSONC.
// Args: sys marshals settings, indentBase is the root-level indent, indentUnit is one indent level,
// needsTrailingComma indicates whether a trailing comma is required after the managed block.
// Returns: block lines including start/end markers, or an error.
func buildVSCodeManagedBlock(sys System, settings *vscodeSettings, indentBase, indentUnit string, needsTrailingComma bool) ([]string, error) {
	if indentUnit == "" {
		indentUnit = "  "
	}
	data, err := sys.MarshalIndent(settings, "", indentUnit)
	if err != nil {
		return nil, fmt.Errorf(messages.SyncMarshalVSCodeSettingsFailedFmt, err)
	}

	raw := strings.TrimRight(string(data), "\n")
	var innerLines []string
	if strings.TrimSpace(raw) != "{}" {
		lines := strings.Split(raw, "\n")
		if len(lines) < 2 {
			return nil, fmt.Errorf("unexpected settings JSON shape")
		}
		if strings.TrimSpace(lines[0]) != "{" || strings.TrimSpace(lines[len(lines)-1]) != "}" {
			return nil, fmt.Errorf("unexpected settings JSON shape")
		}
		innerLines = lines[1 : len(lines)-1]
	}

	block := []string{indentBase + vscodeSettingsManagedStart}
	for _, line := range vscodeSettingsManagedHeader {
		block = append(block, indentBase+line)
	}
	if len(innerLines) > 0 {
		managed := make([]string, 0, len(innerLines))
		for _, line := range innerLines {
			line = strings.TrimPrefix(line, indentUnit)
			managed = append(managed, indentBase+line)
		}
		if needsTrailingComma {
			lastIdx := len(managed) - 1
			trimmed := strings.TrimRight(managed[lastIdx], " \t")
			if trimmed != "" && !strings.HasSuffix(trimmed, ",") {
				managed[lastIdx] += ","
			}
		}
		block = append(block, managed...)
	}
	block = append(block, indentBase+vscodeSettingsManagedEnd)

	return block, nil
}

// vscodeBlockEntry is user content found inside the managed block: a property with its
// comments, or comments that no property follows.
type vscodeBlockEntry struct {
	comments []string // leading comment lines, verbatim
	property string   // `"key": value` source text; empty for a comment-only entry
	trailing string   // comment after the value on the same line
}

// extractVSCodeUserEntries scans the lines between the managed block markers and returns
// the content Agent Layer does not own, in source order.
// Args: text is the normalized block content without the marker lines.
// Returns: the user entries, whether any of them is a property, or an error if the content
// is not a sequence of JSONC object properties.
func extractVSCodeUserEntries(text string) ([]vscodeBlockEntry, bool, error) {
	managedKeys := vscodeManagedKeys()
	var entries []vscodeBlockEntry
	hasProperty := false
	first := true
	needsComma := false
	var leading strings.Builder
	pos := 0
	for {
		start := pos
		next, err := skipJSONCTrivia(text, pos)
		if err != nil {
			return nil, false, err
		}
		leading.WriteString(text[start:next])
		pos = next
		if pos == len(text) {
			break
		}
		if text[pos] == ',' {
			if !needsComma {
				return nil, false, fmt.Errorf("unexpected ','")
			}
			needsComma = false
			pos++
			continue
		}
		if text[pos] != '"' {
			return nil, false, fmt.Errorf("expected property name")
		}

		keyEnd, err := scanJSONCString(text, pos)
		if err != nil {
			return nil, false, err
		}
		var key string
		if err := json.Unmarshal([]byte(text[pos:keyEnd]), &key); err != nil {
			return nil, false, fmt.Errorf("invalid property name %s", text[pos:keyEnd])
		}
		colon, err := skipJSONCTrivia(text, keyEnd)
		if err != nil {
			return nil, false, err
		}
		if colon == len(text) || text[colon] != ':' {
			return nil, false, fmt.Errorf("expected ':' after property %q", key)
		}
		valueStart, err := skipJSONCTrivia(text, colon+1)
		if err != nil {
			return nil, false, err
		}
		valueEnd, err := scanJSONCValue(text, valueStart)
		if err != nil {
			return nil, false, fmt.Errorf("property %q: %w", key, err)
		}
		trailing, hadComma, after, err := scanJSONCTrailing(text, valueEnd)
		if err != nil {
			return nil, false, err
		}

		comments := vscodeCommentLines(leading.String(), first)
		if !managedKeys[key] {
			entries = append(entries, vscodeBlockEntry{comments: comments, property: text[pos:valueEnd], trailing: trailing})
			hasProperty = true
		}
		leading.Reset()
		first = false
		needsComma = !hadComma
		pos = after
	}

	if comments := vscodeCommentLines(leading.String(), first); len(comments) > 0 {
		entries = append(entries, vscodeBlockEntry{comments: comments})
	}
	return entries, hasProperty, nil
}

// renderVSCodeUserEntries renders user entries moved out of the managed block.
// Args: entries are the moved entries, indent is the root-level indent, needsTrailingComma
// reports whether the last moved property needs a separator added for following content.
// Returns: the lines to place after the managed block end marker.
func renderVSCodeUserEntries(entries []vscodeBlockEntry, indent string, needsTrailingComma bool) []string {
	lastProperty := -1
	for i, entry := range entries {
		if entry.property != "" {
			lastProperty = i
		}
	}
	var out []string
	for i, entry := range entries {
		out = append(out, entry.comments...)
		if entry.property == "" {
			continue
		}
		property := indent + entry.property
		if i < lastProperty || needsTrailingComma {
			property += ","
		}
		if entry.trailing != "" {
			property += " " + entry.trailing
		}
		out = append(out, strings.Split(property, "\n")...)
	}
	return out
}

// vscodeManagedKeys returns the setting keys the managed block owns: every field Agent Layer
// writes plus the retired keys earlier releases wrote.
func vscodeManagedKeys() map[string]bool {
	keys := make(map[string]bool)
	settingsType := reflect.TypeOf(vscodeSettings{})
	for i := 0; i < settingsType.NumField(); i++ {
		name, _, _ := strings.Cut(settingsType.Field(i).Tag.Get("json"), ",")
		keys[name] = true
	}
	for _, key := range vscodeRetiredManagedKeys {
		keys[key] = true
	}
	return keys
}

// vscodeCommentLines converts trivia before a block entry into comment lines to keep.
// Args: trivia is whitespace and comments; stripHeader removes the managed block header lines,
// which precede the first entry.
// Returns: the trivia lines without surrounding blank lines, or nil if only whitespace remains.
func vscodeCommentLines(trivia string, stripHeader bool) []string {
	lines := strings.Split(trivia, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if stripHeader && slices.Contains(vscodeSettingsManagedHeader, strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	for len(kept) > 0 && kept[0] == "" {
		kept = kept[1:]
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// skipJSONCTrivia skips whitespace and comments.
// Args: text is normalized JSONC, pos is the start index.
// Returns: the index of the next token or len(text), or an error for an unterminated comment.
func skipJSONCTrivia(text string, pos int) (int, error) {
	for pos < len(text) {
		switch {
		case text[pos] == ' ' || text[pos] == '\t' || text[pos] == '\n':
			pos++
		case strings.HasPrefix(text[pos:], "//"):
			end := strings.IndexByte(text[pos:], '\n')
			if end == -1 {
				return len(text), nil
			}
			pos += end
		case strings.HasPrefix(text[pos:], "/*"):
			end := strings.Index(text[pos+2:], "*/")
			if end == -1 {
				return 0, fmt.Errorf("unterminated block comment")
			}
			pos += end + 4
		default:
			return pos, nil
		}
	}
	return pos, nil
}

// scanJSONCTrailing scans what follows a property value on its line: an optional comma and comments.
// Args: text is normalized JSONC, pos is the index after the value.
// Returns: the trailing comments, whether a comma was consumed, the index where the next entry's
// trivia starts, or an error for an unterminated comment.
func scanJSONCTrailing(text string, pos int) (string, bool, int, error) {
	var comment strings.Builder
	hadComma := false
	for pos < len(text) {
		switch {
		case text[pos] == ' ' || text[pos] == '\t':
			comment.WriteByte(text[pos])
			pos++
		case text[pos] == ',' && !hadComma:
			hadComma = true
			pos++
		case strings.HasPrefix(text[pos:], "//"):
			end := strings.IndexByte(text[pos:], '\n')
			if end == -1 {
				end = len(text) - pos
			}
			comment.WriteString(text[pos : pos+end])
			pos += end
		case strings.HasPrefix(text[pos:], "/*"):
			end := strings.Index(text[pos+2:], "*/")
			if end == -1 {
				return "", false, 0, fmt.Errorf("unterminated block comment")
			}
			comment.WriteString(text[pos : pos+end+4])
			pos += end + 4
		default:
			if text[pos] == '\n' {
				pos++
			}
			return strings.TrimSpace(comment.String()), hadComma, pos, nil
		}
	}
	return strings.TrimSpace(comment.String()), hadComma, pos, nil
}

// scanJSONCString scans and validates a string token.
// Args: text is normalized JSONC, pos is the index of the opening quote.
// Returns: the index after the closing quote, or an error if the string is invalid or unterminated.
func scanJSONCString(text string, pos int) (int, error) {
	for i := pos + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			if !json.Valid([]byte(text[pos : i+1])) {
				return 0, fmt.Errorf("invalid string")
			}
			return i + 1, nil
		case '\n':
			return 0, fmt.Errorf("unterminated string")
		}
	}
	return 0, fmt.Errorf("unterminated string")
}

// scanJSONCValue scans one JSONC value: a string, an object or array, or a bare literal.
// Args: text is normalized JSONC, pos is the index of the value's first character.
// Returns: the index after the value, or an error if no complete value starts at pos.
func scanJSONCValue(text string, pos int) (int, error) {
	if pos == len(text) {
		return 0, fmt.Errorf("missing value")
	}
	switch text[pos] {
	case '"':
		return scanJSONCString(text, pos)
	case '{', '[':
		return scanJSONCContainer(text, pos)
	case ',', '}', ']':
		return 0, fmt.Errorf("missing value")
	}
	end := pos
	for end < len(text) && !strings.ContainsRune(" \t\n,:{}[]\"/", rune(text[end])) {
		end++
	}
	if end == pos {
		return 0, fmt.Errorf("unexpected %q", text[pos])
	}
	return end, nil
}

// scanJSONCContainer validates the properties or elements of an object or array.
// Args: text is normalized JSONC, pos is the opening brace or bracket.
// Returns: the index after the closing delimiter, or an error for malformed entries.
// Comments, trailing commas, bare literals, and missing commas between object properties
// are accepted; the source text is left untouched.
func scanJSONCContainer(text string, pos int) (int, error) {
	object := text[pos] == '{'
	closer := byte(']')
	if object {
		closer = '}'
	}
	pos++
	for {
		next, err := skipJSONCTrivia(text, pos)
		if err != nil {
			return 0, err
		}
		pos = next
		if pos == len(text) {
			return 0, fmt.Errorf("unterminated value")
		}
		if text[pos] == closer {
			return pos + 1, nil
		}
		if object {
			if text[pos] != '"' {
				return 0, fmt.Errorf("expected property name")
			}
			keyEnd, err := scanJSONCString(text, pos)
			if err != nil {
				return 0, err
			}
			colon, err := skipJSONCTrivia(text, keyEnd)
			if err != nil {
				return 0, err
			}
			if colon == len(text) || text[colon] != ':' {
				return 0, fmt.Errorf("expected ':' after property name")
			}
			pos, err = skipJSONCTrivia(text, colon+1)
			if err != nil {
				return 0, err
			}
		}
		end, err := scanJSONCValue(text, pos)
		if err != nil {
			return 0, err
		}
		pos, err = skipJSONCTrivia(text, end)
		if err != nil {
			return 0, err
		}
		if pos == len(text) {
			return 0, fmt.Errorf("unterminated value")
		}
		switch text[pos] {
		case closer:
			return pos + 1, nil
		case ',':
			pos++
		default:
			if !object || text[pos] != '"' {
				return 0, fmt.Errorf("expected ',' or %q", closer)
			}
		}
	}
}

// replaceVSCodeManagedBlock replaces the existing managed block with updated lines.
// Args: lines are normalized content lines, start/end are block line indices, blockLines are replacements.
// Returns: updated lines with the managed block replaced.
func replaceVSCodeManagedBlock(lines []string, start, end int, blockLines []string) []string {
	updated := append([]string{}, lines[:start]...)
	updated = append(updated, blockLines...)
	updated = append(updated, lines[end+1:]...)
	return updated
}

// insertVSCodeManagedBlock inserts the managed block after the root object opening brace.
// Args: lines are normalized content lines, startLine/startCol locate the root '{', blockLines are the block.
// Returns: updated lines with the block inserted.
func insertVSCodeManagedBlock(lines []string, startLine, startCol int, blockLines []string) []string {
	if startLine < 0 || startLine >= len(lines) {
		return lines
	}
	line := lines[startLine]
	if len(line) == 0 {
		return lines
	}
	if startCol < 0 || startCol >= len(line) {
		startCol = len(line) - 1
	}
	before := line[:startCol+1]
	after := line[startCol+1:]

	updated := make([]string, 0, len(lines)+len(blockLines)+1)
	updated = append(updated, lines[:startLine]...)
	updated = append(updated, before)
	updated = append(updated, blockLines...)
	if after != "" {
		updated = append(updated, after)
	}
	updated = append(updated, lines[startLine+1:]...)
	return updated
}

// invalidVSCodeSettingsError wraps a validation error for settings.json parsing.
// Args: message describes the parsing error.
// Returns: a wrapped error tagged as invalid VS Code settings.
func invalidVSCodeSettingsError(message string) error {
	return fmt.Errorf("%w: %s", errInvalidVSCodeSettings, message)
}
