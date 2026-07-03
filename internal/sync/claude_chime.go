package sync

import (
	"encoding/json"
	"fmt"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func injectClaudeChimeHook(settings map[string]any) error {
	existingHooks, present := settings["hooks"]
	var hooks map[string]any
	if !present {
		hooks = make(map[string]any)
		settings["hooks"] = hooks
	} else {
		var ok bool
		hooks, ok = existingHooks.(map[string]any)
		if !ok {
			return fmt.Errorf(messages.SyncChimeKeyTableConflictFmt, "agents.claude.agent_specific.hooks")
		}
	}
	switch hooks["Stop"].(type) {
	case nil, []any:
	default:
		return fmt.Errorf(messages.SyncChimeListConflictFmt, "agents.claude.agent_specific.hooks.Stop")
	}
	hooks["Stop"] = appendClaudeChimeStopHook(hooks["Stop"])
	return nil
}

func appendClaudeChimeStopHook(existing any) []any {
	var out []any
	if values, ok := existing.([]any); ok {
		out = append(out, values...)
		commands := map[string]struct{}{agentLayerClaudeChimeCommand: {}}
		for _, entry := range values {
			group, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			handlers, ok := group["hooks"].([]any)
			if !ok {
				continue
			}
			for _, handler := range handlers {
				if chimeHandlerMatchesAny(handler, commands) {
					return out
				}
			}
		}
	}
	return append(out, map[string]any{
		"hooks": []any{chimeHandler(agentLayerClaudeChimeCommand)},
	})
}

// CleanClaudeChimeHook removes only Agent Layer's generated chime handler from
// .claude/settings.json. It is used when both Claude surfaces are disabled, so
// the normal settings regeneration path will not run.
func CleanClaudeChimeHook(sys System, root string) error {
	path, exists, err := existingChimeCleanupTarget(sys, root, ".claude", "settings.json")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	if !containsChimeCommandText(string(data), agentLayerClaudeChimeCommand) {
		return nil
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("invalid Claude settings %s: %w", path, err)
	}
	changed, err := removeClaudeChimeHook(settings)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	out, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalClaudeSettingsFailedFmt, err)
	}
	out = append(out, '\n')
	if err := sys.WriteFileAtomic(path, out, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func removeClaudeChimeHook(settings map[string]any) (bool, error) {
	hooksValue, ok := settings["hooks"]
	if !ok {
		return false, nil
	}
	hooks, ok := hooksValue.(map[string]any)
	if !ok {
		return false, fmt.Errorf(messages.SyncChimeKeyTableConflictFmt, ".claude/settings.json hooks")
	}
	stopValue, ok := hooks["Stop"]
	if !ok {
		return false, nil
	}
	stopEntries, ok := stopValue.([]any)
	if !ok {
		return false, fmt.Errorf(messages.SyncChimeListConflictFmt, ".claude/settings.json hooks.Stop")
	}

	commands := legacyChimeCommandVariants(agentLayerClaudeChimeCommand)
	changed := false
	filteredStop := make([]any, 0, len(stopEntries))
	for _, entry := range stopEntries {
		group, ok := entry.(map[string]any)
		if !ok {
			filteredStop = append(filteredStop, entry)
			continue
		}
		handlersValue, ok := group["hooks"]
		if !ok {
			filteredStop = append(filteredStop, entry)
			continue
		}
		handlers, ok := handlersValue.([]any)
		if !ok {
			return false, fmt.Errorf(messages.SyncChimeListConflictFmt, ".claude/settings.json hooks.Stop.hooks")
		}
		filteredHandlers := make([]any, 0, len(handlers))
		for _, handler := range handlers {
			if chimeHandlerMatchesAny(handler, commands) {
				changed = true
				continue
			}
			filteredHandlers = append(filteredHandlers, handler)
		}
		if len(filteredHandlers) == 0 && len(group) == 1 {
			changed = true
			continue
		}
		group["hooks"] = filteredHandlers
		filteredStop = append(filteredStop, group)
	}
	if !changed {
		return false, nil
	}
	if len(filteredStop) == 0 {
		delete(hooks, "Stop")
	} else {
		hooks["Stop"] = filteredStop
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return true, nil
}
