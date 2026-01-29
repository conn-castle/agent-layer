package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
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
		filepath.Join(root, ".agent-layer", ".gitignore"),
		filepath.Join(root, ".agent-layer", "gitignore.block"),
		filepath.Join(root, "docs", "agent-layer", "BACKLOG.md"),
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

func TestRunWritesPinVersion(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	path := filepath.Join(root, ".agent-layer", "al.version")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin file: %v", err)
	}
	if string(data) != "0.5.0\n" {
		t.Fatalf("unexpected pin content: %q", string(data))
	}
}

func TestRunPinVersionDoesNotOverwriteWithoutFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("0.4.0\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}
	if err := Run(root, Options{PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin file: %v", err)
	}
	if string(data) != "0.4.0\n" {
		t.Fatalf("expected pin to remain unchanged")
	}
}

func TestRunRejectsInvalidPinVersion(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{PinVersion: "dev"}); err == nil {
		t.Fatalf("expected error for invalid pin version")
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
	err := writeTemplateDir("missing-root", root, nil, nil)
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

	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil); err != nil {
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

	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil); err != nil {
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
	if err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, recordDiff); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}

	if len(recorded) != 1 || recorded[0] != path {
		t.Fatalf("expected diff to be recorded, got %v", recorded)
	}
}

func TestWriteGitignoreBlockReadError(t *testing.T) {
	root := t.TempDir()
	// Create a directory where we expect a file, causing ReadFile to fail
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got %v", err)
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
	if err := Run(root, Options{Overwrite: true, Force: true}); err != nil {
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

func TestRunWithOverwriteForceDeletesUnknowns(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	unknownPath := filepath.Join(root, ".agent-layer", "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	if err := Run(root, Options{Overwrite: true, Force: true}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	if _, err := os.Stat(unknownPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unknown file to be deleted, got %v", err)
	}
}

func TestRunWithOverwritePromptsUnknownDeletion(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	unknownPath := filepath.Join(root, ".agent-layer", "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	var deleteAllPaths []string
	var deletePrompted []string
	promptDeleteAll := func(paths []string) (bool, error) {
		deleteAllPaths = append([]string(nil), paths...)
		return false, nil
	}
	promptDelete := func(path string) (bool, error) {
		deletePrompted = append(deletePrompted, path)
		return true, nil
	}

	if err := Run(root, Options{
		Overwrite:                true,
		PromptOverwriteAll:       func([]string) (bool, error) { return true, nil },
		PromptOverwriteMemoryAll: func([]string) (bool, error) { return true, nil },
		PromptDeleteUnknownAll:   promptDeleteAll,
		PromptDeleteUnknown:      promptDelete,
	}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	if _, err := os.Stat(unknownPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unknown file to be deleted, got %v", err)
	}
	if len(deleteAllPaths) != 1 || deleteAllPaths[0] != filepath.Join(".agent-layer", "custom.txt") {
		t.Fatalf("unexpected delete-all paths: %v", deleteAllPaths)
	}
	if len(deletePrompted) != 1 || deletePrompted[0] != filepath.Join(".agent-layer", "custom.txt") {
		t.Fatalf("unexpected delete prompt paths: %v", deletePrompted)
	}
}

func TestRunWithOverwriteMissingPrompt(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{Overwrite: true}); err == nil {
		t.Fatalf("expected error when overwrite prompt handler is missing")
	}
}

func TestRunWithOverwriteMissingDeletePrompt(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{
		Overwrite:                true,
		PromptOverwriteAll:       func([]string) (bool, error) { return true, nil },
		PromptOverwriteMemoryAll: func([]string) (bool, error) { return true, nil },
	}); err == nil {
		t.Fatalf("expected error when delete prompt handler is missing")
	}
}

func TestRunWithOverwritePromptDecline(t *testing.T) {
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

	var prompted []string
	prompt := func(path string) (bool, error) {
		prompted = append(prompted, path)
		return false, nil
	}

	if err := Run(root, Options{
		Overwrite:                true,
		PromptOverwriteAll:       func([]string) (bool, error) { return false, nil },
		PromptOverwriteMemoryAll: func([]string) (bool, error) { return false, nil },
		PromptOverwrite:          prompt,
		PromptDeleteUnknownAll:   func([]string) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "# custom config" {
		t.Fatalf("expected config to remain after declining prompt")
	}
	if len(prompted) != 1 || prompted[0] != filepath.Join(".agent-layer", "config.toml") {
		t.Fatalf("unexpected prompt paths: %v", prompted)
	}
}

func TestShouldOverwrite_UsesMemoryPrompt(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")

	managedCalled := false
	memoryCalled := false
	inst := &installer{
		root:                     root,
		overwrite:                true,
		promptOverwriteAll:       func([]string) (bool, error) { managedCalled = true; return false, nil },
		promptOverwriteMemoryAll: func([]string) (bool, error) { memoryCalled = true; return false, nil },
		promptOverwrite:          func(string) (bool, error) { return false, nil },
	}

	_, err := inst.shouldOverwrite(memoryPath)
	if err != nil {
		t.Fatalf("shouldOverwrite error: %v", err)
	}
	if managedCalled {
		t.Fatalf("expected managed prompt not to be called for memory paths")
	}
	if !memoryCalled {
		t.Fatalf("expected memory prompt to be called for memory paths")
	}
}

func TestListManagedDiffs_IgnoresGitignoreBlockHash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	rendered := renderGitignoreBlock(normalizeGitignoreBlock(string(templateBytes)))
	gitignorePath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(gitignorePath, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root}
	diffs, err := inst.listManagedDiffs()
	if err != nil {
		t.Fatalf("listManagedDiffs: %v", err)
	}
	if len(diffs) != 1 || diffs[0] != filepath.Join(".agent-layer", "config.toml") {
		t.Fatalf("unexpected diffs: %v", diffs)
	}
}

func TestWriteTemplateFileWithMatch_UsesCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	inst := &installer{}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if _, err := inst.matchTemplate(path, "config.toml", info); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(string) ([]byte, error) {
		return nil, errors.New("unexpected template read")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	if err := writeTemplateFileWithMatch(path, "config.toml", 0o644, nil, nil, inst.matchTemplate); err != nil {
		t.Fatalf("expected cached match to skip template read: %v", err)
	}
}

func TestWriteGitignoreBlockTemplateReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteTemplateDirWalkError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("mock walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	root := t.TempDir()
	err := writeTemplateDir("instructions", root, nil, nil)
	if err == nil {
		t.Fatalf("expected error for walk failure")
	}
	if !strings.Contains(err.Error(), "mock walk error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileMatchesTemplateReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(p string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := fileMatchesTemplate(path, "config.toml")
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}
