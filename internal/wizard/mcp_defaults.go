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
		return nil, fmt.Errorf(messages.WizardTemplateNoMCPServers)
	}
	return defaults, nil
}

// loadCatalogMCPServers parses the embedded mcp-catalog.toml file into typed MCP server entries.
// Errors loudly when the catalog is missing or unparseable; no silent fallback.
func loadCatalogMCPServers() ([]config.MCPServer, error) {
	data, err := templates.Read(catalogTemplatePath)
	if err != nil {
		return nil, fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	var doc struct {
		MCP struct {
			Servers []config.MCPServer `toml:"servers"`
		} `toml:"mcp"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	return doc.MCP.Servers, nil
}

// loadCatalogDocument returns the parsed line-based tomlDocument for the wizard catalog file.
// The wizard uses this document to source default-shaped [[mcp.servers]] blocks while preserving
// comments and formatting. Errors loudly when the catalog is missing or unreadable.
func loadCatalogDocument() (tomlDocument, error) {
	data, err := templates.Read(catalogTemplatePath)
	if err != nil {
		return tomlDocument{}, fmt.Errorf(messages.WizardReadConfigTemplateFailedFmt, err)
	}
	return parseTomlDocument(string(data)), nil
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

// hasAnyDefaultMCPServer reports whether the current config already contains at least
// one catalog-backed default MCP server. A slim seed with zero defaults is expected
// and should not be treated as a legacy "missing defaults" repair case.
func hasAnyDefaultMCPServer(defaults []DefaultMCPServer, servers []config.MCPServer) bool {
	defaultSet := make(map[string]struct{}, len(defaults))
	for _, def := range defaults {
		if def.ID == "" {
			continue
		}
		defaultSet[def.ID] = struct{}{}
	}
	for _, srv := range servers {
		if _, ok := defaultSet[srv.ID]; ok {
			return true
		}
	}
	return false
}
