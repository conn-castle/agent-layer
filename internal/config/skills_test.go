package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
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
	if skills[0].License != "" || skills[0].Compatibility != "" || skills[0].AllowedTools != "" {
		t.Fatalf("expected optional fields to be empty: %#v", skills[0])
	}
	if skills[0].Metadata != nil {
		t.Fatalf("expected metadata to be nil, got %#v", skills[0].Metadata)
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
compatibility: requires git, jq, and internet access
metadata:
  owner: team
  version: "1.0"
allowed-tools: Bash(git:*) Read
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
	if skills[0].License != "MIT" {
		t.Fatalf("unexpected license: %q", skills[0].License)
	}
	if skills[0].Compatibility != "requires git, jq, and internet access" {
		t.Fatalf("unexpected compatibility: %q", skills[0].Compatibility)
	}
	if skills[0].AllowedTools != "Bash(git:*) Read" {
		t.Fatalf("unexpected allowed-tools: %q", skills[0].AllowedTools)
	}
	if len(skills[0].Metadata) != 2 || skills[0].Metadata["owner"] != "team" || skills[0].Metadata["version"] != "1.0" {
		t.Fatalf("unexpected metadata: %#v", skills[0].Metadata)
	}
}

func TestLoadSkills_DirectoryFormat_LowercaseSkillFileFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	content := `---
name: alpha
description: Directory skill
---

Body.`
	files := map[string][]byte{
		filepath.Join(dir, "alpha", "skill.md"): []byte(content),
	}
	skills, err := loadSkills(
		dir,
		func(path string) ([]skillDirEntry, error) {
			switch path {
			case dir:
				return []skillDirEntry{{name: "alpha", isDir: true}}, nil
			case filepath.Join(dir, "alpha"):
				return []skillDirEntry{{name: "skill.md", isDir: false}}, nil
			default:
				return nil, os.ErrNotExist
			}
		},
		func(path string) ([]byte, error) {
			data, ok := files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return data, nil
		},
	)
	if err != nil {
		t.Fatalf("loadSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].SourcePath != filepath.Join(dir, "alpha", "skill.md") {
		t.Fatalf("expected fallback source path, got %q", skills[0].SourcePath)
	}
}

func TestLoadSkills_DirectoryFormat_PrefersCanonicalSkillFileName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	canonicalContent := `---
name: alpha
description: Canonical
---

Body.`
	fallbackContent := `---
name: alpha
description: Fallback
---

Body.`
	files := map[string][]byte{
		filepath.Join(dir, "alpha", "SKILL.md"): []byte(canonicalContent),
		filepath.Join(dir, "alpha", "skill.md"): []byte(fallbackContent),
	}
	skills, err := loadSkills(
		dir,
		func(path string) ([]skillDirEntry, error) {
			switch path {
			case dir:
				return []skillDirEntry{{name: "alpha", isDir: true}}, nil
			case filepath.Join(dir, "alpha"):
				return []skillDirEntry{
					{name: "SKILL.md", isDir: false},
					{name: "skill.md", isDir: false},
				}, nil
			default:
				return nil, os.ErrNotExist
			}
		},
		func(path string) ([]byte, error) {
			data, ok := files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return data, nil
		},
	)
	if err != nil {
		t.Fatalf("loadSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Description != "Canonical" {
		t.Fatalf("expected canonical file to win, got %q", skills[0].Description)
	}
	if skills[0].SourcePath != filepath.Join(dir, "alpha", "SKILL.md") {
		t.Fatalf("expected canonical source path, got %q", skills[0].SourcePath)
	}
}

func TestLoadSkills_DirectoryFormat_NameDerivedWhenFrontMatterNameMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	content := `---
description: Directory skill
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
		t.Fatalf("expected derived skill name alpha, got %q", skills[0].Name)
	}
}

func TestLoadSkills_DirectoryFormat_NameMatchUsesNFKCNormalization(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "café"), 0o755); err != nil {
		t.Fatalf("mkdir café: %v", err)
	}
	content := `---
name: café
description: Directory skill
---

Body.`
	if err := os.WriteFile(filepath.Join(dir, "café", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "café" {
		t.Fatalf("expected derived canonical name café, got %q", skills[0].Name)
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
	if err == nil || !strings.Contains(err.Error(), "missing SKILL.md or skill.md") {
		t.Fatalf("expected missing SKILL.md or skill.md error, got %v", err)
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

	err = parseSkillErr("---\ndescription:\n---\n")
	if err == nil || !strings.Contains(err.Error(), "description is empty") {
		t.Fatalf("expected empty description error, got %v", err)
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

func TestParseSkill_NameEmpty(t *testing.T) {
	_, err := parseSkill("---\nname: \"\"\ndescription: test\n---\n")
	if err == nil || !strings.Contains(err.Error(), "name is empty") {
		t.Fatalf("expected name-empty error, got %v", err)
	}
}

func TestParseSkill_NameMultilineRejected(t *testing.T) {
	_, err := parseSkill("---\nname: |-\n  alpha\ndescription: test\n---\n")
	if err == nil || !strings.Contains(err.Error(), "name must be a single line scalar") {
		t.Fatalf("expected multiline-name error, got %v", err)
	}
}

func TestParseSkill_OptionalFieldsTrimmed(t *testing.T) {
	content := `---
description: desc
license: "  MIT  "
compatibility: "  needs docker  "
allowed-tools: "  Bash(git:*) Read  "
metadata:
  owner: team
---
`
	parsed, err := parseSkill(content)
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.license != "MIT" || parsed.compatibility != "needs docker" || parsed.allowedTools != "Bash(git:*) Read" {
		t.Fatalf("unexpected optional values: %#v", parsed)
	}
}

func TestParseSkill_FoldedAndLiteralDescriptions(t *testing.T) {
	folded, err := parseSkill(`---
description: >-
  First line
  Second line
---
`)
	if err != nil {
		t.Fatalf("parse folded description: %v", err)
	}
	if folded.description != "First line Second line" {
		t.Fatalf("unexpected folded description: %q", folded.description)
	}

	literal, err := parseSkill(`---
description: |
  First line
  Second line
---
`)
	if err != nil {
		t.Fatalf("parse literal description: %v", err)
	}
	if literal.description != "First line\nSecond line" {
		t.Fatalf("unexpected literal description: %q", literal.description)
	}
}

func TestParseSkill_TypeMismatchErrors(t *testing.T) {
	tests := []string{
		"---\ndescription: test\ncompatibility:\n  codex: \">=0.1\"\n---\n",
		"---\ndescription: test\nallowed-tools:\n  - Read\n---\n",
		"---\ndescription: test\nmetadata:\n  owner: 7\n---\n",
	}
	for _, content := range tests {
		if _, err := parseSkill(content); err == nil || !strings.Contains(err.Error(), "invalid front matter type") {
			t.Fatalf("expected front matter type error, got %v for %q", err, content)
		}
	}
}

func TestParseSkill_NullMetadataAccepted(t *testing.T) {
	parsed, err := parseSkill("---\ndescription: test\nmetadata: ~\n---\n")
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.metadata != nil {
		t.Fatalf("expected nil metadata for null value, got %#v", parsed.metadata)
	}

	parsed, err = parseSkill("---\ndescription: test\nmetadata: null\n---\n")
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.metadata != nil {
		t.Fatalf("expected nil metadata for null value, got %#v", parsed.metadata)
	}
}

func TestParseSkill_NullDescriptionFails(t *testing.T) {
	_, err := parseSkill("---\ndescription: null\n---\n")
	if err == nil {
		t.Fatal("expected error for null description")
	}
	if !strings.Contains(err.Error(), messages.ConfigSkillDescriptionEmpty) {
		t.Fatalf("expected %q error, got %v", messages.ConfigSkillDescriptionEmpty, err)
	}
}

func TestParseSkill_NullOptionalStringFieldsTreatedAsAbsent(t *testing.T) {
	parsed, err := parseSkill("---\ndescription: test\nlicense: null\ncompatibility: ~\nallowed-tools: null\n---\n")
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.license != "" || parsed.compatibility != "" || parsed.allowedTools != "" {
		t.Fatalf("expected null optional fields to normalize to empty strings, got %#v", parsed)
	}
}

func TestParseSkill_NullNameFails(t *testing.T) {
	_, err := parseSkill("---\nname: null\ndescription: test\n---\n")
	if err == nil {
		t.Fatal("expected error for null name")
	}
	if !strings.Contains(err.Error(), messages.ConfigSkillNameEmpty) {
		t.Fatalf("expected %q error, got %v", messages.ConfigSkillNameEmpty, err)
	}
}

func TestParseSkill_EmptyOptionalStringsTreatedAsAbsent(t *testing.T) {
	parsed, err := parseSkill("---\ndescription: test\nlicense: \"\"\ncompatibility: \"  \"\nallowed-tools:\n---\n")
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.license != "" || parsed.compatibility != "" || parsed.allowedTools != "" {
		t.Fatalf("expected empty optional fields to normalize to empty strings, got %#v", parsed)
	}
}
