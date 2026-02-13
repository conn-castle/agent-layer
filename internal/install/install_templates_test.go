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

func TestWriteTemplateIfMissingExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := writeTemplateIfMissing(RealSystem{}, path, "config.toml", 0o644); err != nil {
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
	err := writeTemplateDir(RealSystem{}, "missing-root", root, nil, nil)
	if err == nil {
		t.Fatalf("expected error for missing template root")
	}
}

func TestWriteTemplateIfMissingInvalidTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	err := writeTemplateIfMissing(RealSystem{}, path, "missing-template", 0o644)
	if err == nil {
		t.Fatalf("expected error for missing template")
	}
}

func TestWriteSectionAwareTemplateFile_CreatesMissingFile(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root: root,
		sys:  RealSystem{},
	}
	relPath := filepath.ToSlash(filepath.Join("docs", "agent-layer", "ISSUES.md"))
	destPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := inst.writeSectionAwareTemplateFile(destPath, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart); err != nil {
		t.Fatalf("writeSectionAwareTemplateFile: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read written section-aware file: %v", err)
	}
	if !strings.Contains(string(content), ownershipMarkerEntriesStart) {
		t.Fatalf("expected marker %q in written file", ownershipMarkerEntriesStart)
	}
}

func TestWriteTemplateIfMissingStatError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	path := filepath.Join(file, "config.toml")
	if err := writeTemplateIfMissing(RealSystem{}, path, "config.toml", 0o644); err == nil {
		t.Fatalf("expected error for stat failure")
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

	inst := &installer{sys: RealSystem{}}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if _, err := inst.matchTemplate(RealSystem{}, path, "config.toml", info); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(string) ([]byte, error) {
		return nil, errors.New("unexpected template read")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	if err := writeTemplateFileWithMatch(RealSystem{}, path, "config.toml", 0o644, nil, nil, inst.matchTemplate); err != nil {
		t.Fatalf("expected cached match to skip template read: %v", err)
	}
}

func TestWriteTemplateDirWalkError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("mock walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	root := t.TempDir()
	err := writeTemplateDir(RealSystem{}, "instructions", root, nil, nil)
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

	_, err := fileMatchesTemplate(RealSystem{}, path, "config.toml")
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteTemplateFile_FileMatchesError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	matchTemplate := func(sys System, path string, templatePath string, info fs.FileInfo) (bool, error) {
		return false, errors.New("match error")
	}
	err := writeTemplateFileWithMatch(RealSystem{}, path, "config.toml", 0o644, nil, nil, matchTemplate)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteTemplateFile_OverwritePromptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("different"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	prompt := func(path string) (bool, error) {
		return false, errors.New("prompt error")
	}
	err := writeTemplateFile(RealSystem{}, path, "config.toml", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error from prompt")
	}
}

func TestBuildKnownPaths_TemplateError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	_, err := inst.buildKnownPaths()
	if err == nil {
		t.Fatalf("expected error from templates walk")
	}
}

func TestBuildKnownPaths_TemplatePathError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return fn("other/file", &mockDirEntry{name: "file"}, nil)
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	_, err := inst.buildKnownPaths()
	if err == nil {
		t.Fatalf("expected error for unexpected path")
	}
}

func TestWriteTemplateDir_WalkError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	err := writeTemplateDir(RealSystem{}, "instructions", "/tmp/dest", nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteTemplateDir_PathError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		// Pass a path that doesn't start with root + "/"
		return fn("other/file", &mockDirEntry{name: "file"}, nil)
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	err := writeTemplateDir(RealSystem{}, "instructions", "/tmp/dest", nil, nil)
	if err == nil {
		t.Fatalf("expected error for unexpected path")
	}
}

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
	err := writeTemplateFile(RealSystem{}, path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

func TestWriteTemplateFile_MkdirError(t *testing.T) {
	root := t.TempDir()
	// Create a file where directory should be
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(blocker, "subdir", "config.toml")
	err := writeTemplateFile(RealSystem{}, path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestWriteTemplateFiles_GitignoreBlockError(t *testing.T) {
	root := t.TempDir()
	// Create parent dir but block gitignore.block directory creation
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
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

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.writeTemplateFiles()
	if err == nil {
		t.Fatalf("expected error from template file write")
	}
}

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

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.writeTemplateDirs()
	if err == nil {
		t.Fatalf("expected error from template dir write")
	}
}

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
	err := writeTemplateFile(RealSystem{}, path, "config.toml", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteTemplateFile_ReadTemplateError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.toml")
	err := writeTemplateFile(RealSystem{}, path, "nonexistent-template", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for template read failure")
	}
}

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
	err = writeTemplateFile(RealSystem{}, path, "config.toml", 0o644, prompt, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overwriteCalled {
		t.Fatalf("overwrite should not have been called when file matches")
	}
}

func TestBuildKnownPaths_Success(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
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

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestWriteTemplateDirSuccess(t *testing.T) {
	root := t.TempDir()
	destRoot := filepath.Join(root, "dest")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var recorded []string
	recordDiff := func(path string) {
		recorded = append(recorded, path)
	}

	err := writeTemplateDir(RealSystem{}, "instructions", destRoot, nil, recordDiff)
	if err != nil {
		t.Fatalf("writeTemplateDir error: %v", err)
	}

	// Check that at least one instruction file was written
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		t.Fatalf("read dest dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected instruction files to be written")
	}
}

func TestWriteTemplateDirWithOverwrite(t *testing.T) {
	root := t.TempDir()
	destRoot := filepath.Join(root, "dest")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write an existing file that differs
	existingPath := filepath.Join(destRoot, "00_base.md")
	if err := os.WriteFile(existingPath, []byte("different content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	overwriteCalled := false
	shouldOverwrite := func(path string) (bool, error) {
		overwriteCalled = true
		return true, nil
	}

	err := writeTemplateDir(RealSystem{}, "instructions", destRoot, shouldOverwrite, nil)
	if err != nil {
		t.Fatalf("writeTemplateDir error: %v", err)
	}
	if !overwriteCalled {
		t.Fatalf("expected overwrite prompt to be called")
	}
}

func TestWriteTemplateDirNoOverwrite(t *testing.T) {
	root := t.TempDir()
	destRoot := filepath.Join(root, "dest")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write an existing file that differs
	existingPath := filepath.Join(destRoot, "00_base.md")
	if err := os.WriteFile(existingPath, []byte("different content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var recorded []string
	recordDiff := func(path string) {
		recorded = append(recorded, path)
	}
	shouldOverwrite := func(path string) (bool, error) {
		return false, nil
	}

	err := writeTemplateDir(RealSystem{}, "instructions", destRoot, shouldOverwrite, recordDiff)
	if err != nil {
		t.Fatalf("writeTemplateDir error: %v", err)
	}
	if len(recorded) == 0 {
		t.Fatalf("expected diff to be recorded when not overwriting")
	}
}

func TestListManagedDiffs_DirDiffError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	instrDir := filepath.Join(alDir, "instructions")
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create an instruction file as a directory to cause stat error during matchTemplate
	instrFile := filepath.Join(instrDir, "00_base.md")
	if err := os.Mkdir(instrFile, 0o755); err != nil {
		t.Fatalf("mkdir instruction: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.listManagedDiffs()
	if err == nil {
		t.Fatalf("expected error from dir diff")
	}
}

func TestWriteTemplateDirCached_Success(t *testing.T) {
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     instrDir,
	}

	err := inst.writeTemplateDirCached(dir)
	if err != nil {
		t.Fatalf("writeTemplateDirCached error: %v", err)
	}

	// Verify files were written
	entries, _ := os.ReadDir(instrDir)
	if len(entries) == 0 {
		t.Fatalf("expected instruction files")
	}
}

func TestWriteTemplateDirCached_Error(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	// Create with full permissions first
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write an existing file that differs from template
	existingFile := filepath.Join(instrDir, "00_base.md")
	if err := os.WriteFile(existingFile, []byte("different content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make the directory read-only (can't create/modify files)
	if err := os.Chmod(instrDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) })

	inst := &installer{
		root:      root,
		sys:       RealSystem{},
		overwrite: true,
		force:     true,
	}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     instrDir,
	}

	err := inst.writeTemplateDirCached(dir)
	if err == nil {
		t.Fatalf("expected error writing to read-only dir")
	}
}

func TestTemplateDirEntries_Cached(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     filepath.Join(root, ".agent-layer", "instructions"),
	}

	entries1, err := inst.templateDirEntries(dir)
	if err != nil {
		t.Fatalf("templateDirEntries error: %v", err)
	}

	// Second call should use cache
	entries2, err := inst.templateDirEntries(dir)
	if err != nil {
		t.Fatalf("templateDirEntries error: %v", err)
	}

	if len(entries1) != len(entries2) {
		t.Fatalf("expected cached entries to match")
	}
}

func TestMatchTemplate_NilSys(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(configPath, templateBytes, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	info, _ := os.Stat(configPath)

	// Call with nil sys - should use inst.sys
	matches, err := inst.matchTemplate(nil, configPath, "config.toml", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if !matches {
		t.Fatalf("expected file to match template")
	}
}

func TestMatchTemplate_NoInfo(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(configPath, templateBytes, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}

	// Call with nil info - should still work, just not use cache
	matches, err := inst.matchTemplate(RealSystem{}, configPath, "config.toml", nil)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if !matches {
		t.Fatalf("expected file to match template")
	}
}

func TestAppendPinnedVersionDiff_InvalidVersion(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionPath := filepath.Join(alDir, "al.version")
	// Write invalid version that can't be normalized
	if err := os.WriteFile(versionPath, []byte("not-a-version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid version should trigger a diff
	if len(diffs) != 1 {
		t.Fatalf("expected diff for invalid version")
	}
}

func TestTemplateFileMatches_GitignoreBlockMatchesTemplate(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write content that matches the template exactly.
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(blockPath, templateBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	matches, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if !matches {
		t.Fatalf("expected gitignore.block to match")
	}
}

func TestTemplateFileMatches_GitignoreBlockNoMatch(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write different content that doesn't match
	if err := os.WriteFile(blockPath, []byte("# custom content\nsome-pattern\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	matches, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if matches {
		t.Fatalf("expected gitignore.block NOT to match custom content")
	}
}

func TestListManagedDiffs_PinnedVersionError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create al.version as a directory to cause read error
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.Mkdir(versionPath, 0o755); err != nil {
		t.Fatalf("mkdir version: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	_, err := inst.listManagedDiffs()
	if err == nil {
		t.Fatalf("expected error from pinned version diff")
	}
}

func TestAppendTemplateFileDiffs_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create config.toml as a directory to cause a stat error (not ErrNotExist)
	configPath := filepath.Join(alDir, "config.toml")
	if err := os.Mkdir(configPath, 0o000); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o755) })

	inst := &installer{root: root, sys: RealSystem{}}
	diffs := make(map[string]struct{})
	files := []templateFile{
		{path: configPath, template: "config.toml", perm: 0o644},
	}
	err := inst.appendTemplateFileDiffs(diffs, files)
	if err == nil {
		t.Fatalf("expected error from stat failure")
	}
}

func TestAppendTemplateDirDiffs_StatError_Permissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a file with unreadable permissions to cause stat error
	basePath := filepath.Join(instrDir, "00_base.md")
	if err := os.WriteFile(basePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make parent directory unreadable
	if err := os.Chmod(instrDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) })

	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     instrDir,
	}

	diffs := make(map[string]struct{})
	err := inst.appendTemplateDirDiffs(diffs, dir)
	if err == nil {
		t.Fatalf("expected error from stat failure")
	}
}

func TestAppendPinnedVersionDiff_ReadError_IsDirectory(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create al.version as a directory to cause read error (not ErrNotExist)
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.Mkdir(versionPath, 0o755); err != nil {
		t.Fatalf("mkdir version: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err == nil {
		t.Fatalf("expected error from read failure")
	}
}

func TestWriteTemplateDirCached_EntriesError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     filepath.Join(root, "instructions"),
	}

	err := inst.writeTemplateDirCached(dir)
	if err == nil {
		t.Fatalf("expected error from walk failure")
	}
}

func TestTemplateDirEntries_WalkCallbackError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		// Pass an error through the callback
		return fn("instructions/test.md", &mockDirEntry{name: "test.md"}, errors.New("callback error"))
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     filepath.Join(root, "instructions"),
	}

	_, err := inst.templateDirEntries(dir)
	if err == nil {
		t.Fatalf("expected error from walk callback")
	}
}

func TestTemplateDirEntries_UnexpectedPath(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		// Pass a path that doesn't start with root + "/"
		return fn("other/file.md", &mockDirEntry{name: "file.md"}, nil)
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "instructions",
		destRoot:     filepath.Join(root, "instructions"),
	}

	_, err := inst.templateDirEntries(dir)
	if err == nil {
		t.Fatalf("expected error for unexpected path")
	}
}

func TestWriteTemplateFileWithMatch_NilMatchTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.toml")
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Call with nil matchTemplate - should use default
	err = writeTemplateFileWithMatch(RealSystem{}, path, "config.toml", 0o644, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteTemplateFileWithMatch_MkdirAllError(t *testing.T) {
	root := t.TempDir()
	// Create a file that will block directory creation
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Try to write to a path where the parent can't be created
	path := filepath.Join(blocker, "subdir", "config.toml")
	err := writeTemplateFileWithMatch(RealSystem{}, path, "config.toml", 0o644, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestTemplateFileMatches_ReadError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write a file but make it unreadable
	if err := os.WriteFile(blockPath, []byte("content"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockPath, 0o644) })

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error from read failure")
	}
}

func TestTemplateFileMatches_TemplateReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(p string) ([]byte, error) {
		if p == "gitignore.block" {
			return nil, errors.New("template read error")
		}
		return original(p)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error from template read failure")
	}
}
