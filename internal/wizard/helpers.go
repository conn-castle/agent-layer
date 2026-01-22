package wizard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	leaveBlankOption = "Leave blank (use client default)"
	customOption     = "Custom..."
)

// buildSummary returns a formatted summary of wizard choices.
// c is the current choices; returns the summary text.
// Assumes c.DefaultMCPServers has been populated (see wizard.Run).
func buildSummary(c *Choices) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Approvals Mode: %s\n", c.ApprovalMode))

	agents := agentSummaryLines(c)
	sort.Strings(agents)
	sb.WriteString("\nEnabled Agents:\n")
	for _, a := range agents {
		sb.WriteString(a + "\n")
	}

	var mcp []string
	for _, s := range c.DefaultMCPServers {
		if c.EnabledMCPServers[s.ID] {
			mcp = append(mcp, s.ID)
		}
	}
	sb.WriteString("\nEnabled MCP Servers:\n")
	if len(c.DefaultMCPServers) == 0 {
		sb.WriteString("(none loaded)\n")
	} else if len(mcp) > 0 {
		for _, m := range mcp {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
	} else {
		sb.WriteString("(none)\n")
	}

	restoredMCP := restoredMCPServers(c)
	if len(restoredMCP) > 0 {
		sb.WriteString("\nRestored Default MCP Servers:\n")
		for _, m := range restoredMCP {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
	}

	disabledMCP := disabledMCPServers(c)
	sb.WriteString("\nDisabled MCP Servers (missing secrets):\n")
	if len(c.DefaultMCPServers) == 0 {
		sb.WriteString("(none loaded)\n")
	} else if len(disabledMCP) > 0 {
		for _, m := range disabledMCP {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
	} else {
		sb.WriteString("(none)\n")
	}

	sb.WriteString("\nSecrets to Update:\n")
	if len(c.Secrets) > 0 {
		for k := range c.Secrets {
			sb.WriteString(fmt.Sprintf("- %s\n", k))
		}
	} else {
		sb.WriteString("(none)\n")
	}

	sb.WriteString("\nWarnings:\n")
	if !c.WarningsEnabled {
		sb.WriteString("(disabled)\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("- instruction_token_threshold = %d\n", c.InstructionTokenThreshold))
	sb.WriteString(fmt.Sprintf("- mcp_server_threshold = %d\n", c.MCPServerThreshold))
	sb.WriteString(fmt.Sprintf("- mcp_tools_total_threshold = %d\n", c.MCPToolsTotalThreshold))
	sb.WriteString(fmt.Sprintf("- mcp_server_tools_threshold = %d\n", c.MCPServerToolsThreshold))
	sb.WriteString(fmt.Sprintf("- mcp_schema_tokens_total_threshold = %d\n", c.MCPSchemaTokensTotalThreshold))
	sb.WriteString(fmt.Sprintf("- mcp_schema_tokens_server_threshold = %d\n", c.MCPSchemaTokensServerThreshold))

	return sb.String()
}

type agentEnabledConfig struct {
	id      string
	enabled *bool
}

// setEnabledAgentsFromConfig updates dest using the enabled flags in configs.
// dest is the map to update; configs are the source values.
func setEnabledAgentsFromConfig(dest map[string]bool, configs []agentEnabledConfig) {
	for _, cfg := range configs {
		if cfg.enabled != nil && *cfg.enabled {
			dest[cfg.id] = true
		}
	}
}

// enabledAgentIDs returns enabled agent IDs from the provided map.
// enabled is a map of agent ID to enabled state; returns enabled IDs.
func enabledAgentIDs(enabled map[string]bool) []string {
	ids := make([]string, 0, len(enabled))
	for id, isEnabled := range enabled {
		if isEnabled {
			ids = append(ids, id)
		}
	}
	return ids
}

// agentIDSet converts a list of IDs into a map of enabled states.
// ids is the list of enabled IDs; returns a map with enabled entries set to true.
func agentIDSet(ids []string) map[string]bool {
	enabled := make(map[string]bool, len(ids))
	for _, id := range ids {
		enabled[id] = true
	}
	return enabled
}

// selectOptionalValue prompts for an optional selection and updates value.
// title and options define the prompt; value holds the current selection.
// Returns an error if the prompt fails or a custom value is left blank.
func selectOptionalValue(ui UI, title string, options []string, value *string) error {
	selection := *value
	if selection == "" {
		selection = leaveBlankOption
	} else {
		found := false
		for _, option := range options {
			if selection == option {
				found = true
				break
			}
		}
		if !found {
			selection = customOption
		}
	}
	opts := make([]string, 0, len(options)+2)
	opts = append(opts, leaveBlankOption)
	opts = append(opts, options...)
	opts = append(opts, customOption)
	if err := ui.Select(title, opts, &selection); err != nil {
		return err
	}
	if selection == leaveBlankOption {
		*value = ""
		return nil
	}
	if selection == customOption {
		customValue := *value
		if err := ui.Input(fmt.Sprintf("Custom %s", title), &customValue); err != nil {
			return err
		}
		customValue = strings.TrimSpace(customValue)
		if customValue == "" {
			return fmt.Errorf("custom value required for %s", title)
		}
		*value = customValue
		return nil
	}
	*value = selection
	return nil
}

// promptPositiveInt asks for a positive integer, defaulting to the current value.
// ui is the wizard UI; title is the prompt label; value holds the default and receives the parsed value.
func promptPositiveInt(ui UI, title string, value *int) error {
	input := strconv.Itoa(*value)
	if err := ui.Input(title, &input); err != nil {
		return err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	parsed, err := strconv.Atoi(input)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive integer", title)
	}
	*value = parsed
	return nil
}

// agentSummaryLines returns summary lines for enabled agents.
// c holds wizard choices; returns formatted summary lines.
func agentSummaryLines(c *Choices) []string {
	var agents []string
	for _, agent := range SupportedAgents {
		if !c.EnabledAgents[agent] {
			continue
		}
		modelSummary := agentModelSummary(agent, c)
		if modelSummary == "" {
			agents = append(agents, fmt.Sprintf("- %s", agent))
			continue
		}
		agents = append(agents, fmt.Sprintf("- %s: %s", agent, modelSummary))
	}
	return agents
}

// agentModelSummary returns the model summary for a given agent.
// agent identifies the agent; c holds wizard choices; returns summary text.
func agentModelSummary(agent string, c *Choices) string {
	switch agent {
	case AgentGemini:
		return c.GeminiModel
	case AgentClaude:
		return c.ClaudeModel
	case AgentCodex:
		return codexModelSummary(c)
	default:
		return ""
	}
}

// codexModelSummary returns the combined Codex model and reasoning summary.
// c holds wizard choices; returns the summary text.
func codexModelSummary(c *Choices) string {
	if c.CodexModel != "" && c.CodexReasoning != "" {
		return fmt.Sprintf("%s (%s)", c.CodexModel, c.CodexReasoning)
	}
	if c.CodexModel != "" {
		return c.CodexModel
	}
	if c.CodexReasoning != "" {
		return fmt.Sprintf("reasoning: %s", c.CodexReasoning)
	}
	return ""
}

// disabledMCPServers returns sorted IDs of servers disabled due to missing secrets.
// c is the current wizard choices; returns nil when none are disabled.
func disabledMCPServers(c *Choices) []string {
	if len(c.DisabledMCPServers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(c.DisabledMCPServers))
	for _, srv := range c.DefaultMCPServers {
		if c.DisabledMCPServers[srv.ID] {
			ids = append(ids, srv.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// restoredMCPServers returns IDs of default servers being restored to config.toml.
// c is the current wizard choices; returns nil when no restoration is requested.
func restoredMCPServers(c *Choices) []string {
	if !c.RestoreMissingMCPServers || len(c.MissingDefaultMCPServers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(c.MissingDefaultMCPServers))
	ids = append(ids, c.MissingDefaultMCPServers...)
	return ids
}
