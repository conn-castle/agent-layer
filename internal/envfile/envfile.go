package envfile

import (
	"bufio"
	"fmt"
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
		key, value, ok, err := parseLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf(messages.EnvfileLineErrorFmt, lineNo, err)
		}
		if !ok {
			continue
		}
		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(messages.EnvfileReadFailedFmt, err)
	}

	return env, nil
}

// Patch updates .env content with the provided key/value pairs.
// content is the existing file content; updates supplies key/value pairs to merge.
func Patch(content string, updates map[string]string) string {
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	firstIndex := make(map[string]int)
	for i, line := range lines {
		key, _, ok, err := parseLine(line)
		if err != nil || !ok {
			continue
		}
		if _, exists := firstIndex[key]; !exists {
			firstIndex[key] = i
		}
	}

	updatedKeys := make(map[string]bool)
	for key, value := range updates {
		if value == "" {
			continue
		}

		encodedValue := encodeValue(value)
		if idx, ok := firstIndex[key]; ok {
			lines[idx] = fmt.Sprintf("%s=%s", key, encodedValue)
		} else {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, fmt.Sprintf("%s=%s", key, encodedValue))
			firstIndex[key] = len(lines) - 1
		}
		updatedKeys[key] = true
	}

	if len(updatedKeys) == 0 {
		return strings.Join(lines, "\n")
	}

	filtered := make([]string, 0, len(lines))
	for i, line := range lines {
		key, _, ok, err := parseLine(line)
		if err == nil && ok && updatedKeys[key] && firstIndex[key] != i {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

// parseLine parses a single .env line and returns key/value when present.
// line is the raw line; returns key/value, a boolean for presence, and an error for invalid syntax.
func parseLine(line string) (string, string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", "", false, fmt.Errorf(messages.EnvfileExpectedKeyValue)
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", "", false, fmt.Errorf(messages.EnvfileExpectedKeyValue)
	}
	value := strings.TrimSpace(trimmed[idx+1:])
	if strings.HasPrefix(value, `"`) {
		parsed, err := parseDoubleQuotedValue(value)
		if err != nil {
			return "", "", false, err
		}
		value = parsed
	} else if strings.HasPrefix(value, `'`) {
		parsed, err := parseSingleQuotedValue(value)
		if err != nil {
			return "", "", false, err
		}
		value = parsed
	}
	return key, value, true, nil
}

// parseDoubleQuotedValue parses a double-quoted .env value and validates trailing content.
// value is expected to start with a double quote.
func parseDoubleQuotedValue(value string) (string, error) {
	closing := findClosingDoubleQuote(value)
	if closing < 0 {
		return "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	if err := validateQuotedValueSuffix(value[closing+1:]); err != nil {
		return "", err
	}
	return unescapeDoubleQuotedValue(value[1:closing]), nil
}

// parseSingleQuotedValue parses a single-quoted .env value and validates trailing content.
// value is expected to start with a single quote.
func parseSingleQuotedValue(value string) (string, error) {
	if len(value) < 2 {
		return "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	closingOffset := strings.IndexByte(value[1:], '\'')
	if closingOffset < 0 {
		return "", fmt.Errorf(messages.EnvfileUnterminatedQuotedValue)
	}
	closing := 1 + closingOffset
	if err := validateQuotedValueSuffix(value[closing+1:]); err != nil {
		return "", err
	}
	return value[1:closing], nil
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

// validateQuotedValueSuffix validates trailing content after a quoted value.
// suffix may contain whitespace and an optional comment beginning with #.
func validateQuotedValueSuffix(suffix string) error {
	trimmed := strings.TrimSpace(suffix)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}
	return fmt.Errorf(messages.EnvfileInvalidQuotedSuffix)
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
	if strings.ContainsAny(val, " \t#\n\r") || strings.Contains(val, "\"") {
		val = strings.ReplaceAll(val, "\\", "\\\\")
		val = strings.ReplaceAll(val, "\"", "\\\"")
		val = strings.ReplaceAll(val, "\n", "\\n")
		val = strings.ReplaceAll(val, "\r", "\\r")
		return fmt.Sprintf(`"%s"`, val)
	}
	return val
}
