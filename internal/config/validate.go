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

func isValidFieldOption(key string, value string) bool {
	field, ok := LookupField(key)
	if !ok {
		return false
	}
	for _, opt := range field.Options {
		if opt.Value == value {
			return true
		}
	}
	return false
}

// ClaudeModelSupportsReasoningEffort reports whether the given Claude model
// string identifies an Opus variant that supports reasoning effort.
// Matches "opus", "opusplan", "claude-opus-4-6", etc., but not "corpus".
func ClaudeModelSupportsReasoningEffort(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" || normalized == "default" {
		return false
	}
	// Match model strings that start with "opus" (e.g., "opus", "opusplan") or
	// contain "opus" after a delimiter (e.g., "claude-opus-4-6").
	if strings.HasPrefix(normalized, "opus") {
		return true
	}
	return strings.Contains(normalized, "-opus") || strings.Contains(normalized, "_opus")
}

// validClients lists clients that can appear in mcp.servers[].clients.
// "claude_vscode" is intentionally absent — the Claude VS Code extension shares
// .mcp.json with Claude CLI, so "claude" covers both.
// See Decision p12-unified-vscode-launcher.
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
	"quiet":   {},
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
	if strings.TrimSpace(c.Agents.Gemini.ReasoningEffort) != "" {
		return fmt.Errorf(messages.ConfigGeminiReasoningEffortUnsupportedFmt, path)
	}

	claudeReasoningEffort := strings.TrimSpace(c.Agents.Claude.ReasoningEffort)
	if claudeReasoningEffort != "" {
		if !isValidFieldOption("agents.claude.reasoning_effort", claudeReasoningEffort) {
			return fmt.Errorf(messages.ConfigClaudeReasoningEffortInvalidFmt, path)
		}
		if !ClaudeModelSupportsReasoningEffort(c.Agents.Claude.Model) {
			return fmt.Errorf(messages.ConfigClaudeReasoningEffortModelUnsupportedFmt, path, c.Agents.Claude.Model)
		}
	}

	// Model validation: agent model values (agents.gemini.model, agents.claude.model,
	// agents.codex.model, agents.codex.reasoning_effort) are intentionally NOT validated
	// here. The field catalog (fields.go) defines known options with AllowCustom: true,
	// meaning arbitrary model strings are accepted. The downstream client is the authority
	// on valid model names. See Decision config-field-catalog. Claude reasoning effort is
	// validated separately above because only a subset of models support it.

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
