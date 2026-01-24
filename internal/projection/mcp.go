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
	ID        string
	Transport string
	URL       string
	Headers   map[string]string
	Command   string
	Args      []string
	Env       map[string]string
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

		entry := ResolvedMCPServer{
			ID:        server.ID,
			Transport: server.Transport,
		}

		switch server.Transport {
		case "http":
			url, err := config.SubstituteEnvVarsWith(server.URL, env, resolver)
			if err != nil {
				return nil, fmt.Errorf(messages.MCPServerURLFmt, server.ID, err)
			}
			entry.URL = url

			if len(server.Headers) > 0 {
				headers := make(map[string]string, len(server.Headers))
				for key, value := range server.Headers {
					resolvedValue, err := config.SubstituteEnvVarsWith(value, env, resolver)
					if err != nil {
						return nil, fmt.Errorf(messages.MCPServerHeaderFmt, server.ID, key, err)
					}
					headers[key] = resolvedValue
				}
				entry.Headers = headers
			}
		case "stdio":
			command, err := config.SubstituteEnvVarsWith(server.Command, env, resolver)
			if err != nil {
				return nil, fmt.Errorf(messages.MCPServerCommandFmt, server.ID, err)
			}
			entry.Command = command

			if len(server.Args) > 0 {
				args := make([]string, 0, len(server.Args))
				for _, arg := range server.Args {
					resolvedArg, err := config.SubstituteEnvVarsWith(arg, env, resolver)
					if err != nil {
						return nil, fmt.Errorf(messages.MCPServerArgFmt, server.ID, arg, err)
					}
					args = append(args, resolvedArg)
				}
				entry.Args = args
			}

			if len(server.Env) > 0 {
				envMap := make(map[string]string, len(server.Env))
				for key, value := range server.Env {
					resolvedValue, err := config.SubstituteEnvVarsWith(value, env, resolver)
					if err != nil {
						return nil, fmt.Errorf(messages.MCPServerEnvFmt, server.ID, key, err)
					}
					envMap[key] = resolvedValue
				}
				entry.Env = envMap
			}
		default:
			return nil, fmt.Errorf(messages.MCPServerUnsupportedTransportFmt, server.ID, server.Transport)
		}

		resolved = append(resolved, entry)
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].ID < resolved[j].ID
	})

	return resolved, nil
}
