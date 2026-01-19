package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInstructions_ReadDirError(t *testing.T) {
	_, err := LoadInstructions("/non-existent/dir")
	if err == nil {
		t.Fatalf("expected error from ReadDir")
	}
}

func TestLoadInstructions_ReadFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping read error test as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte{}, 0o000); err != nil {
		t.Fatalf("write file: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()

	_, err := LoadInstructions(dir)
	if err == nil {
		t.Fatalf("expected error from ReadFile")
	}
}

func TestWalkInstructionFiles_ReadDirError(t *testing.T) {
	err := WalkInstructionFiles("/non-existent/dir", func(path string, entry fs.DirEntry) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error from ReadDir")
	}
}

func TestWalkInstructionFiles_FnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := WalkInstructionFiles(dir, func(path string, entry fs.DirEntry) error {
		return errors.New("boom")
	})
	if err == nil {
		t.Fatalf("expected error from fn")
	}
}
