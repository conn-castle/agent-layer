package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func TestLoadSkills_ReadDirError(t *testing.T) {
	_, err := LoadSkills("/non-existent/dir")
	if err == nil {
		t.Fatalf("expected error from ReadDir")
	}
}

func TestLoadSkills_ReadFileError(t *testing.T) {
	dir := filepath.Join("root", "skills")
	skillDir := filepath.Join(dir, "bad")
	_, err := loadSkills(
		dir,
		func(path string) ([]skillDirEntry, error) {
			switch path {
			case dir:
				return []skillDirEntry{{name: "bad", isDir: true}}, nil
			case skillDir:
				return []skillDirEntry{{name: "SKILL.md", isDir: false}}, nil
			default:
				t.Fatalf("unexpected directory read path: %q", path)
				return nil, nil
			}
		},
		func(path string) (skilltree.Tree, error) {
			if path != skillDir {
				t.Fatalf("unexpected tree read path: got %q, want %q", path, skillDir)
			}
			return skilltree.Tree{}, errors.New("injected read error")
		},
	)
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
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	// Invalid content (no frontmatter)
	if err := os.WriteFile(skillPath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("expected error from parseSkill")
	}
}
