package wizard

import (
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// PatchConfig applies wizard choices to TOML config content.
// content is the current config; choices holds selections; returns updated content or error.
func PatchConfig(content string, choices *Choices) (string, error) {
	configTree, err := toml.LoadBytes([]byte(content))
	if err != nil {
		return "", fmt.Errorf(messages.WizardParseConfigFailedFmt, err)
	}
	lines := strings.Split(content, "\n")

	// 1. Approvals
	if choices.ApprovalModeTouched {
		setPathPreservingComment(configTree, lines, []string{"approvals", "mode"}, choices.ApprovalMode)
	}

	// 2. Agents
	if choices.EnabledAgentsTouched {
		for _, agent := range SupportedAgents {
			enabled := choices.EnabledAgents[agent]
			setPathPreservingComment(configTree, lines, []string{"agents", agent, "enabled"}, enabled)
		}
	}

	optionalKeys := []struct {
		path    []string
		value   string
		touched bool
	}{
		{path: []string{"agents", "gemini", "model"}, value: choices.GeminiModel, touched: choices.GeminiModelTouched},
		{path: []string{"agents", "claude", "model"}, value: choices.ClaudeModel, touched: choices.ClaudeModelTouched},
		{path: []string{"agents", "codex", "model"}, value: choices.CodexModel, touched: choices.CodexModelTouched},
		{path: []string{"agents", "codex", "reasoning_effort"}, value: choices.CodexReasoning, touched: choices.CodexReasoningTouched},
	}
	for _, item := range optionalKeys {
		if !item.touched {
			continue
		}
		if item.value == "" {
			if err := deletePath(configTree, item.path); err != nil {
				return "", err
			}
			continue
		}
		setPathPreservingComment(configTree, lines, item.path, item.value)
	}

	if (choices.EnabledMCPServersTouched || choices.RestoreMissingMCPServers) && len(choices.DefaultMCPServers) == 0 {
		return "", fmt.Errorf(messages.WizardDefaultMCPServersRequired)
	}

	restoredServers := map[string]bool{}
	if choices.RestoreMissingMCPServers && len(choices.MissingDefaultMCPServers) > 0 {
		if err := appendMissingDefaultMCPServers(configTree, choices.MissingDefaultMCPServers); err != nil {
			return "", err
		}
		for _, id := range choices.MissingDefaultMCPServers {
			restoredServers[id] = true
		}
	}

	// 3. MCP Servers (only default ones)
	if choices.EnabledMCPServersTouched {
		servers, err := mcpServerTrees(configTree)
		if err != nil {
			return "", err
		}
		for _, server := range choices.DefaultMCPServers {
			enabled := choices.EnabledMCPServers[server.ID]
			preserveComment := !restoredServers[server.ID]
			setMCPServerEnabled(servers, lines, server.ID, enabled, preserveComment)
		}
	}

	// 4. Warnings
	if choices.WarningsEnabledTouched {
		if !choices.WarningsEnabled {
			if err := deletePath(configTree, []string{"warnings"}); err != nil {
				return "", err
			}
		} else {
			setPathPreservingComment(configTree, lines, []string{"warnings", "instruction_token_threshold"}, choices.InstructionTokenThreshold)
			setPathPreservingComment(configTree, lines, []string{"warnings", "mcp_server_threshold"}, choices.MCPServerThreshold)
			setPathPreservingComment(configTree, lines, []string{"warnings", "mcp_tools_total_threshold"}, choices.MCPToolsTotalThreshold)
			setPathPreservingComment(configTree, lines, []string{"warnings", "mcp_server_tools_threshold"}, choices.MCPServerToolsThreshold)
			setPathPreservingComment(configTree, lines, []string{"warnings", "mcp_schema_tokens_total_threshold"}, choices.MCPSchemaTokensTotalThreshold)
			setPathPreservingComment(configTree, lines, []string{"warnings", "mcp_schema_tokens_server_threshold"}, choices.MCPSchemaTokensServerThreshold)
		}
	}

	updated, err := configTree.ToTomlString()
	if err != nil {
		return "", fmt.Errorf(messages.WizardRenderConfigFailedFmt, err)
	}
	formatted, err := formatTomlNoIndent(updated)
	if err != nil {
		return "", fmt.Errorf(messages.WizardFormatConfigFailedFmt, err)
	}
	return formatted, nil
}

// setPathPreservingComment sets a value while retaining existing inline/leading comments when possible.
// tree is the parsed config; lines is the original content split by line; keys is the TOML path; value is the new value.
func setPathPreservingComment(tree *toml.Tree, lines []string, keys []string, value interface{}) {
	switch v := value.(type) {
	case int:
		value = int64(v)
	}
	comment := commentForPath(tree, lines, keys)
	if comment != "" {
		tree.SetPathWithComment(keys, comment, false, value)
		return
	}
	tree.SetPath(keys, value)
}

// deletePath removes a TOML path if it exists.
// tree is the parsed config; keys is the TOML path to delete; returns an error when deletion fails.
func deletePath(tree *toml.Tree, keys []string) error {
	if !tree.HasPath(keys) {
		return nil
	}
	return tree.DeletePath(keys)
}

// setMCPServerEnabled updates an MCP server's enabled flag when the server is present.
// servers is the parsed MCP server list; lines is the original config; serverID identifies the target server.
// preserveComment controls whether inline/leading comments should be retained.
func setMCPServerEnabled(servers []*toml.Tree, lines []string, serverID string, enabled bool, preserveComment bool) {
	for _, server := range servers {
		id, ok := server.Get("id").(string)
		if !ok || id == "" {
			continue
		}
		if id == serverID {
			if preserveComment {
				setPathPreservingComment(server, lines, []string{"enabled"}, enabled)
				return
			}
			server.SetPath([]string{"enabled"}, enabled)
			return
		}
	}
}

// commentForPath returns the combined comment text for a TOML path, or empty if none found.
// tree is the parsed config; lines is the original content; keys identifies the TOML path.
func commentForPath(tree *toml.Tree, lines []string, keys []string) string {
	if !tree.HasPath(keys) {
		return ""
	}
	pos := tree.GetPositionPath(keys)
	if pos.Invalid() {
		return ""
	}
	return commentForLine(lines, pos.Line-1)
}
