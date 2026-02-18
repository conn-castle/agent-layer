package sync

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type claudeSettings struct {
	Permissions *claudePermissions `json:"permissions,omitempty"`
}

type claudePermissions struct {
	Allow []string `json:"allow,omitempty"`
}

// WriteClaudeSettings generates .claude/settings.json.
func WriteClaudeSettings(sys System, root string, project *config.ProjectConfig) error {
	settings, _, err := buildClaudeSettings(project)
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

func buildClaudeSettings(project *config.ProjectConfig) (*claudeSettings, []string, error) {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	var allow []string

	if approvals.AllowCommands {
		for _, cmd := range approvals.Commands {
			allow = append(allow, fmt.Sprintf("Bash(%s:*)", cmd))
		}
	}

	if approvals.AllowMCP {
		ids := projection.EnabledServerIDs(project.Config.MCP.Servers, "claude")
		ids = append(ids, "agent-layer")
		sort.Strings(ids)
		for _, id := range ids {
			allow = append(allow, fmt.Sprintf("mcp__%s__*", id))
		}
	}

	// Auto-approved skills: add prompt server tool patterns.
	// Skip if AllowMCP is true — the wildcard mcp__agent-layer__* already covers all.
	var autoApprovedNames []string
	if !approvals.AllowMCP {
		for _, cmd := range project.SlashCommands {
			if cmd.AutoApprove {
				autoApprovedNames = append(autoApprovedNames, cmd.Name)
				allow = append(allow, fmt.Sprintf("mcp__agent-layer__%s", cmd.Name))
			}
		}
		sort.Strings(autoApprovedNames)
	}

	settings := &claudeSettings{}
	if len(allow) > 0 {
		settings.Permissions = &claudePermissions{Allow: allow}
	}

	return settings, autoApprovedNames, nil
}
