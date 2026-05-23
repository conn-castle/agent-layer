package sync

import (
	"fmt"
	"path/filepath"
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

	mergeAgentSpecificSettings(settings, project.Config.Agents.Claude.AgentSpecific)
	return settings, nil
}
