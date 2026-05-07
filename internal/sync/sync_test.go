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
		".agents/skills/alpha/SKILL.md",
		".agents/skills/beta/SKILL.md",
		".claude/skills/alpha/SKILL.md",
		".claude/skills/beta/SKILL.md",
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

	absent := []string{
		".codex/skills",
		".agent/skills",
		".gemini/skills",
		".github/skills",
		".vscode/prompts",
	}
	for _, rel := range absent {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected retired skill output %s to be absent", rel)
		}
	}
}

func TestRunCleansLegacySkillOutputs(t *testing.T) {
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

	// Seed legacy projection paths with both generated and manual content. Per the
	// SKILL-CLIENT-SPEC ownership contract, Agent Layer claims these directories
	// exclusively and must remove all of it on every sync.
	legacyEntries := []struct {
		path    string
		content string
	}{
		{filepath.Join(".codex", "skills", "old", "SKILL.md"), generatedMarkerFixture},
		{filepath.Join(".agent", "skills", "old", "SKILL.md"), generatedMarkerFixture},
		{filepath.Join(".gemini", "skills", "old", "SKILL.md"), generatedMarkerFixture},
		{filepath.Join(".github", "skills", "manual", "SKILL.md"), "# manual\n"},
		{filepath.Join(".vscode", "prompts", "old.prompt.md"), generatedMarkerFixture},
	}
	for _, entry := range legacyEntries {
		full := filepath.Join(root, entry.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(entry.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	if _, err := Run(root); err != nil {
		t.Fatalf("sync run: %v", err)
	}

	for _, rel := range []string{
		".codex/skills",
		".agent/skills",
		".gemini/skills",
		".github/skills",
		".vscode/prompts",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected retired %s to be removed (Agent Layer claims exclusive ownership), got err=%v", rel, err)
		}
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
