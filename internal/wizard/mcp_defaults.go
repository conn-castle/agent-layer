package wizard

import (
	"fmt"

	toml "github.com/pelletier/go-toml"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
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

// appendMissingDefaultMCPServers appends template MCP servers for the provided IDs.
// tree is the current config; missing lists server IDs to append; returns an error on failure.
func appendMissingDefaultMCPServers(tree *toml.Tree, missing []string) error {
	if len(missing) == 0 {
		return nil
	}

	blocks, err := defaultMCPServerTrees()
	if err != nil {
		return err
	}

	servers, err := mcpServerTrees(tree)
	if err != nil {
		return err
	}

	for _, id := range missing {
		block, ok := blocks[id]
		if !ok {
			return fmt.Errorf(messages.WizardMissingDefaultMCPServerTemplateFmt, id)
		}
		servers = append(servers, block)
	}

	tree.SetPath([]string{"mcp", "servers"}, servers)
	return nil
}

// mcpServerTrees returns the parsed MCP server trees from the config.
// tree is the parsed config; returns a slice of server trees or an error for unexpected data.
func mcpServerTrees(tree *toml.Tree) ([]*toml.Tree, error) {
	raw := tree.GetPath([]string{"mcp", "servers"})
	if raw == nil {
		return nil, nil
	}
	servers, ok := raw.([]*toml.Tree)
	if !ok {
		return nil, fmt.Errorf(messages.WizardMCPServersUnexpectedTypeFmt, raw)
	}
	return servers, nil
}

// defaultMCPServerTrees loads MCP server trees from the embedded config template.
// It returns a map of server ID to the parsed server tree.
func defaultMCPServerTrees() (map[string]*toml.Tree, error) {
	data, err := templates.Read("config.toml")
	if err != nil {
		return nil, fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	templateTree, err := toml.LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf(messages.WizardParseConfigTemplateFailedFmt, err)
	}
	servers, err := mcpServerTrees(templateTree)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf(messages.WizardNoMCPServerBlocksFound)
	}

	blocks := make(map[string]*toml.Tree, len(servers))
	for _, server := range servers {
		id, ok := server.Get("id").(string)
		if !ok || id == "" {
			return nil, fmt.Errorf(messages.WizardMissingMCPServerIDInTemplate)
		}
		if _, exists := blocks[id]; exists {
			return nil, fmt.Errorf(messages.WizardDuplicateMCPServerIDInTemplateFmt, id)
		}
		blocks[id] = server
	}
	return blocks, nil
}
