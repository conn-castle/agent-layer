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
# Navigate to the parent root
PARENT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
export CODEX_HOME="$PARENT_ROOT/.codex"
cd "$PARENT_ROOT"
if command -v code >/dev/null 2>&1; then
  code .
else
  echo "Error: 'code' command not found."
  echo "To install: Open VS Code, press Cmd+Shift+P, type 'Shell Command: Install code command in PATH', and run it."
  exit 1
fi
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
set "PARENT_ROOT=%~dp0.."
set "CODEX_HOME=%PARENT_ROOT%\.codex"
cd /d "%PARENT_ROOT%"
where code >nul 2>&1
if %ERRORLEVEL% equ 0 (
  code .
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

	// Linux launcher (.desktop)
	desktopContent := `[Desktop Entry]
Type=Application
Name=Open VS Code
Comment=Open this repo in VS Code with CODEX_HOME set
Exec=sh -c "PARENT_ROOT=\"$(cd \"$(dirname \"$0\")/..\" && pwd -P)\"; export CODEX_HOME=\"$PARENT_ROOT/.codex\"; cd \"$PARENT_ROOT\"; if command -v code >/dev/null 2>&1; then exec code .; else MSG1=\"Error: code command not found.\"; MSG2=\"To install: Open VS Code, press Ctrl+Shift+P, run Shell Command: Install code command in PATH.\"; if command -v zenity >/dev/null 2>&1; then zenity --error --title=\"VS Code\" --text=\"$MSG1\n\n$MSG2\"; elif command -v kdialog >/dev/null 2>&1; then kdialog --error \"$MSG1\n\n$MSG2\" --title \"VS Code\"; elif command -v notify-send >/dev/null 2>&1; then notify-send \"VS Code\" \"$MSG1 $MSG2\"; elif command -v x-terminal-emulator >/dev/null 2>&1; then exec x-terminal-emulator -e sh -c \"echo \\\"$MSG1\\\"; echo \\\"$MSG2\\\"; printf 'Press Enter to exit.'; read -r _\"; elif command -v gnome-terminal >/dev/null 2>&1; then exec gnome-terminal -- sh -c \"echo \\\"$MSG1\\\"; echo \\\"$MSG2\\\"; printf 'Press Enter to exit.'; read -r _\"; elif command -v konsole >/dev/null 2>&1; then exec konsole -e sh -c \"echo \\\"$MSG1\\\"; echo \\\"$MSG2\\\"; printf 'Press Enter to exit.'; read -r _\"; elif command -v xterm >/dev/null 2>&1; then exec xterm -e sh -c \"echo \\\"$MSG1\\\"; echo \\\"$MSG2\\\"; printf 'Press Enter to exit.'; read -r _\"; else echo \"$MSG1\"; echo \"$MSG2\"; fi; exit 1; fi" "%k"
Terminal=false
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
	// Uses osascript with a login shell to ensure full PATH is available for 'code' command
	// Sources .agent-layer/.env so MCP servers can access API keys
	execContent := `#!/usr/bin/env bash
set -euo pipefail

# Navigate from .app/Contents/MacOS/ up to the parent root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PARENT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd -P)"

# Ensure VS Code sees the right Codex config location
export CODEX_HOME="$PARENT_ROOT/.codex"

# Run in a login shell so PATH is correct for VS Code MCP subprocesses
# Source .agent-layer/.env to export API keys for MCP servers
# Do not open Terminal; just start VS Code.
exec /usr/bin/osascript <<EOF
do shell script "/bin/zsh -l -c " & quoted form of ("cd " & quoted form of "$PARENT_ROOT" & " && export CODEX_HOME=" & quoted form of "$PARENT_ROOT/.codex" & " && [ -f .agent-layer/.env ] && set -a && source .agent-layer/.env && set +a; code .") & " >/dev/null 2>&1 &"
EOF
`
	if err := sys.WriteFileAtomic(paths.AppExec, []byte(execContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, paths.AppExec, err)
	}

	return nil
}
