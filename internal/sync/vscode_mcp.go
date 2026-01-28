package sync

import (
	"fmt"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type vscodeMCPConfig struct {
	Servers OrderedMap[vscodeMCPServer] `json:"servers"`
}

type vscodeMCPServer struct {
	Type    string             `json:"type,omitempty"`
	URL     string             `json:"url,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
}

// WriteVSCodeMCPConfig generates .vscode/mcp.json.
func WriteVSCodeMCPConfig(sys System, root string, project *config.ProjectConfig) error {
	cfg, err := buildVSCodeMCPConfig(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := sys.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, vscodeDir, err)
	}

	data, err := sys.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalVSCodeMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(vscodeDir, "mcp.json")
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildVSCodeMCPConfig(project *config.ProjectConfig) (*vscodeMCPConfig, error) {
	cfg := &vscodeMCPConfig{
		Servers: make(OrderedMap[vscodeMCPServer]),
	}

	// Transform to VS Code env syntax - VS Code resolves ${env:VAR} at runtime.
	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"vscode",
		projection.ClientPlaceholderResolver("${env:%s}"),
	)
	if err != nil {
		return nil, err
	}

	for _, server := range resolved {
		entry := vscodeMCPServer{
			Type: server.Transport,
			URL:  server.URL,
		}

		if server.Transport == "stdio" {
			entry.Command = server.Command
			entry.Args = server.Args
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
