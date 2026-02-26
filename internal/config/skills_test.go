package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillContent = `---
description: >-
  First line
  Second line
---

Do the thing.
`

func TestLoadSkills_FlatFilesSorted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name != "a" {
		t.Fatalf("expected lexicographic order, got %s", skills[0].Name)
	}
	if skills[0].Description != "First line Second line" {
		t.Fatalf("unexpected description: %q", skills[0].Description)
	}
	if skills[0].Body != "Do the thing." {
		t.Fatalf("unexpected body: %q", skills[0].Body)
	}
	if skills[0].SourcePath == "" {
		t.Fatalf("expected source path to be set")
	}
}

func TestLoadSkills_DirectoryFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	content := `---
name: alpha
description: Directory skill
license: MIT
compatibility:
  codex: ">=0.1"
metadata:
  owner: team
allowed-tools: ["ripgrep"]
---

Body.`
	if err := os.WriteFile(filepath.Join(dir, "alpha", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "alpha" {
		t.Fatalf("unexpected name: %s", skills[0].Name)
	}
	if skills[0].Description != "Directory skill" {
		t.Fatalf("unexpected description: %q", skills[0].Description)
	}
}

func TestLoadSkills_DuplicateFlatAndDirectoryConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("---\ndescription: flat\n---\n"), 0o644); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo", "SKILL.md"), []byte("---\ndescription: dir\n---\n"), 0o644); err != nil {
		t.Fatalf("write dir skill: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate skill name") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}
}

func TestLoadSkills_DirectoryMissingSkillFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("expected missing SKILL.md error, got %v", err)
	}
}

func TestLoadSkills_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("---\nname: bar\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "expected \"foo\"") {
		t.Fatalf("expected name mismatch error, got %v", err)
	}
}

func parseSkillErr(content string) error {
	_, err := parseSkill(content)
	return err
}

func TestParseSkillErrors(t *testing.T) {
	err := parseSkillErr("")
	if err == nil || !strings.Contains(err.Error(), "missing content") {
		t.Fatalf("expected missing content error, got %v", err)
	}

	err = parseSkillErr("no front matter")
	if err == nil || !strings.Contains(err.Error(), "missing front matter") {
		t.Fatalf("expected front matter error, got %v", err)
	}

	err = parseSkillErr("---\nname: alpha\n")
	if err == nil || !strings.Contains(err.Error(), "unterminated front matter") {
		t.Fatalf("expected unterminated front matter error, got %v", err)
	}

	err = parseSkillErr("---\n---\n")
	if err == nil || !strings.Contains(err.Error(), "missing description") {
		t.Fatalf("expected missing description error, got %v", err)
	}
}

func TestParseSkill_UnknownFrontMatterAllowed(t *testing.T) {
	content := "---\ndescription: Test skill\nfoo: bar\n---\n\nBody."
	parsed, err := parseSkill(content)
	if err != nil {
		t.Fatalf("unexpected error for unknown key: %v", err)
	}
	if parsed.description != "Test skill" {
		t.Fatalf("unexpected description: %q", parsed.description)
	}
}

func TestParseNameScalar(t *testing.T) {
	name, err := parseName([]string{"name: \"hello\""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "hello" {
		t.Fatalf("unexpected name: %q", name)
	}
}

func TestParseNameEmpty(t *testing.T) {
	_, err := parseName([]string{"name:"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseNameMultilineRejected(t *testing.T) {
	_, err := parseName([]string{"name: |-", "  alpha"})
	if err == nil {
		t.Fatalf("expected error")
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
