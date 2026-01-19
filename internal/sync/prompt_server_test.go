package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePromptServerCommandRootEmptyDefaultsToLocalBinary(t *testing.T) {
	command, args, err := resolvePromptServerCommand("")
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "./al" {
		t.Fatalf("expected ./al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandUsesLocalBinary(t *testing.T) {
	root := t.TempDir()
	writePromptServerBinary(t, root)

	originalLookPath := lookPath
	t.Cleanup(func() { lookPath = originalLookPath })
	called := false
	lookPath = func(file string) (string, error) {
		called = true
		return "", errors.New("unexpected")
	}

	command, args, err := resolvePromptServerCommand(root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if called {
		t.Fatalf("expected lookPath not to be called")
	}
	if command != "./al" {
		t.Fatalf("expected ./al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandFallsBackToGoRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "al"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/al: %v", err)
	}

	originalLookPath := lookPath
	t.Cleanup(func() { lookPath = originalLookPath })
	lookPath = func(file string) (string, error) {
		if file != "go" {
			return "", errors.New("unexpected lookup")
		}
		return "/usr/bin/go", nil
	}

	command, args, err := resolvePromptServerCommand(root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "go" {
		t.Fatalf("expected go, got %q", command)
	}
	expectedSource := filepath.Join(root, "cmd", "al")
	if len(args) != 3 || args[0] != "run" || args[1] != expectedSource || args[2] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandMissingGo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "al"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/al: %v", err)
	}

	originalLookPath := lookPath
	t.Cleanup(func() { lookPath = originalLookPath })
	lookPath = func(file string) (string, error) {
		return "", errors.New("missing")
	}

	_, _, err := resolvePromptServerCommand(root)
	if err == nil {
		t.Fatalf("expected error")
	}
}
