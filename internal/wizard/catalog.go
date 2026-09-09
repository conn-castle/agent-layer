package wizard

import (
	"context"
	"fmt"
	"io"

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
	AgentGrok         = "grok"
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
		"agents.grok.enabled",
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
	project *config.ProjectConfig
	out     io.Writer
	ctx     context.Context
	entries map[string]*wizardModelDiscovery
}

type wizardModelDiscovery struct {
	done   chan struct{}
	option agentoptions.OptionSet
}

// The UI goroutine owns the map. Workers publish results by closing done.
func (c *wizardOptionDiscoveryCache) prefetch(agent string) *wizardModelDiscovery {
	if c.entries == nil {
		c.entries = make(map[string]*wizardModelDiscovery)
	}
	if entry := c.entries[agent]; entry != nil {
		return entry
	}
	entry := &wizardModelDiscovery{done: make(chan struct{})}
	c.entries[agent] = entry
	req := wizardOptionDiscoveryRequestFunc()
	req.Project = c.project
	req.Context = c.ctx
	if !req.Live || !agentoptions.HasModelDiscovery(agent) {
		entry.option = agentoptions.Resolve(config.Config{}, agent, agentoptions.KindModel, req)
		close(entry.done)
		return entry
	}
	go func() {
		entry.option = agentoptions.Resolve(config.Config{}, agent, agentoptions.KindModel, req)
		close(entry.done)
	}()
	return entry
}

func (c *wizardOptionDiscoveryCache) prefetchAll() {
	for _, agent := range SupportedAgents() {
		if agentoptions.HasModelDiscovery(agent) {
			c.prefetch(agent)
		}
	}
}

func (c *wizardOptionDiscoveryCache) selectModel(ui UI, agent, title string, value *string) error {
	// Scripted answers are explicit configuration, not a request for suggestions.
	if scripted, ok := ui.(*ScriptedUI); ok {
		answer, _, found := lookupStringScriptedAnswer(scripted.answers.Select, title)
		if !found {
			return missingScriptedAnswer("select", title)
		}
		return selectOptionalValue(ui, title, []string{answer}, value)
	}
	if !agentoptions.HasModelDiscovery(agent) {
		// Agents without a discovery adapter accept explicit input only.
		return selectOptionalValue(ui, title, nil, value)
	}
	entry := c.prefetch(agent)
	select {
	case <-entry.done:
	default:
		if c.out != nil {
			_, _ = fmt.Fprintf(c.out, "Discovering %s models…\n", agent)
		}
		<-entry.done
	}
	if entry.option.DiscoveryError != "" {
		if err := ui.Note(fmt.Sprintf("Cannot discover %s models", agent), fmt.Sprintf("%s; check harness installation, authentication, and connectivity. You can still use the client default or enter a custom model.", entry.option.DiscoveryError)); err != nil {
			return err
		}
		return selectOptionalValue(ui, title, nil, value)
	}
	return selectOptionalValue(ui, title, entry.option.Suggestions, value)
}

func reasoningEffortOptions(agent string) []string {
	return agentoptions.Values(agent, agentoptions.KindReasoningEffort, wizardOptionDiscoveryRequestFunc())
}
