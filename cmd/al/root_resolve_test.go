package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestResolveRepoRootForPromptServerUsesEnvHint(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	t.Setenv(config.BuiltinRepoRootEnvVar, root)
	t.Chdir(t.TempDir())

	got, err := resolveRepoRootForPromptServer()
	if err != nil {
		t.Fatalf("resolveRepoRootForPromptServer error: %v", err)
	}
	if got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}
}

func TestResolveRepoRootForPromptServerFallsBackToCWD(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	t.Setenv(config.BuiltinRepoRootEnvVar, "")
	t.Chdir(nested)

	got, err := resolveRepoRootForPromptServer()
	if err != nil {
		t.Fatalf("resolveRepoRootForPromptServer error: %v", err)
	}
	if got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}
}

func TestResolveRepoRootForPromptServerInvalidEnvHint(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	invalidHint := t.TempDir()

	t.Setenv(config.BuiltinRepoRootEnvVar, invalidHint)
	t.Chdir(root)

	_, err := resolveRepoRootForPromptServer()
	if err == nil {
		t.Fatal("expected error for invalid AL_REPO_ROOT hint")
	}
	if err.Error() != messages.RootMissingAgentLayer {
		t.Fatalf("unexpected error: %v", err)
	}
}

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

func TestResolveRepoRootForPromptServer_HintFindError(t *testing.T) {
	original := findAgentLayerRoot
	findAgentLayerRoot = func(string) (string, bool, error) {
		return "", false, errors.New("hint find failed")
	}
	t.Cleanup(func() { findAgentLayerRoot = original })

	t.Setenv(config.BuiltinRepoRootEnvVar, "/tmp/non-empty-hint")

	_, err := resolveRepoRootForPromptServer()
	if err == nil {
		t.Fatal("expected resolveRepoRootForPromptServer to propagate hint find error")
	}
	if err.Error() != "hint find failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}
