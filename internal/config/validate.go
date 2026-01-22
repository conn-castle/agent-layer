package config

import (
	"fmt"
)

var validApprovals = map[string]struct{}{
	"all":      {},
	"mcp":      {},
	"commands": {},
	"none":     {},
}

var validClients = map[string]struct{}{
	"gemini":      {},
	"claude":      {},
	"vscode":      {},
	"codex":       {},
	"antigravity": {},
}

// Validate ensures the config is complete and consistent.
func (c *Config) Validate(path string) error {
	if _, ok := validApprovals[c.Approvals.Mode]; !ok {
		return fmt.Errorf("%s: approvals.mode must be one of all, mcp, commands, none", path)
	}

	if c.Agents.Gemini.Enabled == nil {
		return fmt.Errorf("%s: agents.gemini.enabled is required", path)
	}
	if c.Agents.Claude.Enabled == nil {
		return fmt.Errorf("%s: agents.claude.enabled is required", path)
	}
	if c.Agents.Codex.Enabled == nil {
		return fmt.Errorf("%s: agents.codex.enabled is required", path)
	}
	if c.Agents.VSCode.Enabled == nil {
		return fmt.Errorf("%s: agents.vscode.enabled is required", path)
	}
	if c.Agents.Antigravity.Enabled == nil {
		return fmt.Errorf("%s: agents.antigravity.enabled is required", path)
	}

	for i, server := range c.MCP.Servers {
		if server.ID == "" {
			return fmt.Errorf("%s: mcp.servers[%d].id is required", path, i)
		}
		if server.ID == "agent-layer" {
			return fmt.Errorf("%s: mcp.servers[%d].id is reserved for the internal prompt server", path, i)
		}
		if server.Enabled == nil {
			return fmt.Errorf("%s: mcp.servers[%d].enabled is required", path, i)
		}
		switch server.Transport {
		case "http":
			if server.URL == "" {
				return fmt.Errorf("%s: mcp.servers[%d].url is required for http transport", path, i)
			}
			if server.Command != "" || len(server.Args) > 0 {
				return fmt.Errorf("%s: mcp.servers[%d].command/args are not allowed for http transport", path, i)
			}
			if len(server.Env) > 0 {
				return fmt.Errorf("%s: mcp.servers[%d].env is not allowed for http transport", path, i)
			}
		case "stdio":
			if server.Command == "" {
				return fmt.Errorf("%s: mcp.servers[%d].command is required for stdio transport", path, i)
			}
			if server.URL != "" {
				return fmt.Errorf("%s: mcp.servers[%d].url is not allowed for stdio transport", path, i)
			}
			if len(server.Headers) > 0 {
				return fmt.Errorf("%s: mcp.servers[%d].headers are not allowed for stdio transport", path, i)
			}
		default:
			return fmt.Errorf("%s: mcp.servers[%d].transport must be http or stdio", path, i)
		}

		for _, client := range server.Clients {
			if _, ok := validClients[client]; !ok {
				return fmt.Errorf("%s: mcp.servers[%d].clients contains invalid client %q", path, i, client)
			}
		}
	}

	if err := validateWarnings(path, c.Warnings); err != nil {
		return err
	}

	return nil
}

// validateWarnings validates optional warning thresholds.
// path is used for error context; warnings carries the thresholds; returns an error when a threshold is non-positive.
func validateWarnings(path string, warnings WarningsConfig) error {
	thresholds := []struct {
		name  string
		value *int
	}{
		{"warnings.instruction_token_threshold", warnings.InstructionTokenThreshold},
		{"warnings.mcp_server_threshold", warnings.MCPServerThreshold},
		{"warnings.mcp_tools_total_threshold", warnings.MCPToolsTotalThreshold},
		{"warnings.mcp_server_tools_threshold", warnings.MCPServerToolsThreshold},
		{"warnings.mcp_schema_tokens_total_threshold", warnings.MCPSchemaTokensTotalThreshold},
		{"warnings.mcp_schema_tokens_server_threshold", warnings.MCPSchemaTokensServerThreshold},
	}
	for _, threshold := range thresholds {
		if threshold.value != nil && *threshold.value <= 0 {
			return fmt.Errorf("%s: %s must be greater than zero", path, threshold.name)
		}
	}
	return nil
}
