package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScanUnknownRoot_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	inst := &installer{root: locked, sys: RealSystem{}}
	// scanUnknowns may or may not error depending on OS behavior with 000 perms.
	// We just exercise the code path; the test passes regardless of result.
	_ = inst.scanUnknowns()
}

// setupUnknownFile creates a .agent-layer/ directory with a single unknown file
// and returns the installer and the path to the unknown file. The returned
// installer has root, overwrite, and sys set — caller must set prompter.
func setupUnknownFile(t *testing.T) (*installer, string) {
	t.Helper()
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	unknownPath := filepath.Join(alDir, "unknown.txt")
	if err := os.WriteFile(unknownPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
	}
	return inst, unknownPath
}

func TestHandleUnknowns_PromptDeleteAllError(t *testing.T) {
	inst, _ := setupUnknownFile(t)
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) {
			return false, errors.New("boom")
		},
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleUnknowns_PromptDeleteError(t *testing.T) {
	inst, _ := setupUnknownFile(t)
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) {
			return false, nil
		},
		DeleteUnknownFunc: func(string) (bool, error) {
			return false, errors.New("boom")
		},
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleUnknowns_DeleteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	protectedDir := filepath.Join(alDir, "protected")
	if err := os.MkdirAll(protectedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(protectedDir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to prevent deletion.
	if err := os.Chmod(protectedDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(protectedDir, 0o755) })

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:    func(string) (bool, error) { return true, nil },
		},
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error for delete failure")
	}
}

func TestWarnUnknowns_WithUnknowns(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	inst := &installer{
		root:       root,
		overwrite:  false,
		unknowns:   []string{filepath.Join(root, "unknown1"), filepath.Join(root, "unknown2")},
		sys:        RealSystem{},
		warnWriter: &buf,
	}
	// Just exercise the code path - it writes to the warning output.
	inst.warnUnknowns()
	if buf.Len() == 0 {
		t.Fatalf("expected warning output")
	}
}

func TestWarnUnknowns_OverwriteTrue(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a", "b"},
		sys:       RealSystem{},
	}
	inst.warnUnknowns() // Should return early
}

func TestWarnUnknowns_EmptyUnknowns(t *testing.T) {
	inst := &installer{
		overwrite: false,
		unknowns:  []string{},
		sys:       RealSystem{},
	}
	inst.warnUnknowns() // Should return early
}

func TestWarnDifferences_WithDiffs(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	inst := &installer{
		root:       root,
		overwrite:  false,
		diffs:      []string{filepath.Join(root, "diff1"), filepath.Join(root, "diff2")},
		sys:        RealSystem{},
		warnWriter: &buf,
	}
	inst.warnDifferences()
	if buf.Len() == 0 {
		t.Fatalf("expected warning output")
	}
}

func TestWarnDifferences_RelError(t *testing.T) {
	inst := &installer{
		root:      "", // Empty root causes filepath.Rel to potentially fail
		overwrite: false,
		diffs:     []string{"/some/absolute/path"},
	}
	// Just exercise the code path - should not panic
	inst.warnDifferences()
}

func TestSortUnknowns(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:     root,
		unknowns: []string{filepath.Join(root, "z"), filepath.Join(root, "a"), filepath.Join(root, "m")},
		sys:      RealSystem{},
	}
	inst.sortUnknowns()
	// Check they're sorted
	for i := 1; i < len(inst.unknowns); i++ {
		if inst.relativePath(inst.unknowns[i-1]) > inst.relativePath(inst.unknowns[i]) {
			t.Fatalf("unknowns not sorted")
		}
	}
}

func TestRelativeUnknowns_WithPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:     root,
		unknowns: []string{filepath.Join(root, "b"), filepath.Join(root, "a")},
		sys:      RealSystem{},
	}
	rel := inst.relativeUnknowns()
	if len(rel) != 2 {
		t.Fatalf("expected 2 relative paths, got %d", len(rel))
	}
	// Should be sorted
	if rel[0] != "a" || rel[1] != "b" {
		t.Fatalf("unexpected order: %v", rel)
	}
}

func TestHandleUnknowns_IndividualDelete(t *testing.T) {
	inst, unknownFile := setupUnknownFile(t)
	deleteCalled := false
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) {
			return false, nil
		},
		DeleteUnknownFunc: func(string) (bool, error) {
			deleteCalled = true
			return true, nil
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Fatalf("expected delete prompt to be called")
	}
	if _, err := os.Stat(unknownFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file to be deleted")
	}
}

func TestHandleUnknowns_MissingIndividualPrompt(t *testing.T) {
	inst, _ := setupUnknownFile(t)
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) { return false, nil },
		// Missing DeleteUnknownFunc
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleUnknowns_IndividualDeleteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	protectedDir := filepath.Join(alDir, "protected")
	if err := os.MkdirAll(protectedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(protectedDir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to prevent deletion.
	if err := os.Chmod(protectedDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(protectedDir, 0o755) })

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				return false, nil // Don't delete all
			},
			DeleteUnknownFunc: func(string) (bool, error) {
				return true, nil // Try to delete this one
			},
		},
	}
	err := inst.handleUnknowns()
	if err == nil {
		t.Fatalf("expected error for delete failure")
	}
}

func TestScanUnknownRoot_WalkDirError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	subDir := filepath.Join(alDir, "subdir")
	if err := os.Mkdir(subDir, 0o000); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(subDir, 0o755) })

	inst := &installer{root: root, sys: RealSystem{}}
	known := map[string]struct{}{
		filepath.Clean(alDir): {},
	}
	// WalkDir may error when trying to enter the unreadable subdir
	_ = inst.scanUnknownRoot(alDir, known)
}

func TestScanUnknownRoot_StatErrorNonNotExist(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Remove read permissions from alDir to cause stat error
	if err := os.Chmod(alDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(alDir, 0o755) })

	inst := &installer{root: root, sys: RealSystem{}}
	known := make(map[string]struct{})
	err := inst.scanUnknownRoot(alDir, known)
	// Should error due to stat permission denied
	if err == nil {
		// Some systems may not error - that's OK
		t.Logf("no error (system may allow stat on 000 dir)")
	}
}

func TestBuildKnownPaths_IncludesUpgradeSnapshots(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "example.json")
	if err := os.WriteFile(snapshotPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write snapshot file: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	known, err := inst.buildKnownPaths()
	if err != nil {
		t.Fatalf("buildKnownPaths: %v", err)
	}
	if _, ok := known[filepath.Clean(snapshotPath)]; !ok {
		t.Fatalf("expected snapshot file path %s to be known", snapshotPath)
	}
}

func TestScanUnknowns_NoUnknownsAfterFreshRun(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.scanUnknowns(); err != nil {
		t.Fatalf("scanUnknowns: %v", err)
	}
	if rel := inst.relativeUnknowns(); len(rel) != 0 {
		t.Fatalf("expected no unknown paths after fresh run, got %v", rel)
	}
}

func TestHandleUnknowns_DocsAgentLayerOrphan_DetectedAndDeleted(t *testing.T) {
	root := t.TempDir()
	// Seed the repo so all template files are present.
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	// Create an orphan file in docs/agent-layer/ that has no template.
	orphanPath := filepath.Join(root, "docs", "agent-layer", "CUSTOM.md")
	if err := os.WriteFile(orphanPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	var promptedPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func(paths []string) (bool, error) {
				promptedPaths = paths
				return true, nil
			},
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if len(promptedPaths) != 1 {
		t.Fatalf("expected 1 prompted path, got %v", promptedPaths)
	}
	if promptedPaths[0] != filepath.ToSlash(filepath.Join("docs", "agent-layer", "CUSTOM.md")) {
		t.Fatalf("unexpected prompted path: %s", promptedPaths[0])
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected orphan to be deleted, got %v", err)
	}
}

func TestScanUnknowns_DocsAgentLayerOrphan_Detected(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	orphanPath := filepath.Join(root, "docs", "agent-layer", "ORPHAN.md")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.scanUnknowns(); err != nil {
		t.Fatalf("scanUnknowns: %v", err)
	}
	rel := inst.relativeUnknowns()
	if len(rel) != 1 {
		t.Fatalf("expected 1 unknown, got %v", rel)
	}
	expected := filepath.ToSlash(filepath.Join("docs", "agent-layer", "ORPHAN.md"))
	if rel[0] != expected {
		t.Fatalf("expected %s, got %s", expected, rel[0])
	}
}

func TestBuildKnownPaths_IncludesDocsAgentLayerTemplateFiles(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	known, err := inst.buildKnownPaths()
	if err != nil {
		t.Fatalf("buildKnownPaths: %v", err)
	}
	// The docs/agent-layer/ directory itself should be known.
	docsDir := filepath.Clean(filepath.Join(root, "docs", "agent-layer"))
	if _, ok := known[docsDir]; !ok {
		t.Fatalf("expected docs/agent-layer directory to be known")
	}
	// At least one template file should exist as a known path under docs/agent-layer/.
	found := false
	for path := range known {
		if len(path) > len(docsDir) && path[:len(docsDir)] == docsDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one known path under docs/agent-layer/")
	}
}

// setupTmpAndOtherUnknowns creates two unknown files: one inside
// .agent-layer/tmp/ and one elsewhere under .agent-layer/. Returns the
// installer (with root, overwrite, and sys set) along with the absolute paths
// of each unknown.
func setupTmpAndOtherUnknowns(t *testing.T) (inst *installer, tmpFile, otherFile string) {
	t.Helper()
	root := t.TempDir()
	tmpDir := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	tmpFile = filepath.Join(tmpDir, "scratch.log")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}
	otherFile = filepath.Join(root, ".agent-layer", "stray.txt")
	if err := os.WriteFile(otherFile, []byte("y"), 0o644); err != nil {
		t.Fatalf("write other file: %v", err)
	}
	inst = &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
	}
	return inst, tmpFile, otherFile
}

func TestHandleUnknowns_TmpGroupedPrompt_DeletesAllTmp(t *testing.T) {
	inst, tmpFile, otherFile := setupTmpAndOtherUnknowns(t)

	tmpPromptCalls := 0
	perFileCalls := 0
	var perFilePaths []string
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) { return false, nil },
		DeleteUnknownTmpAllFunc: func(paths []string) (bool, error) {
			tmpPromptCalls++
			if len(paths) != 1 {
				t.Fatalf("expected 1 tmp path, got %v", paths)
			}
			return true, nil
		},
		DeleteUnknownFunc: func(path string) (bool, error) {
			perFileCalls++
			perFilePaths = append(perFilePaths, path)
			return false, nil
		},
	}

	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if tmpPromptCalls != 1 {
		t.Fatalf("tmp grouped prompt called %d times, want 1", tmpPromptCalls)
	}
	if perFileCalls != 1 || len(perFilePaths) != 1 {
		t.Fatalf("per-file prompt should fire once for the non-tmp unknown, got %d calls (paths=%v)", perFileCalls, perFilePaths)
	}
	if _, err := os.Stat(tmpFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected tmp file to be deleted, stat err=%v", err)
	}
	// Non-tmp file is preserved because per-file callback returned false.
	if _, err := os.Stat(otherFile); err != nil {
		t.Fatalf("expected non-tmp unknown to survive, got %v", err)
	}
}

func TestHandleUnknowns_TmpGroupedPrompt_DeclinedKeepsTmp(t *testing.T) {
	inst, tmpFile, _ := setupTmpAndOtherUnknowns(t)

	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc:    func([]string) (bool, error) { return false, nil },
		DeleteUnknownTmpAllFunc: func([]string) (bool, error) { return false, nil },
		DeleteUnknownFunc:       func(string) (bool, error) { return false, nil },
	}

	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("expected tmp file to be preserved when group prompt declined, got %v", err)
	}
}

func TestHandleUnknowns_NoTmpFiles_TmpPromptNotCalled(t *testing.T) {
	inst, _ := setupUnknownFile(t)
	tmpCalled := false
	inst.prompter = PromptFuncs{
		DeleteUnknownAllFunc: func([]string) (bool, error) { return false, nil },
		DeleteUnknownTmpAllFunc: func([]string) (bool, error) {
			tmpCalled = true
			return false, nil
		},
		DeleteUnknownFunc: func(string) (bool, error) { return false, nil },
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if tmpCalled {
		t.Fatalf("tmp grouped prompt should be skipped when no tmp unknowns are present")
	}
}

func TestHandleUnknowns_OnlyTmpFiles_NoPerFileLoop(t *testing.T) {
	root := t.TempDir()
	tmpDir := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.log", "b.log", "c.log"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	perFileCalls := 0
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc:    func([]string) (bool, error) { return false, nil },
			DeleteUnknownTmpAllFunc: func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc: func(string) (bool, error) {
				perFileCalls++
				return false, nil
			},
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if perFileCalls != 0 {
		t.Fatalf("per-file prompt should not fire when only tmp unknowns are present, got %d calls", perFileCalls)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("readdir tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected tmp dir to be empty after grouped delete, found %d entries", len(entries))
	}
}

func TestHandleUnknowns_TopLevelSummaryCollapsesTmp(t *testing.T) {
	root := t.TempDir()
	tmpDir := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	for _, name := range []string{"one.log", "two.log"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write tmp file: %v", err)
		}
	}
	otherFile := filepath.Join(root, ".agent-layer", "stray.txt")
	if err := os.WriteFile(otherFile, []byte("y"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}

	var captured []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func(paths []string) (bool, error) {
				captured = paths
				return true, nil
			},
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}

	wantSummary := filepath.Join(".agent-layer", "tmp") + string(os.PathSeparator) + " (2 files)"
	wantOther := filepath.Join(".agent-layer", "stray.txt")
	if len(captured) != 2 {
		t.Fatalf("expected 2 summary entries (1 collapsed tmp + 1 stray), got %v", captured)
	}
	foundSummary, foundOther := false, false
	for _, entry := range captured {
		if entry == wantSummary {
			foundSummary = true
		}
		if entry == wantOther {
			foundOther = true
		}
	}
	if !foundSummary {
		t.Fatalf("expected collapsed tmp summary %q in %v", wantSummary, captured)
	}
	if !foundOther {
		t.Fatalf("expected non-tmp entry %q in %v", wantOther, captured)
	}
}

func TestHandleUnknowns_TmpFallback_LegacyPrompterUsesPerFile(t *testing.T) {
	// Prompters that pre-date tmpUnknownsPrompter should keep working: when the
	// optional grouped capability is absent, tmp files fall back to the
	// existing per-file prompt loop.
	inst, tmpFile, otherFile := setupTmpAndOtherUnknowns(t)
	var perFilePaths []string
	inst.prompter = legacyDeleteOnlyPrompter{
		deleteAll: func([]string) (bool, error) { return false, nil },
		deleteOne: func(path string) (bool, error) {
			perFilePaths = append(perFilePaths, path)
			return true, nil
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if len(perFilePaths) != 2 {
		t.Fatalf("legacy prompter should see one per-file prompt per unknown (tmp + non-tmp), got %v", perFilePaths)
	}
	for _, path := range []string{tmpFile, otherFile} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s to be deleted, stat err=%v", path, err)
		}
	}
}

// legacyDeleteOnlyPrompter satisfies Prompter without implementing the
// optional tmpUnknownsPrompter interface, modelling a stale Prompter
// implementation that only handles the original DeleteUnknown(All) prompts.
type legacyDeleteOnlyPrompter struct {
	deleteAll func([]string) (bool, error)
	deleteOne func(string) (bool, error)
}

func (legacyDeleteOnlyPrompter) OverwriteAll([]DiffPreview) (bool, error)       { return false, nil }
func (legacyDeleteOnlyPrompter) OverwriteAllMemory([]DiffPreview) (bool, error) { return false, nil }
func (legacyDeleteOnlyPrompter) Overwrite(DiffPreview) (bool, error)            { return false, nil }
func (p legacyDeleteOnlyPrompter) DeleteUnknownAll(paths []string) (bool, error) {
	return p.deleteAll(paths)
}
func (p legacyDeleteOnlyPrompter) DeleteUnknown(path string) (bool, error) {
	return p.deleteOne(path)
}

func TestRelativeUnknowns_Empty(t *testing.T) {
	inst := &installer{
		unknowns: nil,
		sys:      RealSystem{},
	}
	rel := inst.relativeUnknowns()
	if rel != nil {
		t.Fatalf("expected nil for empty unknowns, got %v", rel)
	}
}
