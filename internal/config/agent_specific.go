package config

// CodexReservedKeys are top-level keys in .codex/config.toml managed by Agent Layer.
var CodexReservedKeys = map[string]struct{}{
	"approval_policy":        {},
	"mcp_servers":            {},
	"model":                  {},
	"model_reasoning_effort": {},
	"sandbox_mode":           {},
	"web_search":             {},
}

// ClaudeReservedKeys are top-level keys in .claude/settings.json managed by Agent Layer.
var ClaudeReservedKeys = map[string]struct{}{
	"permissions": {},
}

// HasAgentSpecificKey returns true when the agent-specific map defines a top-level key.
func HasAgentSpecificKey(agentSpecific map[string]any, key string) bool {
	if len(agentSpecific) == 0 {
		return false
	}
	_, ok := agentSpecific[key]
	return ok
}
