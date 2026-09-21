package sync

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const claudeDirectory = ".claude"

// writeClaudeSettings generates .claude/settings.json.
func writeClaudeSettings(sys System, root string, project *config.ProjectConfig) error {
	settings, err := buildClaudeSettings(root, project)
	if err != nil {
		return err
	}

	claudeDir := filepath.Join(root, claudeDirectory)
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

func buildClaudeSettings(root string, project *config.ProjectConfig) (map[string]any, error) {
	settings := make(map[string]any)
	permissions := buildPermissionsBlock(
		project.Config,
		project.CommandsAllow,
		projection.EffectiveServerIDs(project.Config, projection.ClientClaude),
		claudeRenderer{},
	)
	if permissions != nil {
		settings["permissions"] = permissions
	}
	// Write effortLevel to settings.json for persistable values only.
	// "max" is session-only in Claude Code (passed via --effort CLI flag) and
	// not valid in settings.json, so it is excluded here. Trim to match the
	// warning-helper's canonical form so " max " is treated as "max".
	effort := strings.TrimSpace(project.Config.Agents.Claude.ReasoningEffort)
	if effort != "" && effort != maxEffort {
		settings["effortLevel"] = effort
	}

	// Wire the status line before merging agent_specific so an explicit
	// agent_specific.statusLine override wins. The referenced script is produced
	// by writeClaudeStatusline before settings are written in the same sync.
	if config.ClaudeStatuslineEnabled(project.Config.Agents.Claude) {
		settings["statusLine"] = map[string]any{
			chimeHandlerTypeKey:    chimeHandlerCommandType,
			chimeHandlerCommandKey: "bash " + shellSingleQuote(claudeStatuslinePath(root)),
		}
	}

	if err := ensureNoLegacyAgentSpecificChime(
		"agents.claude.agent_specific.hooks",
		project.Config.Agents.Claude.AgentSpecific[hooksKey],
		agentLayerClaudeChimeCommand,
	); err != nil {
		return nil, err
	}

	mergeAgentSpecificSettings(settings, project.Config.Agents.Claude.AgentSpecific)

	if config.NotificationsChimeEnabled(project.Config) {
		if err := injectClaudeChimeHook(settings); err != nil {
			return nil, err
		}
	}

	// Inject the AskUserQuestion block last so it unions with (rather than is
	// replaced by) any user-supplied agent_specific deny / PreToolUse entries.
	if isQuestionToolDisabled(project.Config.Agents.Claude) {
		if err := injectAskUserQuestionBlock(settings); err != nil {
			return nil, err
		}
	}
	if config.IsAgentEnabled(project.Config.Agents.Muse.Enabled) {
		claudeIDs := make(map[string]bool)
		for _, id := range projection.EffectiveServerIDs(project.Config, projection.ClientClaude) {
			claudeIDs[id] = true
		}
		var excluded []string
		for _, id := range projection.EffectiveServerIDs(project.Config, projection.ClientMuse) {
			if !claudeIDs[id] {
				excluded = append(excluded, id)
			}
		}
		if len(excluded) > 0 {
			// Preserve explicit user exclusions while enforcing client filters.
			existing, err := stringSliceSetting(settings, "disabledMcpjsonServers")
			if err != nil {
				return nil, err
			}
			for _, id := range excluded {
				if !slices.Contains(existing, id) {
					existing = append(existing, id)
				}
			}
			sort.Strings(existing)
			settings["disabledMcpjsonServers"] = existing
		}
	}
	return settings, nil
}

// shellSingleQuote returns a POSIX shell single-quoted word.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// stringSliceSetting validates an agent-specific list before adding managed values.
func stringSliceSetting(settings map[string]any, key string) ([]string, error) {
	raw, exists := settings[key]
	if !exists {
		return nil, nil
	}
	if list, ok := raw.([]string); ok {
		return slices.Clone(list), nil
	}
	if list, ok := raw.([]any); ok {
		result := make([]string, 0, len(list))
		for _, item := range list {
			value, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("agents.claude.agent_specific.%s must be an array of strings", key)
			}
			result = append(result, value)
		}
		return result, nil
	}
	return nil, fmt.Errorf("agents.claude.agent_specific.%s must be an array of strings", key)
}
