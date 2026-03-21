package main

import (
	"errors"
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
