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

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("expected error from ReadFile")
	}
}

func TestLoadSkills_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.md")
	// Invalid content (no frontmatter)
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
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
