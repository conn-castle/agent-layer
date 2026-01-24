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
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true, nil
}

// encodeValue escapes and quotes a value when required for .env formatting.
// val is the raw value; returns the encoded representation.
func encodeValue(val string) string {
	if strings.ContainsAny(val, " \t#") || strings.Contains(val, "\"") {
		val = strings.ReplaceAll(val, "\\", "\\\\")
		val = strings.ReplaceAll(val, "\"", "\\\"")
		return fmt.Sprintf(`"%s"`, val)
	}
	return val
}
