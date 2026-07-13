package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// writeAntigravitySettings patches Agent Layer-managed keys into the user's
// native .agy/antigravity-cli/settings.json, preserving native state and the
// file's existing permissions.
func writeAntigravitySettings(sys System, root string, project *config.ProjectConfig) error {
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := ensureAntigravityPathRealParentContained(root, path); err != nil {
		return err
	}
	existing, err := readAntigravitySettings(sys, path)
	if err != nil {
		return err
	}
	settings, err := mergeAntigravitySettings(existing, buildAntigravitySettings(project))
	if err != nil {
		return fmt.Errorf("merge Antigravity settings %s: %w", path, err)
	}
	data, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalAntigravitySettingsFailedFmt, err)
	}
	data = append(data, '\n')
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(path), err)
	}
	if err := sys.WriteFileAtomic(path, data, antigravitySettingsFileMode(sys, path)); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

// readAntigravitySettings loads and validates the user's native Antigravity
// settings.json for merging. A missing, empty, or whitespace-only file yields a
// fresh empty object because there is no native state to preserve. It rejects a
// symlink or non-regular target, malformed or non-object JSON, and trailing
// data so a corrupt native file fails loud before any write, and uses a
// number-preserving decoder so large or high-precision native numbers survive
// the round trip.
func readAntigravitySettings(sys System, path string) (map[string]any, error) {
	info, err := sys.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("antigravity settings must be a regular file, not a symlink or special file: %s", path)
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Antigravity settings %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		// An empty or whitespace-only file holds no native state to preserve;
		// treat it like a missing file so a truncated or editor-created empty
		// file does not fail the whole sync.
		return make(map[string]any), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var settings map[string]any
	if err := decoder.Decode(&settings); err != nil {
		return nil, fmt.Errorf("decode Antigravity settings %s: %w", path, err)
	}
	if settings == nil {
		return nil, fmt.Errorf("decode Antigravity settings %s: top-level JSON value must be an object", path)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode Antigravity settings %s: trailing JSON value", path)
		}
		return nil, fmt.Errorf("decode Antigravity settings %s: trailing data: %w", path, err)
	}
	return settings, nil
}

// mergeAntigravitySettings overlays the Agent Layer-managed projection (model,
// permissions.allow, and any agent_specific paths, all produced by
// buildAntigravitySettings) onto the user's native Antigravity settings,
// preserving every native key Agent Layer does not produce. A managed key
// absent from desired is left untouched rather than deleted, because
// .agy/antigravity-cli/settings.json is owned by Antigravity and the user: an
// omitted Agent Layer value is not an instruction to erase native state. It
// returns an error only when a managed key Agent Layer must write collides with
// an incompatible native shape (object vs. scalar); a native path Agent Layer
// does not target is never inspected.
func mergeAntigravitySettings(existing, desired map[string]any) (map[string]any, error) {
	merged, ok := cloneAgentSpecificValue(existing).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("existing Antigravity settings must be a JSON object")
	}
	if err := overlayAntigravityManagedMap(merged, desired, nil); err != nil {
		return nil, err
	}
	return merged, nil
}

// antigravitySettingsFileMode preserves the existing settings.json permission
// bits when the file is already a regular file, falling back to owner-only
// 0o600 for a newly created file so native trust or approval state is never
// written with widened permissions.
func antigravitySettingsFileMode(sys System, path string) os.FileMode {
	if info, err := sys.Lstat(path); err == nil && info.Mode().IsRegular() {
		return info.Mode().Perm()
	}
	return 0o600
}

// overlayAntigravityManagedMap recursively overlays the managed desired map onto
// target, preserving native sibling keys. Nested objects are merged; scalar and
// array values replace the value at their key. It returns an error when a
// desired object would overwrite a native scalar or vice versa, so Agent Layer
// never silently reshapes native state. prefix carries the path walked so far
// for error messages.
func overlayAntigravityManagedMap(target, desired map[string]any, prefix []string) error {
	for key, value := range desired {
		path := append(append([]string{}, prefix...), key)
		if desiredMap, ok := value.(map[string]any); ok {
			current, exists := target[key]
			if !exists {
				current = make(map[string]any)
				target[key] = current
			}
			targetMap, ok := current.(map[string]any)
			if !ok {
				return fmt.Errorf("managed path %s requires an object", strings.Join(path, "."))
			}
			if err := overlayAntigravityManagedMap(targetMap, desiredMap, path); err != nil {
				return err
			}
			continue
		}
		if current, exists := target[key]; exists && antigravityShapeConflict(current, value) {
			return fmt.Errorf("managed path %s has incompatible existing shape", strings.Join(path, "."))
		}
		target[key] = cloneAgentSpecificValue(value)
	}
	return nil
}

// antigravityShapeConflict reports whether current and desired disagree on being
// a JSON object, which the overlay treats as an incompatible managed-path shape.
func antigravityShapeConflict(current, desired any) bool {
	_, currentObject := current.(map[string]any)
	_, desiredObject := desired.(map[string]any)
	return currentObject != desiredObject
}

func buildAntigravitySettings(project *config.ProjectConfig) map[string]any {
	settings := make(map[string]any)
	if model := strings.TrimSpace(project.Config.Agents.Antigravity.Model); model != "" {
		settings["model"] = model
	}
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

// writeAntigravityMCPConfig generates .agy/antigravity-cli/mcp_config.json.
func writeAntigravityMCPConfig(sys System, root string, project *config.ProjectConfig) error {
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

// cleanAntigravityOutputs removes Agent Layer-managed Antigravity files.
func cleanAntigravityOutputs(sys System, root string) error {
	for _, rel := range []string{
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
