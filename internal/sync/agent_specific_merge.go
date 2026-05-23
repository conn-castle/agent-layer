package sync

// mergeAgentSpecificSettings layers agent-specific JSON-shaped overrides onto
// the managed settings map. Map values are deep-merged; scalar and slice
// values are replaced at their key. Shared by Claude and Antigravity since
// both project into a JSON settings.json with identical merge semantics.
func mergeAgentSpecificSettings(settings map[string]any, agentSpecific map[string]any) {
	for key, customValue := range agentSpecific {
		managedMap, managedOK := settings[key].(map[string]any)
		customMap, customOK := customValue.(map[string]any)
		if managedOK && customOK {
			settings[key] = mergeAgentSpecificMap(managedMap, customMap)
			continue
		}
		settings[key] = cloneAgentSpecificValue(customValue)
	}
}

func mergeAgentSpecificMap(managed map[string]any, custom map[string]any) map[string]any {
	merged := make(map[string]any, len(managed)+len(custom))
	for key, value := range managed {
		if _, ok := custom[key]; ok {
			continue
		}
		merged[key] = cloneAgentSpecificValue(value)
	}
	for key, customValue := range custom {
		managedMap, managedOK := managed[key].(map[string]any)
		customMap, customOK := customValue.(map[string]any)
		if managedOK && customOK {
			merged[key] = mergeAgentSpecificMap(managedMap, customMap)
			continue
		}
		merged[key] = cloneAgentSpecificValue(customValue)
	}
	return merged
}

// cloneAgentSpecificValue deep-copies the value types produced by the TOML
// decoder when reading agent_specific (map[string]any, []any, []string).
// Scalars are returned as-is — TOML decode produces no shared references for
// those.
func cloneAgentSpecificValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nestedValue := range typed {
			clone[key] = cloneAgentSpecificValue(nestedValue)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneAgentSpecificValue(item)
		}
		return clone
	case []string:
		clone := make([]string, len(typed))
		copy(clone, typed)
		return clone
	}
	return value
}
