package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkills_ReadDirError(t *testing.T) {
	_, err := LoadSkills("/non-existent/dir")
	if err == nil {
		t.Fatalf("expected error from ReadDir")
	}
}

func TestLoadSkills_ReadFileError(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "bad")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	orig := osReadFileFunc
	osReadFileFunc = func(name string) ([]byte, error) {
		if name == skillPath {
			return nil, errors.New("injected read error")
		}
		return orig(name)
	}
	t.Cleanup(func() { osReadFileFunc = orig })

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("expected error from ReadFile")
	}
	if !strings.Contains(err.Error(), "injected read error") {
		t.Fatalf("expected injected read error, got: %v", err)
	}
}

func TestLoadSkills_ParseError(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "invalid")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	// Invalid content (no frontmatter)
	if err := os.WriteFile(skillPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("expected error from parseSkill")
	}
}

func TestParseSkill_InvalidFrontMatterSyntax(t *testing.T) {
	_, err := parseSkill("---\ndescription: test\nmetadata: [\n---\n")
	if err == nil || !strings.Contains(err.Error(), "invalid front matter") {
		t.Fatalf("expected invalid front matter syntax error, got %v", err)
	}
}

func TestParseSkill_LegacyUnquotedColonScalarRejected(t *testing.T) {
	_, err := parseSkill("---\ndescription: legacy parser style: value\n---\n")
	if err == nil || !strings.Contains(err.Error(), "invalid front matter") {
		t.Fatalf("expected invalid front matter error, got %v", err)
	}
}

func TestParseSkill_MetadataNilWhenEmpty(t *testing.T) {
	parsed, err := parseSkill("---\ndescription: test\nmetadata: {}\n---\n")
	if err != nil {
		t.Fatalf("parseSkill error: %v", err)
	}
	if parsed.metadata != nil {
		t.Fatalf("expected nil metadata for empty map, got %#v", parsed.metadata)
	}
}
