package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
	projectroot "github.com/conn-castle/agent-layer/internal/root"
)

const mcpGeneratedBy = "agent-layer"
const ghConfigDirEnv = "GH_CONFIG_DIR"
const xdgConfigHomeEnv = "XDG_CONFIG_HOME"
const xdgDataHomeEnv = "XDG_DATA_HOME"

type mcpConfig struct {
	GeneratedBy string                `json:"_generatedBy"`
	Servers     OrderedMap[mcpServer] `json:"mcpServers"`
}

type mcpServer struct {
	Type               string             `json:"type"`
	Command            string             `json:"command,omitempty"`
	Args               []string           `json:"args,omitempty"`
	Env                OrderedMap[string] `json:"env,omitempty"`
	URL                string             `json:"url,omitempty"`
	Headers            OrderedMap[string] `json:"headers,omitempty"`
	Enabled            *bool              `json:"enabled,omitempty"`
	ToolTimeoutSeconds int                `json:"tool_timeout_sec,omitempty"`
}

// writeMCPConfig generates the native project file shared by Claude and Muse.
func writeMCPConfig(sys System, root string, project *config.ProjectConfig) error {
	cfg, err := buildMCPConfig(project)
	if err != nil {
		return err
	}

	data, err := sys.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(root, ".mcp.json")
	mode := os.FileMode(0o644)
	if config.IsAgentEnabled(project.Config.Agents.Muse.Enabled) {
		// Muse does not expand HTTP header placeholders. Its entries contain
		// resolved values, just as the historical private settings file did.
		mode = 0o600
		if err := protectMuseMCPOutput(root, project.Env); err != nil {
			return err
		}
	}
	if err := sys.WriteFileAtomic(path, data, mode); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildMCPConfig(project *config.ProjectConfig) (*mcpConfig, error) {
	cfg := &mcpConfig{
		GeneratedBy: mcpGeneratedBy,
		Servers:     make(OrderedMap[mcpServer]),
	}

	// Claude Code documents no per-server execution timeout, only the
	// client-wide MCP_TOOL_TIMEOUT. Agent Layer deliberately leaves that global
	// value alone — changing it would alter every unrelated MCP server — so the
	// built-in server's own 40-minute guard is Claude's recovery bound.
	var resolved []projection.ResolvedMCPServer
	if config.IsAgentEnabled(project.Config.Agents.Claude.Enabled) || config.IsAgentEnabled(project.Config.Agents.ClaudeVSCode.Enabled) {
		var err error
		resolved, err = projection.EffectiveMCPServers(project.Config, project.Env, projection.ClientClaude, projection.ClientPlaceholderResolver("${%s}"))
		if err != nil {
			return nil, err
		}
	}
	museEnabled := config.IsAgentEnabled(project.Config.Agents.Muse.Enabled)
	museIDs := make(map[string]bool)
	if museEnabled {
		museServers, err := projection.EffectiveMCPServers(project.Config, project.Env, projection.ClientMuse, projection.FullValueResolver(project.Env))
		if err != nil {
			return nil, err
		}
		for _, server := range museServers {
			if server.Transport == config.TransportHTTP && server.HTTPTransport != config.HTTPTransportStreamable {
				return nil, fmt.Errorf("MCP server %q uses %s HTTP transport, which Muse does not support; set http_transport = %q or exclude muse in clients", server.ID, server.HTTPTransport, config.HTTPTransportStreamable)
			}
			museIDs[server.ID] = true
		}
		// Muse entries follow Claude entries, replacing shared IDs with fully
		// resolved values. A single definition serves both clients.
		resolved = append(resolved, museServers...)
	}

	for _, server := range resolved {
		entry := mcpServer{
			Type:    server.Transport,
			Command: server.Command,
			Args:    server.Args,
			URL:     server.URL,
		}
		if museEnabled {
			enabled := museIDs[server.ID]
			entry.Enabled = &enabled
			if entry.Type == config.TransportHTTP {
				// Both native clients accept this spelling; Muse rejects "http"
				// even for disabled entries, disabling the entire MCP runtime.
				entry.Type = "streamable-http"
			}
			if enabled {
				entry.ToolTimeoutSeconds = server.ToolTimeoutSeconds
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
		if museEnabled && museIDs[server.ID] && server.Transport == config.TransportStdio {
			// Muse filters these selectors from MCP subprocess inheritance. Native
			// runtime expansion preserves launch-time values, including unset fallback.
			if entry.Env == nil {
				entry.Env = make(OrderedMap[string])
			}
			for _, key := range []string{xdgConfigHomeEnv, xdgDataHomeEnv, ghConfigDirEnv} {
				if _, explicit := entry.Env[key]; !explicit {
					entry.Env[key] = "${" + key + ":-}"
				}
			}
			if server.ID == projection.BuiltInDispatchServerID {
				for _, key := range projection.BuiltInDispatchEnvVars() {
					fallback := ""
					if key == "AL_DISPATCH_ACTIVE" {
						// Empty depth is invalid; an ordinary session starts at zero.
						fallback = "0"
					}
					entry.Env[key] = "${" + key + ":-" + fallback + "}"
				}
			}
		}
		cfg.Servers[server.ID] = entry
	}

	return cfg, nil
}

// cleanMCPConfig removes only Agent Layer's generated file once no client uses it.
func cleanMCPConfig(sys System, root string) error {
	path := filepath.Join(root, ".mcp.json")
	info, err := sys.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil // No ownership proof; preserve user paths, including symlinks.
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return err
	}
	var document struct {
		GeneratedBy string `json:"_generatedBy"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil //nolint:nilerr // Unparseable content is not ownership proof; preserve user data.
	}
	if document.GeneratedBy != mcpGeneratedBy {
		return nil
	}
	if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove generated project MCP configuration %s: %w", path, err)
	}
	return nil
}

// Non-Git projects need no Git executable. A marker (including a gitfile)
// requires authoritative index/ignore checks before writing resolved values.
func protectMuseMCPOutput(root string, env map[string]string) error {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("locate Muse MCP output: %w", err)
	}
	_, found, err := projectroot.FindGitRoot(physical)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	runner, err := gitrepo.NewRunner(env)
	if err != nil {
		return fmt.Errorf("verify private Muse MCP output: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ignored, err := runner.IgnoredWorktreePath(ctx, root, ".mcp.json")
	if err != nil {
		return fmt.Errorf("verify private Muse MCP output: %w", err)
	}
	if !ignored {
		return fmt.Errorf("refusing to write resolved Muse credentials to %s: the file must be untracked and gitignored in a valid worktree; remove it from Git tracking and add /.mcp.json to the project's ignore rules before syncing", filepath.Join(root, ".mcp.json"))
	}
	return nil
}
