package launchers

import (
	"errors"
	"os"
	"path/filepath"
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

	// Verify Windows launcher
	batPath := filepath.Join(root, ".agent-layer", "open-vscode.bat")
	batInfo, err := os.Stat(batPath)
	if err != nil {
		t.Fatalf("expected open-vscode.bat: %v", err)
	}
	if batInfo.Mode().Perm() != 0o755 {
		t.Fatalf("expected 0755 permissions on .bat file, got %o", batInfo.Mode().Perm())
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
	shContent, err := os.ReadFile(shPath)
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

	infoPlistContent, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist"))
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

	execContent, err := os.ReadFile(filepath.Join(appDir, "Contents", "MacOS", "open-vscode"))
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

	// Verify Windows launcher content
	batPath := filepath.Join(root, ".agent-layer", "open-vscode.bat")
	batContent, err := os.ReadFile(batPath)
	if err != nil {
		t.Fatalf("read .bat file: %v", err)
	}
	batStr := string(batContent)

	if len(batStr) == 0 {
		t.Fatal("Windows launcher is empty")
	}
	if !strings.Contains(batStr, "@echo off") {
		t.Fatal("Windows launcher missing @echo off")
	}
	if !strings.Contains(batStr, "al vscode --no-sync") {
		t.Fatal("Windows launcher must invoke al vscode --no-sync")
	}
	if !strings.Contains(batStr, "where al") {
		t.Fatal("Windows launcher must check for al command")
	}
	if !strings.Contains(batStr, "where code") {
		t.Fatal("Windows launcher must check for code command")
	}
	if !strings.Contains(batStr, "Shell Command: Install") {
		t.Fatal("Windows launcher missing install instructions")
	}
	if strings.Contains(batStr, ".env") {
		t.Fatal("Windows launcher must not parse .env directly (use al)")
	}

	// Verify Linux shell script content
	shLinuxPath := filepath.Join(root, ".agent-layer", "open-vscode.sh")
	shLinuxContent, err := os.ReadFile(shLinuxPath)
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
	desktopContent, err := os.ReadFile(desktopPath)
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
	if !strings.Contains(desktopStr, "%k") {
		t.Fatal("Linux desktop entry missing path (%k)")
	}
	if !strings.Contains(desktopStr, "Terminal=true") {
		t.Fatal("Linux desktop entry should use terminal for script output")
	}
}

func TestWriteVSCodeLaunchersDirectoryError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Create a file where the directory should be
	file := filepath.Join(root, ".agent-layer")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := WriteVSCodeLaunchers(RealSystem{}, root); err == nil {
		t.Fatalf("expected error when .agent-layer is a file")
	}
}

func TestWriteVSCodeLaunchersWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := WriteVSCodeLaunchers(RealSystem{}, root); err == nil {
		t.Fatalf("expected error when directory is read-only")
	}
}

func TestWriteVSCodeAppBundle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := VSCodePaths(root)
	if err := os.MkdirAll(paths.AgentLayerDir, 0o755); err != nil {
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

func TestWriteVSCodeAppBundleMkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := VSCodePaths(root)
	if err := os.MkdirAll(paths.AgentLayerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a file where the .app directory should be
	if err := os.WriteFile(paths.AppDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := writeVSCodeAppBundle(RealSystem{}, paths); err == nil {
		t.Fatalf("expected error when .app path is a file")
	}
}

// mockSystem implements System for testing.
type mockSystem struct {
	MkdirAllFunc        func(path string, perm os.FileMode) error
	WriteFileAtomicFunc func(path string, data []byte, perm os.FileMode) error
}

func (m *mockSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(path, perm)
	}
	return nil
}

func (m *mockSystem) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if m.WriteFileAtomicFunc != nil {
		return m.WriteFileAtomicFunc(path, data, perm)
	}
	return nil
}

func TestWriteVSCodeLaunchersAppBundleError(t *testing.T) {
	t.Parallel()
	sys := &mockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "Info.plist" {
				return errors.New("bundle fail")
			}
			return nil
		},
	}
	if err := WriteVSCodeLaunchers(sys, t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeLaunchersBatWriteError(t *testing.T) {
	t.Parallel()
	sys := &mockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "open-vscode.bat" {
				return errors.New("write fail")
			}
			return nil
		},
	}
	if err := WriteVSCodeLaunchers(sys, t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeLaunchersDesktopWriteError(t *testing.T) {
	t.Parallel()
	sys := &mockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "open-vscode.desktop" {
				return errors.New("write fail")
			}
			return nil
		},
	}
	if err := WriteVSCodeLaunchers(sys, t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeAppBundleInfoPlistWriteError(t *testing.T) {
	t.Parallel()
	sys := &mockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "Info.plist" {
				return errors.New("write fail")
			}
			return nil
		},
	}
	if err := writeVSCodeAppBundle(sys, VSCodePaths(t.TempDir())); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeAppBundleExecWriteError(t *testing.T) {
	t.Parallel()
	sys := &mockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "open-vscode" {
				return errors.New("write fail")
			}
			return nil
		},
	}
	if err := writeVSCodeAppBundle(sys, VSCodePaths(t.TempDir())); err == nil {
		t.Fatal("expected error")
	}
}
