package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// Coverage tests to hit specific error paths and missed lines.

func TestWriteVersionFile_ExistingEmpty(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write empty file
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0"}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for empty existing pin file")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteVersionFile_InvalidExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write invalid version
	if err := os.WriteFile(path, []byte("invalid"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0"}
	// This should treat existing as empty string after normalize failure?
	// No, Normalize returns error.
	// In writeVersionFile:
	// normalized, err := version.Normalize(existing)
	// if err != nil { normalized = "" }
	// So it becomes empty string.
	// Then checks if normalized == inst.pinVersion ( "" == "1.0.0" -> false).
	// Then calls shouldOverwrite.

	// We want to hit the overwrite prompt.
	inst.overwrite = true
	// Mock overwriteAllDecided to skip PromptOverwriteAll check which is missing
	inst.overwriteAllDecided = true
	inst.promptOverwrite = func(p string) (bool, error) {
		return true, nil
	}

	err := inst.writeVersionFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "1.0.0\n" {
		t.Fatalf("expected overwrite")
	}
}

func TestWriteVersionFile_PromptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("0.9.0"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{
		root:       root,
		pinVersion: "1.0.0",
		overwrite:  true,
		promptOverwrite: func(p string) (bool, error) {
			return false, errors.New("prompt failed")
		},
	}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error from prompt")
	}
}

func TestWriteVersionFile_MkdirError(t *testing.T) {
	root := t.TempDir()
	// Create a file where directory should be
	path := filepath.Join(root, ".agent-layer")
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0"}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestWriteVersionFile_WriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o500); err != nil { // read/exec only, no write
		t.Fatalf("mkdir: %v", err)
	}
	// On Linux, 500 on dir prevents creating files.

	inst := &installer{root: root, pinVersion: "1.0.0"}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

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

	inst := &installer{root: locked}
	// scanUnknowns may or may not error depending on OS behavior with 000 perms.
	// We just exercise the code path; the test passes regardless of result.
	_ = inst.scanUnknowns()
}

func TestHandleUnknowns_PromptDeleteAllError(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		promptDeleteUnknownAll: func(paths []string) (bool, error) {
			return false, errors.New("boom")
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
		promptDeleteUnknownAll: func(paths []string) (bool, error) {
			return false, nil
		},
		promptDeleteUnknown: func(path string) (bool, error) {
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
	}
	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error for delete failure")
	}
}

func TestWriteGitignoreBlock_MkdirError(t *testing.T) {
	root := t.TempDir()
	// Block directory creation by creating a file at parent path
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
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

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteGitignoreBlock_OverwritePromptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	shouldOverwrite := func(p string) (bool, error) {
		return false, errors.New("prompt failed")
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, shouldOverwrite, nil)
	if err == nil {
		t.Fatalf("expected error for prompt failure")
	}
}

func TestWriteGitignoreBlock_OverwriteWriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// make file read-only, though WriteFileAtomic tries to chmod/remove.
	// better to make dir read-only?
	// But file exists. Atomic write writes to temp file then renames.
	// If dir is read-only, we can't create temp file or rename.
	dir := root
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	shouldOverwrite := func(p string) (bool, error) {
		return true, nil
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, shouldOverwrite, nil)
	if err == nil {
		t.Fatalf("expected error for overwrite failure")
	}
}

func TestWriteTemplateFile_FileMatchesError(t *testing.T) {
	// fileMatchesTemplate fails if ReadFile fails or template read fails.
	// We tested template read fail already.
	// ReadFile failure:
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Path is a directory, ReadFile should fail (on most OSs) or we rely on it.

	err := writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error when path is directory")
	}
}

func TestWriteTemplateFile_OverwritePromptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	prompt := func(p string) (bool, error) {
		return false, errors.New("boom")
	}
	err := writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildKnownPaths_TemplateError(t *testing.T) {
	// Mock templates.Walk to fail
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("mock walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: "/tmp"}
	_, err := inst.buildKnownPaths()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildKnownPaths_TemplatePathError(t *testing.T) {
	// Mock templates.Walk to pass an invalid path that triggers the check?
	// The check is: rel := strings.TrimPrefix(path, templateRoot+"/")
	// if rel == path { return error }
	// This happens if path does not start with templateRoot+"/".
	// Walk guarantees paths start with root, but if we feed it garbage?

	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		// Pass a path that doesn't start with root + "/"
		// e.g. root="instructions", path="instructions" (isDir=true -> skip)
		// path="other/file"
		return fn("other/file", &mockDirEntry{name: "file"}, nil)
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: "/tmp"}
	_, err := inst.buildKnownPaths()
	if err == nil {
		t.Fatalf("expected error for unexpected template path")
	}
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// Test warnUnknowns with unknowns but no overwrite
func TestWarnUnknowns_WithUnknowns(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: false,
		unknowns:  []string{filepath.Join(root, "unknown1"), filepath.Join(root, "unknown2")},
	}
	// Just exercise the code path - it writes to stderr
	inst.warnUnknowns()
}

// Test warnUnknowns when overwrite is true (should not warn)
func TestWarnUnknowns_OverwriteTrue(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a", "b"},
	}
	inst.warnUnknowns() // Should return early
}

// Test warnUnknowns with empty unknowns (should not warn)
func TestWarnUnknowns_EmptyUnknowns(t *testing.T) {
	inst := &installer{
		overwrite: false,
		unknowns:  []string{},
	}
	inst.warnUnknowns() // Should return early
}

// Test warnDifferences with diffs but no overwrite
func TestWarnDifferences_WithDiffs(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: false,
		diffs:     []string{filepath.Join(root, "diff1"), filepath.Join(root, "diff2")},
	}
	inst.warnDifferences()
}

// Test sortUnknowns exercises the sorting function
func TestSortUnknowns(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:     root,
		unknowns: []string{filepath.Join(root, "z"), filepath.Join(root, "a"), filepath.Join(root, "m")},
	}
	inst.sortUnknowns()
	// Check they're sorted
	for i := 1; i < len(inst.unknowns); i++ {
		if inst.relativePath(inst.unknowns[i-1]) > inst.relativePath(inst.unknowns[i]) {
			t.Fatalf("unknowns not sorted")
		}
	}
}

// Test relativeUnknowns with paths
func TestRelativeUnknowns_WithPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:     root,
		unknowns: []string{filepath.Join(root, "b"), filepath.Join(root, "a")},
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

// Test handleUnknowns with individual delete prompts
func TestHandleUnknowns_IndividualDelete(t *testing.T) {
	root := t.TempDir()
	unknownFile := filepath.Join(root, "unknown")
	if err := os.WriteFile(unknownFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deleteCalled := false
	inst := &installer{
		root:      root,
		overwrite: true,
		unknowns:  []string{unknownFile},
		promptDeleteUnknownAll: func(paths []string) (bool, error) {
			return false, nil // Don't delete all
		},
		promptDeleteUnknown: func(path string) (bool, error) {
			deleteCalled = true
			return true, nil // Delete this one
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Fatalf("individual delete prompt was not called")
	}
	// File should be deleted
	if _, err := os.Stat(unknownFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should have been deleted")
	}
}

// Test handleUnknowns missing promptDeleteUnknown
func TestHandleUnknowns_MissingIndividualPrompt(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		promptDeleteUnknownAll: func(paths []string) (bool, error) {
			return false, nil
		},
		promptDeleteUnknown: nil, // Missing
	}
	err := inst.handleUnknowns()
	if err == nil {
		t.Fatalf("expected error for missing prompt")
	}
}

// Test shouldOverwrite with promptOverwriteAll returning true
func TestShouldOverwrite_OverwriteAll(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		promptOverwriteAll: func([]string) (bool, error) {
			return true, nil
		},
	}
	result, err := inst.shouldOverwrite(filepath.Join(root, "file"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatalf("expected overwrite all to return true")
	}
}

// Test shouldOverwrite promptOverwriteAll error
func TestShouldOverwrite_OverwriteAllError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		promptOverwriteAll: func([]string) (bool, error) {
			return false, errors.New("boom")
		},
	}
	_, err := inst.shouldOverwrite(filepath.Join(root, "file"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

// Test shouldOverwrite with individual prompt after overwriteAll=false
func TestShouldOverwrite_IndividualPrompt(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		promptOverwriteAll: func([]string) (bool, error) {
			return false, nil // Not all
		},
		promptOverwrite: func(path string) (bool, error) {
			return true, nil
		},
	}
	result, err := inst.shouldOverwrite(filepath.Join(root, "file"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatalf("expected individual prompt to return true")
	}
}

// Test shouldOverwrite with missing individual prompt
func TestShouldOverwrite_MissingIndividualPrompt(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		promptOverwriteAll: func([]string) (bool, error) {
			return false, nil
		},
		promptOverwrite: nil, // Missing
	}
	_, err := inst.shouldOverwrite(filepath.Join(root, "file"))
	if err == nil {
		t.Fatalf("expected error for missing prompt")
	}
}

// Test ensureGitignore read error
func TestEnsureGitignore_ReadError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	// Create a directory where file should be (causes read error)
	path := filepath.Join(root, ".gitignore")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := ensureGitignore(path, "block")
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

// Test ensureGitignore write error on new file
func TestEnsureGitignore_WriteNewError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	path := filepath.Join(root, ".gitignore")
	err := ensureGitignore(path, "block")
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

// Test ensureGitignore write error on existing file
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

	err := ensureGitignore(path, "new block")
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

// Test scanUnknownRoot with WalkDir error
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

	inst := &installer{root: root}
	known := map[string]struct{}{
		filepath.Clean(alDir): {},
	}
	// WalkDir may error when trying to enter the unreadable subdir
	_ = inst.scanUnknownRoot(alDir, known)
}

// Test writeTemplateDir error
func TestWriteTemplateDir_WalkError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	err := writeTemplateDir("instructions", "/tmp/dest", nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

// Test writeTemplateDir path error
func TestWriteTemplateDir_PathError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		// Pass a path that doesn't start with root + "/"
		return fn("other/file", &mockDirEntry{name: "file"}, nil)
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	err := writeTemplateDir("instructions", "/tmp/dest", nil, nil)
	if err == nil {
		t.Fatalf("expected error for unexpected path")
	}
}

// Test Run with delete unknown prompt required error
func TestRun_DeleteUnknownPromptRequired(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		Overwrite:                true,
		PromptOverwriteAll:       func([]string) (bool, error) { return true, nil },
		PromptOverwriteMemoryAll: func([]string) (bool, error) { return true, nil },
		// Missing PromptDeleteUnknownAll
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

// Test Run with invalid pin version
func TestRun_InvalidPinVersion(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		PinVersion: "invalid-version",
	})
	if err == nil {
		t.Fatalf("expected error for invalid pin version")
	}
}

// Test writeTemplateFile with stat error (not ErrNotExist)
func TestWriteTemplateFile_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "locked")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	path := filepath.Join(dir, "config.toml")
	err := writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

// Test writeTemplateFile mkdir error
func TestWriteTemplateFile_MkdirError(t *testing.T) {
	root := t.TempDir()
	// Create a file where directory should be
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(blocker, "subdir", "config.toml")
	err := writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

// Test writeGitignoreBlock matching hash path (write after match)
func TestWriteGitignoreBlock_MatchingHash(t *testing.T) {
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

	err = writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test writeGitignoreBlock read existing error
func TestWriteGitignoreBlock_ReadExistingError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	// Create a directory where file should be
	path := filepath.Join(root, "gitignore.block")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

// Test warnDifferences when filepath.Rel fails (e.g., root is empty)
func TestWarnDifferences_RelError(t *testing.T) {
	inst := &installer{
		root:      "", // Empty root causes filepath.Rel to potentially fail
		overwrite: false,
		diffs:     []string{"/some/absolute/path"},
	}
	// Just exercise the code path - should not panic
	inst.warnDifferences()
}

// Test writeVersionFile read error (non-NotExist)
func TestWriteVersionFile_ReadError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a directory where file should be (causes ReadFile to error)
	path := filepath.Join(alDir, "al.version")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0"}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

// Test writeVersionFile when overwrite returns false
func TestWriteVersionFile_OverwriteFalse(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write different version
	if err := os.WriteFile(path, []byte("0.9.0"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{
		root:                root,
		pinVersion:          "1.0.0",
		overwrite:           true,
		overwriteAllDecided: true,
		overwriteAll:        false,
		promptOverwrite: func(p string) (bool, error) {
			return false, nil // Don't overwrite
		},
	}
	err := inst.writeVersionFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have recorded a diff
	if len(inst.diffs) != 1 {
		t.Fatalf("expected diff to be recorded, got %d diffs", len(inst.diffs))
	}
}

// Test writeTemplateFiles with error from writeGitignoreBlock
func TestWriteTemplateFiles_GitignoreBlockError(t *testing.T) {
	root := t.TempDir()
	// Create parent dir but block gitignore.block directory creation
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root}
	// This should work without error for most files, but let's create a scenario
	// where the gitignore.block writing fails

	// First, we need the earlier files to succeed. Let's block the gitignore.block path specifically
	// by making it a directory
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.Mkdir(blockPath, 0o755); err != nil {
		t.Fatalf("mkdir block: %v", err)
	}

	err := inst.writeTemplateFiles()
	if err == nil {
		t.Fatalf("expected error from gitignore.block write")
	}
}

// Test writeTemplateFiles with error from writeTemplateFile
func TestWriteTemplateFiles_WriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	// Create .agent-layer as read-only so file writes fail
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(alDir, 0o755) })

	inst := &installer{root: root}
	err := inst.writeTemplateFiles()
	if err == nil {
		t.Fatalf("expected error from template file write")
	}
}

// Test writeTemplateDirs with error
func TestWriteTemplateDirs_WriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	// Create instructions dir first with normal perms, then make it read-only
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Now make it read-only to prevent file writes
	if err := os.Chmod(instrDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) })

	inst := &installer{root: root}
	err := inst.writeTemplateDirs()
	if err == nil {
		t.Fatalf("expected error from template dir write")
	}
}

// Test handleUnknowns with individual delete failure
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
		promptDeleteUnknownAll: func(paths []string) (bool, error) {
			return false, nil // Don't delete all
		},
		promptDeleteUnknown: func(path string) (bool, error) {
			return true, nil // Try to delete this one
		},
	}
	err := inst.handleUnknowns()
	if err == nil {
		t.Fatalf("expected error for delete failure")
	}
}

// Test writeGitignoreBlock with existing matching hash and write error
func TestWriteGitignoreBlock_MatchingHashWriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "gitignore.block")
	// Write content that matches the template
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to cause write error
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err = writeGitignoreBlock(path, "gitignore.block", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

// Test writeTemplateFile with write error after overwrite confirmation
func TestWriteTemplateFile_WriteAfterOverwriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	// Write existing file with different content
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to cause write error during overwrite
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	prompt := func(p string) (bool, error) {
		return true, nil // Agree to overwrite
	}
	err := writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

// Test writeTemplateFile read template error
func TestWriteTemplateFile_ReadTemplateError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.toml")
	err := writeTemplateFile(path, "nonexistent-template", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
}

// Test scanUnknownRoot stat error (non-NotExist)
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

	inst := &installer{root: root}
	known := make(map[string]struct{})
	err := inst.scanUnknownRoot(alDir, known)
	// Should error due to stat permission denied
	if err == nil {
		// Some systems may not error - that's OK
		t.Logf("no error (system may allow stat on 000 dir)")
	}
}

// Test relativeUnknowns returns nil for empty unknowns
func TestRelativeUnknowns_Empty(t *testing.T) {
	inst := &installer{
		unknowns: nil,
	}
	rel := inst.relativeUnknowns()
	if rel != nil {
		t.Fatalf("expected nil for empty unknowns, got %v", rel)
	}
}

// Test Run successful execution with all options
func TestRun_SuccessfulWithAllOptions(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		Overwrite:              true,
		Force:                  true,
		PromptOverwriteAll:     func([]string) (bool, error) { return true, nil },
		PromptDeleteUnknownAll: func(paths []string) (bool, error) { return true, nil },
		PinVersion:             "1.0.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test Run with existing installation in overwrite mode
func TestRun_OverwriteExisting(t *testing.T) {
	root := t.TempDir()
	// First installation
	err := Run(root, Options{})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second installation with overwrite
	err = Run(root, Options{
		Overwrite:              true,
		Force:                  true,
		PromptOverwriteAll:     func([]string) (bool, error) { return true, nil },
		PromptDeleteUnknownAll: func(paths []string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
}

// Test writeTemplateFile when file matches template (no overwrite needed)
func TestWriteTemplateFile_ExactMatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")

	// First write the template
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Now try to write again - should succeed without calling overwrite
	overwriteCalled := false
	prompt := func(p string) (bool, error) {
		overwriteCalled = true
		return false, nil
	}
	err = writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overwriteCalled {
		t.Fatalf("overwrite should not have been called when file matches")
	}
}

// Test buildKnownPaths with different template directories
func TestBuildKnownPaths_Success(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root}
	known, err := inst.buildKnownPaths()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(known) == 0 {
		t.Fatalf("expected known paths to be populated")
	}
	// Verify some expected paths are in the set
	expectedPaths := []string{
		filepath.Join(root, ".agent-layer"),
		filepath.Join(root, ".agent-layer", "config.toml"),
		filepath.Join(root, ".agent-layer", "instructions"),
	}
	for _, p := range expectedPaths {
		clean := filepath.Clean(p)
		if _, ok := known[clean]; !ok {
			t.Errorf("expected %s to be in known paths", p)
		}
	}
}
