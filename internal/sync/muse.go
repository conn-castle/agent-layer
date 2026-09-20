package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	museEnabledKey   = "enabled"
	museRequiredMode = "required"
)

func writeMuseSettings(sys System, root string, project *config.ProjectConfig) error {
	if err := ensureMuseConfigHome(root); err != nil {
		return err
	}
	path := museSettingsPath(root)
	settings, err := readMuseSettings(sys, path, true)
	if err != nil {
		return err
	}
	servers, err := projection.EffectiveMCPServers(project.Config, project.Env, projection.ClientMuse, projection.FullValueResolver(project.Env))
	if err != nil {
		return err
	}
	projectIDs, err := museProjectMCPIDs(sys, root)
	if err != nil {
		return err
	}
	// Disable the shared project scope in Muse only. Its definitions otherwise
	// override user settings, even for servers excluded by the Muse client filter.
	mcpServers := make(map[string]any, len(servers)+len(projectIDs))
	for id := range projectIDs {
		mcpServers[id] = map[string]any{museEnabledKey: false}
	}
	// Keep native names separate from project entries, including user-owned ones.
	prefix := "muse-"
	for {
		collision := false
		for _, server := range servers {
			if projectIDs[prefix+server.ID] {
				collision = true
				break
			}
		}
		if !collision {
			break
		}
		prefix += "muse-"
	}
	for _, server := range servers {
		entry := map[string]any{}
		switch server.Transport {
		case config.TransportStdio:
			entry["type"] = config.TransportStdio
			entry["command"] = server.Command
			if len(server.Args) > 0 {
				entry["args"] = server.Args
			}
			if len(server.Env) > 0 {
				entry["env"] = server.Env
			}
		case config.TransportHTTP:
			if server.HTTPTransport != config.HTTPTransportStreamable {
				return fmt.Errorf("MCP server %q uses %s HTTP transport, which Muse does not support; set http_transport = %q or exclude muse in clients", server.ID, server.HTTPTransport, config.HTTPTransportStreamable)
			}
			entry["type"] = "streamable-http"
			entry["url"] = server.URL
			if len(server.Headers) > 0 {
				entry["headers"] = server.Headers
			}
		default:
			return fmt.Errorf("MCP server %q uses unsupported transport %q", server.ID, server.Transport)
		}
		if server.ID == projection.BuiltInDispatchServerID {
			entry["mode"] = museRequiredMode
		} else {
			entry["mode"] = "optional"
		}
		if server.ToolTimeoutSeconds > 0 {
			entry["tool_timeout_sec"] = server.ToolTimeoutSeconds
		}
		mcpServers[prefix+server.ID] = entry
	}
	settings["schema_version"] = 1
	settings["mcpServers"] = mcpServers
	delete(settings, "mcp_servers")
	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Muse settings: %w", err)
	}
	data = append(data, '\n')
	if err := sys.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write Muse settings %s: %w", path, err)
	}
	return nil
}

// museProjectMCPIDs reads names only; Claude's values and secret placeholders
// remain untouched. Sync invokes this after refreshing Claude's project file.
func museProjectMCPIDs(sys System, root string) (map[string]bool, error) {
	path := filepath.Join(root, ".mcp.json")
	data, err := sys.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project MCP names for Muse: %w", err)
	}
	var document struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode project MCP names for Muse in %s: %w", path, err)
	}
	ids := make(map[string]bool, len(document.Servers))
	for id := range document.Servers {
		ids[id] = true
	}
	return ids, nil
}

func readMuseSettings(sys System, path string, requireSupportedSchema bool) (map[string]any, error) {
	info, err := sys.Lstat(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat Muse settings %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("muse settings must be a regular file, not a symlink or special file: %s", path)
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Muse settings %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if !requireSupportedSchema {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("decode Muse settings %s: existing file is empty and has no schema_version", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Muse settings %s: %w", path, err)
	}
	if result == nil {
		return nil, fmt.Errorf("decode Muse settings %s: top-level JSON value must be an object", path)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("decode Muse settings %s: trailing data", path)
	}
	if !requireSupportedSchema {
		return result, nil
	}
	version, ok := result["schema_version"].(json.Number)
	if !ok {
		return nil, fmt.Errorf("decode Muse settings %s: existing file must declare numeric schema_version 1", path)
	}
	if version.String() != "1" {
		return nil, fmt.Errorf("unsupported Muse settings schema_version %s in %s; install a compatible Agent Layer version or migrate the file before syncing", version.String(), path)
	}
	return result, nil
}

func cleanMuseSettings(sys System, root string) error {
	for _, dir := range []string{filepath.Join(root, ".muse-config"), filepath.Join(root, ".muse-config", "muse")} {
		info, err := sys.Lstat(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat Muse configuration directory %s: %w", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("muse configuration directory must be a real directory: %s", dir)
		}
	}
	path := museSettingsPath(root)
	settings, err := readMuseSettings(sys, path, false)
	if err != nil {
		return err
	}
	_, canonical := settings["mcpServers"]
	_, legacy := settings["mcp_servers"]
	if !canonical && !legacy {
		return nil
	}
	delete(settings, "mcpServers")
	delete(settings, "mcp_servers")
	if len(settings) == 0 || (len(settings) == 1 && settings["schema_version"] != nil) {
		if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

func museSettingsPath(root string) string {
	return filepath.Join(root, ".muse-config", "muse", "settings.json")
}

func ensureMuseConfigHome(root string) error {
	for _, path := range []string{filepath.Join(root, ".muse-config"), filepath.Join(root, ".muse-config", "muse")} {
		if err := fsutil.EnsurePrivateDir(path); err != nil {
			return fmt.Errorf("prepare Muse private directory %s: %w", path, err)
		}
	}
	return nil
}
