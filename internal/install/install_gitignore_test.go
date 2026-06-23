package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

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
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	got := string(data)
	// Assert hand-written structural invariants of a freshly created file so a
	// defect in the render/wrap helpers (dropped marker, missing header, absent
	// hash, lost block body) actually fails the test — re-deriving `expected`
	// from the same helpers would mutate both sides together and never fail.
	wantLines := []string{
		"# >>> agent-layer",
		"# Managed by Agent Layer. To customize, edit .agent-layer/gitignore.block",
		"# and re-run `al sync` to apply changes.",
		"al",
		"# <<< agent-layer",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("expected gitignore to contain line %q, got:\n%s", line, got)
		}
	}
	if !strings.Contains(got, "# Template hash: ") {
		t.Fatalf("expected a template hash line, got:\n%s", got)
	}
	// The managed block must start at the start marker and the body must follow
	// the header (start marker before hash before header before the block body).
	startIdx := strings.Index(got, "# >>> agent-layer")
	hashIdx := strings.Index(got, "# Template hash: ")
	bodyIdx := strings.Index(got, "\nal\n")
	endIdx := strings.Index(got, "# <<< agent-layer")
	if startIdx != 0 || startIdx >= hashIdx || hashIdx >= bodyIdx || bodyIdx >= endIdx {
		t.Fatalf("unexpected ordering of marker/hash/header/body/end markers: %q", got)
	}
	if !strings.HasSuffix(got, "# <<< agent-layer\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatalf("expected exactly one trailing newline after end marker, got %q", got)
	}
}

func TestEnsureGitignoreReplacesBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n# <<< agent-layer\nend\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), "new") {
		t.Fatalf("expected appended block, got %q", string(data))
	}
}

func TestEnsureGitignorePartialBlock(t *testing.T) {
	// A .gitignore with an agent-layer start marker but no matching end marker is
	// corrupt. Appending a second managed block (the previous behavior) would, on
	// the next sync, silently delete everything between the orphaned start marker
	// and the appended block — i.e. the user's "old" line here. EnsureGitignore
	// must instead fail loud and leave the file untouched.
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "new\n" // No markers - EnsureGitignore adds them
	err := EnsureGitignore(RealSystem{}, path, block)
	if err == nil {
		t.Fatalf("expected error for orphaned start marker, got nil")
	}
	if !strings.Contains(err.Error(), "# >>> agent-layer") || !strings.Contains(err.Error(), "# <<< agent-layer") {
		t.Fatalf("expected error to name both markers, got %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if string(data) != original {
		t.Fatalf("expected file to be left untouched on error, got %q", string(data))
	}
}

func TestEnsureGitignoreSingleBlankLineAfterBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	original := "keep\n# >>> agent-layer\nold\n# <<< agent-layer\n\n\nnext\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	block := "new\n" // No markers - EnsureGitignore adds them
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	firstRun := string(data)
	// The input had TWO blank lines between the old end marker and `next`; the
	// merge must collapse them to exactly one. Assert that hand-written
	// invariant directly instead of re-deriving the expected output from the
	// same production function (which could never catch a collapse defect).
	if !strings.Contains(firstRun, "# <<< agent-layer\n\nnext\n") {
		t.Fatalf("expected exactly one blank line between end marker and following content, got %q", firstRun)
	}
	if strings.Contains(firstRun, "# <<< agent-layer\n\n\n") {
		t.Fatalf("blank lines after the managed block were not collapsed to one, got %q", firstRun)
	}
	if !strings.HasPrefix(firstRun, "keep\n") {
		t.Fatalf("expected pre-block content to be preserved, got %q", firstRun)
	}
	if strings.Contains(firstRun, "old") {
		t.Fatalf("expected the old managed block body to be replaced, got %q", firstRun)
	}

	// Re-running must be idempotent: a second apply produces byte-identical
	// content (no drift, no extra blank lines accumulating).
	if err := EnsureGitignore(RealSystem{}, path, block); err != nil {
		t.Fatalf("EnsureGitignore second run error: %v", err)
	}
	data, err = os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore second run: %v", err)
	}
	if string(data) != firstRun {
		t.Fatalf("second run not idempotent: %q != %q", string(data), firstRun)
	}
}

func TestEnsureGitignoreReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := EnsureGitignore(RealSystem{}, path, "content\n")
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
}

func TestUpdateGitignoreMissingBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
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
	if err := os.MkdirAll(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	blockPath := filepath.Join(alDir, "gitignore.block")
	block := "# >>> agent-layer\n# Template hash: abc\ncontent\n# <<< agent-layer\n"
	if err := os.WriteFile(blockPath, []byte(block), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	if err := os.WriteFile(path, []byte(templateBlock), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	custom := "# custom content\n/my-custom-path/\n"
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, nil); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a custom block that differs from template.
	custom := "# custom content\n"
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
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
	if err := os.MkdirAll(path, 0o700); err != nil {
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
	if err := os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("file"), 0o600); err != nil {
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
	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
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
	if err := os.Mkdir(path, 0o700); err != nil {
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
	if err := os.Chmod(root, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

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
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(root, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

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
	if err := os.WriteFile(path, templateBytes, 0o600); err != nil {
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
	if err := os.Mkdir(path, 0o700); err != nil {
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
	if err := os.WriteFile(path, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to cause write error
	if err := os.Chmod(root, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

	prompt := func(path string) (bool, error) {
		return true, nil
	}
	err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestRepairGitignoreBlock(t *testing.T) {
	root := t.TempDir()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	blockPath := filepath.Join(agentLayerDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte("# >>> agent-layer\nbad\n"), 0o600); err != nil {
		t.Fatalf("write invalid block: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("write root .gitignore: %v", err)
	}

	if err := RepairGitignoreBlock(root, RepairGitignoreBlockOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("RepairGitignoreBlock: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	blockBytes, err := os.ReadFile(blockPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read repaired block: %v", err)
	}
	if string(blockBytes) != string(templateBytes) {
		t.Fatalf("repaired block did not match template")
	}

	gitignoreBytes, err := os.ReadFile(filepath.Join(root, ".gitignore")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read root .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignoreBytes), "# >>> agent-layer") || !strings.Contains(string(gitignoreBytes), "# <<< agent-layer") {
		t.Fatalf("expected managed agent-layer block markers in root .gitignore, got:\n%s", string(gitignoreBytes))
	}
}

func TestRepairGitignoreBlock_RequiresRootAndSystem(t *testing.T) {
	if err := RepairGitignoreBlock("", RepairGitignoreBlockOptions{System: RealSystem{}}); err == nil {
		t.Fatal("expected error when root is empty")
	}
	if err := RepairGitignoreBlock(t.TempDir(), RepairGitignoreBlockOptions{}); err == nil {
		t.Fatal("expected error when system is nil")
	}
}

func TestRepairGitignoreBlock_TemplateReadError(t *testing.T) {
	origRead := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("template read failed")
	}
	t.Cleanup(func() { templates.ReadFunc = origRead })

	root := t.TempDir()
	if err := RepairGitignoreBlock(root, RepairGitignoreBlockOptions{System: RealSystem{}}); err == nil {
		t.Fatal("expected template read error")
	}
}

func TestAgentLayerGitignoreTemplateEntries(t *testing.T) {
	data, err := templates.Read("agent-layer.gitignore")
	if err != nil {
		t.Fatalf("read agent-layer.gitignore template: %v", err)
	}
	lines := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		lines[strings.TrimSpace(line)] = struct{}{}
	}
	required := []string{
		".env",
		"config.toml.bak",
		".env.bak",
		"/templates/",
		"state/",
		"tmp/",
		"open-vscode.app/",
		"open-vscode.command",
		"open-vscode.desktop",
		"open-vscode.sh",
	}
	for _, entry := range required {
		if _, ok := lines[entry]; !ok {
			t.Errorf("agent-layer.gitignore template missing required entry %q", entry)
		}
	}
}

func TestRepairGitignoreBlock_WriteBlockError(t *testing.T) {
	root := t.TempDir()
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(blockPath, 0o700); err != nil {
		t.Fatalf("mkdir block path as dir: %v", err)
	}

	if err := RepairGitignoreBlock(root, RepairGitignoreBlockOptions{System: RealSystem{}}); err == nil {
		t.Fatal("expected write error when block path is a directory")
	}
}
