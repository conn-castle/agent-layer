package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// claudeStatuslineSourceName is the basename of the editable Claude status line
// source under .agent-layer and of the embedded template seeded from.
const claudeStatuslineSourceName = "claude-statusline.sh"

// legacyClaudeStatuslineSourceName is the pre-rename editable source basename.
// It is migrated write-if-missing into claudeStatuslineSourceName.
const legacyClaudeStatuslineSourceName = "statusline.sh"

// claudeStatuslinePath returns the absolute path to the generated status line
// copy that Claude Code executes (referenced from .claude/settings.json).
func claudeStatuslinePath(root string) string {
	return filepath.Join(root, ".claude", claudeStatuslineSourceName)
}

func legacyClaudeStatuslinePath(root string) string {
	return filepath.Join(root, ".claude", legacyClaudeStatuslineSourceName)
}

// claudeStatuslineSourcePath returns the absolute path to the editable
// source-of-truth status line under .agent-layer.
func claudeStatuslineSourcePath(root string) string {
	return filepath.Join(root, ".agent-layer", claudeStatuslineSourceName)
}

func legacyClaudeStatuslineSourcePath(root string) string {
	return filepath.Join(root, ".agent-layer", legacyClaudeStatuslineSourceName)
}

// WriteClaudeStatusline projects the editable .agent-layer/claude-statusline.sh
// source into .claude/claude-statusline.sh when the status line is enabled,
// seeding the source from the embedded template if it is missing. When disabled
// it removes any previously generated copy so a stale script does not linger.
func WriteClaudeStatusline(sys System, root string, project *config.ProjectConfig) error {
	dest := claudeStatuslinePath(root)
	if !config.ClaudeStatuslineEnabled(project.Config.Agents.Claude) {
		for _, stalePath := range []string{dest, legacyClaudeStatuslinePath(root)} {
			if err := sys.Remove(stalePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf(messages.SyncRemoveFailedFmt, stalePath, err)
			}
		}
		return nil
	}

	data, err := ensureStatuslineSource(sys, root)
	if err != nil {
		return err
	}
	// Remove a stale legacy projection (.claude/statusline.sh) left by a prior
	// version so the rename does not leave two scripts behind when enabled.
	if legacy := legacyClaudeStatuslinePath(root); legacy != dest {
		if err := sys.Remove(legacy); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, legacy, err)
		}
	}
	if err := sys.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(dest), err)
	}
	if err := sys.WriteFileAtomic(dest, data, 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, dest, err)
	}
	return nil
}

// ensureStatuslineSource returns the contents of
// .agent-layer/claude-statusline.sh, seeding it from the legacy source or
// embedded template (write-if-missing) when absent so that a standalone `al
// sync` works even if install never seeded it. Existing user edits are read and
// preserved — the template only fills a missing source.
func ensureStatuslineSource(sys System, root string) ([]byte, error) {
	src := claudeStatuslineSourcePath(root)
	data, err := sys.ReadFile(src)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, src, err)
	}

	legacySrc := legacyClaudeStatuslineSourcePath(root)
	legacyData, legacyErr := sys.ReadFile(legacySrc)
	if legacyErr == nil {
		if err := sys.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			return nil, fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(src), err)
		}
		if err := sys.WriteFileAtomic(src, legacyData, 0o755); err != nil {
			return nil, fmt.Errorf(messages.SyncWriteFileFailedFmt, src, err)
		}
		return legacyData, nil
	}
	if !os.IsNotExist(legacyErr) {
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, legacySrc, legacyErr)
	}

	tpl, err := templates.Read(claudeStatuslineSourceName)
	if err != nil {
		return nil, fmt.Errorf(messages.SyncReadTemplateFailedFmt, claudeStatuslineSourceName, err)
	}
	if err := sys.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		return nil, fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(src), err)
	}
	if err := sys.WriteFileAtomic(src, tpl, 0o755); err != nil {
		return nil, fmt.Errorf(messages.SyncWriteFileFailedFmt, src, err)
	}
	return tpl, nil
}
