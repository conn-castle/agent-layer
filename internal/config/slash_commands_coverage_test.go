package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSlashCommands_ReadDirError(t *testing.T) {
	_, err := LoadSlashCommands("/non-existent/dir")
	if err == nil {
		t.Fatalf("expected error from ReadDir")
	}
}

func TestLoadSlashCommands_ReadFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping read error test as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte{}, 0o000); err != nil {
		t.Fatalf("write file: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()

	_, err := LoadSlashCommands(dir)
	if err == nil {
		t.Fatalf("expected error from ReadFile")
	}
}

func TestLoadSlashCommands_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.md")
	// Invalid content (no frontmatter)
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := LoadSlashCommands(dir)
	if err == nil {
		t.Fatalf("expected error from parseSlashCommand")
	}
}

func TestParseDescription_EmptyValue(t *testing.T) {
	_, err := parseDescription([]string{"description:"})
	if err == nil {
		t.Fatalf("expected error for empty description")
	}
	if !strings.Contains(err.Error(), "description is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDescription_EmptyBlock(t *testing.T) {
	_, err := parseDescription([]string{"description: >-", "  "})
	if err == nil {
		t.Fatalf("expected error for empty block")
	}
	if !strings.Contains(err.Error(), "description is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}
