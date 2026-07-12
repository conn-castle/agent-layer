package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const antigravityChimePluginName = "agent-layer-chime"

func antigravityChimePluginDir(root string) string {
	return filepath.Join(root, ".agents", "plugins", antigravityChimePluginName)
}

// writeAntigravityChimePlugin writes the Agent Layer-owned Antigravity Stop
// hook plugin used by notifications.chime.
func writeAntigravityChimePlugin(sys System, root string, project *config.ProjectConfig) error {
	if !config.NotificationsChimeEnabled(project.Config) {
		return cleanAntigravityChimePlugin(sys, root)
	}
	dir := antigravityChimePluginDir(root)
	if err := ensureAntigravityChimePathContained(sys, root, dir); err != nil {
		return err
	}
	info, err := sys.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
		}
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
	} else if err := antigravityChimePluginIsManaged(sys, dir); err != nil {
		return err
	}
	if err := sys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, dir, err)
	}
	for _, file := range antigravityChimePluginFiles() {
		path := filepath.Join(dir, file.name)
		if err := sys.WriteFileAtomic(path, file.data, 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}
	return nil
}

// cleanAntigravityChimePlugin removes the dedicated Agent Layer-owned
// Antigravity chime plugin, preserving any other user plugins or .agents state.
func cleanAntigravityChimePlugin(sys System, root string) error {
	dir := antigravityChimePluginDir(root)
	if err := ensureAntigravityChimePathContained(sys, root, dir); err != nil {
		return err
	}
	info, err := sys.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
	}
	if err := antigravityChimePluginIsManaged(sys, dir); err != nil {
		return err
	}
	if err := sys.RemoveAll(dir); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err)
	}
	return nil
}

type antigravityChimePluginFile struct {
	name string
	data []byte
}

func antigravityChimePluginFiles() []antigravityChimePluginFile {
	return []antigravityChimePluginFile{
		{name: "plugin.json", data: []byte(fmt.Sprintf("{\n  \"name\": %q\n}\n", antigravityChimePluginName))},
		{name: "hooks.json", data: []byte(fmt.Sprintf(
			"{\n  %q: {\n    \"enabled\": true,\n    \"Stop\": [\n      {\n        %q: %q,\n        %q: %q,\n        %q: %d\n      }\n    ]\n  }\n}\n",
			antigravityChimePluginName,
			chimeHandlerTypeKey,
			chimeHandlerCommandType,
			chimeHandlerCommandKey,
			agentLayerAntigravityChimeCommand,
			chimeHandlerTimeoutKey,
			agentLayerChimeTimeout,
		))},
	}
}

func antigravityChimePluginIsManaged(sys System, dir string) error {
	for _, file := range antigravityChimePluginFiles() {
		path := filepath.Join(dir, file.name)
		data, err := sys.ReadFile(path)
		if err != nil {
			return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
		}
		if string(data) != string(file.data) {
			return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
		}
	}
	entries, err := sys.ReadDir(dir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, dir, err)
	}
	if len(entries) != len(antigravityChimePluginFiles()) {
		return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
	}
	return nil
}

func ensureAntigravityChimePathContained(sys System, root string, target string) error {
	pluginsRoot := filepath.Clean(filepath.Join(root, ".agents", "plugins"))
	cleanTarget := filepath.Clean(target)
	if cleanTarget != pluginsRoot && !strings.HasPrefix(cleanTarget, pluginsRoot+string(os.PathSeparator)) {
		return fmt.Errorf("antigravity chime plugin path points outside .agents/plugins: %s", target)
	}
	for _, path := range []string{
		filepath.Join(root, ".agents"),
		pluginsRoot,
		cleanTarget,
	} {
		info, err := sys.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, path)
		}
	}
	return nil
}
