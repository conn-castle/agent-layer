package root

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindAgentLayerRoot_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permission test on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

	// Starting at an unreadable directory should produce a non-NotExist
	// stat error which FindAgentLayerRoot wraps as RootCheckPathFmt.
	if _, _, err := FindAgentLayerRoot(locked); err == nil {
		t.Skip("filesystem allowed stat under 0o000; cannot exercise check-path error")
	}
}

func TestFindRepoRoot_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permission test on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

	// FindRepoRoot calls FindAgentLayerRoot first; an EACCES from the locked
	// directory must propagate as an error rather than fall back to start.
	if _, err := FindRepoRoot(locked); err == nil {
		t.Skip("filesystem allowed stat under 0o000; cannot exercise check-path error")
	}
}

func TestFindRoots_CheckPathErrorViaSymlinkLoop(t *testing.T) {
	root := t.TempDir()

	agentLayerPath := filepath.Join(root, ".agent-layer")
	if err := os.Symlink(".agent-layer", agentLayerPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	if _, _, err := FindAgentLayerRoot(root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check") {
		t.Fatalf("expected check-path error for .agent-layer symlink loop, got %v", err)
	}

	gitPath := filepath.Join(root, ".git")
	if err := os.Remove(agentLayerPath); err != nil {
		t.Fatalf("remove .agent-layer symlink: %v", err)
	}
	if err := os.Symlink(".git", gitPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	if _, err := FindRepoRoot(root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check") {
		t.Fatalf("expected check-path error for .git symlink loop, got %v", err)
	}
}
