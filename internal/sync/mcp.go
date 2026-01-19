package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/projection"
)

type mcpConfig struct {
	Servers OrderedMap[mcpServer] `json:"mcpServers,omitempty"`
}

type mcpServer struct {
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
	URL     string             `json:"url,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
}

// WriteMCPConfig generates .mcp.json for Claude Code.
func WriteMCPConfig(root string, project *config.ProjectConfig) error {
	cfg, err := buildMCPConfig(project)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(root, ".mcp.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

func buildMCPConfig(project *config.ProjectConfig) (*mcpConfig, error) {
	cfg := &mcpConfig{
		Servers: make(OrderedMap[mcpServer]),
	}

	// Internal prompt server for Claude.
	promptCommand, promptArgs, err := resolvePromptServerCommand(project.Root)
	if err != nil {
		return nil, err
	}
	cfg.Servers["agent-layer"] = mcpServer{
		Command: promptCommand,
		Args:    promptArgs,
	}

	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"claude",
		func(name string, _ string) string {
			return fmt.Sprintf("${%s}", name)
		},
	)
	if err != nil {
		return nil, err
	}

	for _, server := range resolved {
		entry := mcpServer{
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
