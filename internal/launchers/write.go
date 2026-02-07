package launchers

import (
	"fmt"
	"os"

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

	writes := []struct {
		destPath     string
		templatePath string
		perm         os.FileMode
	}{
		{paths.Shell, openVSCodeShellTemplatePath, 0o755},
		{paths.Desktop, openVSCodeDesktopTemplatePath, 0o755},
	}

	for _, w := range writes {
		if err := writeTemplateFile(sys, w.destPath, w.templatePath, w.perm); err != nil {
			return err
		}
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
