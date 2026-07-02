// Package tomlpatch provides line-aware TOML patch helpers for generated
// configuration files that must preserve unrelated comments and formatting.
package tomlpatch

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Block is a contiguous TOML table or array-of-table block.
type Block struct {
	Name  string
	Lines []string
}

// Document is a lightweight line-based TOML split into preamble, table blocks,
// and array-of-table blocks.
type Document struct {
	Preamble []string
	Sections map[string]*Block
	Arrays   map[string][]*Block
	Order    []string
}

// StringState tracks parser position relative to TOML string literals.
type StringState int

const (
	// StateNone means parsing is outside any TOML string.
	StateNone StringState = iota
	// StateBasic means parsing is inside a basic string.
	StateBasic
	// StateLiteral means parsing is inside a literal string.
	StateLiteral
	// StateMultiBasic means parsing is inside a multiline basic string.
	StateMultiBasic
	// StateMultiLiteral means parsing is inside a multiline literal string.
	StateMultiLiteral
)

const (
	// tripleBasicQuote opens and closes a TOML multiline basic string.
	tripleBasicQuote = `"""`
	// tripleLiteralQuote opens and closes a TOML multiline literal string.
	tripleLiteralQuote = `'''`
)

// LineWalkResult controls TOML line walking.
type LineWalkResult struct {
	AdvanceTo int
	Stop      bool
}

// KeyLine holds a parsed key/value line with comment metadata.
type KeyLine struct {
	Raw           string
	Indent        string
	Commented     bool
	InlineComment string
}

// ScanLineForComment scans a TOML line and returns the position of any inline
// comment, or -1 if none, plus the next parser state for multiline strings.
func ScanLineForComment(line string, state StringState) (commentPos int, nextState StringState) {
	i := 0
	for i < len(line) {
		ch := line[i]

		if state == StateBasic || state == StateMultiBasic {
			if ch == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
		}

		switch state {
		case StateNone:
			if ch == '#' {
				return i, state
			}
			if ch == '"' {
				if len(line) > i+2 && line[i:i+3] == tripleBasicQuote {
					state = StateMultiBasic
					i += 3
					continue
				}
				state = StateBasic
			} else if ch == '\'' {
				if len(line) > i+2 && line[i:i+3] == tripleLiteralQuote {
					state = StateMultiLiteral
					i += 3
					continue
				}
				state = StateLiteral
			}

		case StateBasic:
			if ch == '"' {
				state = StateNone
			}

		case StateLiteral:
			if ch == '\'' {
				state = StateNone
			}

		case StateMultiBasic:
			if ch == '"' && len(line) > i+2 && line[i:i+3] == tripleBasicQuote {
				state = StateNone
				i += 3
				continue
			}

		case StateMultiLiteral:
			if ch == '\'' && len(line) > i+2 && line[i:i+3] == tripleLiteralQuote {
				state = StateNone
				i += 3
				continue
			}
		}
		i++
	}
	return -1, state
}

// StateInMultiline returns true if state is inside a multiline string.
func StateInMultiline(state StringState) bool {
	return state == StateMultiBasic || state == StateMultiLiteral
}

// InlineCommentForLine extracts a TOML inline comment on a specific line.
func InlineCommentForLine(lines []string, lineIndex int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	state := StateNone
	for i, line := range lines {
		commentPos, nextState := ScanLineForComment(line, state)
		if i == lineIndex && commentPos >= 0 {
			return strings.TrimSpace(line[commentPos+1:])
		}
		state = nextState
	}
	return ""
}

// CommentForLine collects contiguous leading comment lines and any inline
// comment on a line.
func CommentForLine(lines []string, lineIndex int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	var commentLines []string
	for i := lineIndex - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		commentLines = append(commentLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
	}
	slices.Reverse(commentLines)
	if inline := InlineCommentForLine(lines, lineIndex); inline != "" {
		commentLines = append(commentLines, inline)
	}
	if len(commentLines) == 0 {
		return ""
	}
	return strings.Join(commentLines, "\n")
}

// WalkLinesOutsideMultiline walks lines while skipping callbacks inside
// multiline TOML string bodies.
func WalkLinesOutsideMultiline(lines []string, fn func(i int, line string, state StringState) LineWalkResult) {
	state := StateNone
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if StateInMultiline(state) {
			_, state = ScanLineForComment(line, state)
			continue
		}

		result := fn(i, line, state)
		if result.AdvanceTo < i {
			result.AdvanceTo = i
		}

		for j := i; j <= result.AdvanceTo && j < len(lines); j++ {
			_, state = ScanLineForComment(lines[j], state)
		}
		if result.Stop {
			return
		}
		i = result.AdvanceTo
	}
}

// ExtractBlockKeyValue returns the unquoted value for a key in a TOML block.
func ExtractBlockKeyValue(lines []string, key string) string {
	value := ""
	WalkLinesOutsideMultiline(lines, func(_ int, line string, state StringState) LineWalkResult {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			return LineWalkResult{}
		}
		_, parsedValue, ok := ParseKeyValueWithState(trimmed, key, state)
		if ok {
			value = strings.Trim(parsedValue, "\"'")
			return LineWalkResult{Stop: true}
		}
		return LineWalkResult{}
	})
	return value
}

// RemoveKeyFromBlock removes all uncommented lines for key from block,
// including multiline continuation lines and dotted sub-key assignments.
func RemoveKeyFromBlock(block *Block, key string) {
	type lineRange struct{ start, end int }
	var ranges []lineRange
	WalkLinesOutsideMultiline(block.Lines, func(i int, line string, state StringState) LineWalkResult {
		parsed, ok := ParseKeyLineWithState(line, key, state)
		if !ok {
			parsed, ok = ParseDottedPrefixLine(line, key)
		}
		if ok && !parsed.Commented {
			endIdx := MultilineValueEndIndex(block.Lines, i)
			ranges = append(ranges, lineRange{i, endIdx})
			return LineWalkResult{AdvanceTo: endIdx}
		}
		return LineWalkResult{}
	})
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		block.Lines = append(block.Lines[:r.start], block.Lines[r.end+1:]...)
	}
}

// ParseDottedPrefixLine checks if line defines a dotted sub-key of key.
func ParseDottedPrefixLine(line string, key string) (KeyLine, bool) {
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	trimmed := strings.TrimLeft(line[indentLen:], " \t")
	commented := false
	if strings.HasPrefix(trimmed, "#") {
		commented = true
		trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, "#"), " \t")
	}
	commentPos, _ := ScanLineForComment(trimmed, StateNone)
	clean := trimmed
	if commentPos >= 0 {
		clean = strings.TrimSpace(trimmed[:commentPos])
	}
	left, _, ok := strings.Cut(clean, "=")
	if !ok {
		return KeyLine{}, false
	}
	path, ok := ParseKeyPath(strings.TrimSpace(left))
	if !ok || len(path) < 2 || path[0] != key {
		return KeyLine{}, false
	}
	return KeyLine{Raw: line, Indent: indent, Commented: commented}, true
}

// MultilineValueEndIndex returns the index of the last line of a value that
// starts at startIdx.
func MultilineValueEndIndex(lines []string, startIdx int) int {
	if startIdx >= len(lines) {
		return startIdx
	}
	line := lines[startIdx]
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return startIdx
	}
	valuePart := strings.TrimSpace(line[eqIdx+1:])

	if strings.HasPrefix(valuePart, tripleBasicQuote) {
		rest := valuePart[3:]
		if ContainsUnescapedTripleQuote(rest) {
			return startIdx
		}
		for i := startIdx + 1; i < len(lines); i++ {
			if ContainsUnescapedTripleQuote(lines[i]) {
				return i
			}
		}
		return startIdx
	}

	if strings.HasPrefix(valuePart, tripleLiteralQuote) {
		rest := valuePart[3:]
		if strings.Contains(rest, tripleLiteralQuote) {
			return startIdx
		}
		for i := startIdx + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], tripleLiteralQuote) {
				return i
			}
		}
		return startIdx
	}

	var opener, closer byte
	switch {
	case strings.HasPrefix(valuePart, "["):
		opener, closer = '[', ']'
	case strings.HasPrefix(valuePart, "{"):
		opener, closer = '{', '}'
	default:
		return startIdx
	}

	depth := 0
	state := StateNone
	for i := startIdx; i < len(lines); i++ {
		from := 0
		if i == startIdx {
			from = eqIdx + 1
		}
		var delta int
		delta, state = countBracketDepth(lines[i][from:], opener, closer, state)
		depth += delta
		if depth <= 0 {
			return i
		}
	}
	return startIdx
}

// countBracketDepth returns the net bracket-depth delta for opener/closer in s,
// carrying TOML string state (including triple-quoted multiline strings) across
// lines so brackets and comment markers inside string bodies are ignored. It
// shares the string state machine used by ScanLineForComment.
func countBracketDepth(s string, opener, closer byte, state StringState) (int, StringState) {
	depth := 0
	i := 0
	for i < len(s) {
		ch := s[i]
		if state == StateBasic || state == StateMultiBasic {
			if ch == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
		}
		switch state {
		case StateNone:
			switch ch {
			case '#':
				return depth, state
			case '"':
				if len(s) > i+2 && s[i:i+3] == tripleBasicQuote {
					state = StateMultiBasic
					i += 3
					continue
				}
				state = StateBasic
			case '\'':
				if len(s) > i+2 && s[i:i+3] == tripleLiteralQuote {
					state = StateMultiLiteral
					i += 3
					continue
				}
				state = StateLiteral
			case opener:
				depth++
			case closer:
				depth--
			}
		case StateBasic:
			if ch == '"' {
				state = StateNone
			}
		case StateLiteral:
			if ch == '\'' {
				state = StateNone
			}
		case StateMultiBasic:
			if ch == '"' && len(s) > i+2 && s[i:i+3] == tripleBasicQuote {
				state = StateNone
				i += 3
				continue
			}
		case StateMultiLiteral:
			if ch == '\'' && len(s) > i+2 && s[i:i+3] == tripleLiteralQuote {
				state = StateNone
				i += 3
				continue
			}
		}
		i++
	}
	return depth, state
}

// ContainsUnescapedTripleQuote reports whether s contains an unescaped basic
// multiline-string delimiter.
func ContainsUnescapedTripleQuote(s string) bool {
	for search := s; ; {
		idx := strings.Index(search, tripleBasicQuote)
		if idx < 0 {
			return false
		}
		backslashes := 0
		for i := idx - 1; i >= 0 && search[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return true
		}
		search = search[idx+1:]
	}
}

// ParseKeyValueWithState extracts a simple key/value pair from a TOML line.
func ParseKeyValueWithState(line string, key string, state StringState) (string, string, bool) {
	commentPos, _ := ScanLineForComment(line, state)
	clean := line
	if commentPos >= 0 {
		clean = strings.TrimSpace(line[:commentPos])
	}
	path, ok := assignmentKeyPath(clean)
	if !ok {
		return "", "", false
	}
	wantPath, ok := ParseKeyPath(key)
	if !ok || !slices.Equal(path, wantPath) {
		return "", "", false
	}
	_, value, _ := strings.Cut(clean, "=")
	value = strings.TrimSpace(value)
	return key, value, true
}

// SetKeyValue updates or inserts key = value in a section block.
func SetKeyValue(block *Block, templateBlock *Block, key string, value string, afterKey string) {
	var base KeyLine
	if templateBlock != nil {
		if templateLine, ok := FindKeyLine(templateBlock.Lines, key); ok {
			base = templateLine
		}
	}
	if base.Raw == "" {
		if existingLine, ok := FindKeyLine(block.Lines, key); ok {
			base = existingLine
		}
	}
	if base.Raw == "" {
		newLine := BuildKeyLine(KeyLine{Indent: ""}, key, value, false)
		ReplaceOrInsertLine(block, key, newLine, afterKey)
		return
	}

	newLine := BuildKeyLine(base, key, value, false)
	ReplaceOrInsertLine(block, key, newLine, afterKey)
}

// SetCommentedKeyLine ensures key is present as a commented line when possible.
func SetCommentedKeyLine(block *Block, templateBlock *Block, key string, afterKey string) {
	if templateBlock != nil {
		if templateLine, ok := FindKeyLine(templateBlock.Lines, key); ok {
			commentedLine := EnsureCommented(templateLine.Raw)
			ReplaceOrInsertLine(block, key, commentedLine, afterKey)
			return
		}
	}
	if existingLine, ok := FindKeyLine(block.Lines, key); ok {
		commentedLine := EnsureCommented(existingLine.Raw)
		ReplaceOrInsertLine(block, key, commentedLine, afterKey)
	}
}

// FindKeyLine searches lines for a key/value assignment.
func FindKeyLine(lines []string, key string) (KeyLine, bool) {
	result := KeyLine{}
	found := false
	WalkLinesOutsideMultiline(lines, func(_ int, line string, state StringState) LineWalkResult {
		parsed, ok := ParseKeyLineWithState(line, key, state)
		if ok {
			result = parsed
			found = true
			return LineWalkResult{Stop: true}
		}
		return LineWalkResult{}
	})
	return result, found
}

// ParseKeyLineWithState parses a key/value assignment line with explicit state.
func ParseKeyLineWithState(line string, key string, state StringState) (KeyLine, bool) {
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	trimmed := strings.TrimLeft(line[indentLen:], " \t")
	commented := false
	if strings.HasPrefix(trimmed, "#") {
		commented = true
		trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, "#"), " \t")
	}
	commentPos, _ := ScanLineForComment(trimmed, state)
	clean := trimmed
	if commentPos >= 0 {
		clean = strings.TrimSpace(trimmed[:commentPos])
	}
	path, ok := assignmentKeyPath(clean)
	if !ok {
		return KeyLine{}, false
	}
	wantPath, ok := ParseKeyPath(key)
	if !ok || !slices.Equal(path, wantPath) {
		return KeyLine{}, false
	}
	inlineComment := ExtractInlineCommentWithState(trimmed, state)
	return KeyLine{Raw: line, Indent: indent, Commented: commented, InlineComment: inlineComment}, true
}

// ExtractInlineCommentWithState returns the inline comment portion.
func ExtractInlineCommentWithState(line string, state StringState) string {
	commentPos, _ := ScanLineForComment(line, state)
	if commentPos < 0 {
		return ""
	}
	return strings.TrimSpace(line[commentPos:])
}

// BuildKeyLine renders a key/value line using indentation and inline comment
// from base.
func BuildKeyLine(base KeyLine, key string, value string, commented bool) string {
	indent := base.Indent
	prefix := ""
	if commented {
		prefix = "# "
	}
	line := fmt.Sprintf("%s%s%s = %s", indent, prefix, key, value)
	if base.InlineComment != "" {
		line += " " + base.InlineComment
	}
	return line
}

// EnsureCommented returns line with a leading comment marker.
func EnsureCommented(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return line
	}
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	return indent + "# " + strings.TrimLeft(line[indentLen:], " \t")
}

// ReplaceOrInsertLine replaces an existing key line or inserts a new line.
func ReplaceOrInsertLine(block *Block, key string, newLine string, afterKey string) {
	var matches []int
	uncommentedIndex := -1
	WalkLinesOutsideMultiline(block.Lines, func(i int, line string, state StringState) LineWalkResult {
		parsed, ok := ParseKeyLineWithState(line, key, state)
		if !ok {
			return LineWalkResult{}
		}
		matches = append(matches, i)
		if !parsed.Commented && uncommentedIndex == -1 {
			uncommentedIndex = i
		}
		return LineWalkResult{}
	})
	if len(matches) > 0 {
		replaceAt := matches[0]
		if uncommentedIndex >= 0 {
			replaceAt = uncommentedIndex
		}
		block.Lines[replaceAt] = newLine
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] == replaceAt {
				continue
			}
			block.Lines = append(block.Lines[:matches[i]], block.Lines[matches[i]+1:]...)
		}
		return
	}
	insertAt := FindInsertIndex(block.Lines, afterKey)
	block.Lines = append(block.Lines[:insertAt], append([]string{newLine}, block.Lines[insertAt:]...)...)
}

// FindInsertIndex returns the line index to insert a new key line after afterKey.
func FindInsertIndex(lines []string, afterKey string) int {
	if len(lines) == 0 {
		return 0
	}
	if afterKey != "" {
		insertAt := -1
		WalkLinesOutsideMultiline(lines, func(i int, line string, state StringState) LineWalkResult {
			if _, ok := ParseKeyLineWithState(line, afterKey, state); ok {
				insertAt = i + 1
				return LineWalkResult{Stop: true}
			}
			return LineWalkResult{}
		})
		if insertAt >= 0 {
			return insertAt
		}
	}
	return 1
}

// FormatValue converts a scalar value into a TOML literal string.
func FormatValue(value any) string {
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

// ParseDocument splits TOML content into a line-aware document.
func ParseDocument(content string) Document {
	lines := strings.Split(content, "\n")
	sections := make(map[string]*Block)
	arrays := make(map[string][]*Block)
	var order []string
	var preamble []string
	var current *Block
	var currentIsArray bool

	flush := func() {
		if current == nil {
			return
		}
		if currentIsArray {
			arrays[current.Name] = append(arrays[current.Name], current)
		} else if _, exists := sections[current.Name]; !exists {
			sections[current.Name] = current
			order = append(order, current.Name)
		}
		current = nil
		currentIsArray = false
	}

	state := StateNone
	for _, line := range lines {
		// A header-looking line inside a multiline string body (e.g. a
		// "[mcp_servers.x]" line embedded in a triple-quoted value) must be treated
		// as string content, not a real table header, so it stays in its block.
		if StateInMultiline(state) {
			if current == nil {
				preamble = append(preamble, line)
			} else {
				current.Lines = append(current.Lines, line)
			}
			_, state = ScanLineForComment(line, state)
			continue
		}
		name, isArray, ok := ParseHeader(line)
		if ok {
			flush()
			current = &Block{Name: name, Lines: []string{line}}
			currentIsArray = isArray
			_, state = ScanLineForComment(line, state)
			continue
		}
		if current == nil {
			preamble = append(preamble, line)
		} else {
			current.Lines = append(current.Lines, line)
		}
		_, state = ScanLineForComment(line, state)
	}
	flush()

	return Document{
		Preamble: preamble,
		Sections: sections,
		Arrays:   arrays,
		Order:    order,
	}
}

// ParseHeader detects a TOML table header and extracts its name.
func ParseHeader(line string) (string, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false, false
	}
	commentPos, _ := ScanLineForComment(trimmed, StateNone)
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

// CloneLines returns a copy of lines.
func CloneLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

// AppendBlock appends a block to output with one blank line separator.
func AppendBlock(output *[]string, block []string) {
	trimmed := TrimEmptyLines(block)
	if len(trimmed) == 0 {
		return
	}
	if len(*output) > 0 && (*output)[len(*output)-1] != "" {
		*output = append(*output, "")
	}
	*output = append(*output, trimmed...)
}

// TrimEmptyLines removes leading and trailing blank lines.
func TrimEmptyLines(lines []string) []string {
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

// TrimTrailingEmptyLines removes trailing blank lines.
func TrimTrailingEmptyLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// ParseKeyPath parses a TOML dotted key or table path into unescaped segments.
func ParseKeyPath(input string) ([]string, bool) {
	var parts []string
	rest := strings.TrimSpace(input)
	for rest != "" {
		var part string
		var ok bool
		part, rest, ok = parseKeyPathSegment(rest)
		if !ok || part == "" {
			return nil, false
		}
		parts = append(parts, part)
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		if rest[0] != '.' {
			return nil, false
		}
		rest = strings.TrimSpace(rest[1:])
		if rest == "" {
			return nil, false
		}
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func parseKeyPathSegment(input string) (string, string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", false
	}
	switch input[0] {
	case '"':
		idx, ok := closingBasicStringIndex(input)
		if !ok {
			return "", "", false
		}
		value, err := strconv.Unquote(input[:idx+1])
		if err != nil {
			return "", "", false
		}
		return value, input[idx+1:], true
	case '\'':
		idx := strings.Index(input[1:], "'")
		if idx < 0 {
			return "", "", false
		}
		idx++
		return input[1:idx], input[idx+1:], true
	default:
		end := 0
		for end < len(input) && input[end] != '.' {
			end++
		}
		part := strings.TrimSpace(input[:end])
		if part == "" {
			return "", "", false
		}
		return part, input[end:], true
	}
}

func closingBasicStringIndex(input string) (int, bool) {
	escaped := false
	for i := 1; i < len(input); i++ {
		ch := input[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return i, true
		}
	}
	return 0, false
}

// FormatDottedKeyPath renders path as a TOML dotted key with quoted segments
// where required.
func FormatDottedKeyPath(path []string) string {
	parts := make([]string, 0, len(path))
	for _, part := range path {
		parts = append(parts, FormatKey(part))
	}
	return strings.Join(parts, ".")
}

// FormatKey renders key as a TOML bare key when possible, otherwise as a basic
// quoted key.
func FormatKey(key string) string {
	if isBareKey(key) {
		return key
	}
	return strconv.Quote(key)
}

func isBareKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func assignmentKeyPath(clean string) ([]string, bool) {
	left, _, ok := strings.Cut(clean, "=")
	if !ok {
		return nil, false
	}
	return ParseKeyPath(strings.TrimSpace(left))
}
