package launchers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWriteVSCodeLaunchers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := WriteVSCodeLaunchers(testSystem{}, root); err != nil {
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

	if err := WriteVSCodeLaunchers(testSystem{}, root); err != nil {
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

// TestVSCodeDesktopEntryExecRunsShellScript covers both known and unknown %k
// locations, and checks real GLib expansion when gio is installed.
func TestVSCodeDesktopEntryExecRunsShellScript(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("desktop entries run through sh on Linux")
	}
	for _, tc := range []struct {
		name     string
		repo     string
		relative bool
	}{
		{name: "spaces", repo: "my repo"},
		{name: "reserved", repo: "repo 'single' \"double\" \\ $cash `tick` 100% %k"},
		{name: "whitespace", repo: "repo\nwith\ttabs"},
		{name: "relative", repo: "relative repo", relative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), tc.repo)
			writeRoot := root
			if tc.relative {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				writeRoot, err = filepath.Rel(cwd, root)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteVSCodeLaunchers(testSystem{}, writeRoot); err != nil {
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
			callerDir := t.TempDir()
			for _, expansion := range []string{"known", "omitted", "empty-argument"} {
				t.Run(expansion, func(t *testing.T) {
					desktopFile := paths.Desktop
					if expansion != "known" {
						desktopFile = ""
					}
					argv, err := parseDesktopExec(execValue, desktopFile)
					if err != nil {
						t.Fatalf("parse Exec %q: %v", execValue, err)
					}
					if expansion == "empty-argument" {
						argv = append(argv, "")
					}
					cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- argv comes from the embedded launcher template.
					cmd.Dir = callerDir
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("run Exec %q: %v\n%s", argv, err, out)
					}
					if err := os.Remove(marker); err != nil {
						t.Fatalf("Exec did not run open-vscode.sh: %v", err)
					}
				})
			}

			t.Run("desktop-file-validate", func(t *testing.T) {
				validator, err := exec.LookPath("desktop-file-validate")
				if err != nil {
					t.Skip("desktop-file-validate is not installed")
				}
				if out, err := exec.Command(validator, paths.Desktop).CombinedOutput(); err != nil { // #nosec G204 -- validator and desktop path are test-controlled.
					t.Fatalf("desktop-file-validate: %v\n%s", err, out)
				}
			})
			t.Run("gio", func(t *testing.T) {
				if runtime.GOOS != "linux" {
					t.Skip("gio launch is only supported on Linux")
				}
				gio, err := exec.LookPath("gio")
				if err != nil {
					t.Skip("gio is not installed")
				}
				// Test the actual Exec parser and field expansion without needing
				// a graphical terminal. The generated entry keeps Terminal=true.
				desktop := filepath.Join(paths.AgentLayerDir, "gio.desktop")
				headless := strings.ReplaceAll(string(content), "Terminal=true", "Terminal=false")
				if err := os.WriteFile(desktop, []byte(headless), 0o600); err != nil { // #nosec G703 -- path is constructed from test-controlled inputs.
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, gio, "launch", desktop) // #nosec G204 -- gio and desktop path are test-controlled.
				cmd.Dir = callerDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("gio launch: %v\n%s", err, out)
				}
				// gio can return before the launched application finishes.
				for {
					if err := os.Remove(marker); err == nil {
						break
					} else if !os.IsNotExist(err) {
						t.Fatal(err)
					}
					select {
					case <-ctx.Done():
						t.Fatal("gio did not run open-vscode.sh")
					case <-time.After(10 * time.Millisecond):
					}
				}
			})
			t.Run("moved-with-known-location", func(t *testing.T) {
				movedRoot := filepath.Join(t.TempDir(), tc.repo)
				if err := os.Rename(root, movedRoot); err != nil {
					t.Fatal(err)
				}
				movedPaths := VSCodePaths(movedRoot)
				argv, err := parseDesktopExec(execValue, movedPaths.Desktop)
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- argv comes from the embedded launcher template.
				cmd.Dir = callerDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("run moved Exec %q: %v\n%s", argv, err, out)
				}
				if _, err := os.Stat(filepath.Join(movedPaths.AgentLayerDir, "ran")); err != nil {
					t.Fatalf("Exec did not run the moved sibling script: %v", err)
				}
			})
		})
	}
}

// parseDesktopExec splits a Desktop Entry Exec value into argv per the Desktop
// Entry Specification, substituting desktopFile for the %k field code (or
// omitting it when the location is unknown). It rejects reserved characters
// outside double quotes and field codes inside them, while allowing literal %%.
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
		case c == '%' && i+1 < len(value) && value[i+1] == '%':
			arg.WriteString("%%")
			inArg = true
			i++
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
	expanded := argv[:0]
	for _, a := range argv {
		if a == "%k" {
			if desktopFile != "" {
				expanded = append(expanded, desktopFile)
			}
		} else if strings.Contains(strings.ReplaceAll(a, "%%", ""), "%") {
			return nil, fmt.Errorf("unsupported field code in argument %q", a)
		} else {
			expanded = append(expanded, strings.ReplaceAll(a, "%%", "%"))
		}
	}
	argv = expanded
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

	if err := writeVSCodeAppBundle(testSystem{}, paths); err != nil {
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
