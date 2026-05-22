package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const antigravityClientID = "antigravity"

type antigravityMCPConfig struct {
	Servers OrderedMap[antigravityMCPServer] `json:"mcpServers"`
}

type antigravityMCPServer struct {
	Command   string             `json:"command,omitempty"`
	Args      []string           `json:"args,omitempty"`
	Env       OrderedMap[string] `json:"env,omitempty"`
	ServerURL string             `json:"serverUrl,omitempty"`
	Headers   OrderedMap[string] `json:"headers,omitempty"`
}

type antigravityRenderer struct{}

func (antigravityRenderer) RenderCommand(pattern string) string {
	return "command(" + pattern + ")"
}

func (antigravityRenderer) RenderMCP(serverID string) string {
	return "mcp(" + serverID + "/)"
}

// WriteAntigravitySettings generates .agy/antigravity-cli/settings.json.
func WriteAntigravitySettings(sys System, root string, project *config.ProjectConfig) error {
	settings := buildAntigravitySettings(project)
	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalAntigravitySettingsFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := ensureAntigravityPathRealParentContained(root, path); err != nil {
		return err
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(path), err)
	}
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func buildAntigravitySettings(project *config.ProjectConfig) map[string]any {
	settings := make(map[string]any)
	permissions := buildPermissionsBlock(
		project.Config,
		project.CommandsAllow,
		projection.EnabledServerIDs(project.Config.MCP.Servers, antigravityClientID),
		antigravityRenderer{},
	)
	if permissions != nil {
		settings["permissions"] = permissions
	}
	mergeAgentSpecificSettings(settings, project.Config.Agents.Antigravity.AgentSpecific)
	return settings
}

// WriteAntigravityMCPConfig generates .agy/antigravity-cli/mcp_config.json.
func WriteAntigravityMCPConfig(sys System, root string, project *config.ProjectConfig) error {
	cfg, err := buildAntigravityMCPConfig(project)
	if err != nil {
		return err
	}
	data, err := sys.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalAntigravityMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	path, err := antigravityMCPConfigWritePath(sys, root)
	if err != nil {
		return err
	}
	if err := ensureAntigravityPathRealParentContained(root, path); err != nil {
		return err
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(path), err)
	}
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func antigravityMCPConfigWritePath(sys System, root string) (string, error) {
	legacyPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	migratedPath := filepath.Join(root, ".agy", "config", "mcp_config.json")

	info, err := sys.Lstat(legacyPath)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, readErr := sys.Readlink(legacyPath)
		if readErr != nil {
			return "", fmt.Errorf("read Antigravity MCP config symlink %s: %w", legacyPath, readErr)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(legacyPath), target)
		}
		target = filepath.Clean(target)
		if !antigravityPathIsUnderAgy(root, target) {
			return "", fmt.Errorf("antigravity MCP config symlink points outside .agy: %s -> %s", legacyPath, target)
		}
		if err := ensureAntigravityPathRealParentContained(root, target); err != nil {
			return "", err
		}
		return target, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf(messages.InstallFailedStatFmt, legacyPath, err)
	}
	if migratedInfo, statErr := sys.Stat(migratedPath); statErr == nil && !migratedInfo.IsDir() {
		if err := ensureAntigravityPathRealParentContained(root, migratedPath); err != nil {
			return "", err
		}
		return migratedPath, nil
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", fmt.Errorf(messages.InstallFailedStatFmt, migratedPath, statErr)
	}
	return legacyPath, nil
}

func antigravityPathIsUnderAgy(root string, path string) bool {
	agyRoot := filepath.Clean(filepath.Join(root, ".agy"))
	cleanPath := filepath.Clean(path)
	return cleanPath == agyRoot || strings.HasPrefix(cleanPath, agyRoot+string(os.PathSeparator))
}

func ensureAntigravityPathRealParentContained(root string, path string) error {
	if !antigravityPathIsUnderAgy(root, path) {
		return fmt.Errorf("antigravity path points outside .agy: %s", path)
	}
	agyRoot := filepath.Clean(filepath.Join(root, ".agy"))
	if info, err := os.Lstat(agyRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("antigravity data dir must not be a symlink: %s", agyRoot)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.InstallFailedStatFmt, agyRoot, err)
	}
	realAgyRoot, rootErr := filepath.EvalSymlinks(agyRoot)
	if rootErr != nil {
		if os.IsNotExist(rootErr) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, agyRoot, rootErr)
	}
	parent := filepath.Dir(filepath.Clean(path))
	realParent, parentErr := filepath.EvalSymlinks(parent)
	if parentErr != nil {
		if os.IsNotExist(parentErr) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, parent, parentErr)
	}
	realAgyRoot = filepath.Clean(realAgyRoot)
	realParent = filepath.Clean(realParent)
	if realParent != realAgyRoot && !strings.HasPrefix(realParent, realAgyRoot+string(os.PathSeparator)) {
		return fmt.Errorf("antigravity path resolves outside .agy: %s", path)
	}
	return nil
}

func buildAntigravityMCPConfig(project *config.ProjectConfig) (*antigravityMCPConfig, error) {
	cfg := &antigravityMCPConfig{
		Servers: make(OrderedMap[antigravityMCPServer]),
	}
	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		antigravityClientID,
		projection.ClientPlaceholderResolver("${%s}"),
	)
	if err != nil {
		return nil, err
	}
	for _, server := range resolved {
		entry := antigravityMCPServer{
			Command:   server.Command,
			Args:      server.Args,
			ServerURL: server.URL,
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

// CleanAntigravityOutputs removes Agent Layer-managed Antigravity files.
func CleanAntigravityOutputs(sys System, root string) error {
	for _, rel := range []string{
		filepath.Join(".agy", "antigravity-cli", "settings.json"),
		filepath.Join(".agy", "antigravity-cli", "mcp_config.json"),
		filepath.Join(".agy", "config", "mcp_config.json"),
	} {
		path := filepath.Join(root, rel)
		if _, statErr := os.Lstat(path); statErr == nil {
			if err := ensureAntigravityPathRealParentContained(root, path); err != nil {
				return err
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf(messages.InstallFailedStatFmt, path, statErr)
		}
		if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
		}
	}
	return nil
}
