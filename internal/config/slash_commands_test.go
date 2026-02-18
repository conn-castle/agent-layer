package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const slashCommandContent = `---
name: alpha
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
	_, _, _, err := parseSlashCommand(content) //nolint:dogsled // test helper discards parsed values
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

	err = parseSlashCommandErr("---\nname: alpha\n---\n")
	if err == nil || !strings.Contains(err.Error(), "missing description") {
		t.Fatalf("expected missing description error, got %v", err)
	}
}

func TestParseAutoApproveTrue(t *testing.T) {
	content := "---\ndescription: Test command\nauto-approve: true\n---\n\nBody text."
	desc, autoApprove, body, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc != "Test command" {
		t.Fatalf("unexpected description: %q", desc)
	}
	if !autoApprove {
		t.Fatal("expected auto-approve to be true")
	}
	if body != "Body text." {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestParseAutoApproveFalse(t *testing.T) {
	content := "---\ndescription: Test command\nauto-approve: false\n---\n\nBody."
	_, autoApprove, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if autoApprove {
		t.Fatal("expected auto-approve to be false")
	}
}

func TestParseAutoApproveAbsent(t *testing.T) {
	content := "---\ndescription: Test command\n---\n\nBody."
	_, autoApprove, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if autoApprove {
		t.Fatal("expected auto-approve to default to false when absent")
	}
}

func TestParseAutoApproveEmpty(t *testing.T) {
	content := "---\ndescription: Test command\nauto-approve:\n---\n\nBody."
	err := parseSlashCommandErr(content)
	if err == nil || !strings.Contains(err.Error(), "auto-approve") {
		t.Fatalf("expected auto-approve error for empty value, got %v", err)
	}
}

func TestParseAutoApproveInvalid(t *testing.T) {
	content := "---\ndescription: Test command\nauto-approve: notaboolean\n---\n\nBody."
	err := parseSlashCommandErr(content)
	if err == nil || !strings.Contains(err.Error(), "auto-approve") {
		t.Fatalf("expected auto-approve error for invalid value, got %v", err)
	}
}

func TestLoadSlashCommandsAutoApprove(t *testing.T) {
	dir := t.TempDir()
	autoApproveContent := "---\ndescription: Auto-approved skill\nauto-approve: true\n---\n\nDo something."
	normalContent := "---\ndescription: Normal skill\n---\n\nDo something else."
	if err := os.WriteFile(filepath.Join(dir, "approved.md"), []byte(autoApproveContent), 0o644); err != nil {
		t.Fatalf("write approved: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "normal.md"), []byte(normalContent), 0o644); err != nil {
		t.Fatalf("write normal: %v", err)
	}

	commands, err := LoadSlashCommands(dir)
	if err != nil {
		t.Fatalf("LoadSlashCommands error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	// Lexicographic: approved before normal
	if !commands[0].AutoApprove {
		t.Fatal("expected approved.md to have AutoApprove=true")
	}
	if commands[1].AutoApprove {
		t.Fatal("expected normal.md to have AutoApprove=false")
	}
}

func TestParseAutoApproveSkipsDescriptionContinuationLine(t *testing.T) {
	// A multiline description continuation line that looks like "auto-approve: true"
	// must not flip the flag. The real auto-approve key is unindented and wins.
	content := "---\ndescription: >-\n  auto-approve: true\nauto-approve: false\n---\n\nBody."
	_, autoApprove, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if autoApprove {
		t.Fatal("expected auto-approve to be false; continuation line should not match")
	}
}

func TestParseAutoApproveSkipsIndentedLine(t *testing.T) {
	// If auto-approve appears only in an indented line, it should be silently skipped
	// (defaults to false) rather than matching or erroring.
	content := "---\ndescription: >-\n  auto-approve: true\n---\n\nBody."
	_, autoApprove, _, err := parseSlashCommand(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if autoApprove {
		t.Fatal("expected auto-approve to default to false when only in indented line")
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
