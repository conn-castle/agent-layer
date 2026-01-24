package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
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
func WriteClaudeSettings(root string, project *config.ProjectConfig) error {
	settings, err := buildClaudeSettings(project)
	if err != nil {
		return err
	}

	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, claudeDir, err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalClaudeSettingsFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(claudeDir, "settings.json")
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildClaudeSettings(project *config.ProjectConfig) (*claudeSettings, error) {
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

	settings := &claudeSettings{}
	if len(allow) > 0 {
		settings.Permissions = &claudePermissions{Allow: allow}
	}

	return settings, nil
}
