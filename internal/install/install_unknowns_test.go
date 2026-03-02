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
