package launchers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// System is the minimal interface needed for launcher operations.
type System interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFileAtomic(filename string, data []byte, perm os.FileMode) error
}

// RealSystem implements System using actual system calls.
type RealSystem struct{}

const (
	openVSCodeCommandTemplatePath = "launchers/open-vscode.command"
	openVSCodeShellTemplatePath   = "launchers/open-vscode.sh"
	openVSCodeDesktopTemplatePath = "launchers/open-vscode.desktop"
	openVSCodeAppInfoTemplatePath = "launchers/open-vscode.app/Contents/Info.plist"
	openVSCodeAppExecTemplatePath = "launchers/open-vscode.app/Contents/MacOS/open-vscode"
)

var readTemplate = templates.Read

// MkdirAll creates a directory and all parent directories.
func (RealSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WriteFileAtomic writes data to path atomically.
func (RealSystem) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileAtomic(path, data, perm)
}

// WriteVSCodeLaunchers generates VS Code launchers for macOS and Linux:
// - .agent-layer/open-vscode.command (macOS Terminal script)
// - .agent-layer/open-vscode.app (macOS app bundle - no Terminal window)
// - .agent-layer/open-vscode.desktop (Linux desktop entry)
func WriteVSCodeLaunchers(sys System, root string) error {
	paths := VSCodePaths(root)
	if err := sys.MkdirAll(paths.AgentLayerDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, paths.AgentLayerDir, err)
	}

	if err := writeTemplateFile(sys, paths.Command, openVSCodeCommandTemplatePath, 0o755); err != nil {
		return err
	}

	if err := writeVSCodeAppBundle(sys, paths); err != nil {
		return err
	}

	if err := writeTemplateFile(sys, paths.Shell, openVSCodeShellTemplatePath, 0o755); err != nil {
		return err
	}

	return writeVSCodeDesktopEntry(sys, paths)
}

// writeVSCodeDesktopEntry embeds a fallback for launchers that cannot expand %k.
// Keep the path in a separate argument so it is never interpreted as shell code.
func writeVSCodeDesktopEntry(sys System, paths VSCodeLauncherPaths) error {
	data, err := readTemplate(openVSCodeDesktopTemplatePath)
	if err != nil {
		return fmt.Errorf(messages.SyncReadTemplateFailedFmt, openVSCodeDesktopTemplatePath, err)
	}
	shellPath, err := filepath.Abs(paths.Shell)
	if err != nil {
		return fmt.Errorf("resolve VS Code launcher path: %w", err)
	}
	// Escape the quoted Exec argument, then the desktop string value. Literal
	// percent signs must be doubled so they are not expanded as field codes.
	escapedPath := strings.NewReplacer(
		`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`,
		"\n", `\n`, "\r", `\r`, "\t", `\t`,
	).Replace(shellPath)
	data = []byte(strings.ReplaceAll(string(data), "__OPEN_VSCODE_SHELL__", escapedPath))
	if err := sys.WriteFileAtomic(paths.Desktop, data, 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, paths.Desktop, err)
	}
	return nil
}

// writeVSCodeAppBundle creates a macOS .app bundle that launches VS Code without opening Terminal.
func writeVSCodeAppBundle(sys System, paths VSCodeLauncherPaths) error {
	if err := sys.MkdirAll(paths.AppMacOS, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, paths.AppMacOS, err)
	}

	writes := []struct {
		destPath     string
		templatePath string
		perm         os.FileMode
	}{
		{paths.AppInfoPlist, openVSCodeAppInfoTemplatePath, 0o644},
		{paths.AppExec, openVSCodeAppExecTemplatePath, 0o755},
	}

	for _, w := range writes {
		if err := writeTemplateFile(sys, w.destPath, w.templatePath, w.perm); err != nil {
			return err
		}
	}

	return nil
}

func writeTemplateFile(sys System, destinationPath string, templatePath string, perm os.FileMode) error {
	data, err := readTemplate(templatePath)
	if err != nil {
		return fmt.Errorf(messages.SyncReadTemplateFailedFmt, templatePath, err)
	}
	if err := sys.WriteFileAtomic(destinationPath, data, perm); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, destinationPath, err)
	}
	return nil
}
