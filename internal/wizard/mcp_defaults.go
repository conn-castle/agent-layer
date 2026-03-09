package wizard

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// DefaultMCPServer describes a default MCP server and its required env vars.
type DefaultMCPServer struct {
	ID          string
	RequiredEnv []string
}

// loadDefaultMCPServers returns default MCP servers derived from the template config.
func loadDefaultMCPServers() ([]DefaultMCPServer, error) {
	cfg, err := config.LoadTemplateConfig()
	if err != nil {
		return nil, err
	}
	defaults := make([]DefaultMCPServer, 0, len(cfg.MCP.Servers))
	for _, server := range cfg.MCP.Servers {
		required := config.RequiredEnvVarsForMCPServer(server)
		defaults = append(defaults, DefaultMCPServer{
			ID:          server.ID,
			RequiredEnv: required,
		})
	}
	if len(defaults) == 0 {
		return nil, fmt.Errorf(messages.WizardTemplateNoMCPServers)
	}
	return defaults, nil
}

// missingDefaultMCPServers returns default MCP server IDs absent from the current config.
// defaults is the list of default servers; servers is the current config server list.
func missingDefaultMCPServers(defaults []DefaultMCPServer, servers []config.MCPServer) []string {
	existing := make(map[string]bool, len(servers))
	for _, srv := range servers {
		if srv.ID == "" {
			continue
		}
		existing[srv.ID] = true
	}

	var missing []string
	for _, def := range defaults {
		if !existing[def.ID] {
			missing = append(missing, def.ID)
		}
	}
	return missing
}
