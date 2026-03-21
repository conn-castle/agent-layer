package sync

import (
	"fmt"
	"path/filepath"
	"sort"

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
	if len(allow) > 0 && !config.HasAgentSpecificKey(project.Config.Agents.Claude.AgentSpecific, "permissions") {
		settings["permissions"] = map[string]any{
			"allow": allow,
		}
	}
	// Write effortLevel to settings.json for persistable values only.
	// "max" is session-only in Claude Code (passed via --effort CLI flag) and
	// not valid in settings.json, so it is excluded here.
	effort := project.Config.Agents.Claude.ReasoningEffort
	if effort != "" && effort != "max" && !config.HasAgentSpecificKey(project.Config.Agents.Claude.AgentSpecific, "effortLevel") {
		settings["effortLevel"] = effort
	}

	if len(project.Config.Agents.Claude.AgentSpecific) > 0 {
		for key, value := range project.Config.Agents.Claude.AgentSpecific {
			settings[key] = value
		}
	}

	return settings, nil
}
