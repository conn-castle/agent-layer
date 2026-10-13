package config

import "slices"

// AppliesToClient reports whether the server is enabled for the given client.
func (s MCPServer) AppliesToClient(client string) bool {
	return len(s.Clients) == 0 || slices.Contains(s.Clients, client)
}
