package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/projection"
)

type vscodeSettings struct {
	ChatToolsTerminalAutoApprove OrderedMap[bool] `json:"chat.tools.terminal.autoApprove,omitempty"`
}

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

// WriteVSCodeSettings generates .vscode/settings.json.
func WriteVSCodeSettings(root string, project *config.ProjectConfig) error {
	settings, err := buildVSCodeSettings(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", vscodeDir, err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal vscode settings: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(vscodeDir, "settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// WriteVSCodeMCPConfig generates .vscode/mcp.json.
func WriteVSCodeMCPConfig(root string, project *config.ProjectConfig) error {
	cfg, err := buildVSCodeMCPConfig(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", vscodeDir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal vscode mcp config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(vscodeDir, "mcp.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

func buildVSCodeSettings(project *config.ProjectConfig) (*vscodeSettings, error) {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	settings := &vscodeSettings{}

	if approvals.AllowCommands {
		autoApprove := make(OrderedMap[bool])
		for _, cmd := range approvals.Commands {
			pattern := fmt.Sprintf("/^%s(\\b.*)?$/", regexp.QuoteMeta(cmd))
			autoApprove[pattern] = true
		}
		if len(autoApprove) > 0 {
			settings.ChatToolsTerminalAutoApprove = autoApprove
		}
	}

	return settings, nil
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
		func(name string, _ string) string {
			return fmt.Sprintf("${env:%s}", name)
		},
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
