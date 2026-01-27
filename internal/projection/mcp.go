package projection

import (
	"fmt"
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// EnvVarResolver returns a replacement string for a resolved env var.
type EnvVarResolver = config.EnvVarReplacer

// ResolvedMCPServer is a normalized MCP server with env substitution applied.
type ResolvedMCPServer struct {
	ID            string
	Transport     string
	URL           string
	Headers       map[string]string
	HTTPTransport string
	Command       string
	Args          []string
	Env           map[string]string
}

// EnabledServerIDs returns sorted MCP server ids enabled for the client.
func EnabledServerIDs(servers []config.MCPServer, client string) []string {
	var ids []string
	for _, server := range servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		if !server.AppliesToClient(client) {
			continue
		}
		ids = append(ids, server.ID)
	}
	sort.Strings(ids)
	return ids
}

// ResolveMCPServers filters and resolves MCP servers for a client.
func ResolveMCPServers(servers []config.MCPServer, env map[string]string, client string, resolver EnvVarResolver) ([]ResolvedMCPServer, error) {
	if resolver == nil {
		resolver = func(_ string, value string) string {
			return value
		}
	}

	var resolved []ResolvedMCPServer
	for _, server := range servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		if !server.AppliesToClient(client) {
			continue
		}

		entry, err := resolveSingleServer(server, env, resolver)
		if err != nil {
			return nil, &MCPServerResolveError{ServerID: server.ID, Err: err}
		}
		if server.Transport != "http" && server.Transport != "stdio" {
			return nil, fmt.Errorf(messages.MCPServerUnsupportedTransportFmt, server.ID, server.Transport)
		}

		resolved = append(resolved, entry)
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].ID < resolved[j].ID
	})

	return resolved, nil
}
