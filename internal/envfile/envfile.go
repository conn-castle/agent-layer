package envfile

import (
	"bufio"
	"fmt"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// Parse reads .env content into a key-value map.
// content is the raw file content; returns parsed key/value pairs or an error.
func Parse(content string) (map[string]string, error) {
	env := make(map[string]string)
	if content == "" {
		return env, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		assignment, ok, err := ParseLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf(messages.EnvfileLineErrorFmt, lineNo, err)
		}
		if !ok {
			continue
		}
		env[assignment.Key] = assignment.Value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(messages.EnvfileReadFailedFmt, err)
	}

	return env, nil
}

// Patch updates .env content with the provided key/value pairs.
// content is the existing file content; updates supplies key/value pairs to merge.
// New keys are appended in sorted order and leave the result ending in a newline,
// so a later shell append cannot merge onto the last value; otherwise the input's
// final-newline state is kept.
func Patch(content string, updates map[string]string) string {
	finalNewline := strings.HasSuffix(content, "\n")
	var lines []string
	if content != "" {
		lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	}

	firstIndex := make(map[string]int)
	exported := make(map[string]bool)
	for i, line := range lines {
		assignment, ok, err := ParseLine(line)
		if err != nil || !ok {
			continue
		}
		if _, exists := firstIndex[assignment.Key]; !exists {
			firstIndex[assignment.Key] = i
			exported[assignment.Key] = assignment.Export
		}
	}

	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	updatedKeys := make(map[string]bool)
	for _, key := range keys {
		value := updates[key]
		if value == "" {
			continue
		}

		encodedValue := encodeValue(value)
		if idx, ok := firstIndex[key]; ok {
			prefix := ""
			if exported[key] {
				prefix = "export "
			}
			lines[idx] = fmt.Sprintf("%s%s=%s", prefix, key, encodedValue)
		} else {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, fmt.Sprintf("%s=%s", key, encodedValue))
			firstIndex[key] = len(lines) - 1
			finalNewline = true
		}
		updatedKeys[key] = true
	}

	filtered := make([]string, 0, len(lines))
	for i, line := range lines {
		assignment, ok, err := ParseLine(line)
		if err == nil && ok && updatedKeys[assignment.Key] && firstIndex[assignment.Key] != i {
			continue
		}
		filtered = append(filtered, line)
	}

	result := strings.Join(filtered, "\n")
	if finalNewline {
		result += "\n"
	}
	return result
}

// Assignment is one KEY=value line of .env content.
type Assignment struct {
	// Export reports whether the line starts with "export ".
	Export bool
	Key    string
	// Value is the decoded value.
	Value string
	// Comment is the raw trailing comment after a quoted value, including its
	// leading whitespace; it is empty when the value is unquoted or has no comment.
	Comment string
}

// ParseLine parses a single .env line.
// line is the raw line; returns the assignment, a boolean for presence (false for
// blank and comment-only lines), and an error for invalid syntax.
func ParseLine(line string) (Assignment, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return Assignment{}, false, nil
	}
	var assignment Assignment
	if strings.HasPrefix(trimmed, "export ") {
		assignment.Export = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return Assignment{}, false, fmt.Errorf(messages.EnvfileExpectedKeyValue)
	}
	assignment.Key = strings.TrimSpace(trimmed[:idx])
	if assignment.Key == "" {
		return Assignment{}, false, fmt.Errorf(messages.EnvfileExpectedKeyValue)
	}
	value := strings.TrimSpace(trimmed[idx+1:])
	assignment.Value = value
	var err error
	if strings.HasPrefix(value, `"`) {
		assignment.Value, assignment.Comment, err = parseDoubleQuotedValue(value)
	} else if strings.HasPrefix(value, `'`) {
		assignment.Value, assignment.Comment, err = parseSingleQuotedValue(value)
	}
	if err != nil {
		return Assignment{}, false, err
	}
	return assignment, true, nil
}

// parseDoubleQuotedValue parses a double-quoted .env value and its trailing comment.
// value is expected to start with a double quote.
func parseDoubleQuotedValue(value string) (string, string, error) {
	closing := findClosingDoubleQuote(value)
	if closing < 0 {
		return "", "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	comment, err := quotedValueComment(value[closing+1:])
	if err != nil {
		return "", "", err
	}
	return unescapeDoubleQuotedValue(value[1:closing]), comment, nil
}

// parseSingleQuotedValue parses a single-quoted .env value and its trailing comment.
// value is expected to start with a single quote.
func parseSingleQuotedValue(value string) (string, string, error) {
	if len(value) < 2 {
		return "", "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	closingOffset := strings.IndexByte(value[1:], '\'')
	if closingOffset < 0 {
		return "", "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	closing := 1 + closingOffset
	comment, err := quotedValueComment(value[closing+1:])
	if err != nil {
		return "", "", err
	}
	return value[1:closing], comment, nil
}

// findClosingDoubleQuote returns the index of the first unescaped closing quote in value.
// value is expected to start with a double quote.
func findClosingDoubleQuote(value string) int {
	escaped := false
	for i := 1; i < len(value); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch value[i] {
		case '\\':
			escaped = true
		case '"':
			return i
		}
	}
	return -1
}

// quotedValueComment validates trailing content after a quoted value.
// suffix may contain whitespace and an optional comment beginning with #;
// returns suffix when it holds a comment and "" when it is blank.
func quotedValueComment(suffix string) (string, error) {
	trimmed := strings.TrimSpace(suffix)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "#") {
		return suffix, nil
	}
	return "", fmt.Errorf(messages.EnvfileInvalidQuotedSuffix)
}

// unescapeDoubleQuotedValue decodes the escape forms produced by encodeValue.
// escaped is the double-quoted payload without surrounding quotes.
func unescapeDoubleQuotedValue(escaped string) string {
	var b strings.Builder
	b.Grow(len(escaped))
	for i := 0; i < len(escaped); i++ {
		if escaped[i] == '\\' && i+1 < len(escaped) {
			switch escaped[i+1] {
			case '\\', '"':
				b.WriteByte(escaped[i+1])
				i++
				continue
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			}
		}
		b.WriteByte(escaped[i])
	}
	return b.String()
}

// encodeValue escapes and quotes a value when required for .env formatting.
// val is the raw value; returns the encoded representation.
func encodeValue(val string) string {
	if strings.ContainsAny(val, " \t#\n\r") || strings.Contains(val, "\"") || strings.HasPrefix(val, "'") {
		val = strings.ReplaceAll(val, "\\", "\\\\")
		val = strings.ReplaceAll(val, "\"", "\\\"")
		val = strings.ReplaceAll(val, "\n", "\\n")
		val = strings.ReplaceAll(val, "\r", "\\r")
		return fmt.Sprintf(`"%s"`, val)
	}
	return val
}
