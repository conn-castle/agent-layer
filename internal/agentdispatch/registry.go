package agentdispatch

import (
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

type targetMeta struct {
	Name                       string
	Binary                     string
	SkillPrefix                string
	SharedSkillProject         bool
	SessionMCPServersSupported bool
	SessionMCPServersReason    string
}

func targetRegistry() []targetMeta {
	return []targetMeta{
		{
			Name:                       AgentCodex,
			Binary:                     AgentCodex,
			SkillPrefix:                "$",
			SharedSkillProject:         true,
			SessionMCPServersSupported: true,
		},
		{
			Name:                       AgentClaude,
			Binary:                     AgentClaude,
			SkillPrefix:                "/",
			SessionMCPServersSupported: true,
		},
		{
			Name:                       AgentAntigravity,
			Binary:                     "agy",
			SkillPrefix:                "/",
			SharedSkillProject:         true,
			SessionMCPServersSupported: true,
		},
		{
			Name:                       AgentGrok,
			Binary:                     AgentGrok,
			SkillPrefix:                "/",
			SharedSkillProject:         true,
			SessionMCPServersSupported: true,
		},
		{
			Name:                       AgentMuse,
			Binary:                     AgentMuse,
			SkillPrefix:                "/",
			SharedSkillProject:         true,
			SessionMCPServersSupported: true,
		},
	}
}

func lookupTarget(name string) (targetMeta, bool) {
	normalized := normalizeAgent(name)
	for _, target := range targetRegistry() {
		if target.Name == normalized {
			return target, true
		}
	}
	return targetMeta{}, false
}

func normalizeAgent(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func targetEnabled(cfg config.Config, target string) bool {
	switch target {
	case AgentCodex:
		return config.IsAgentEnabled(cfg.Agents.Codex.Enabled)
	case AgentClaude:
		return config.IsAgentEnabled(cfg.Agents.Claude.Enabled)
	case AgentAntigravity:
		return config.IsAgentEnabled(cfg.Agents.Antigravity.Enabled)
	case AgentGrok:
		return config.IsAgentEnabled(cfg.Agents.Grok.Enabled)
	case AgentMuse:
		return config.IsAgentEnabled(cfg.Agents.Muse.Enabled)
	default:
		return false
	}
}
