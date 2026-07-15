package sync

import (
	"bytes"
	"errors"
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
	created := false
	info, err := sys.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
		}
		created = true
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
	} else if err := antigravityChimePluginIsManaged(sys, dir); err != nil {
		return err
	}
	if err := sys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, dir, err)
	}
	written := make([]antigravityChimePluginFile, 0, len(antigravityChimePluginFiles()))
	for _, file := range antigravityChimePluginFiles() {
		path := filepath.Join(dir, file.name)
		if err := sys.WriteFileAtomic(path, file.data, 0o644); err != nil {
			writeErr := fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
			if !created {
				return writeErr
			}
			if rollbackErr := rollbackNewAntigravityChimePlugin(sys, dir, append(written, file)); rollbackErr != nil {
				return errors.Join(writeErr, rollbackErr)
			}
			return writeErr
		}
		written = append(written, file)
	}
	return nil
}

// rollbackNewAntigravityChimePlugin removes only files proven to be written by
// the failed invocation, preserving ambiguous filesystem state for resolution.
func rollbackNewAntigravityChimePlugin(sys System, dir string, written []antigravityChimePluginFile) error {
	var rollbackErrs []error
	for i := len(written) - 1; i >= 0; i-- {
		path := filepath.Join(dir, written[i].name)
		info, err := sys.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.InstallFailedStatFmt, path, err))
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, path))
			continue
		}
		data, err := sys.ReadFile(path)
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.SyncReadFailedFmt, path, err))
			continue
		}
		if !bytes.Equal(data, written[i].data) {
			rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, path))
			continue
		}
		if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
			rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.SyncRemoveFailedFmt, path, err))
		}
	}
	if err := sys.Remove(dir); err != nil && !os.IsNotExist(err) {
		rollbackErrs = append(rollbackErrs, fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err))
	}
	if len(rollbackErrs) == 0 {
		return nil
	}
	return fmt.Errorf("roll back incomplete Antigravity chime plugin: %w", errors.Join(rollbackErrs...))
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
	return antigravityChimePluginFilesForCommand(agentLayerAntigravityChimeCommand)
}

func legacyAntigravityChimePluginFiles() []antigravityChimePluginFile {
	return antigravityChimePluginFilesForCommand(legacyAgentLayerAntigravityChimeCommand)
}

func antigravityChimePluginFilesForCommand(command string) []antigravityChimePluginFile {
	return []antigravityChimePluginFile{
		{name: "plugin.json", data: []byte(fmt.Sprintf("{\n  \"name\": %q\n}\n", antigravityChimePluginName))},
		{name: "hooks.json", data: []byte(fmt.Sprintf(
			"{\n  %q: {\n    \"enabled\": true,\n    \"Stop\": [\n      {\n        %q: %q,\n        %q: %q,\n        %q: %d\n      }\n    ]\n  }\n}\n",
			antigravityChimePluginName,
			chimeHandlerTypeKey,
			chimeHandlerCommandType,
			chimeHandlerCommandKey,
			command,
			chimeHandlerTimeoutKey,
			agentLayerChimeTimeout,
		))},
	}
}

func antigravityChimePluginIsManaged(sys System, dir string) error {
	entries, err := sys.ReadDir(dir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, dir, err)
	}
	if len(entries) != len(antigravityChimePluginFiles()) {
		return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
	}
	for _, file := range antigravityChimePluginFiles() {
		path := filepath.Join(dir, file.name)
		info, err := sys.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
		}
	}
	for _, files := range [][]antigravityChimePluginFile{antigravityChimePluginFiles(), legacyAntigravityChimePluginFiles()} {
		matched := true
		for _, file := range files {
			data, readErr := sys.ReadFile(filepath.Join(dir, file.name))
			if readErr != nil || string(data) != string(file.data) {
				matched = false
				break
			}
		}
		if matched {
			return nil
		}
	}
	return fmt.Errorf(messages.SyncAntigravityChimePluginConflictFmt, dir)
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
