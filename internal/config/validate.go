package config

import (
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// MCP transport type constants.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// isValidApprovalMode checks the value against the config field catalog.
func isValidApprovalMode(mode string) bool {
	field, ok := LookupField("approvals.mode")
	if !ok {
		return false
	}
	for _, opt := range field.Options {
		if opt.Value == mode {
			return true
		}
	}
	return false
}

var validClients = map[string]struct{}{
	"gemini":      {},
	"claude":      {},
	"vscode":      {},
	"codex":       {},
	"antigravity": {},
}

var validHTTPTransports = map[string]struct{}{
	"sse":        {},
	"streamable": {},
}

var validWarningNoiseModes = map[string]struct{}{
	"":        {},
	"default": {},
	"reduce":  {},
}

// Validate ensures the config is complete and consistent.
func (c *Config) Validate(path string) error {
	if !isValidApprovalMode(c.Approvals.Mode) {
		return fmt.Errorf(messages.ConfigApprovalsModeInvalidFmt, path)
	}

	if c.Agents.Gemini.Enabled == nil {
		return fmt.Errorf(messages.ConfigGeminiEnabledRequiredFmt, path)
	}
	if c.Agents.Claude.Enabled == nil {
		return fmt.Errorf(messages.ConfigClaudeEnabledRequiredFmt, path)
	}
	if c.Agents.ClaudeVSCode.Enabled == nil {
		return fmt.Errorf(messages.ConfigClaudeVSCodeEnabledRequiredFmt, path)
	}
	if c.Agents.Codex.Enabled == nil {
		return fmt.Errorf(messages.ConfigCodexEnabledRequiredFmt, path)
	}
	if c.Agents.VSCode.Enabled == nil {
		return fmt.Errorf(messages.ConfigVSCodeEnabledRequiredFmt, path)
	}
	if c.Agents.Antigravity.Enabled == nil {
		return fmt.Errorf(messages.ConfigAntigravityEnabledRequiredFmt, path)
	}

	seenServerIDs := make(map[string]int, len(c.MCP.Servers))
	for i, server := range c.MCP.Servers {
		if server.ID == "" {
			return fmt.Errorf(messages.ConfigMcpServerIDRequiredFmt, path, i)
		}
		if server.ID == "agent-layer" {
			return fmt.Errorf(messages.ConfigMcpServerIDReservedFmt, path, i)
		}
		if firstIndex, ok := seenServerIDs[server.ID]; ok {
			return fmt.Errorf(messages.ConfigMcpServerIDDuplicateFmt, path, i, server.ID, firstIndex)
		}
		seenServerIDs[server.ID] = i
		if server.Enabled == nil {
			return fmt.Errorf(messages.ConfigMcpServerEnabledRequiredFmt, path, i)
		}
		switch server.Transport {
		case TransportHTTP:
			// Silently strip stdio-only fields; they are meaningless for HTTP.
			c.MCP.Servers[i].Command = ""
			c.MCP.Servers[i].Args = nil
			c.MCP.Servers[i].Env = nil
			if server.URL == "" {
				return fmt.Errorf(messages.ConfigMcpServerURLRequiredFmt, path, i)
			}
			if server.HTTPTransport != "" {
				if _, ok := validHTTPTransports[server.HTTPTransport]; !ok {
					return fmt.Errorf(messages.ConfigMcpServerHTTPTransportInvalidFmt, path, i)
				}
			}
		case TransportStdio:
			// Silently strip HTTP-only fields; they are meaningless for stdio.
			c.MCP.Servers[i].HTTPTransport = ""
			c.MCP.Servers[i].URL = ""
			c.MCP.Servers[i].Headers = nil
			if server.Command == "" {
				return fmt.Errorf(messages.ConfigMcpServerCommandRequiredFmt, path, i)
			}
		default:
			return fmt.Errorf(messages.ConfigMcpServerTransportInvalidFmt, path, i)
		}

		for _, client := range server.Clients {
			if _, ok := validClients[client]; !ok {
				return fmt.Errorf(messages.ConfigMcpServerClientInvalidFmt, path, i, client)
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
	mode := strings.ToLower(strings.TrimSpace(warnings.NoiseMode))
	if _, ok := validWarningNoiseModes[mode]; !ok {
		return fmt.Errorf(messages.ConfigWarningNoiseModeInvalidFmt, path, warnings.NoiseMode)
	}

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
			return fmt.Errorf(messages.ConfigWarningThresholdInvalidFmt, path, threshold.name)
		}
	}
	return nil
}
