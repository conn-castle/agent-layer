package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealSystem_Stat(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "test.txt")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sys := RealSystem{}

	// Test stat on existing file
	info, err := sys.Stat(file)
	if err != nil {
		t.Fatalf("stat existing file: %v", err)
	}
	if info.Name() != "test.txt" {
		t.Fatalf("unexpected name: %s", info.Name())
	}
	if info.Size() != 7 {
		t.Fatalf("unexpected size: %d", info.Size())
	}

	// Test stat on non-existent file
	_, err = sys.Stat(filepath.Join(root, "nonexistent"))
	if err == nil {
		t.Fatalf("expected error for non-existent file")
	}
}

func TestRunWithSystemFS_NilSystem(t *testing.T) {
	_, err := RunWithSystemFS(nil, os.DirFS("."), ".")
	if err == nil {
		t.Fatalf("expected error for nil system")
	}
}

func TestRunWithSystemFS_NilFS(t *testing.T) {
	_, err := RunWithSystemFS(RealSystem{}, nil, ".")
	if err == nil {
		t.Fatalf("expected error for nil fsys")
	}
}

func TestRunWithSystemFS_InvalidConfig(t *testing.T) {
	// Use empty temp dir which won't have .agent-layer/config.toml
	_, err := RunWithSystemFS(RealSystem{}, os.DirFS(t.TempDir()), t.TempDir())
	if err == nil {
		t.Fatalf("expected error for missing config")
	}
}
