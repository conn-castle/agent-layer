package wizard

import (
	"fmt"
	"strings"
)

// PatchConfig applies wizard choices to TOML config content.
// content is the current config; choices holds selections; returns updated content or error.
func PatchConfig(content string, choices *Choices) (string, error) {
	var err error

	// 1. Approvals
	if choices.ApprovalModeTouched {
		content, err = patchTableKey(content, "approvals", "mode", fmt.Sprintf("%q", choices.ApprovalMode))
		if err != nil {
			return "", err
		}
	}

	// 2. Agents
	for _, agent := range SupportedAgents {
		if !choices.EnabledAgentsTouched {
			continue
		}
		enabled := choices.EnabledAgents[agent]
		content, err = patchTableKey(content, fmt.Sprintf("agents.%s", agent), "enabled", fmt.Sprintf("%t", enabled))
		if err != nil {
			return "", err
		}
	}

	optionalKeys := []struct {
		table   string
		key     string
		value   string
		touched bool
	}{
		{table: "agents.gemini", key: "model", value: choices.GeminiModel, touched: choices.GeminiModelTouched},
		{table: "agents.claude", key: "model", value: choices.ClaudeModel, touched: choices.ClaudeModelTouched},
		{table: "agents.codex", key: "model", value: choices.CodexModel, touched: choices.CodexModelTouched},
		{table: "agents.codex", key: "reasoning_effort", value: choices.CodexReasoning, touched: choices.CodexReasoningTouched},
	}
	for _, item := range optionalKeys {
		if !item.touched {
			continue
		}
		content, err = patchOptionalTableKey(content, item.table, item.key, item.value)
		if err != nil {
			return "", err
		}
	}

	if choices.RestoreMissingMCPServers && len(choices.MissingDefaultMCPServers) > 0 {
		content, err = appendMissingDefaultMCPServers(content, choices.MissingDefaultMCPServers)
		if err != nil {
			return "", err
		}
	}

	// 3. MCP Servers (only default ones)
	if choices.EnabledMCPServersTouched {
		for _, server := range KnownDefaultMCPServers {
			enabled := choices.EnabledMCPServers[server.ID]
			content = patchMCPServer(content, server.ID, enabled)
		}
	}

	return content, nil
}

// patchTableKey finds [table] and updates/inserts key = value.
// content/tableName/key/value identify the target; returns updated content or error.
func patchTableKey(content, tableName, key, value string) (string, error) {
	lines := strings.Split(content, "\n")
	tableHeader := fmt.Sprintf("[%s]", tableName)

	tableIndex := -1
	keyIndex := -1
	insertionIndex := -1

	// Find table
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == tableHeader {
			tableIndex = i
			break
		}
	}

	if tableIndex == -1 {
		// Table not found, append it
		newLines := append(lines, "", tableHeader, fmt.Sprintf("%s = %s", key, value))
		return strings.Join(newLines, "\n"), nil
	}

	// Find key within table (until next section)
	for i := tableIndex + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") {
			// Start of new section
			insertionIndex = i
			break
		}

		// Check for key
		// key = value or key=value
		if strings.HasPrefix(trimmed, key) {
			// Check if it's the exact key, not a prefix of another key
			rest := strings.TrimPrefix(trimmed, key)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "=") {
				keyIndex = i
				break
			}
		}
	}

	if keyIndex != -1 {
		// Update existing key
		// Preserve indentation if possible
		originalLine := lines[keyIndex]
		indent := ""
		if strings.HasPrefix(originalLine, "\t") || strings.HasPrefix(originalLine, " ") {
			trimmed := strings.TrimLeft(originalLine, " \t")
			indent = originalLine[:len(originalLine)-len(trimmed)]
		}
		lines[keyIndex] = fmt.Sprintf("%s%s = %s", indent, key, value)
	} else {
		// Insert key
		line := fmt.Sprintf("%s = %s", key, value)
		if insertionIndex != -1 {
			// Insert before next section
			lines = insertStringAt(lines, insertionIndex, line)
		} else {
			// Append to end
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n"), nil
}

// patchOptionalTableKey sets a key or removes it when value is blank.
// content/tableName/key/value identify the target; returns updated content or error.
func patchOptionalTableKey(content, tableName, key, value string) (string, error) {
	if value == "" {
		return deleteTableKey(content, tableName, key)
	}
	return patchTableKey(content, tableName, key, fmt.Sprintf("%q", value))
}

// deleteTableKey removes a key from a table if present.
// content/tableName/key identify the target; returns updated content or error.
func deleteTableKey(content, tableName, key string) (string, error) {
	lines := strings.Split(content, "\n")
	tableHeader := fmt.Sprintf("[%s]", tableName)

	tableIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == tableHeader {
			tableIndex = i
			break
		}
	}
	if tableIndex == -1 {
		return content, nil
	}

	for i := tableIndex + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		if strings.HasPrefix(trimmed, key) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
			if strings.HasPrefix(rest, "=") {
				lines = append(lines[:i], lines[i+1:]...)
				break
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}

// patchMCPServer finds [[mcp.servers]] with specific id and toggles enabled.
// content is the TOML config; returns updated content with the enabled flag set.
func patchMCPServer(content, serverID string, enabled bool) string {
	lines := strings.Split(content, "\n")

	inTargetServer := false

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		if trimmed == "[[mcp.servers]]" {
			// Check if this is the target server by looking ahead for id
			// This is a simplification: assumes id is near the top of the block
			// A full parser would be better, but we scan until next [[ or [

			// We need to know if THIS [[mcp.servers]] block has id = "serverID"
			// We can scan ahead.
			foundID := false
			for j := i + 1; j < len(lines); j++ {
				subTrimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(subTrimmed, "[") {
					break // End of block
				}
				if strings.HasPrefix(subTrimmed, "id") {
					rest := strings.TrimPrefix(subTrimmed, "id")
					rest = strings.TrimSpace(rest)
					if strings.HasPrefix(rest, "=") {
						val := strings.TrimPrefix(rest, "=")
						val = strings.TrimSpace(val)
						val = strings.Trim(val, "\"")
						if val == serverID {
							foundID = true
						}
						break // Found an ID
					}
				}
			}

			if foundID {
				inTargetServer = true
			} else {
				inTargetServer = false
			}
		} else if strings.HasPrefix(trimmed, "[") {
			inTargetServer = false
		}

		if inTargetServer {
			// Look for enabled key
			if strings.HasPrefix(trimmed, "enabled") {
				rest := strings.TrimPrefix(trimmed, "enabled")
				rest = strings.TrimSpace(rest)
				if strings.HasPrefix(rest, "=") {
					// Found it, replace
					indent := ""
					if strings.HasPrefix(lines[i], "\t") || strings.HasPrefix(lines[i], " ") {
						trimmedL := strings.TrimLeft(lines[i], " \t")
						indent = lines[i][:len(lines[i])-len(trimmedL)]
					}
					lines[i] = fmt.Sprintf("%senabled = %t", indent, enabled)
					// We are done with this server
					inTargetServer = false
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// insertStringAt inserts value at index in slice and returns the updated slice.
func insertStringAt(slice []string, index int, value string) []string {
	if index >= len(slice) {
		return append(slice, value)
	}
	slice = append(slice[:index+1], slice[index:]...)
	slice[index] = value
	return slice
}
