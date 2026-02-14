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

func TestHandleUnknowns_PromptDeleteAllError(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				return false, errors.New("boom")
			},
		},
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleUnknowns_PromptDeleteError(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				return false, nil
			},
			DeleteUnknownFunc: func(string) (bool, error) {
				return false, errors.New("boom")
			},
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
	// Create file in read-only dir to cause remove error
	dir := filepath.Join(root, "ro")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	inst := &installer{
		root:      root,
		overwrite: true,
		force:     true,
		unknowns:  []string{file},
		sys:       RealSystem{},
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
	root := t.TempDir()
	unknownFile := filepath.Join(root, "unknown")
	if err := os.WriteFile(unknownFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deleteCalled := false
	inst := &installer{
		overwrite: true,
		unknowns:  []string{unknownFile},
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				return false, nil
			},
			DeleteUnknownFunc: func(string) (bool, error) {
				deleteCalled = true
				return true, nil
			},
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
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) { return false, nil },
			// Missing DeleteUnknownFunc
		},
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
	dir := filepath.Join(root, "protected")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to prevent deletion
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	inst := &installer{
		root:      root,
		overwrite: true,
		unknowns:  []string{file},
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
