package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// UpgradeKeepListFileName is the repo-local, gitignored list of paths that
// upgrade should treat as intentional user-owned content.
const UpgradeKeepListFileName = "upgrade-keep-list"

type upgradeKeepList map[string]struct{}

func (inst *installer) upgradeKeepListPath() string {
	return filepath.Join(inst.root, ".agent-layer", UpgradeKeepListFileName)
}

func (inst *installer) loadUpgradeKeepList() (upgradeKeepList, error) {
	data, err := inst.sys.ReadFile(inst.upgradeKeepListPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return upgradeKeepList{}, nil
		}
		return nil, fmt.Errorf(messages.InstallFailedReadFmt, inst.upgradeKeepListPath(), err)
	}

	kept := upgradeKeepList{}
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalized, err := normalizeUpgradeKeepPath(line)
		if err != nil {
			return nil, fmt.Errorf(messages.InstallInvalidUpgradeKeepListEntryFmt, inst.upgradeKeepListPath(), index+1, err)
		}
		kept[normalized] = struct{}{}
	}
	return kept, nil
}

func normalizeUpgradeKeepPath(path string) (string, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimSuffix(path, "/")
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "" || clean == "." || filepath.IsAbs(filepath.FromSlash(path)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must be relative to the repository root")
	}
	if clean == ".agent-layer" || clean == docsAgentLayerDir {
		return "", fmt.Errorf("path must be below .agent-layer/ or docs/agent-layer/")
	}
	if !strings.HasPrefix(clean, ".agent-layer/") && !strings.HasPrefix(clean, docsAgentLayerDir+"/") {
		return "", fmt.Errorf("path must be below .agent-layer/ or docs/agent-layer/")
	}
	if clean == ".agent-layer/tmp" || strings.HasPrefix(clean, ".agent-layer/tmp/") {
		return "", fmt.Errorf(".agent-layer/tmp/ uses its own protected cleanup flow")
	}
	if clean == ".agent-layer/"+UpgradeKeepListFileName {
		return "", fmt.Errorf("the keep list cannot contain itself")
	}
	return clean, nil
}

func normalizeUpgradeRelPath(path string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
}

func upgradePathIsKept(path string, kept upgradeKeepList) bool {
	normalized := normalizeUpgradeRelPath(path)
	for keepPath := range kept {
		if normalized == keepPath || strings.HasPrefix(normalized, keepPath+"/") {
			return true
		}
	}
	return false
}

func upgradePathHasKeptDescendant(path string, kept upgradeKeepList) bool {
	normalized := normalizeUpgradeRelPath(path)
	prefix := normalized + "/"
	for keepPath := range kept {
		if strings.HasPrefix(keepPath, prefix) {
			return true
		}
	}
	return false
}

func (inst *installer) filterKeptAbsolutePaths(paths []string, kept upgradeKeepList) []string {
	if len(paths) == 0 || len(kept) == 0 {
		return paths
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if upgradePathIsKept(inst.relativePath(path), kept) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func filterKeptUpgradeChanges(changes []upgradeChangeWithTemplate, kept upgradeKeepList) []upgradeChangeWithTemplate {
	if len(changes) == 0 || len(kept) == 0 {
		return changes
	}
	filtered := make([]upgradeChangeWithTemplate, 0, len(changes))
	for _, change := range changes {
		if upgradePathIsKept(change.path, kept) {
			continue
		}
		filtered = append(filtered, change)
	}
	return filtered
}

func (inst *installer) addUpgradeKeepPaths(kept upgradeKeepList, selected, offered []string) (upgradeKeepList, error) {
	offeredSet := make(map[string]struct{}, len(offered))
	for _, path := range offered {
		normalized, err := normalizeUpgradeKeepPath(path)
		if err != nil {
			return nil, err
		}
		offeredSet[normalized] = struct{}{}
	}
	added := make([]string, 0, len(selected))
	for _, path := range selected {
		normalized, err := normalizeUpgradeKeepPath(path)
		if err != nil {
			return nil, err
		}
		if _, ok := offeredSet[normalized]; !ok {
			return nil, fmt.Errorf(messages.InstallUpgradeKeepListSelectionInvalidFmt, normalized)
		}
		if _, exists := kept[normalized]; exists {
			continue
		}
		kept[normalized] = struct{}{}
		added = append(added, normalized)
	}
	if len(added) == 0 {
		return kept, nil
	}

	sort.Strings(added)
	data, err := inst.sys.ReadFile(inst.upgradeKeepListPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf(messages.InstallFailedReadFmt, inst.upgradeKeepListPath(), err)
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte(strings.Join(added, "\n")+"\n")...)
	if err := inst.sys.WriteFileAtomic(inst.upgradeKeepListPath(), data, 0o644); err != nil {
		return nil, fmt.Errorf(messages.InstallFailedWriteFmt, inst.upgradeKeepListPath(), err)
	}
	return kept, nil
}
