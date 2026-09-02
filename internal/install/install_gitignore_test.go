package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
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

func TestEnsureGitignoreRejectsCorruptManagedMarkersWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "orphaned start", content: "keep\n# >>> agent-layer\nuser-content\n"},
		{name: "orphaned end", content: "keep\n# <<< agent-layer\nuser-content\n"},
		{name: "inverted", content: "# <<< agent-layer\nuser-content\n# >>> agent-layer\n"},
		{name: "duplicate start", content: "# >>> agent-layer\nold\n# >>> agent-layer\nuser-content\n# <<< agent-layer\n"},
		{name: "duplicate end", content: "# >>> agent-layer\nold\n# <<< agent-layer\nuser-content\n# <<< agent-layer\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".gitignore")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := EnsureGitignore(RealSystem{}, path, "new\n")
			if err == nil || !strings.Contains(err.Error(), "# >>> agent-layer") || !strings.Contains(err.Error(), "# <<< agent-layer") {
				t.Fatalf("corrupt marker error = %v", err)
			}
			data, readErr := os.ReadFile(path) // #nosec G304 -- test-owned path.
			if readErr != nil || string(data) != test.content {
				t.Fatalf("corrupt .gitignore changed: %q, %v", data, readErr)
			}
		})
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

func TestWriteGitignoreBlockMergesTrackingSettingsIntoTemplateUpdate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	customized := customizeGitignoreTracking(string(templateBytes))
	if err := os.WriteFile(path, []byte(customized), 0o600); err != nil {
		t.Fatalf("write customized block: %v", err)
	}

	originalRead := templates.ReadFunc
	templates.ReadFunc = func(templatePath string) ([]byte, error) {
		data, readErr := originalRead(templatePath)
		if readErr != nil || templatePath != "gitignore.block" {
			return data, readErr
		}
		return append(data, []byte("\n/new-generated-output/\n")...), nil
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	approve := func(string) (bool, error) { return true, nil }
	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, approve, nil); err != nil {
		t.Fatalf("writeGitignoreBlock: %v", err)
	}
	updated, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read updated block: %v", err)
	}
	settings, err := ParseGitignoreTrackingSettings(string(updated))
	if err != nil {
		t.Fatalf("parse updated tracking settings: %v", err)
	}
	if !settings.TrackAgentLayerDir || settings.TrackDocsAgentLayerDir {
		t.Fatalf("tracking settings changed: %+v", settings)
	}
	if !strings.Contains(string(updated), "/new-generated-output/") {
		t.Fatalf("template update was not applied:\n%s", updated)
	}
}

func TestWriteGitignoreBlockTrackingOnlyChangeIsNoop(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	customized := customizeGitignoreTracking(string(templateBytes))
	if err := os.WriteFile(path, []byte(customized), 0o600); err != nil {
		t.Fatalf("write customized block: %v", err)
	}

	var recorded []string
	if err := writeGitignoreBlock(RealSystem{}, path, "gitignore.block", 0o644, nil, func(p string) {
		recorded = append(recorded, p)
	}); err != nil {
		t.Fatalf("writeGitignoreBlock error: %v", err)
	}
	if len(recorded) != 0 {
		t.Fatalf("expected no outdated-file preview when merged write is a no-op, got %v", recorded)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read gitignore block: %v", err)
	}
	if string(data) != customized {
		t.Fatalf("customized tracking block was rewritten")
	}
}

func customizeGitignoreTracking(content string) string {
	customized := strings.Replace(content, AgentLayerGitignorePattern, "# "+AgentLayerGitignorePattern, 1)
	return strings.Replace(customized, "# "+DocsAgentLayerGitignorePattern, DocsAgentLayerGitignorePattern, 1)
}

func TestGitignoreTrackingSettings(t *testing.T) {
	t.Run("missing patterns are tracked", func(t *testing.T) {
		settings, err := ParseGitignoreTrackingSettings("# unrelated\n")
		if err != nil {
			t.Fatalf("parse settings: %v", err)
		}
		if !settings.TrackAgentLayerDir || !settings.TrackDocsAgentLayerDir {
			t.Fatalf("missing patterns should be tracked: %+v", settings)
		}
	})

	t.Run("unsupported inline comment is rejected", func(t *testing.T) {
		content := AgentLayerGitignorePattern + " # local reason\n"
		if _, err := ParseGitignoreTrackingSettings(content); err == nil ||
			!strings.Contains(err.Error(), "unsupported inline comment") ||
			!strings.Contains(err.Error(), AgentLayerGitignorePattern) {
			t.Fatalf("expected unsupported inline-comment parse error, got %v", err)
		}
		if _, err := ApplyGitignoreTrackingSettings(content, GitignoreTrackingSettings{}); err == nil ||
			!strings.Contains(err.Error(), "unsupported inline comment") {
			t.Fatalf("expected unsupported inline-comment apply error, got %v", err)
		}
	})

	t.Run("full-line comments with extra text stay tracked", func(t *testing.T) {
		settings, err := ParseGitignoreTrackingSettings("# " + AgentLayerGitignorePattern + " # local reason\n")
		if err != nil {
			t.Fatalf("parse settings: %v", err)
		}
		if !settings.TrackAgentLayerDir {
			t.Fatal("a commented pattern should remain tracked")
		}
	})

	t.Run("duplicate source patterns fail", func(t *testing.T) {
		for _, content := range []string{
			AgentLayerGitignorePattern + "\n# " + AgentLayerGitignorePattern + "\n",
			DocsAgentLayerGitignorePattern + "\n# " + DocsAgentLayerGitignorePattern + "\n",
		} {
			_, err := ParseGitignoreTrackingSettings(content)
			if err == nil {
				t.Fatalf("expected duplicate pattern error for %q", content)
			}
			if !strings.Contains(err.Error(), "managed gitignore block") {
				t.Fatalf("expected managed gitignore block context, got %v", err)
			}
		}
	})

	t.Run("duplicate template patterns fail", func(t *testing.T) {
		settings := GitignoreTrackingSettings{}
		for _, content := range []string{
			AgentLayerGitignorePattern + "\n# " + AgentLayerGitignorePattern + "\n",
			DocsAgentLayerGitignorePattern + "\n# " + DocsAgentLayerGitignorePattern + "\n",
		} {
			_, err := ApplyGitignoreTrackingSettings(content, settings)
			if err == nil {
				t.Fatalf("expected duplicate pattern error for %q", content)
			}
			if !strings.Contains(err.Error(), "managed gitignore block") {
				t.Fatalf("expected managed gitignore block context, got %v", err)
			}
		}
	})

	t.Run("missing ignored patterns are appended", func(t *testing.T) {
		next, err := ApplyGitignoreTrackingSettings("# unrelated\n", GitignoreTrackingSettings{})
		if err != nil {
			t.Fatalf("apply settings: %v", err)
		}
		if !strings.Contains(next, "\n"+AgentLayerGitignorePattern+"\n") ||
			!strings.Contains(next, "\n"+DocsAgentLayerGitignorePattern+"\n") {
			t.Fatalf("ignored patterns were not appended:\n%s", next)
		}
	})
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
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, dir)
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
	testutil.SkipIfWritable(t, root)

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
	testutil.SkipIfWritable(t, root)

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
	testutil.SkipIfWritable(t, root)

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

func TestGitDoesNotTreatHashAfterPatternAsComment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(AgentLayerGitignorePattern+" # local reason\n"), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	probe := filepath.Join(root, ".agent-layer", "probe")
	if err := os.MkdirAll(filepath.Dir(probe), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(probe, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	initCmd := exec.Command("git", "-C", root, "init", "--quiet") // #nosec G204 -- fixed test command and temp path.
	initCmd.Env = gitenv.WithoutDiscovery()
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize gitignore fixture: %v: %s", err, output)
	}
	cmd := exec.Command("git", "-C", root, "check-ignore", "--no-index", "--quiet", "--", ".agent-layer/probe") // #nosec G204 -- fixed test command and test-controlled path.
	cmd.Env = gitenv.WithoutDiscovery()
	err := cmd.Run()
	if err == nil {
		t.Fatal("Git ignored .agent-layer/probe using a # suffix; that suffix is part of the pattern, not a comment")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("check ignore status: %v", err)
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
		"sync.lock",
		"tmp/",
		UpgradeKeepListFileName,
		// The import transaction's scratch space is machine-local even though
		// the imported skills around it are committed.
		"skills-imported/.staging/",
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

	// Exercise Git's actual matching rules so equivalent patterns such as a
	// leading slash, omitted trailing slash, or wildcard suffix cannot bypass an
	// exact-line assertion.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), data, 0o600); err != nil {
		t.Fatalf("write template fixture: %v", err)
	}
	// Resolve the repository from -C above, never from an inherited GIT_DIR: git
	// exports it to hooks, so under pre-commit this fixture would otherwise
	// re-initialize the developer's own checkout.
	initCmd := exec.Command("git", "-C", root, "init", "--quiet") // #nosec G204 -- fixed test command and temp path.
	initCmd.Env = gitenv.WithoutDiscovery()
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize template fixture: %v: %s", err, output)
	}
	isIgnored := func(path string) bool {
		t.Helper()
		cmd := exec.Command("git", "-C", root, "check-ignore", "--no-index", "--quiet", "--", path) // #nosec G204 -- fixed test command and test-controlled path.
		cmd.Env = gitenv.WithoutDiscovery()
		err := cmd.Run()
		if err == nil {
			return true
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false
		}
		t.Fatalf("check ignore status for %q: %v", path, err)
		return false
	}
	for _, path := range []string{"skills.lock.json", "skills-imported/example/SKILL.md"} {
		if isIgnored(path) {
			t.Errorf("agent-layer.gitignore template must not ignore %q", path)
		}
	}
	if !isIgnored("skills-imported/.staging/transaction") {
		t.Error("agent-layer.gitignore template must ignore imported-skill staging content")
	}
	if !isIgnored(UpgradeKeepListFileName) {
		t.Error("agent-layer.gitignore template must ignore the repo-local upgrade keep list")
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
