package projection

import (
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
)

// BuiltInDispatchServerID is the reserved ID of the Agent Layer MCP server that
// exposes the Agent Dispatch tools. It is derived Agent Layer state rather than
// a `[[mcp.servers]]` entry, and config validation keeps the same ID
// unavailable to user-defined servers.
const BuiltInDispatchServerID = "agent-layer"

// Client identifiers used by MCP projection and `mcp.servers[].clients`.
const (
	ClientAntigravity = "antigravity"
	ClientClaude      = "claude"
	ClientCodex       = "codex"
	ClientCopilot     = "copilot"
	ClientVSCode      = "vscode"
)

const (
	builtInDispatchCommand = "al"
	builtInDispatchSubject = "dispatch"
	builtInDispatchAction  = "mcp-server"
)

// BuiltInDispatchServer returns the derived Agent Dispatch MCP server for one
// client, or false when that client is not an enabled dispatch caller. It is
// the single source every surface reads from: native MCP projection, permission
// allowlists, warning accounting, and doctor.
func BuiltInDispatchServer(cfg config.Config, client string) (ResolvedMCPServer, bool) {
	if !builtInDispatchClientEnabled(cfg, client) {
		return ResolvedMCPServer{}, false
	}
	return ResolvedMCPServer{
		ID:                 BuiltInDispatchServerID,
		Transport:          config.TransportStdio,
		Command:            builtInDispatchCommand,
		Args:               []string{builtInDispatchSubject, builtInDispatchAction},
		ToolTimeoutSeconds: int(config.DispatchMCPToolTimeout(cfg).Seconds()),
	}, true
}

// builtInDispatchClientEnabled reports whether a client surface actually acts
// as an Agent Dispatch caller. Claude Code's terminal and Visual Studio Code
// surfaces share one project MCP configuration, so either one enables it.
func builtInDispatchClientEnabled(cfg config.Config, client string) bool {
	switch client {
	case ClientCodex:
		return config.IsAgentEnabled(cfg.Agents.Codex.Enabled)
	case ClientClaude:
		return config.IsAgentEnabled(cfg.Agents.Claude.Enabled) ||
			config.IsAgentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
	case ClientAntigravity:
		return config.IsAgentEnabled(cfg.Agents.Antigravity.Enabled)
	case ClientCopilot:
		return config.IsAgentEnabled(cfg.Agents.CopilotCLI.Enabled)
	case ClientVSCode:
		return config.IsAgentEnabled(cfg.Agents.VSCode.Enabled)
	default:
		return false
	}
}

// EffectiveMCPServers returns every MCP server a client actually receives: the
// user's enabled servers plus the derived built-in Agent Dispatch server.
func EffectiveMCPServers(cfg config.Config, env map[string]string, client string, resolver EnvVarResolver) ([]ResolvedMCPServer, error) {
	resolved, err := ResolveMCPServers(cfg.MCP.Servers, env, client, resolver)
	if err != nil {
		return nil, err
	}
	return withBuiltInDispatchServer(cfg, client, resolved), nil
}

// EffectiveServerIDs returns the sorted IDs of every MCP server a client
// actually receives.
func EffectiveServerIDs(cfg config.Config, client string) []string {
	ids := EnabledServerIDs(cfg.MCP.Servers, client)
	if _, ok := BuiltInDispatchServer(cfg, client); ok {
		ids = append(ids, BuiltInDispatchServerID)
		sort.Strings(ids)
	}
	return ids
}

// ResolveEffectiveEnabledMCPServers resolves every MCP server that at least one
// enabled client actually receives, including the built-in Agent Dispatch
// server. Warning and doctor accounting use it so the tools Agent Layer itself
// adds are measured alongside user-configured servers.
func ResolveEffectiveEnabledMCPServers(cfg config.Config, env map[string]string) ([]ResolvedMCPServer, error) {
	resolved, err := ResolveEnabledMCPServers(cfg.MCP.Servers, env)
	if err != nil {
		return nil, err
	}
	for _, client := range []string{ClientAntigravity, ClientClaude, ClientCodex, ClientCopilot, ClientVSCode} {
		if builtIn, ok := BuiltInDispatchServer(cfg, client); ok {
			resolved = append(resolved, builtIn)
			sort.Slice(resolved, func(i, j int) bool { return resolved[i].ID < resolved[j].ID })
			break
		}
	}
	return resolved, nil
}

func withBuiltInDispatchServer(cfg config.Config, client string, resolved []ResolvedMCPServer) []ResolvedMCPServer {
	builtIn, ok := BuiltInDispatchServer(cfg, client)
	if !ok {
		return resolved
	}
	resolved = append(resolved, builtIn)
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].ID < resolved[j].ID })
	return resolved
}
