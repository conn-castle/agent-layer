package sync

import (
	"fmt"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type geminiSettings struct {
	Tools      *geminiTools                `json:"tools,omitempty"`
	MCPServers OrderedMap[geminiMCPServer] `json:"mcpServers,omitempty"`
}

type geminiTools struct {
	Allowed []string `json:"allowed,omitempty"`
}

type geminiMCPServer struct {
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
	URL     string             `json:"url,omitempty"`
	HTTPURL string             `json:"httpUrl,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
	Trust   *bool              `json:"trust,omitempty"`
}

// WriteGeminiSettings generates .gemini/settings.json.
func WriteGeminiSettings(sys System, root string, project *config.ProjectConfig) error {
	settings, err := buildGeminiSettings(sys, project)
	if err != nil {
		return err
	}

	geminiDir := filepath.Join(root, ".gemini")
	if err := sys.MkdirAll(geminiDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, geminiDir, err)
	}

	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalGeminiSettingsFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(geminiDir, "settings.json")
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildGeminiSettings(sys System, project *config.ProjectConfig) (*geminiSettings, error) {
	settings := &geminiSettings{
		MCPServers: make(OrderedMap[geminiMCPServer]),
	}

	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	allowCommands := approvals.AllowCommands
	if allowCommands {
		allowed := make([]string, 0, len(approvals.Commands))
		for _, cmd := range approvals.Commands {
			allowed = append(allowed, fmt.Sprintf("run_shell_command(%s)", cmd))
		}
		if len(allowed) > 0 {
			settings.Tools = &geminiTools{Allowed: allowed}
		}
	}

	allowMCP := approvals.AllowMCP
	trust := allowMCP

	// Internal prompt server
	promptCommand, promptArgs, err := resolvePromptServerCommand(sys, project.Root)
	if err != nil {
		return nil, err
	}
	promptEnv, err := resolvePromptServerEnv(project.Root)
	if err != nil {
		return nil, err
	}
	settings.MCPServers["agent-layer"] = geminiMCPServer{
		Command: promptCommand,
		Args:    promptArgs,
		Env:     promptEnv,
		Trust:   &trust,
	}

	// Preserve env var placeholders - Gemini CLI resolves ${VAR} at runtime.
	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"gemini",
		projection.ClientPlaceholderResolver("${%s}"),
	)
	if err != nil {
		return nil, err
	}
	for _, server := range resolved {
		entry := geminiMCPServer{
			Command: server.Command,
			Args:    server.Args,
			Trust:   &trust,
		}
		if server.Transport == config.TransportHTTP {
			if server.HTTPTransport == "streamable" {
				entry.HTTPURL = server.URL
			} else {
				entry.URL = server.URL
			}
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
		settings.MCPServers[server.ID] = entry
	}

	return settings, nil
}
