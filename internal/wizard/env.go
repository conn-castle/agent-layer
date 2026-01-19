package wizard

import (
	"fmt"
	"strings"
)

// PatchEnv updates the .env content with the provided secrets.
func PatchEnv(content string, secrets map[string]string) string {
	// If content is empty, just append
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	for key, value := range secrets {
		if value == "" {
			continue
		}

		encodedValue := encodeEnvValue(value)
		found := false

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Check for KEY=... or KEY = ...
			// Simple check: start with key=
			if strings.HasPrefix(trimmed, key+"=") {
				// Replace existing line
				lines[i] = fmt.Sprintf("%s=%s", key, encodedValue)
				found = true
				break
			}
		}

		if !found {
			// Append
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, fmt.Sprintf("%s=%s", key, encodedValue))
		}
	}

	return strings.Join(lines, "\n")
}

// encodeEnvValue quotes the value if necessary.
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
