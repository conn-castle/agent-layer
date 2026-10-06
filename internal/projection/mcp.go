package projection

import "github.com/conn-castle/agent-layer/internal/config"

// EnvVarResolver returns a replacement string for a resolved env var.
type EnvVarResolver = config.EnvVarReplacer

// ResolvedMCPServer is a normalized MCP server with env substitution applied.
type ResolvedMCPServer struct {
	ID            string
	Transport     string
	URL           string
	Headers       map[string]string
	HTTPTransport string
	Auth          string
	Command       string
	Args          []string
	Env           map[string]string
	// ToolTimeoutSeconds is a per-server execution bound. Only the built-in
	// Agent Dispatch server sets it, and only clients with a documented
	// per-server timeout field project it; zero means the client default.
	ToolTimeoutSeconds int
}

// clientServers returns the enabled servers that apply to client, in
// configuration order.
func clientServers(servers []config.MCPServer, client string) []config.MCPServer {
	var matched []config.MCPServer
	for _, server := range servers {
		if config.IsAgentEnabled(server.Enabled) && server.AppliesToClient(client) {
			matched = append(matched, server)
		}
	}
	return matched
}
