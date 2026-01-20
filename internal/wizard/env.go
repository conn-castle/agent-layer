package wizard

import (
	"bufio"
	"fmt"
	"strings"
)

// ParseEnv reads .env content into a key-value map.
// content is the raw file content; returns the parsed key/value pairs or an error.
func ParseEnv(content string) (map[string]string, error) {
	env := make(map[string]string)
	if content == "" {
		return env, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid env content line %d: expected KEY=VALUE", lineNo)
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			return nil, fmt.Errorf("invalid env content line %d: expected KEY=VALUE", lineNo)
		}
		value := strings.TrimSpace(line[idx+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading env content: %w", err)
	}

	return env, nil
}

// PatchEnv updates .env content with the provided secrets.
// content is the existing file content; secrets supplies key/value pairs to merge.
func PatchEnv(content string, secrets map[string]string) string {
	// If content is empty, just append
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	firstIndex := make(map[string]int)
	for i, line := range lines {
		key, ok := parseEnvKey(line)
		if !ok {
			continue
		}
		if _, exists := firstIndex[key]; !exists {
			firstIndex[key] = i
		}
	}

	updatedKeys := make(map[string]bool)
	for key, value := range secrets {
		if value == "" {
			continue
		}

		encodedValue := encodeEnvValue(value)
		if idx, ok := firstIndex[key]; ok {
			lines[idx] = fmt.Sprintf("%s=%s", key, encodedValue)
		} else {
			// Append
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
		key, ok := parseEnvKey(line)
		if ok && updatedKeys[key] && firstIndex[key] != i {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

// parseEnvKey extracts a key name from a .env line.
// line is a single line; returns the key and true when it looks like KEY=VALUE.
func parseEnvKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", false
	}
	return key, true
}

// encodeEnvValue returns a .env-safe value with quotes/escapes if needed.
// val is the raw value; returns the encoded representation.
func encodeEnvValue(val string) string {
	// If it contains whitespace, hash, or needs quoting
	if strings.ContainsAny(val, " \t#") || strings.Contains(val, "\"") {
		// Simple quoting: escape " and \
		val = strings.ReplaceAll(val, "\\", "\\\\")
		val = strings.ReplaceAll(val, "\"", "\\\"")
		return fmt.Sprintf(`"%s"`, val)
	}
	return val
}
