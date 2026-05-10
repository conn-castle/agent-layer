package sync

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// WriteClaudeSettings generates .claude/settings.json.
func WriteClaudeSettings(sys System, root string, project *config.ProjectConfig) error {
	settings, err := buildClaudeSettings(project)
	if err != nil {
		return err
	}

	claudeDir := filepath.Join(root, ".claude")
	if err := sys.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, claudeDir, err)
	}

	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalClaudeSettingsFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(claudeDir, "settings.json")
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildClaudeSettings(project *config.ProjectConfig) (map[string]any, error) {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	var allow []string

	if approvals.AllowCommands {
		for _, cmd := range approvals.Commands {
			allow = append(allow, fmt.Sprintf("Bash(%s:*)", cmd))
		}
	}

	if approvals.AllowMCP {
		ids := projection.EnabledServerIDs(project.Config.MCP.Servers, "claude")
		sort.Strings(ids)
		for _, id := range ids {
			allow = append(allow, fmt.Sprintf("mcp__%s__*", id))
		}
	}

	settings := make(map[string]any)
	if len(allow) > 0 {
		settings["permissions"] = map[string]any{
			"allow": allow,
		}
	}
	// Write effortLevel to settings.json for persistable values only.
	// "max" is session-only in Claude Code (passed via --effort CLI flag) and
	// not valid in settings.json, so it is excluded here. Trim to match the
	// warning-helper's canonical form so " max " is treated as "max".
	effort := strings.TrimSpace(project.Config.Agents.Claude.ReasoningEffort)
	if effort != "" && effort != "max" {
		settings["effortLevel"] = effort
	}

	mergeClaudeSettings(settings, project.Config.Agents.Claude.AgentSpecific)
	return settings, nil
}

func mergeClaudeSettings(settings map[string]any, agentSpecific map[string]any) {
	for key, customValue := range agentSpecific {
		managedMap, managedOK := settings[key].(map[string]any)
		customMap, customOK := customValue.(map[string]any)
		if managedOK && customOK {
			settings[key] = mergeClaudeSettingsMap(managedMap, customMap)
			continue
		}
		settings[key] = cloneClaudeSettingValue(customValue)
	}
}

func mergeClaudeSettingsMap(managed map[string]any, custom map[string]any) map[string]any {
	merged := make(map[string]any, len(managed)+len(custom))
	for key, value := range managed {
		if _, ok := custom[key]; ok {
			continue
		}
		merged[key] = cloneClaudeSettingValue(value)
	}
	for key, customValue := range custom {
		managedMap, managedOK := managed[key].(map[string]any)
		customMap, customOK := customValue.(map[string]any)
		if managedOK && customOK {
			merged[key] = mergeClaudeSettingsMap(managedMap, customMap)
			continue
		}
		merged[key] = cloneClaudeSettingValue(customValue)
	}
	return merged
}

// cloneClaudeSettingValue returns a deep copy of value for the types Agent Layer
// projects into .claude/settings.json: map[string]any, []any, and []string. Other
// types are returned as-is because TOML decode does not produce shared references
// for them (scalars are copied by value).
func cloneClaudeSettingValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nestedValue := range typed {
			clone[key] = cloneClaudeSettingValue(nestedValue)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneClaudeSettingValue(item)
		}
		return clone
	case []string:
		clone := make([]string, len(typed))
		copy(clone, typed)
		return clone
	}
	return value
}
