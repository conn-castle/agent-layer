package wizard

import "github.com/conn-castle/agent-layer/internal/config"

// AgentID constants matching config keys
const (
	AgentGemini       = "gemini"
	AgentClaude       = "claude"
	AgentClaudeVSCode = "claude-vscode"
	AgentCodex        = "codex"
	AgentVSCode       = "vscode"
	AgentAntigravity  = "antigravity"
)

// supportedAgentKeys lists the config field keys for agent enablement in UI order.
var supportedAgentKeys = []string{
	"agents.gemini.enabled",
	"agents.claude.enabled",
	"agents.claude-vscode.enabled",
	"agents.codex.enabled",
	"agents.vscode.enabled",
	"agents.antigravity.enabled",
}

// SupportedAgents returns the agent IDs the wizard can configure.
// Order matches the config field catalog registration order.
func SupportedAgents() []string {
	agents := make([]string, len(supportedAgentKeys))
	for i, key := range supportedAgentKeys {
		f, ok := config.LookupField(key)
		if !ok {
			// Defensive: key must exist in catalog.
			panic("wizard: agent field " + key + " not in config catalog")
		}
		// Extract agent ID from "agents.<id>.enabled"
		agents[i] = extractAgentID(f.Key)
	}
	return agents
}

// extractAgentID extracts the agent ID from a key like "agents.gemini.enabled".
func extractAgentID(key string) string {
	// "agents." = 7 chars, ".enabled" = 8 chars
	return key[7 : len(key)-8]
}

// ApprovalMode constants
const (
	ApprovalAll      = "all"
	ApprovalMCP      = "mcp"
	ApprovalCommands = "commands"
	ApprovalNone     = "none"
	ApprovalYOLO     = "yolo"
)

// ApprovalModeFieldOptions returns approval mode options from the config field catalog.
// Panics if the approvals.mode field is not in the catalog (programming error).
func ApprovalModeFieldOptions() []config.FieldOption {
	f, ok := config.LookupField("approvals.mode")
	if !ok {
		panic("wizard: approvals.mode field not in config catalog")
	}
	return f.Options
}

// GeminiModels returns supported Gemini model values from the config field catalog.
func GeminiModels() []string {
	return config.FieldOptionValues("agents.gemini.model")
}

// ClaudeModels returns supported Claude model values from the config field catalog.
func ClaudeModels() []string {
	return config.FieldOptionValues("agents.claude.model")
}

// CodexModels returns supported Codex model values from the config field catalog.
func CodexModels() []string {
	return config.FieldOptionValues("agents.codex.model")
}

// CodexReasoningEfforts returns supported reasoning effort values from the config field catalog.
func CodexReasoningEfforts() []string {
	return config.FieldOptionValues("agents.codex.reasoning_effort")
}
