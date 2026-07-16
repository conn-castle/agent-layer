package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepoRoot_FindAgentLayerError(t *testing.T) {
	original := findAgentLayerRoot
	findAgentLayerRoot = func(string) (string, bool, error) {
		return "", false, errors.New("find failed")
	}
	t.Cleanup(func() { findAgentLayerRoot = original })

	_, err := resolveRepoRoot()
	if err == nil {
		t.Fatal("expected resolveRepoRoot to propagate find error")
	}
	if err.Error() != "find failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRepoRootAndWorkingDirPreservesCallerDirectory(t *testing.T) {
	workingDir := filepath.Join(t.TempDir(), "linked-worktree")
	originalGetwd := getwd
	originalFind := findAgentLayerRoot
	getwd = func() (string, error) { return workingDir, nil }
	findAgentLayerRoot = func(got string) (string, bool, error) {
		if got != workingDir {
			t.Fatalf("find root input = %q, want %q", got, workingDir)
		}
		return "/config-root", true, nil
	}
	t.Cleanup(func() {
		getwd = originalGetwd
		findAgentLayerRoot = originalFind
	})

	root, gotWorkingDir, err := resolveRepoRootAndWorkingDir()
	if err != nil {
		t.Fatalf("resolve root and working directory: %v", err)
	}
	if root != "/config-root" || gotWorkingDir != workingDir {
		t.Fatalf("resolved root/workdir = %q/%q", root, gotWorkingDir)
	}
}

func TestResolveRepoRootAndWorkingDirResolvesRootWithoutRewritingCallerDirectory(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	descendant := filepath.Join(repo, "packages", "service")
	if err := os.MkdirAll(descendant, 0o700); err != nil {
		t.Fatalf("mkdir descendant: %v", err)
	}

	logicalParent := t.TempDir()
	linkedRepo := filepath.Join(logicalParent, "linked-repo")
	if err := os.Symlink(repo, linkedRepo); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	logicalWorkingDir := filepath.Join(linkedRepo, "packages", "service")

	originalGetwd := getwd
	getwd = func() (string, error) { return logicalWorkingDir, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	gotRoot, gotWorkingDir, err := resolveRepoRootAndWorkingDir()
	if err != nil {
		t.Fatalf("resolve root and working directory: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("resolve expected repository root: %v", err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("resolved root = %q, want real root %q", gotRoot, wantRoot)
	}
	if gotWorkingDir != logicalWorkingDir {
		t.Fatalf("working directory = %q, want original caller value %q", gotWorkingDir, logicalWorkingDir)
	}
}
