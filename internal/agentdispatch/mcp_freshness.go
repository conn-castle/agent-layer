package agentdispatch

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/executableimage"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// checkFreshness runs before discovery, configuration or provider access. Work
// already admitted keeps this image; durable control operations remain available.
func (s *dispatchToolServer) checkFreshness() error {
	var err error
	selection := versiondispatch.DevelopmentExecutable(s.root)
	if selection != "" {
		err = executableimage.SameAs(selection)
	} else {
		var matches bool
		selection, matches, err = versiondispatch.ReleaseSelection(s.root, s.version)
		if err == nil && !matches {
			err = fmt.Errorf("project selects %s", selection)
		}
		if err == nil {
			err = executableimage.Unchanged()
		}
	}
	if err == nil {
		return nil
	}
	return fmt.Errorf("agent-layer MCP server is stale or freshness cannot be verified (running %s, selected %q): %w. No provider work was started. Reconnect the agent-layer MCP server (Claude Code: /mcp reconnect; Codex app: restart the app or reload MCP configuration), then retry. Wait, inspect, output and cancel remain available", s.version, selection, err)
}
