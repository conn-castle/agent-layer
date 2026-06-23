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

	// All() must return exactly the projected launcher artifacts (every field
	// except AgentLayerDir, which is the container, not an artifact). Asserting the
	// exact set — not just the count — catches a dropped, swapped, or added entry.
	want := []string{
		paths.Command,
		paths.Shell,
		paths.Desktop,
		paths.AppDir,
		paths.AppContents,
		paths.AppMacOS,
		paths.AppInfoPlist,
		paths.AppExec,
	}
	got := paths.All()
	if len(got) != len(want) {
		t.Fatalf("expected %d paths, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, p := range got {
		if p == paths.AgentLayerDir {
			t.Fatalf("All() must not include the AgentLayerDir container path")
		}
	}
}
