package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const skillContent = `---
description: >-
  First line
  Second line
---

Do the thing.
`

func TestLoadSkills_FlatFormatReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(skillContent), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatal("expected error for flat-format skill")
	}
	if !strings.Contains(err.Error(), "flat format is no longer supported") {
		t.Fatalf("expected flat-format unsupported error, got %v", err)
	}
	if !strings.Contains(err.Error(), "al upgrade") {
		t.Fatalf("expected upgrade guidance in error, got %v", err)
	}
}

func TestLoadSkills_DirectoryFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o700); err != nil {
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
disable-model-invocation: true
---

Body.`
	if err := os.WriteFile(filepath.Join(dir, "alpha", "SKILL.md"), []byte(content), 0o600); err != nil {
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
	manifest, ok := skills[0].Tree.File("SKILL.md")
	if !ok || string(manifest.Data) != content {
		t.Fatalf("canonical content was not retained exactly: %q", manifest.Data)
	}
	expectedDir := filepath.Join(dir, "alpha")
	if skills[0].SourceDir != expectedDir {
		t.Fatalf("unexpected SourceDir: got %q, want %q", skills[0].SourceDir, expectedDir)
	}
}

func TestLoadSkills_DirectoryFormat_LowercaseSkillFileFailsActionably(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	content := `---
name: alpha
description: Directory skill
---

Body.`
	_, err := loadSkills(
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
		func(string) (skilltree.Tree, error) {
			return skilltree.NewTree([]skilltree.File{{Path: "skill.md", Data: []byte(content)}})
		},
	)
	if err == nil || !strings.Contains(err.Error(), "rename") || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("expected canonical rename guidance, got %v", err)
	}
}

func TestLoadSkills_DirectoryFormat_BothManifestSpellingsAreAmbiguous(t *testing.T) {
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
	_, err := loadSkills(
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
		func(string) (skilltree.Tree, error) {
			return skilltree.NewTree([]skilltree.File{
				{Path: "SKILL.md", Data: []byte(canonicalContent)},
				{Path: "skill.md", Data: []byte(fallbackContent)},
			})
		},
	)
	if err == nil || !strings.Contains(err.Error(), "both SKILL.md and skill.md") {
		t.Fatalf("expected ambiguous manifest error, got %v", err)
	}
}

func TestLoadSkills_DirectoryFormat_NameIsRequired(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	content := `---
description: Directory skill
---

Body.`
	if err := os.WriteFile(filepath.Join(dir, "alpha", "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "missing required frontmatter field \"name\"") {
		t.Fatalf("expected required name error, got %v", err)
	}
}

func TestLoadSkills_DirectoryFormat_NameMatchUsesNFKCNormalization(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ﬁle"), 0o700); err != nil {
		t.Fatalf("mkdir ligature: %v", err)
	}
	content := `---
name: file
description: Directory skill
---

Body.`
	if err := os.WriteFile(filepath.Join(dir, "ﬁle", "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "file" {
		t.Fatalf("expected normalized canonical name file, got %q", skills[0].Name)
	}
}

func TestSourceNodeTypeNamesActionableFilesystemKinds(t *testing.T) {
	t.Parallel()
	for mode, want := range map[os.FileMode]string{
		os.ModeSymlink:   "symlink",
		os.ModeNamedPipe: "named pipe",
		os.ModeSocket:    "socket",
		os.ModeDevice:    "device",
		os.ModeIrregular: "unsupported filesystem node",
	} {
		if got := sourceNodeType(mode); got != want {
			t.Fatalf("sourceNodeType(%v) = %q, want %q", mode, got, want)
		}
	}
}

func TestLoadSkills_FlatFileRejectsBeforeDirectoryLoads(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("---\ndescription: flat\n---\n"), 0o600); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o700); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo", "SKILL.md"), []byte("---\ndescription: dir\n---\n"), 0o600); err != nil {
		t.Fatalf("write dir skill: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "flat format is no longer supported") {
		t.Fatalf("expected flat-format unsupported error, got %v", err)
	}
}

func TestLoadSkills_DirectoryMissingSkillFileFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o700); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatal("expected error for skill directory without SKILL.md")
	}
	if !strings.Contains(err.Error(), "has no SKILL.md") {
		t.Fatalf("expected missing SKILL.md error, got %v", err)
	}
}

func TestLoadSkills_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	// Flat .md at the skills root is now rejected before we even parse
	// the front-matter, so put the mismatched skill in directory format.
	skillDir := filepath.Join(dir, "foo")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: bar\ndescription: test\n---\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "canonical source name \"foo\"") {
		t.Fatalf("expected name mismatch error, got %v", err)
	}
}
