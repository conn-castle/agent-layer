package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type vscodeSettings struct {
	ChatToolsGlobalAutoApprove   *bool            `json:"chat.tools.global.autoApprove,omitempty"`
	ChatToolsTerminalAutoApprove OrderedMap[bool] `json:"chat.tools.terminal.autoApprove,omitempty"`
}

const (
	vscodeSettingsManagedStart = "// >>> agent-layer"
	vscodeSettingsManagedEnd   = "// <<< agent-layer"
)

var vscodeSettingsManagedHeader = []string{
	"// Managed by Agent Layer. To customize, edit .agent-layer/config.toml",
	"// and .agent-layer/commands.allow, then re-run `al sync`.",
	"//",
}

var errInvalidVSCodeSettings = errors.New("invalid vscode settings.json")

// WriteVSCodeSettings generates .vscode/settings.json.
func WriteVSCodeSettings(sys System, root string, project *config.ProjectConfig) error {
	return writeVSCodeSettings(sys, root, project, buildVSCodeSettings)
}

// writeVSCodeSettings builds settings and writes them to disk.
// Args: sys provides system calls, root is the repo root, project holds config, build constructs settings.
// Returns: an error if build or any filesystem operation fails.
func writeVSCodeSettings(sys System, root string, project *config.ProjectConfig, build func(*config.ProjectConfig) (*vscodeSettings, error)) error {
	settings, err := build(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := sys.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, vscodeDir, err)
	}

	path := filepath.Join(vscodeDir, "settings.json")
	existing, err := sys.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}

	updated, err := renderVSCodeSettingsContent(sys, string(existing), settings)
	if err != nil {
		if errors.Is(err, errInvalidVSCodeSettings) {
			return fmt.Errorf(messages.SyncInvalidVSCodeSettingsFmt, path, err)
		}
		return err
	}

	if err := sys.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildVSCodeSettings(project *config.ProjectConfig) (*vscodeSettings, error) {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	settings := &vscodeSettings{}

	if project.Config.Approvals.Mode == "yolo" {
		trueVal := true
		settings.ChatToolsGlobalAutoApprove = &trueVal
	}

	if approvals.AllowCommands {
		autoApprove := make(OrderedMap[bool])
		for _, cmd := range approvals.Commands {
			pattern := formatVSCodeAutoApprovePattern(cmd)
			autoApprove[pattern] = true
		}
		if len(autoApprove) > 0 {
			settings.ChatToolsTerminalAutoApprove = autoApprove
		}
	}

	return settings, nil
}

// formatVSCodeAutoApprovePattern builds a VS Code regex literal string for a command.
// Args: cmd is the allowed command string.
// Returns: a regex literal string like `/^<escaped>(\b.*)?$/` safe for JSONC.
func formatVSCodeAutoApprovePattern(cmd string) string {
	escaped := regexp.QuoteMeta(cmd)
	escaped = strings.ReplaceAll(escaped, "/", "\\/")
	return fmt.Sprintf("/^%s(\\b.*)?$/", escaped)
}
