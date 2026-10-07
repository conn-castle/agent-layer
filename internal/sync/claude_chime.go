package sync

import (
	"encoding/json"
	"fmt"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func injectClaudeChimeHook(settings map[string]any) error {
	existingHooks, present := settings[hooksKey]
	var hooks map[string]any
	if !present {
		hooks = make(map[string]any)
		settings[hooksKey] = hooks
	} else {
		var ok bool
		hooks, ok = existingHooks.(map[string]any)
		if !ok {
			return fmt.Errorf(messages.SyncChimeKeyTableConflictFmt, "agents.claude.agent_specific.hooks")
		}
	}
	switch hooks[stopHookKey].(type) {
	case nil, []any:
	default:
		return fmt.Errorf(messages.SyncChimeListConflictFmt, "agents.claude.agent_specific.hooks.Stop")
	}
	hooks[stopHookKey] = appendClaudeChimeStopHook(hooks[stopHookKey])
	return nil
}

func appendClaudeChimeStopHook(existing any) []any {
	values, _ := existing.([]any)
	managedCommands := managedChimeCommandVariants(agentLayerClaudeChimeCommand)
	out, _, _ := filterHookGroupHandlers(values, func(handler any) bool {
		return chimeHandlerMatchesAny(handler, managedCommands)
	}, nil)
	return append(out, map[string]any{
		hooksKey: []any{chimeHandler(agentLayerClaudeChimeCommand)},
	})
}

// cleanClaudeChimeHook removes only Agent Layer's generated chime handler from
// .claude/settings.json. It is used when both Claude surfaces are disabled, so
// the normal settings regeneration path will not run. A symlinked settings
// path is left alone unless it holds the hook, which fails instead of
// rewriting a file outside the repository.
func cleanClaudeChimeHook(sys System, root string) error {
	target, exists, err := existingChimeCleanupTarget(sys, root, ".claude", "settings.json")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	path := target.path
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
	if err := target.checkWritable(); err != nil {
		return err
	}
	out, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalClaudeSettingsFailedFmt, err)
	}
	out = append(out, '\n')
	if err := sys.WriteFileAtomic(path, out, target.mode); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func removeClaudeChimeHook(settings map[string]any) (bool, error) {
	hooksValue, ok := settings[hooksKey]
	if !ok {
		return false, nil
	}
	hooks, ok := hooksValue.(map[string]any)
	if !ok {
		return false, fmt.Errorf(messages.SyncChimeKeyTableConflictFmt, ".claude/settings.json hooks")
	}
	stopValue, ok := hooks[stopHookKey]
	if !ok {
		return false, nil
	}
	stopEntries, ok := stopValue.([]any)
	if !ok {
		return false, fmt.Errorf(messages.SyncChimeListConflictFmt, ".claude/settings.json hooks.Stop")
	}

	commands := managedChimeCommandVariants(agentLayerClaudeChimeCommand)
	filteredStop, changed, err := filterHookGroupHandlers(stopEntries, func(handler any) bool {
		return chimeHandlerMatchesAny(handler, commands)
	}, fmt.Errorf(messages.SyncChimeListConflictFmt, ".claude/settings.json hooks.Stop.hooks"))
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if len(filteredStop) == 0 {
		delete(hooks, stopHookKey)
	} else {
		hooks[stopHookKey] = filteredStop
	}
	if len(hooks) == 0 {
		delete(settings, hooksKey)
	}
	return true, nil
}
