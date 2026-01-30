package sync

import (
	"fmt"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type mcpConfig struct {
	Servers OrderedMap[mcpServer] `json:"mcpServers,omitempty"`
}

type mcpServer struct {
	Type    string             `json:"type"`
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
	URL     string             `json:"url,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
}

// WriteMCPConfig generates .mcp.json for Claude Code.
func WriteMCPConfig(sys System, root string, project *config.ProjectConfig) error {
	cfg, err := buildMCPConfig(sys, project)
	if err != nil {
		return err
	}

	data, err := sys.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(root, ".mcp.json")
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildMCPConfig(sys System, project *config.ProjectConfig) (*mcpConfig, error) {
	cfg := &mcpConfig{
		Servers: make(OrderedMap[mcpServer]),
	}

	// Internal prompt server for Claude.
	promptCommand, promptArgs, err := resolvePromptServerCommand(sys, project.Root)
	if err != nil {
		return nil, err
	}
	cfg.Servers["agent-layer"] = mcpServer{
		Type:    config.TransportStdio,
		Command: promptCommand,
		Args:    promptArgs,
	}

	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"claude",
		projection.ClientPlaceholderResolver("${%s}"),
	)
	if err != nil {
		return nil, err
	}

	for _, server := range resolved {
		entry := mcpServer{
			Type:    server.Transport,
			Command: server.Command,
			Args:    server.Args,
			URL:     server.URL,
		}
		if len(server.Headers) > 0 {
			headers := make(OrderedMap[string], len(server.Headers))
			for key, value := range server.Headers {
				headers[key] = value
			}
			entry.Headers = headers
		}
		if len(server.Env) > 0 {
			envMap := make(OrderedMap[string], len(server.Env))
			for key, value := range server.Env {
				envMap[key] = value
			}
			entry.Env = envMap
		}
		cfg.Servers[server.ID] = entry
	}

	return cfg, nil
}
