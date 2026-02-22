package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGolden(t *testing.T) {
	home := t.TempDir()
	origHome := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = origHome })

	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	root := t.TempDir()
	if err := copyFixtureRepo(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	envPath := filepath.Join(root, ".agent-layer", ".env")
	if err := os.WriteFile(envPath, []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	writePromptServerBinary(t, root)

	result, err := Run(root)
	if err != nil {
		t.Fatalf("sync run: %v", err)
	}
	// No warnings expected for the fixture (small content, few servers)
	if len(result.Warnings) > 0 {
		t.Logf("unexpected warnings: %v", result.Warnings)
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
		".agent/skills/alpha/SKILL.md",
		".agent/skills/beta/SKILL.md",
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
		assertFileEquals(t, expected, actual, root)
	}
}

func copyFixtureRepo(src string, dest string) error {
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

func assertFileEquals(t *testing.T, expectedPath string, actualPath string, repoRoot string) {
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected %s: %v", expectedPath, err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("read actual %s: %v", actualPath, err)
	}
	expectedContent := strings.ReplaceAll(string(expected), "__REPO_ROOT__", repoRoot)
	if expectedContent != string(actual) {
		t.Fatalf("mismatch for %s", actualPath)
	}
}
