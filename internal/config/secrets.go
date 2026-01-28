package config

import "sort"

// RequiredEnvVarsForMCPServer returns required env var names for a single MCP server.
// server is the MCP server definition; returns a sorted list of unique names.
func RequiredEnvVarsForMCPServer(server MCPServer) []string {
	seen := make(map[string]struct{})
	add := func(value string) {
		for _, name := range ExtractEnvVarNames(value) {
			if IsBuiltInEnvVar(name) {
				continue
			}
			seen[name] = struct{}{}
		}
	}

	add(server.URL)
	add(server.Command)
	for _, arg := range server.Args {
		add(arg)
	}
	for _, value := range server.Headers {
		add(value)
	}
	for _, value := range server.Env {
		add(value)
	}

	if len(seen) == 0 {
		return nil
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RequiredEnvVarsForMCPServers returns required env var names across all MCP servers.
// servers is a list of MCP server definitions; returns a sorted list of unique names.
func RequiredEnvVarsForMCPServers(servers []MCPServer) []string {
	seen := make(map[string]struct{})
	for _, server := range servers {
		for _, name := range RequiredEnvVarsForMCPServer(server) {
			seen[name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
