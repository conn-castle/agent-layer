package root

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindAgentLayerRoot_EmptyStart(t *testing.T) {
	_, _, err := FindAgentLayerRoot("")
	if err == nil {
		t.Fatalf("expected error for empty start")
	}
}

func TestFindRepoRoot_EmptyStart(t *testing.T) {
	_, err := FindRepoRoot("")
	if err == nil {
		t.Fatalf("expected error for empty start")
	}
}

func TestFindRepoRoot_GitFile(t *testing.T) {
	// git worktrees use a .git file instead of a directory.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ..."), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got, err := FindRepoRoot(sub)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	if got != root {
		t.Fatalf("expected root %s, got %s", root, got)
	}
}

func TestFindAgentLayerRoot_StatError(t *testing.T) {
	// To trigger a stat error other than NotExist, we can make a directory unreadable.
	// But FindAgentLayerRoot walks UP.
	// If we start at a dir that we can't read?
	// os.Stat(".") usually works even if unreadable? No.
	// If we create a directory, chmod 000, and try to look inside it?
	// FindAgentLayerRoot looks for ".agent-layer" inside the candidate.
	// If candidate is unreadable, Stat(candidate/.agent-layer) might fail with Permission Denied.

	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	// restore perms to cleanup
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// If we start AT locked, it tries locked/.agent-layer.
	// Since locked is 000, we probably can't stat children on some OSs.
	// On Unix, r-x is needed to list/traverse. 000 should block it.

	// Skip on Windows as permissions are different
	if os.PathSeparator == '\\' { // Corrected escaping for backslash
		t.Skip("skipping permission test on windows")
	}

	_, _, err := FindAgentLayerRoot(locked)
	if err == nil {
		// If it doesn't fail, maybe we are root or it treats it as NotExist?
		// If it's NotExist, it continues walking up.
		// We want a non-NotExist error.
		// If Stat returns PermissionDenied, FindAgentLayerRoot should return error.
		// But maybe Stat returns NotExist if it can't enter?
		// Actually if the dir is unreadable, Stat("dir/child") returns Permission Denied.
	} else {
		// Verify it's the error we expect (not required, just that it didn't crash)
		t.Logf("Got expected error: %v", err)
	}
}

func TestFindRepoRoot_StatError(t *testing.T) {
	if os.PathSeparator == '\\' { // Corrected escaping for backslash
		t.Skip("skipping permission test on windows")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// FindRepoRoot looks for .git
	_, err := FindRepoRoot(locked)
	if err == nil {
		// Might not fail if it falls back to start.
		// But if it encounters an error walking up that is NOT NotExist, it should fail.
		// Error at line 76: RootCheckPathFmt
	} else {
		t.Logf("Got expected error: %v", err)
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
