package main

import (
	"errors"
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
