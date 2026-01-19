package wizard

import (
	"fmt"
	"strings"
)

// PatchConfig applies the choices to the config content.
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
		// Enablement
		if choices.EnabledAgentsTouched {
			enabled := choices.EnabledAgents[agent]
			content, err = patchTableKey(content, fmt.Sprintf("agents.%s", agent), "enabled", fmt.Sprintf("%t", enabled))
			if err != nil {
				return "", err
			}
		}

		// Models (only if touched)
		if agent == AgentGemini && choices.GeminiModelTouched {
			content, err = patchTableKey(content, "agents.gemini", "model", fmt.Sprintf("%q", choices.GeminiModel))
			if err != nil {
				return "", err
			}
		}
		if agent == AgentClaude && choices.ClaudeModelTouched {
			content, err = patchTableKey(content, "agents.claude", "model", fmt.Sprintf("%q", choices.ClaudeModel))
			if err != nil {
				return "", err
			}
		}
		if agent == AgentCodex && choices.CodexModelTouched {
			content, err = patchTableKey(content, "agents.codex", "model", fmt.Sprintf("%q", choices.CodexModel))
			if err != nil {
				return "", err
			}
		}
		if agent == AgentCodex && choices.CodexReasoningTouched {
			content, err = patchTableKey(content, "agents.codex", "reasoning_effort", fmt.Sprintf("%q", choices.CodexReasoning))
			if err != nil {
				return "", err
			}
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

// patchMCPServer finds [[mcp.servers]] with specific id and toggles enabled.
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

func insertStringAt(slice []string, index int, value string) []string {
	if index >= len(slice) {
		return append(slice, value)
	}
	slice = append(slice[:index+1], slice[index:]...)
	slice[index] = value
	return slice
}
