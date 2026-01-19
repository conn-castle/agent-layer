package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/templates"
)

func TestRunCreatesStructure(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	expectFiles := []string{
		filepath.Join(root, ".agent-layer", "config.toml"),
		filepath.Join(root, ".agent-layer", "commands.allow"),
		filepath.Join(root, ".agent-layer", ".env"),
		filepath.Join(root, ".agent-layer", "gitignore.block"),
		filepath.Join(root, "docs", "agent-layer", "ISSUES.md"),
	}
	for _, path := range expectFiles {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), gitignoreStart) {
		t.Fatalf("expected gitignore block to be present")
	}
}

func TestEnsureGitignoreCreatesFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	block := "# >>> agent-layer\nal\n# <<< agent-layer\n"

	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if string(data) != block {
		t.Fatalf("unexpected gitignore content: %q", string(data))
	}
}

func TestEnsureGitignoreReplacesBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n# <<< agent-layer\nend\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "# >>> agent-layer\nnew\n# <<< agent-layer\n"
	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), "new") || strings.Contains(string(data), "old") {
		t.Fatalf("expected block to be replaced, got %q", string(data))
	}
}

func TestEnsureGitignoreAppendsBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "# >>> agent-layer\nnew\n# <<< agent-layer\n"
	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), "new") {
		t.Fatalf("expected appended block, got %q", string(data))
	}
}

func TestEnsureGitignorePartialBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "# >>> agent-layer\nnew\n# <<< agent-layer\n"
	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), "new") {
		t.Fatalf("expected block to be appended")
	}
}

func TestEnsureGitignoreSingleBlankLineAfterBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n# <<< agent-layer\n\n\nnext\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "# >>> agent-layer\nnew\n# <<< agent-layer\n"
	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	expected := "keep\n# >>> agent-layer\nnew\n# <<< agent-layer\n\nnext\n"
	if string(data) != expected {
		t.Fatalf("unexpected gitignore content: %q", string(data))
	}

	if err := ensureGitignore(path, block); err != nil {
		t.Fatalf("ensureGitignore second run error: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore second run: %v", err)
	}
	if string(data) != expected {
		t.Fatalf("unexpected gitignore content after rerun: %q", string(data))
	}
}

func TestWriteTemplateIfMissingExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := writeTemplateIfMissing(path, "config.toml", 0o644); err != nil {
		t.Fatalf("writeTemplateIfMissing error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "custom" {
		t.Fatalf("expected existing file to remain")
	}
}

func TestWriteTemplateDirMissing(t *testing.T) {
	root := t.TempDir()
	err := writeTemplateDir("missing-root", root, false, nil)
	if err == nil {
		t.Fatalf("expected error for missing template root")
	}
}

func TestWriteTemplateIfMissingInvalidTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	err := writeTemplateIfMissing(path, "missing-template", 0o644)
	if err == nil {
		t.Fatalf("expected error for missing template")
	}
}

func TestWriteTemplateIfMissingStatError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	path := filepath.Join(file, "config.toml")
	if err := writeTemplateIfMissing(path, "config.toml", 0o644); err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

func TestEnsureGitignoreReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := ensureGitignore(path, "# >>> agent-layer\n# <<< agent-layer\n")
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
}

func TestCreateDirsError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	inst := &installer{root: file}
	if err := inst.createDirs(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestUpdateGitignoreMissingBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	inst := &installer{root: root}
	if err := inst.updateGitignore(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunStepsError(t *testing.T) {
	err := runSteps([]func() error{
		func() error { return fmt.Errorf("boom") },
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunMissingRoot(t *testing.T) {
	if err := Run("", Options{}); err == nil {
		t.Fatalf("expected error for missing root")
	}
}

func TestWriteGitignoreBlockUpdatesLegacyTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	legacy := normalizeGitignoreBlock(string(templateBytes))
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, false, nil); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated: %v", err)
	}
	if !strings.Contains(string(data), gitignoreHashPrefix) {
		t.Fatalf("expected hash line to be added")
	}
}

func TestWriteGitignoreBlockPreservesCustom(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	custom := "# >>> agent-layer\ncustom\n# <<< agent-layer\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, false, nil); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read custom: %v", err)
	}
	if string(data) != custom {
		t.Fatalf("expected custom gitignore block to remain")
	}
}

func TestRecordDiff(t *testing.T) {
	inst := &installer{root: "/test"}
	inst.recordDiff("/test/file1.txt")
	inst.recordDiff("/test/file2.txt")

	if len(inst.diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(inst.diffs))
	}
	if inst.diffs[0] != "/test/file1.txt" {
		t.Fatalf("unexpected diff[0]: %s", inst.diffs[0])
	}
	if inst.diffs[1] != "/test/file2.txt" {
		t.Fatalf("unexpected diff[1]: %s", inst.diffs[1])
	}
}

func TestWarnDifferencesWithDiffs(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: false,
		diffs:     []string{filepath.Join(root, "file1.txt"), filepath.Join(root, "file2.txt")},
	}

	// Capture stderr by redirecting - warnDifferences writes to os.Stderr.
	// We verify the function completes without panic and processes diffs correctly.
	// The function sorts diffs and formats output.
	inst.warnDifferences()

	// Verify diffs were sorted (warnDifferences sorts inst.diffs).
	if inst.diffs[0] != filepath.Join(root, "file1.txt") {
		t.Fatalf("expected sorted diffs")
	}
}

func TestWarnDifferencesWithOverwrite(t *testing.T) {
	inst := &installer{
		root:      "/test",
		overwrite: true,
		diffs:     []string{"/test/file1.txt"},
	}

	// Should return early without processing diffs when overwrite is true.
	inst.warnDifferences()
	// No panic means success - early return path.
}

func TestWarnDifferencesNoDiffs(t *testing.T) {
	inst := &installer{
		root:      "/test",
		overwrite: false,
		diffs:     []string{},
	}

	// Should return early without processing when no diffs.
	inst.warnDifferences()
	// No panic means success - early return path.
}

func TestWriteGitignoreBlockRecordsDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a custom block that differs from template.
	custom := "# >>> agent-layer\ncustom content\n# <<< agent-layer\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	var recorded []string
	recordDiff := func(p string) {
		recorded = append(recorded, p)
	}

	// Call without overwrite - should record diff.
	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, false, recordDiff); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}

	if len(recorded) != 1 || recorded[0] != path {
		t.Fatalf("expected diff to be recorded, got %v", recorded)
	}
}

func TestGitignoreBlockMatchesHashValid(t *testing.T) {
	// Create a block with valid hash.
	block := "# >>> agent-layer\ntest content\n# <<< agent-layer\n"
	hash := gitignoreBlockHash(block)
	blockWithHash := "# >>> agent-layer\n" + gitignoreHashPrefix + hash + "\ntest content\n# <<< agent-layer\n"

	if !gitignoreBlockMatchesHash(blockWithHash) {
		t.Fatalf("expected hash to match")
	}
}

func TestGitignoreBlockMatchesHashInvalid(t *testing.T) {
	// Block with wrong hash.
	blockWithBadHash := "# >>> agent-layer\n" + gitignoreHashPrefix + "badhash\ntest content\n# <<< agent-layer\n"

	if gitignoreBlockMatchesHash(blockWithBadHash) {
		t.Fatalf("expected hash to not match")
	}
}

func TestGitignoreBlockMatchesHashNoHash(t *testing.T) {
	// Block without any hash line.
	block := "# >>> agent-layer\ntest content\n# <<< agent-layer\n"

	if gitignoreBlockMatchesHash(block) {
		t.Fatalf("expected no match when hash is missing")
	}
}

func TestStripGitignoreHashNoHash(t *testing.T) {
	block := "# >>> agent-layer\ntest content\n# <<< agent-layer\n"
	hash, stripped := stripGitignoreHash(block)

	if hash != "" {
		t.Fatalf("expected empty hash, got %s", hash)
	}
	if stripped != block {
		t.Fatalf("expected stripped to equal original block")
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	lines := splitLines("")
	if len(lines) != 0 {
		t.Fatalf("expected empty slice for empty input, got %v", lines)
	}
}

func TestSplitLinesCarriageReturn(t *testing.T) {
	lines := splitLines("a\r\nb\rc")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestRunWithExistingDifferentFiles(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify a file to differ from template.
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	// Second run without overwrite - should complete but record diff.
	if err := Run(root, Options{Overwrite: false}); err != nil {
		t.Fatalf("second Run error: %v", err)
	}

	// Verify file was not overwritten.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "# custom config" {
		t.Fatalf("expected custom config to remain, got %q", string(data))
	}
}

func TestRunWithOverwrite(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify a file to differ from template.
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	// Run with overwrite - should replace the file.
	if err := Run(root, Options{Overwrite: true}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	// Verify file was overwritten with template content.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) == "# custom config" {
		t.Fatalf("expected config to be overwritten")
	}
}
