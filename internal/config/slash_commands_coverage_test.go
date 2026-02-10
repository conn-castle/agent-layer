package config

import (
	"errors"
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
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	orig := osReadFileFunc
	osReadFileFunc = func(name string) ([]byte, error) {
		if name == path {
			return nil, errors.New("injected read error")
		}
		return orig(name)
	}
	t.Cleanup(func() { osReadFileFunc = orig })

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

func TestParseDescription_MissingDescription(t *testing.T) {
	_, err := parseDescription([]string{"name: test", "version: 1"})
	if err == nil {
		t.Fatalf("expected error for missing description")
	}
	if !strings.Contains(err.Error(), "missing description") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDescription_EmptyLines(t *testing.T) {
	_, err := parseDescription([]string{})
	if err == nil {
		t.Fatalf("expected error for empty lines")
	}
	if !strings.Contains(err.Error(), "missing description") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDescription_MultiLineBlockTerminates(t *testing.T) {
	// Multi-line description that terminates when encountering a non-indented line
	lines := []string{
		"description: >-",
		"  First line of description",
		"  Second line of description",
		"another_field: value",
	}
	desc, err := parseDescription(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc != "First line of description Second line of description" {
		t.Fatalf("expected combined description, got %q", desc)
	}
}
