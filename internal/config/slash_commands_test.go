package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const slashCommandContent = `---
description: >-
  First line
  Second line
---

Do the thing.
`

func TestLoadSlashCommands(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(slashCommandContent), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(slashCommandContent), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}

	commands, err := LoadSlashCommands(dir)
	if err != nil {
		t.Fatalf("LoadSlashCommands error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if commands[0].Name != "a" {
		t.Fatalf("expected lexicographic order, got %s", commands[0].Name)
	}
	if commands[0].Description != "First line Second line" {
		t.Fatalf("unexpected description: %q", commands[0].Description)
	}
	if commands[0].Body != "Do the thing." {
		t.Fatalf("unexpected body: %q", commands[0].Body)
	}
	if commands[0].SourcePath == "" {
		t.Fatalf("expected source path to be set")
	}
}

func parseSlashCommandErr(content string) error {
	_, _, err := parseSlashCommand(content)
	return err
}

func TestParseSlashCommandErrors(t *testing.T) {
	err := parseSlashCommandErr("")
	if err == nil || !strings.Contains(err.Error(), "missing content") {
		t.Fatalf("expected missing content error, got %v", err)
	}

	err = parseSlashCommandErr("no front matter")
	if err == nil || !strings.Contains(err.Error(), "missing front matter") {
		t.Fatalf("expected front matter error, got %v", err)
	}

	err = parseSlashCommandErr("---\nname: alpha\n")
	if err == nil || !strings.Contains(err.Error(), "unterminated front matter") {
		t.Fatalf("expected unterminated front matter error, got %v", err)
	}

	err = parseSlashCommandErr("---\n---\n")
	if err == nil || !strings.Contains(err.Error(), "missing description") {
		t.Fatalf("expected missing description error, got %v", err)
	}
}

func TestParseUnrecognizedFrontMatterKey(t *testing.T) {
	content := "---\ndescription: Test command\nfoo: bar\n---\n\nBody."
	err := parseSlashCommandErr(content)
	if err == nil || !strings.Contains(err.Error(), "unrecognized front matter key \"foo\"") {
		t.Fatalf("expected unrecognized key error, got %v", err)
	}
}

func TestParseUnrecognizedKeyIndentedLineIgnored(t *testing.T) {
	// Indented continuation lines are not top-level keys — should not error.
	content := "---\ndescription: >-\n  some-key: value\n---\n\nBody."
	_, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error for indented line: %v", err)
	}
}

func TestParseUnrecognizedKeyCommentIgnored(t *testing.T) {
	// YAML comment lines containing colons should not be treated as keys.
	content := "---\ndescription: Test command\n# note: this is a comment\n---\n\nBody."
	_, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error for comment line: %v", err)
	}
}

func TestParseDescriptionScalar(t *testing.T) {
	desc, err := parseDescription([]string{"description: \"hello\""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc != "hello" {
		t.Fatalf("unexpected description: %q", desc)
	}
}

func TestParseDescriptionBlockEmpty(t *testing.T) {
	_, err := parseDescription([]string{"description: >-"})
	if err == nil {
		t.Fatalf("expected error")
	}
}
