package wizard

import (
	"fmt"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// catalogTemplatePath is the embedded MCP server catalog used by the wizard.
// It is internal-only: read from the embedded FS, never written to a user repo.
const catalogTemplatePath = "mcp-catalog.toml"

// DefaultMCPServer describes a default MCP server and its required env vars.
type DefaultMCPServer struct {
	ID          string
	RequiredEnv []string
}

// loadDefaultMCPServers returns default MCP servers derived from the wizard catalog file.
// The catalog is the authoritative source for default-shaped MCP server blocks; the install
// seed deliberately ships with no [[mcp.servers]] entries.
func loadDefaultMCPServers() ([]DefaultMCPServer, error) {
	servers, err := loadCatalogMCPServers()
	if err != nil {
		return nil, err
	}
	defaults := make([]DefaultMCPServer, 0, len(servers))
	for _, server := range servers {
		required := config.RequiredEnvVarsForMCPServer(server)
		defaults = append(defaults, DefaultMCPServer{
			ID:          server.ID,
			RequiredEnv: required,
		})
	}
	if len(defaults) == 0 {
		return nil, fmt.Errorf(messages.WizardCatalogNoMCPServers)
	}
	return defaults, nil
}

// loadCatalogMCPServers parses the embedded mcp-catalog.toml file into typed MCP server entries.
// Errors loudly when the catalog is missing or unparseable; no silent fallback.
func loadCatalogMCPServers() ([]config.MCPServer, error) {
	data, err := templates.Read(catalogTemplatePath)
	if err != nil {
		return nil, fmt.Errorf(messages.WizardLoadMCPCatalogFailedFmt, err)
	}
	var doc struct {
		MCP struct {
			Servers []config.MCPServer `toml:"servers"`
		} `toml:"mcp"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf(messages.WizardLoadMCPCatalogFailedFmt, err)
	}
	return doc.MCP.Servers, nil
}

// loadCatalogDocument returns the parsed line-based tomlDocument for the wizard catalog file.
// The wizard uses this document to source default-shaped [[mcp.servers]] blocks while preserving
// comments and formatting. Errors loudly when the catalog is missing or unreadable.
func loadCatalogDocument() (tomlDocument, error) {
	data, err := templates.Read(catalogTemplatePath)
	if err != nil {
		return tomlDocument{}, fmt.Errorf(messages.WizardLoadMCPCatalogFailedFmt, err)
	}
	return parseTomlDocument(string(data)), nil
}

// customMCPServers returns config MCP servers whose id is not a catalog default,
// preserving config order. These are user-defined servers the wizard surfaces for
// keep/disable; unlike catalog defaults they have no template to restore from, so
// disabling them sets enabled = false rather than deleting the entry. Servers with
// an empty id are skipped because they cannot be addressed by id in the prompt.
func customMCPServers(defaults []DefaultMCPServer, servers []config.MCPServer) []config.MCPServer {
	defaultSet := make(map[string]struct{}, len(defaults))
	for _, def := range defaults {
		if def.ID != "" {
			defaultSet[def.ID] = struct{}{}
		}
	}
	// Wizard recovery loads config leniently (no validation), so a malformed
	// config.toml may repeat the same id. Collapse duplicates, keeping first-seen
	// order, so the id-keyed multiselect does not show indistinguishable options.
	seen := make(map[string]struct{}, len(servers))
	var custom []config.MCPServer
	for _, srv := range servers {
		if srv.ID == "" {
			continue
		}
		if _, isDefault := defaultSet[srv.ID]; isDefault {
			continue
		}
		if _, exists := seen[srv.ID]; exists {
			continue
		}
		seen[srv.ID] = struct{}{}
		custom = append(custom, srv)
	}
	return custom
}
