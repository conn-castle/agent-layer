package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const museDirectoryName = "muse"

const agentLayerManagedMcpKey = "agentLayerManagedMcpServers"

// trackedMuseMCPNames returns the mcpServers names a previous sync recorded
// as Agent Layer-owned. A missing or malformed record means no names are
// known-owned, in which case callers preserve rather than delete: an absent
// record is not proof that an entry is unmanaged.
func trackedMuseMCPNames(settings map[string]any) map[string]bool {
	tracked := make(map[string]bool)
	raw, ok := settings[agentLayerManagedMcpKey].([]any)
	if !ok {
		return tracked
	}
	for _, name := range raw {
		if id, ok := name.(string); ok && id != "" {
			tracked[id] = true
		}
	}
	return tracked
}

func readMuseSettings(sys System, path string) (map[string]any, error) {
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
		return map[string]any{}, nil
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
	return result, nil
}

func cleanMuseSettings(sys System, root string) error {
	for _, dir := range []string{filepath.Join(root, ".muse-config"), filepath.Join(root, ".muse-config", museDirectoryName)} {
		info, err := sys.Lstat(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat Muse configuration directory %s: %w", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil // Legacy user-owned paths are never followed for cleanup.
		}
	}
	path := museSettingsPath(root)
	owned, err := museLocalOwnershipEvidence(sys, path, `"`+agentLayerManagedMcpKey+`"`)
	if err != nil || !owned {
		return err
	}
	settings, err := readMuseSettings(sys, path)
	if err != nil {
		return err
	}
	if _, tracked := settings[agentLayerManagedMcpKey]; !tracked {
		return nil
	}
	changed := false
	if servers, ok := settings["mcpServers"].(map[string]any); ok {
		for name := range trackedMuseMCPNames(settings) {
			if _, exists := servers[name]; exists {
				delete(servers, name)
				changed = true
			}
		}
		if changed && len(servers) == 0 {
			delete(settings, "mcpServers")
		}
	}
	delete(settings, agentLayerManagedMcpKey)
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
	return filepath.Join(root, ".muse-config", museDirectoryName, "settings.json")
}

// museLocalOwnershipEvidence inspects only an ordinary local file. Evidence is
// deliberately checked before schema validation: disabled clients do not own
// arbitrary user configuration. A damaged file retaining our marker still goes
// through strict validation, so cleanup failures cannot silently strand grants.
func museLocalOwnershipEvidence(sys System, path string, markers ...string) (bool, error) {
	info, err := sys.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, marker := range markers {
		if !strings.Contains(string(data), marker) {
			return false, nil
		}
	}
	return true, nil
}
