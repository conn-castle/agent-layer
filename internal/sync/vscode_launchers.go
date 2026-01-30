package sync

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// WriteVSCodeLaunchers generates VS Code launchers for macOS, Windows, and Linux:
// - .agent-layer/open-vscode.command (macOS Terminal script)
// - .agent-layer/open-vscode.app (macOS app bundle - no Terminal window)
// - .agent-layer/open-vscode.bat (Windows batch file)
// - .agent-layer/open-vscode.desktop (Linux desktop entry)
func WriteVSCodeLaunchers(sys System, root string) error {
	paths := launchers.VSCodePaths(root)
	if err := sys.MkdirAll(paths.AgentLayerDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, paths.AgentLayerDir, err)
	}

	// macOS .command launcher (opens Terminal)
	shContent := `#!/usr/bin/env bash
set -e
PARENT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$PARENT_ROOT"

if ! command -v al >/dev/null 2>&1; then
  echo "Error: 'al' command not found."
  echo "Install Agent Layer and ensure 'al' is on your PATH."
  exit 1
fi

if ! command -v code >/dev/null 2>&1; then
  echo "Error: 'code' command not found."
  echo "To install: Open VS Code, press Cmd+Shift+P, type 'Shell Command: Install code command in PATH', and run it."
  exit 1
fi

al vscode --no-sync
`
	shPath := paths.Command
	if err := sys.WriteFileAtomic(shPath, []byte(shContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, shPath, err)
	}

	// macOS .app bundle (no Terminal window)
	if err := writeVSCodeAppBundle(sys, paths); err != nil {
		return err
	}

	// Windows launcher
	batContent := `@echo off
setlocal EnableExtensions
set "PARENT_ROOT=%~dp0.."
cd /d "%PARENT_ROOT%"

where al >nul 2>&1
if %ERRORLEVEL% neq 0 (
  echo Error: 'al' command not found.
  echo Install Agent Layer and ensure 'al' is on your PATH.
  pause
  exit /b 1
)

where code >nul 2>&1
if %ERRORLEVEL% equ 0 (
  al vscode --no-sync
) else (
  echo Error: 'code' command not found.
  echo To install: Open VS Code, press Ctrl+Shift+P, type 'Shell Command: Install code command in PATH', and run it.
  pause
)
`
	batPath := paths.Bat
	if err := sys.WriteFileAtomic(batPath, []byte(batContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, batPath, err)
	}

	// Linux launcher (.desktop) - delegates to open-vscode.command for launch and error handling
	desktopContent := `[Desktop Entry]
Type=Application
Name=Open VS Code
Comment=Open this repo in VS Code with CODEX_HOME and AL_* environment variables
Exec=sh -c 'exec "$(dirname "$1")/open-vscode.command"' _ "%k"
Terminal=true
Categories=Development;IDE;
`
	desktopPath := paths.Desktop
	if err := sys.WriteFileAtomic(desktopPath, []byte(desktopContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, desktopPath, err)
	}

	return nil
}

// writeVSCodeAppBundle creates a macOS .app bundle that launches VS Code without opening Terminal.
func writeVSCodeAppBundle(sys System, paths launchers.VSCodeLauncherPaths) error {
	if err := sys.MkdirAll(paths.AppMacOS, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, paths.AppMacOS, err)
	}

	// Info.plist - macOS app metadata
	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>open-vscode</string>
  <key>CFBundleIdentifier</key>
  <string>com.agent-layer.open-vscode</string>
  <key>CFBundleName</key>
  <string>Open VS Code</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleVersion</key>
  <string>1.0</string>
  <key>LSMinimumSystemVersion</key>
  <string>10.13</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
`
	if err := sys.WriteFileAtomic(paths.AppInfoPlist, []byte(infoPlist), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, paths.AppInfoPlist, err)
	}

	// Executable script - navigates up from .app/Contents/MacOS/ to .agent-layer/ then to parent root
	// Uses osascript with a login shell to ensure full PATH is available for 'al' and 'code'
	execContent := `#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PARENT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd -P)"

# Build zsh command and pass to osascript (uses 'quoted form of' for safe quoting)
ZSH_CMD="cd '${PARENT_ROOT}' && command -v al >/dev/null 2>&1 || exit 126; command -v code >/dev/null 2>&1 || exit 127; al vscode --no-sync"

exec /usr/bin/osascript - "$ZSH_CMD" <<'APPLESCRIPT'
on run argv
  set zshCmd to item 1 of argv
  try
    do shell script "/bin/zsh -l -c " & quoted form of zshCmd
  on error errMsg number errNum
    if errNum is 126 then
      display alert "Unable to launch VS Code" message "The 'al' command could not be found in your PATH. Install Agent Layer and ensure 'al' is on your PATH." as critical
    else if errNum is 127 then
      display alert "Unable to launch VS Code" message "The 'code' command could not be found in your PATH. Install it from VS Code via Command Palette → 'Shell Command: Install code command in PATH'." as critical
    else
      display alert "Launch Failed" message errMsg as critical
    end if
  end try
end run
APPLESCRIPT
`
	if err := sys.WriteFileAtomic(paths.AppExec, []byte(execContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, paths.AppExec, err)
	}

	return nil
}
