package sync

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// writeClaudeSettings generates .claude/settings.json.
func writeClaudeSettings(sys System, root string, project *config.ProjectConfig) error {
	settings, err := buildClaudeSettings(root, project)
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

func buildClaudeSettings(root string, project *config.ProjectConfig) (map[string]any, error) {
	settings := make(map[string]any)
	permissions := buildPermissionsBlock(
		project.Config,
		project.CommandsAllow,
		projection.EnabledServerIDs(project.Config.MCP.Servers, "claude"),
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
	if effort != "" && effort != "max" {
		settings["effortLevel"] = effort
	}

	// Wire the status line before merging agent_specific so an explicit
	// agent_specific.statusLine override wins. The referenced script is produced
	// by writeClaudeStatusline before settings are written in the same sync.
	if config.ClaudeStatuslineEnabled(project.Config.Agents.Claude) {
		settings["statusLine"] = map[string]any{
			"type":    "command",
			"command": "bash " + shellSingleQuote(claudeStatuslinePath(root)),
		}
	}

	if err := ensureNoLegacyAgentSpecificChime(
		"agents.claude.agent_specific.hooks",
		project.Config.Agents.Claude.AgentSpecific["hooks"],
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
	return settings, nil
}

// shellSingleQuote returns a POSIX shell single-quoted word.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
