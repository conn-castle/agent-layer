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

// validClients lists clients that can appear in mcp.servers[].clients.
// "claude_vscode" is intentionally absent — the Claude VS Code extension shares
// .mcp.json with Claude CLI, so "claude" covers both.
// See Decision p12-unified-vscode-launcher.
var validClients = map[string]struct{}{
	"antigravity": {},
	"claude":      {},
	"vscode":      {},
	"codex":       {},
	"copilot":     {},
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

	if c.Agents.Antigravity.Enabled == nil {
		return fmt.Errorf(messages.ConfigAntigravityEnabledRequiredFmt, path)
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
	if c.Agents.CopilotCLI.Enabled == nil {
		return fmt.Errorf(messages.ConfigCopilotCLIEnabledRequiredFmt, path)
	}
	if err := validateDispatchDefault(path, "agents.antigravity.dispatch.default_agent", c.Agents.Antigravity.Dispatch.DefaultAgent); err != nil {
		return err
	}
	if err := validateAntigravityModelSource(path, c.Agents.Antigravity); err != nil {
		return err
	}
	if err := validateDispatchDefault(path, "agents.claude.dispatch.default_agent", c.Agents.Claude.Dispatch.DefaultAgent); err != nil {
		return err
	}
	if err := validateDispatchDefault(path, "agents.codex.dispatch.default_agent", c.Agents.Codex.Dispatch.DefaultAgent); err != nil {
		return err
	}
	if strings.TrimSpace(c.Agents.CopilotCLI.ReasoningEffort) != "" {
		return fmt.Errorf(messages.ConfigCopilotCLIReasoningEffortUnsupportedFmt, path)
	}

	// Model and reasoning-effort validation: agent model values
	// (agents.antigravity.model, agents.claude.model, agents.codex.model) and
	// reasoning-effort values (agents.claude.reasoning_effort,
	// agents.codex.reasoning_effort) are intentionally NOT validated here. The
	// field catalog (fields.go) defines known options with AllowCustom: true,
	// meaning arbitrary strings are accepted. The downstream client is the
	// authority on which model and effort combinations are valid — Claude Code
	// applies reasoning effort where the active model supports it and ignores it
	// otherwise. See Decision config-field-catalog.
	// (Copilot CLI is the lone exception above: it exposes no reasoning-effort control at
	// all, so a value there is rejected outright.)

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

func validateDispatchDefault(path string, key string, value string) error {
	normalized := strings.TrimSpace(value)
	// Empty/unset is allowed — the registry falls back to "random".
	if normalized == "" {
		return nil
	}
	// Source of truth: the field catalog in fields.go. Looking it up here
	// avoids drift between the wizard's allowed options and the runtime
	// validator, which used to be a duplicate map.
	for _, opt := range dispatchDefaultAgentOptions() {
		if normalized == opt.Value {
			return nil
		}
	}
	return fmt.Errorf(messages.ConfigDispatchDefaultAgentInvalidFmt, path, key, value)
}

func validateAntigravityModelSource(path string, cfg AntigravityConfig) error {
	if HasProviderPassthroughKey(cfg.AgentSpecific, "model") {
		return fmt.Errorf("%w: "+messages.ConfigAntigravityAgentSpecificModelInvalidFmt, ErrConfigNeedsUpgrade, path)
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
