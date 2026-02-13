package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInstructions(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10_second.md"), []byte("second"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}
	bomContent := append([]byte{0xEF, 0xBB, 0xBF}, []byte("first")...)
	if err := os.WriteFile(filepath.Join(dir, "00_first.md"), bomContent, 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}

	files, err := LoadInstructions(dir)
	if err != nil {
		t.Fatalf("LoadInstructions error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 instructions, got %d", len(files))
	}
	if files[0].Name != "00_first.md" || files[0].Content != "first" {
		t.Fatalf("unexpected first instruction: %+v", files[0])
	}
	if files[1].Name != "10_second.md" || files[1].Content != "second" {
		t.Fatalf("unexpected second instruction: %+v", files[1])
	}
}

func TestLoadInstructionsMissingDir(t *testing.T) {
	_, err := LoadInstructions(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatalf("expected missing instructions error")
	}
	if !strings.Contains(err.Error(), "missing instructions directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadInstructionsNoFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := LoadInstructions(dir)
	if err == nil {
		t.Fatalf("expected no instruction files error")
	}
	if !strings.Contains(err.Error(), "no instruction files") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWalkInstructionFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	var seen []string
	err := walkInstructionFiles(dir, func(path string, entry os.DirEntry) error {
		seen = append(seen, filepath.Base(path))
		return nil
	})
	if err != nil {
		t.Fatalf("WalkInstructionFiles error: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(seen))
	}
}

func TestWalkInstructionFilesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	expected := errors.New("boom")
	err := walkInstructionFiles(dir, func(path string, entry os.DirEntry) error {
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected error")
	}
}
