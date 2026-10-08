package sync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const grokChimeHookFileName = "agent-layer-chime.json"

func grokChimeHookPath(root string) string {
	return filepath.Join(root, ".grok", "hooks", grokChimeHookFileName)
}

func grokChimeHookContents() []byte {
	return fmt.Appendf(nil, `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": %q,
            "timeout": %d
          }
        ]
      }
    ]
  }
}
`, agentLayerGrokChimeCommand, agentLayerChimeTimeout)
}

func writeGrokChimeHook(sys System, root string, project *config.ProjectConfig) error {
	if !config.NotificationsChimeEnabled(project.Config) {
		return cleanGrokChimeHook(sys, root)
	}
	if err := ensureNoLegacyAgentSpecificChime(
		"agents.grok.agent_specific.hooks",
		project.Config.Agents.Grok.AgentSpecific[hooksKey],
		agentLayerGrokChimeCommand,
	); err != nil {
		return err
	}

	path := grokChimeHookPath(root)
	dir := filepath.Dir(path)
	if err := ensureGrokChimePathContained(sys, root, dir); err != nil {
		return err
	}
	if err := ensureGrokChimePathContained(sys, root, path); err != nil {
		return err
	}
	info, err := sys.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf(messages.SyncGrokChimeHookConflictFmt, path)
		}
		existing, readErr := sys.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, path, readErr)
		}
		if grokChimeHookIsManaged(existing) {
			if bytes.Equal(existing, grokChimeHookContents()) {
				return nil
			}
			if err := sys.WriteFileAtomic(path, grokChimeHookContents(), 0o600); err != nil {
				return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
			}
			return nil
		}
		return fmt.Errorf(messages.SyncGrokChimeHookConflictFmt, path)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if err := sys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, dir, err)
	}
	if err := sys.WriteFileAtomic(path, grokChimeHookContents(), 0o600); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func cleanGrokChimeHook(sys System, root string) error {
	target, exists, err := existingChimeCleanupTarget(sys, root, filepath.Join(".grok", "hooks"), grokChimeHookFileName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	path := target.path
	data, err := sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	if !grokChimeHookIsManaged(data) {
		return fmt.Errorf(messages.SyncGrokChimeHookConflictFmt, path)
	}
	if err := target.checkWritable(); err != nil {
		return err
	}
	if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
	}
	dir := filepath.Dir(path)
	entries, err := sys.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, dir, err)
	}
	if len(entries) > 0 {
		return nil
	}
	if err := sys.Remove(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err)
	}
	return nil
}

func grokChimeHookIsManaged(data []byte) bool {
	content := string(data)
	return strings.Contains(content, "al hook chime grok") && strings.Contains(content, agentLayerChimeMarker)
}

func ensureGrokChimePathContained(sys System, root string, target string) error {
	return ensureChimePathContained(sys, root, target, ".grok", "hooks",
		"grok chime hook path points outside .grok/hooks: %s", messages.SyncGrokChimeHookConflictFmt)
}
