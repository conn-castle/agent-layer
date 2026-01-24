package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

type vscodeSettings struct {
	ChatToolsTerminalAutoApprove OrderedMap[bool] `json:"chat.tools.terminal.autoApprove,omitempty"`
}

type vscodeMCPConfig struct {
	Servers OrderedMap[vscodeMCPServer] `json:"servers"`
}

type vscodeMCPServer struct {
	Type    string             `json:"type,omitempty"`
	URL     string             `json:"url,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
}

var (
	buildVSCodeSettingsFunc  = buildVSCodeSettings
	buildVSCodeMCPConfigFunc = buildVSCodeMCPConfig
	vscodeMarshalIndent      = json.MarshalIndent
	vscodeWriteFileAtomic    = fsutil.WriteFileAtomic
	writeVSCodeAppBundleFunc = writeVSCodeAppBundle
)

// WriteVSCodeSettings generates .vscode/settings.json.
func WriteVSCodeSettings(root string, project *config.ProjectConfig) error {
	settings, err := buildVSCodeSettingsFunc(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, vscodeDir, err)
	}

	data, err := vscodeMarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalVSCodeSettingsFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(vscodeDir, "settings.json")
	if err := vscodeWriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

// WriteVSCodeMCPConfig generates .vscode/mcp.json.
func WriteVSCodeMCPConfig(root string, project *config.ProjectConfig) error {
	cfg, err := buildVSCodeMCPConfigFunc(project)
	if err != nil {
		return err
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, vscodeDir, err)
	}

	data, err := vscodeMarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalVSCodeMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	path := filepath.Join(vscodeDir, "mcp.json")
	if err := vscodeWriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

// WriteVSCodeLaunchers generates VS Code launchers for macOS, Windows, and Linux:
// - .agent-layer/open-vscode.command (macOS Terminal script)
// - .agent-layer/open-vscode.app (macOS app bundle - no Terminal window)
// - .agent-layer/open-vscode.bat (Windows batch file)
// - .agent-layer/open-vscode.desktop (Linux desktop entry)
func WriteVSCodeLaunchers(root string) error {
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, agentLayerDir, err)
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
	shPath := filepath.Join(agentLayerDir, "open-vscode.command")
	if err := vscodeWriteFileAtomic(shPath, []byte(shContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, shPath, err)
	}

	// macOS .app bundle (no Terminal window)
	if err := writeVSCodeAppBundleFunc(agentLayerDir); err != nil {
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
	batPath := filepath.Join(agentLayerDir, "open-vscode.bat")
	if err := vscodeWriteFileAtomic(batPath, []byte(batContent), 0o755); err != nil {
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
	desktopPath := filepath.Join(agentLayerDir, "open-vscode.desktop")
	if err := vscodeWriteFileAtomic(desktopPath, []byte(desktopContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, desktopPath, err)
	}

	return nil
}

// writeVSCodeAppBundle creates a macOS .app bundle that launches VS Code without opening Terminal.
func writeVSCodeAppBundle(agentLayerDir string) error {
	appDir := filepath.Join(agentLayerDir, "open-vscode.app")
	contentsDir := filepath.Join(appDir, "Contents")
	macOSDir := filepath.Join(contentsDir, "MacOS")

	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, macOSDir, err)
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
	infoPlistPath := filepath.Join(contentsDir, "Info.plist")
	if err := vscodeWriteFileAtomic(infoPlistPath, []byte(infoPlist), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, infoPlistPath, err)
	}

	// Executable script - navigates up from .app/Contents/MacOS/ to .agent-layer/ then to parent root
	// Uses full path to VS Code CLI since Finder-launched apps have minimal PATH
	// The CLI binary inherits environment variables (unlike 'open -a')
	execContent := `#!/usr/bin/env bash
# Navigate from .app/Contents/MacOS/ up to the parent root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PARENT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd -P)"
export CODEX_HOME="$PARENT_ROOT/.codex"
cd "$PARENT_ROOT"
# Use full path to VS Code CLI - it inherits env vars (unlike 'open -a')
VSCODE_CLI="/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
VSCODE_CLI_USER="$HOME/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
if [ -x "$VSCODE_CLI" ]; then
  "$VSCODE_CLI" .
elif [ -x "$VSCODE_CLI_USER" ]; then
  "$VSCODE_CLI_USER" .
else
  osascript -e 'display alert "VS Code not found" message "Please install Visual Studio Code from https://code.visualstudio.com" as critical'
fi
`
	execPath := filepath.Join(macOSDir, "open-vscode")
	if err := vscodeWriteFileAtomic(execPath, []byte(execContent), 0o755); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, execPath, err)
	}

	return nil
}

func buildVSCodeSettings(project *config.ProjectConfig) (*vscodeSettings, error) {
	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	settings := &vscodeSettings{}

	if approvals.AllowCommands {
		autoApprove := make(OrderedMap[bool])
		for _, cmd := range approvals.Commands {
			pattern := fmt.Sprintf("/^%s(\\b.*)?$/", regexp.QuoteMeta(cmd))
			autoApprove[pattern] = true
		}
		if len(autoApprove) > 0 {
			settings.ChatToolsTerminalAutoApprove = autoApprove
		}
	}

	return settings, nil
}

func buildVSCodeMCPConfig(project *config.ProjectConfig) (*vscodeMCPConfig, error) {
	cfg := &vscodeMCPConfig{
		Servers: make(OrderedMap[vscodeMCPServer]),
	}

	// Transform to VS Code env syntax - VS Code resolves ${env:VAR} at runtime.
	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"vscode",
		func(name string, _ string) string {
			return fmt.Sprintf("${env:%s}", name)
		},
	)
	if err != nil {
		return nil, err
	}

	for _, server := range resolved {
		entry := vscodeMCPServer{
			Type: server.Transport,
			URL:  server.URL,
		}

		if server.Transport == "stdio" {
			entry.Command = server.Command
			entry.Args = server.Args
		}

		if len(server.Headers) > 0 {
			headers := make(OrderedMap[string], len(server.Headers))
			for key, value := range server.Headers {
				headers[key] = value
			}
			entry.Headers = headers
		}
		if len(server.Env) > 0 {
			envMap := make(OrderedMap[string], len(server.Env))
			for key, value := range server.Env {
				envMap[key] = value
			}
			entry.Env = envMap
		}

		cfg.Servers[server.ID] = entry
	}

	return cfg, nil
}
