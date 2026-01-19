package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGolden(t *testing.T) {
	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	root := t.TempDir()
	if err := copyFixture(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	writePromptServerBinary(t, root)

	if err := Run(root); err != nil {
		t.Fatalf("sync run: %v", err)
	}

	expectedRoot := filepath.Join(fixtureRoot, "expected")
	files := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"GEMINI.md",
		".github/copilot-instructions.md",
		".codex/AGENTS.md",
		".codex/config.toml",
		".codex/rules/default.rules",
		".codex/skills/alpha/SKILL.md",
		".codex/skills/beta/SKILL.md",
		".vscode/prompts/alpha.prompt.md",
		".vscode/prompts/beta.prompt.md",
		".vscode/settings.json",
		".vscode/mcp.json",
		".gemini/settings.json",
		".claude/settings.json",
		".mcp.json",
	}
	for _, rel := range files {
		expected := filepath.Join(expectedRoot, rel)
		actual := filepath.Join(root, rel)
		assertFileEquals(t, expected, actual)
	}
}

func copyFixture(src string, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == "expected" || strings.HasPrefix(rel, "expected"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func assertFileEquals(t *testing.T, expectedPath string, actualPath string) {
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected %s: %v", expectedPath, err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("read actual %s: %v", actualPath, err)
	}
	if string(expected) != string(actual) {
		t.Fatalf("mismatch for %s", actualPath)
	}
}
