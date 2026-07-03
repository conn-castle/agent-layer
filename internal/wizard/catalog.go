package wizard

import (
	"sync"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// AgentID constants matching config keys
const (
	AgentAntigravity  = "antigravity"
	AgentClaude       = "claude"
	AgentClaudeVSCode = "claude_vscode"
	AgentCodex        = "codex"
	AgentVSCode       = "vscode"
	AgentCopilotCLI   = "copilot_cli"
)

// supportedAgentKeys returns the config field keys for agent enablement in UI order.
func supportedAgentKeys() []string {
	return []string{
		"agents.antigravity.enabled",
		"agents.claude.enabled",
		"agents.claude_vscode.enabled",
		"agents.codex.enabled",
		"agents.vscode.enabled",
		"agents.copilot_cli.enabled",
	}
}

// SupportedAgents returns the agent IDs the wizard can configure.
// Order matches the config field catalog registration order.
func SupportedAgents() []string {
	keys := supportedAgentKeys()
	agents := make([]string, len(keys))
	for i, key := range keys {
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

// extractAgentID extracts the agent ID from a key like "agents.codex.enabled".
func extractAgentID(key string) string {
	// "agents." = 7 chars, ".enabled" = 8 chars
	return key[7 : len(key)-8]
}

// ApprovalModeFieldOptions returns approval mode options from the config field catalog.
// Panics if the approvals.mode field is not in the catalog (programming error).
func ApprovalModeFieldOptions() []config.FieldOption {
	f, ok := config.LookupField("approvals.mode")
	if !ok {
		panic("wizard: approvals.mode field not in config catalog")
	}
	return f.Options
}

var wizardOptionDiscoveryRequestFunc = agentoptions.DefaultDiscoveryRequest

type wizardOptionDiscoveryCache struct {
	mu                     sync.Mutex
	antigravityModelValues []string
	antigravityModelsReady bool
}

func (c *wizardOptionDiscoveryCache) prefetchAntigravityModels() {
	req := wizardOptionDiscoveryRequestFunc()
	go func() {
		values := agentoptions.Values(AgentAntigravity, agentoptions.KindModel, req)
		c.mu.Lock()
		c.antigravityModelValues = values
		c.antigravityModelsReady = true
		c.mu.Unlock()
	}()
}

func (c *wizardOptionDiscoveryCache) antigravityModelOptions() []string {
	c.mu.Lock()
	if c.antigravityModelsReady {
		values := c.antigravityModelValues
		c.mu.Unlock()
		return values
	}
	c.mu.Unlock()
	return agentoptions.Values(AgentAntigravity, agentoptions.KindModel, agentoptions.DiscoveryRequest{})
}

func modelOptions(agent string) []string {
	return agentoptions.Values(agent, agentoptions.KindModel, wizardOptionDiscoveryRequestFunc())
}

func reasoningEffortOptions(agent string) []string {
	return agentoptions.Values(agent, agentoptions.KindReasoningEffort, wizardOptionDiscoveryRequestFunc())
}
