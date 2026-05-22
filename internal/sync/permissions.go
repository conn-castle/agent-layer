package sync

import (
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type permissionRenderer interface {
	RenderCommand(pattern string) string
	RenderMCP(serverID string) string
}

// buildPermissionsBlock builds the shared {permissions: {allow: [...]}} payload
// used by clients with action-pattern permission settings.
func buildPermissionsBlock(cfg config.Config, commandsAllow []string, enabledServerIDs []string, renderer permissionRenderer) map[string]any {
	approvals := projection.BuildApprovals(cfg, commandsAllow)
	var allow []string

	if approvals.AllowCommands {
		for _, cmd := range approvals.Commands {
			allow = append(allow, renderer.RenderCommand(cmd))
		}
	}

	if approvals.AllowMCP {
		ids := append([]string(nil), enabledServerIDs...)
		sort.Strings(ids)
		for _, id := range ids {
			allow = append(allow, renderer.RenderMCP(id))
		}
	}

	if len(allow) == 0 {
		return nil
	}
	return map[string]any{"allow": allow}
}

type claudeRenderer struct{}

func (claudeRenderer) RenderCommand(pattern string) string {
	return "Bash(" + pattern + ":*)"
}

func (claudeRenderer) RenderMCP(serverID string) string {
	return "mcp__" + serverID + "__*"
}
