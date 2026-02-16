package root

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestFindAgentLayerRootFound(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got, found, err := FindAgentLayerRoot(sub)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if !found {
		t.Fatalf("expected root to be found")
	}
	if got != root {
		t.Fatalf("expected root %s, got %s", root, got)
	}
}

func TestFindAgentLayerRootMissing(t *testing.T) {
	root := t.TempDir()
	got, found, err := FindAgentLayerRoot(root)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if found {
		t.Fatalf("expected not found, got %s", got)
	}
}

func TestFindAgentLayerRootFileError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, _, err := FindAgentLayerRoot(root)
	if err == nil {
		t.Fatalf("expected error for file .agent-layer")
	}
}

func TestFindRepoRootPrefersAgentLayer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
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

func TestFindRepoRootUsesGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
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

func TestFindRepoRootFallsBackToStart(t *testing.T) {
	root := t.TempDir()
	got, err := FindRepoRoot(root)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	if got != root {
		t.Fatalf("expected root %s, got %s", root, got)
	}
}

func TestFindRootsRequireStartPath(t *testing.T) {
	if _, _, err := FindAgentLayerRoot(""); err == nil {
		t.Fatal("expected FindAgentLayerRoot to reject empty start")
	}
	if _, err := FindRepoRoot(""); err == nil {
		t.Fatal("expected FindRepoRoot to reject empty start")
	}
}

func TestFindRepoRootUsesGitFile(t *testing.T) {
	root := t.TempDir()
	gitPath := filepath.Join(root, ".git")
	if err := os.WriteFile(gitPath, []byte("gitdir: .git/worktrees/x\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got, err := FindRepoRoot(root)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	if got != root {
		t.Fatalf("expected root %s, got %s", root, got)
	}
}

func TestFindRepoRootGitSpecialFileErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo is not supported on windows")
	}

	root := t.TempDir()
	gitPath := filepath.Join(root, ".git")
	if err := syscall.Mkfifo(gitPath, 0o644); err != nil {
		t.Fatalf("mkfifo .git: %v", err)
	}

	if _, err := FindRepoRoot(root); err == nil {
		t.Fatal("expected error when .git is neither directory nor regular file")
	}
}
