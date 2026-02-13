package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func managedBlock(block string) string {
	return wrapGitignoreBlock(renderGitignoreBlock(block))
}

func TestRenderGitignoreBlock_UsesSyncGuidance(t *testing.T) {
	rendered := renderGitignoreBlock("foo\n")
	if !strings.Contains(rendered, "re-run `al sync`") {
		t.Fatalf("expected rendered block to guide al sync, got %q", rendered)
	}
	if strings.Contains(rendered, "al init") {
		t.Fatalf("expected rendered block to avoid al init guidance, got %q", rendered)
	}
}

func TestEnsureGitignoreCreatesFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	block := "al\n"

	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	// EnsureGitignore wraps with markers and managed header.
	expected := managedBlock(block)
	if string(data) != expected {
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

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
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

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
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

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
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

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	expected := updateGitignoreContent(original, managedBlock(block))
	if string(data) != expected {
		t.Fatalf("unexpected gitignore content: %q", string(data))
	}

	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore second run error: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitignore second run: %v", err)
	}
	if string(data) != expected {
		t.Fatalf("unexpected gitignore content after rerun: %q", string(data))
	}
}

func TestEnsureGitignoreReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := EnsureGitignore(RealSystem{}, path, "content\n")
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
}

func TestUpdateGitignoreMissingBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.updateGitignore(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestUpdateGitignoreRejectsManagedMarkers(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	blockPath := filepath.Join(alDir, "gitignore.block")
	block := "# >>> agent-layer\n# Template hash: abc\ncontent\n# <<< agent-layer\n"
	if err := os.WriteFile(blockPath, []byte(block), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.updateGitignore()
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "gitignore block") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteGitignoreBlockKeepsTemplateVerbatim(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	if err := os.WriteFile(path, []byte(templateBlock), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated: %v", err)
	}
	if string(data) != templateBlock {
		t.Fatalf("expected template to remain verbatim")
	}
}

func TestWriteGitignoreBlockPreservesCustom(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	custom := "# custom content\n/my-custom-path/\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil); err != nil {
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

func TestWriteGitignoreBlockRecordsDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a custom block that differs from template.
	custom := "# custom content\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	var recorded []string
	recordDiff := func(p string) {
		recorded = append(recorded, p)
	}

	// Call without overwrite - should record diff.
	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, recordDiff); err != nil {
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

	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got %v", err)
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

	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGitignoreBlockMatchesHashValid(t *testing.T) {
	// Create a block with valid hash.
	block := "# comment\ntest content\n"
	hash := gitignoreBlockHash(block)
	blockWithHash := "# comment\n" + gitignoreHashPrefix + hash + "\ntest content\n"

	if !gitignoreBlockMatchesHash(blockWithHash) {
		t.Fatalf("expected hash to match")
	}
}

func TestGitignoreBlockMatchesHashInvalid(t *testing.T) {
	// Block with wrong hash.
	blockWithBadHash := "# comment\n" + gitignoreHashPrefix + "badhash\ntest content\n"

	if gitignoreBlockMatchesHash(blockWithBadHash) {
		t.Fatalf("expected hash to not match")
	}
}

func TestGitignoreBlockMatchesHashNoHash(t *testing.T) {
	// Block without any hash line.
	block := "# comment\ntest content\n"

	if gitignoreBlockMatchesHash(block) {
		t.Fatalf("expected no match when hash is missing")
	}
}

func TestStripGitignoreHashNoHash(t *testing.T) {
	block := "# comment\ntest content\n"
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

func TestWriteGitignoreBlock_MkdirError(t *testing.T) {
	root := t.TempDir()
	// Block directory creation by creating a file at parent path
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestWriteGitignoreBlock_WriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "gitignore.block")

	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteGitignoreBlock_OverwritePromptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	prompt := func(path string) (bool, error) {
		return false, errors.New("prompt error")
	}
	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error from prompt")
	}
}

func TestEnsureGitignore_ReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := EnsureGitignore(RealSystem{}, path, "block")
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

func TestEnsureGitignore_WriteNewError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err := EnsureGitignore(RealSystem{}, path, "block")
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestEnsureGitignore_WriteUpdateError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err := EnsureGitignore(RealSystem{}, path, "new block")
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteGitignoreBlock_MatchingTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	// Write content that matches the template exactly
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteGitignoreBlock_ReadExistingError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

func TestWriteGitignoreBlock_OverwriteWriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	// Write custom content to force overwrite.
	if err := os.WriteFile(path, []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to cause write error
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	prompt := func(path string) (bool, error) {
		return true, nil
	}
	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}
