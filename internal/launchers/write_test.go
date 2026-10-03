package launchers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteVSCodeLaunchers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := WriteVSCodeLaunchers(RealSystem{}, root); err != nil {
		t.Fatalf("WriteVSCodeLaunchers error: %v", err)
	}

	// Verify macOS .command launcher
	shPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	shInfo, err := os.Stat(shPath)
	if err != nil {
		t.Fatalf("expected open-vscode.command: %v", err)
	}
	if shInfo.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755 permissions on .command file, got %o", shInfo.Mode().Perm())
	}

	// Verify macOS .app bundle structure
	appDir := filepath.Join(root, ".agent-layer", "open-vscode.app")
	if _, err := os.Stat(appDir); err != nil {
		t.Fatalf("expected open-vscode.app directory: %v", err)
	}

	infoPlistPath := filepath.Join(appDir, "Contents", "Info.plist")
	if _, err := os.Stat(infoPlistPath); err != nil {
		t.Fatalf("expected Info.plist: %v", err)
	}

	execPath := filepath.Join(appDir, "Contents", "MacOS", "open-vscode")
	execInfo, err := os.Stat(execPath)
	if err != nil {
		t.Fatalf("expected open-vscode executable: %v", err)
	}
	if execInfo.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755 permissions on app executable, got %o", execInfo.Mode().Perm())
	}

	// Verify Linux shell script
	shLinuxPath := filepath.Join(root, ".agent-layer", "open-vscode.sh")
	shLinuxInfo, err := os.Stat(shLinuxPath)
	if err != nil {
		t.Fatalf("expected open-vscode.sh: %v", err)
	}
	if shLinuxInfo.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755 permissions on .sh file, got %o", shLinuxInfo.Mode().Perm())
	}

	// Verify Linux desktop entry
	desktopPath := filepath.Join(root, ".agent-layer", "open-vscode.desktop")
	desktopInfo, err := os.Stat(desktopPath)
	if err != nil {
		t.Fatalf("expected open-vscode.desktop: %v", err)
	}
	if desktopInfo.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755 permissions on .desktop file, got %o", desktopInfo.Mode().Perm())
	}
}

func TestWriteVSCodeLaunchersContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := WriteVSCodeLaunchers(RealSystem{}, root); err != nil {
		t.Fatalf("WriteVSCodeLaunchers error: %v", err)
	}

	// Verify macOS .command launcher content
	shPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	shContent, err := os.ReadFile(shPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read .command file: %v", err)
	}
	shStr := string(shContent)

	if len(shStr) == 0 {
		t.Fatal("macOS launcher is empty")
	}
	if shStr[:2] != "#!" {
		t.Fatal("macOS launcher missing shebang")
	}
	if !strings.Contains(shStr, "al vscode --no-sync") {
		t.Fatal("macOS launcher must invoke al vscode --no-sync")
	}
	if !strings.Contains(shStr, "command -v al") {
		t.Fatal("macOS launcher must check for al command")
	}
	if !strings.Contains(shStr, "command -v code") {
		t.Fatal("macOS launcher must check for code command")
	}
	if !strings.Contains(shStr, "Shell Command: Install") {
		t.Fatal("macOS launcher missing install instructions")
	}
	if strings.Contains(shStr, ".env") {
		t.Fatal("macOS launcher must not parse .env directly (use al)")
	}

	// Verify macOS .app bundle content
	appDir := filepath.Join(root, ".agent-layer", "open-vscode.app")

	infoPlistContent, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read Info.plist: %v", err)
	}
	infoPlistStr := string(infoPlistContent)
	if !strings.Contains(infoPlistStr, "CFBundleExecutable") {
		t.Fatal("Info.plist missing CFBundleExecutable")
	}
	if !strings.Contains(infoPlistStr, "com.agent-layer.open-vscode") {
		t.Fatal("Info.plist missing bundle identifier")
	}
	if !strings.Contains(infoPlistStr, "LSUIElement") {
		t.Fatal("Info.plist missing LSUIElement (needed to hide from dock)")
	}

	execContent, err := os.ReadFile(filepath.Join(appDir, "Contents", "MacOS", "open-vscode")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read app executable: %v", err)
	}
	execStr := string(execContent)
	if execStr[:2] != "#!" {
		t.Fatal("app executable missing shebang")
	}
	if !strings.Contains(execStr, "osascript") {
		t.Fatal("app executable missing osascript for launching VS Code")
	}
	if !strings.Contains(execStr, "zsh -l") {
		t.Fatal("app executable missing login shell invocation")
	}
	if !strings.Contains(execStr, "al vscode --no-sync") {
		t.Fatal("app executable must invoke al vscode --no-sync")
	}
	if !strings.Contains(execStr, "command -v al") {
		t.Fatal("app executable must check for al command")
	}
	if !strings.Contains(execStr, "command -v code") {
		t.Fatal("app executable must check for code command")
	}
	if !strings.Contains(execStr, "exit 126") {
		t.Fatal("app executable missing exit 126 for missing al command")
	}
	if !strings.Contains(execStr, "exit 127") {
		t.Fatal("app executable missing exit 127 for missing code command")
	}
	if !strings.Contains(execStr, "display alert") {
		t.Fatal("app executable missing error alert handling")
	}
	if strings.Contains(execStr, ".env") {
		t.Fatal("app executable must not parse .env directly (use al)")
	}

	// Verify Linux shell script content
	shLinuxPath := filepath.Join(root, ".agent-layer", "open-vscode.sh")
	shLinuxContent, err := os.ReadFile(shLinuxPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read .sh file: %v", err)
	}
	shLinuxStr := string(shLinuxContent)

	if len(shLinuxStr) == 0 {
		t.Fatal("Linux shell script is empty")
	}
	if shLinuxStr[:2] != "#!" {
		t.Fatal("Linux shell script missing shebang")
	}
	if !strings.Contains(shLinuxStr, "al vscode --no-sync") {
		t.Fatal("Linux shell script must invoke al vscode --no-sync")
	}
	if !strings.Contains(shLinuxStr, "command -v al") {
		t.Fatal("Linux shell script must check for al command")
	}
	if !strings.Contains(shLinuxStr, "command -v code") {
		t.Fatal("Linux shell script must check for code command")
	}
	if !strings.Contains(shLinuxStr, "Ctrl+Shift+P") {
		t.Fatal("Linux shell script must use Ctrl+Shift+P (not Cmd+Shift+P)")
	}
	if strings.Contains(shLinuxStr, "Cmd+Shift+P") {
		t.Fatal("Linux shell script must not use macOS shortcut Cmd+Shift+P")
	}
	if strings.Contains(shLinuxStr, ".env") {
		t.Fatal("Linux shell script must not parse .env directly (use al)")
	}

	// Verify Linux desktop entry content - delegates to .sh script
	desktopPath := filepath.Join(root, ".agent-layer", "open-vscode.desktop")
	desktopContent, err := os.ReadFile(desktopPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read .desktop file: %v", err)
	}
	desktopStr := string(desktopContent)
	if len(desktopStr) == 0 {
		t.Fatal("Linux desktop entry is empty")
	}
	if !strings.Contains(desktopStr, "[Desktop Entry]") {
		t.Fatal("Linux desktop entry missing Desktop Entry header")
	}
	if !strings.Contains(desktopStr, "open-vscode.sh") {
		t.Fatal("Linux desktop entry must delegate to open-vscode.sh")
	}
	if !strings.Contains(desktopStr, "Terminal=true") {
		t.Fatal("Linux desktop entry should use terminal for script output")
	}
}

// TestVSCodeDesktopEntryExecRunsShellScript parses the generated Exec key the
// way the Desktop Entry Specification requires and runs it, so a quoting error
// that stops GLib-based launchers from finding open-vscode.sh fails here.
func TestVSCodeDesktopEntryExecRunsShellScript(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("desktop entries run through sh on Linux")
	}
	root := filepath.Join(t.TempDir(), "my repo")
	if err := WriteVSCodeLaunchers(RealSystem{}, root); err != nil {
		t.Fatalf("WriteVSCodeLaunchers error: %v", err)
	}
	paths := VSCodePaths(root)
	marker := filepath.Join(paths.AgentLayerDir, "ran")
	stub := "#!/bin/sh\nprintf ran > \"$(dirname \"$0\")/ran\"\n"
	if err := os.WriteFile(paths.Shell, []byte(stub), 0o755); err != nil { // #nosec G306 -- test stub must be executable.
		t.Fatalf("write stub open-vscode.sh: %v", err)
	}

	content, err := os.ReadFile(paths.Desktop) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read .desktop file: %v", err)
	}
	var execValue string
	for _, line := range strings.Split(string(content), "\n") {
		if value, ok := strings.CutPrefix(line, "Exec="); ok {
			execValue = value
		}
	}
	argv, err := parseDesktopExec(execValue, paths.Desktop)
	if err != nil {
		t.Fatalf("parse Exec %q: %v", execValue, err)
	}

	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- argv comes from the embedded launcher template.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run Exec %q: %v\n%s", argv, err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Exec did not run open-vscode.sh: %v", err)
	}
}

// parseDesktopExec splits a Desktop Entry Exec value into argv per the Desktop
// Entry Specification, substituting desktopFile for the %k field code. It
// rejects reserved characters outside double quotes and field codes inside
// them, both of which the specification forbids.
func parseDesktopExec(value string, desktopFile string) ([]string, error) {
	// Exec is a string value, so its general escape sequences apply first.
	value = strings.NewReplacer(`\\`, `\`, `\s`, " ", `\t`, "\t", `\n`, "\n", `\r`, "\r").Replace(value)

	var argv []string
	var arg strings.Builder
	inArg, inQuote := false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case inQuote && c == '\\':
			i++
			if i == len(value) || !strings.ContainsRune("\"`$\\", rune(value[i])) {
				return nil, fmt.Errorf("invalid escape at offset %d", i)
			}
			arg.WriteByte(value[i])
		case inQuote && c == '"':
			inQuote = false
		case inQuote && c == '%':
			return nil, fmt.Errorf("field code inside quotes at offset %d", i)
		case inQuote:
			arg.WriteByte(c)
		case c == '"':
			inQuote, inArg = true, true
		case c == ' ':
			if inArg {
				argv = append(argv, arg.String())
				arg.Reset()
				inArg = false
			}
		case strings.ContainsRune("\t\n'\\><~|&;$*?#()`", rune(c)):
			return nil, fmt.Errorf("reserved character %q outside quotes at offset %d", c, i)
		default:
			arg.WriteByte(c)
			inArg = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inArg {
		argv = append(argv, arg.String())
	}
	for i, a := range argv {
		if a == "%k" {
			argv[i] = desktopFile
		} else if strings.Contains(a, "%") {
			return nil, fmt.Errorf("unsupported field code in argument %q", a)
		}
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty Exec")
	}
	return argv, nil
}

func TestWriteVSCodeAppBundle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := VSCodePaths(root)
	if err := os.MkdirAll(paths.AgentLayerDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := writeVSCodeAppBundle(RealSystem{}, paths); err != nil {
		t.Fatalf("writeVSCodeAppBundle error: %v", err)
	}

	// Verify structure
	if _, err := os.Stat(paths.AppInfoPlist); err != nil {
		t.Fatalf("missing Info.plist: %v", err)
	}
	if _, err := os.Stat(paths.AppExec); err != nil {
		t.Fatalf("missing executable: %v", err)
	}
}
