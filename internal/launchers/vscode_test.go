package launchers

import (
	"path/filepath"
	"testing"
)

func TestVSCodePaths(t *testing.T) {
	root := filepath.Join("repo", "root")
	paths := VSCodePaths(root)

	if paths.AgentLayerDir != filepath.Join(root, ".agent-layer") {
		t.Fatalf("AgentLayerDir mismatch: %s", paths.AgentLayerDir)
	}
	if paths.Command != filepath.Join(root, ".agent-layer", "open-vscode.command") {
		t.Fatalf("Command mismatch: %s", paths.Command)
	}
	if paths.Shell != filepath.Join(root, ".agent-layer", "open-vscode.sh") {
		t.Fatalf("Shell mismatch: %s", paths.Shell)
	}
	if paths.Bat != filepath.Join(root, ".agent-layer", "open-vscode.bat") {
		t.Fatalf("Bat mismatch: %s", paths.Bat)
	}
	if paths.Desktop != filepath.Join(root, ".agent-layer", "open-vscode.desktop") {
		t.Fatalf("Desktop mismatch: %s", paths.Desktop)
	}
	if paths.AppDir != filepath.Join(root, ".agent-layer", "open-vscode.app") {
		t.Fatalf("AppDir mismatch: %s", paths.AppDir)
	}
	if paths.AppContents != filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents") {
		t.Fatalf("AppContents mismatch: %s", paths.AppContents)
	}
	if paths.AppMacOS != filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents", "MacOS") {
		t.Fatalf("AppMacOS mismatch: %s", paths.AppMacOS)
	}
	if paths.AppInfoPlist != filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents", "Info.plist") {
		t.Fatalf("AppInfoPlist mismatch: %s", paths.AppInfoPlist)
	}
	if paths.AppExec != filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents", "MacOS", "open-vscode") {
		t.Fatalf("AppExec mismatch: %s", paths.AppExec)
	}

	all := paths.All()
	if len(all) != 9 {
		t.Fatalf("expected 9 paths, got %d", len(all))
	}
}
