package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestWriteTemplateFile_NoPromptPreservesExisting(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil); err != nil {
		t.Fatalf("writeTemplateFile error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "custom" {
		t.Fatalf("expected existing file to remain")
	}
}

func TestWriteTemplateFile_NoPromptInvalidTemplate(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "config.toml")
	err := inst.templates().writeTemplateFile(path, "missing-template", 0o644, nil, nil)
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
	if err := inst.templates().writeSectionAwareTemplateFile(destPath, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart); err != nil {
		t.Fatalf("writeSectionAwareTemplateFile: %v", err)
	}

	content, err := os.ReadFile(destPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read written section-aware file: %v", err)
	}
	if !strings.Contains(string(content), ownershipMarkerEntriesStart) {
		t.Fatalf("expected marker %q in written file", ownershipMarkerEntriesStart)
	}
}

func TestAppendTemplateFileDiffs_NormalizesRelativePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, `.agent-layer\commands.allow`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("custom allowlist\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	diffs := make(map[string]struct{})
	if err := inst.templates().appendTemplateFileDiffs(diffs, []templateFile{{path: path, template: "commands.allow"}}); err != nil {
		t.Fatal(err)
	}
	paths := sortedKeys(diffs)
	if len(paths) != 1 || paths[0] != commandsAllowRelPath {
		t.Fatalf("expected slash-normalized commands.allow path, got %v", paths)
	}
}

func TestWriteTemplateFile_ParentIsFileStatError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	path := filepath.Join(file, "config.toml")
	if err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil); err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

func TestWriteTemplateFile_UsesCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	inst := &installer{sys: RealSystem{}}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if _, err := inst.templates().matchTemplate(path, "config.toml", info); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(string) ([]byte, error) {
		return nil, errors.New("unexpected template read")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	if err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil); err != nil {
		t.Fatalf("expected cached match to skip template read: %v", err)
	}
}

func TestWriteTemplateFile_OverwriteWithSameMetadataWritesBaseline(t *testing.T) {
	for _, templatePath := range []string{commandsAllowName, templateGitignoreBlock, "docs/agent-layer/CONTEXT.md"} {
		t.Run(templatePath, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".agent-layer", filepath.FromSlash(templatePath))
			target, err := templates.Read(templatePath)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			// Change a comment without changing the size or gitignore tracking choices.
			existing := append([]byte(nil), target...)
			existing[1] = 'X'
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, existing, 0o600); err != nil {
				t.Fatalf("write existing file: %v", err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat existing file: %v", err)
			}
			inst := &installer{root: root, sys: RealSystem{}}
			approve := func(string) (bool, error) { return true, nil }
			if err := inst.templates().writeTemplateFile(path, templatePath, 0o644, approve, nil); err != nil {
				t.Fatalf("overwrite: %v", err)
			}
			// Simulate replacement on a filesystem whose timestamp did not advance.
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatalf("restore timestamp: %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat replacement: %v", err)
			}
			if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
				t.Fatal("replacement must retain size and timestamp to exercise the cached mismatch")
			}
			if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByUpgrade); err != nil {
				t.Fatalf("write baseline: %v", err)
			}
			state, err := readManagedBaselineState(root, inst.sys)
			if err != nil {
				t.Fatalf("baseline must be written after overwrite: %v", err)
			}
			if state.Source != BaselineStateSourceWrittenByUpgrade {
				t.Fatalf("baseline source = %q, want %q", state.Source, BaselineStateSourceWrittenByUpgrade)
			}
		})
	}
}

func TestFileMatchesTemplateReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
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

func TestWriteTemplateFile_ReadExistingError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(path)] = errors.New("read error")
	inst := &installer{sys: sys}
	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected existing-file read error, got %v", err)
	}
}

func TestWriteTemplateFile_OverwritePromptError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("different"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	prompt := func(path string) (bool, error) {
		return false, errors.New("prompt error")
	}
	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
	if err == nil || !strings.Contains(err.Error(), "prompt error") {
		t.Fatalf("expected prompt error, got %v", err)
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

func TestWriteTemplateFile_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	dir := filepath.Join(root, "locked")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, dir)

	path := filepath.Join(dir, "config.toml")
	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

func TestWriteTemplateFile_MkdirError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	// Create a file where directory should be
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(blocker, "subdir", "config.toml")
	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestWriteTemplateFiles_GitignoreBlockError(t *testing.T) {
	root := t.TempDir()
	// Create parent dir but block gitignore.block directory creation
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	// This should work without error for most files, but let's create a scenario
	// where the gitignore.block writing fails

	// First, we need the earlier files to succeed. Let's block the gitignore.block path specifically
	// by making it a directory
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.Mkdir(blockPath, 0o700); err != nil {
		t.Fatalf("mkdir block: %v", err)
	}

	err := inst.templates().writeTemplateFiles()
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
	t.Cleanup(func() { _ = os.Chmod(alDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, alDir)

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.templates().writeTemplateFiles()
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
	instrDir := filepath.Join(root, ".agent-layer", "templates", "docs")
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(instrDir, "CONTEXT.md"), []byte("existing rules"), 0o600); err != nil {
		t.Fatalf("write workflow evidence: %v", err)
	}
	// Now make it read-only to prevent file writes
	if err := os.Chmod(instrDir, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, instrDir)

	inst := &installer{root: root, overwrite: true, overwriteAll: true, overwriteAllDecided: true, sys: RealSystem{}}
	err := inst.templates().writeTemplateDirs()
	if err == nil {
		t.Fatalf("expected error from template dir write")
	}
}

func TestWriteTemplateFile_WriteAfterOverwriteError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "config.toml")
	// Write existing file with different content
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make dir read-only to cause write error during overwrite
	if err := os.Chmod(root, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, root)

	prompt := func(p string) (bool, error) {
		return true, nil // Agree to overwrite
	}
	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteTemplateFile_ReadTemplateError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "file.toml")
	err := inst.templates().writeTemplateFile(path, "nonexistent-template", 0o644, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("expected template read error, got %v", err)
	}
}

func TestWriteTemplateFile_ExactMatch(t *testing.T) {
	root := t.TempDir()
	inst := &installer{sys: RealSystem{}}
	path := filepath.Join(root, "config.toml")

	// First write the template
	templateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(path, templateBytes, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Now try to write again - should succeed without calling overwrite
	overwriteCalled := false
	prompt := func(p string) (bool, error) {
		overwriteCalled = true
		return false, nil
	}
	err = inst.templates().writeTemplateFile(path, "config.toml", 0o644, prompt, nil)
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
		filepath.Join(root, ".agent-layer", "templates", "docs"),
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

func TestListManagedDiffs_DirDiffError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	instrDir := filepath.Join(alDir, "templates", "docs")
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create an instruction file as a directory to cause stat error during matchTemplate
	instrFile := filepath.Join(instrDir, "CONTEXT.md")
	if err := os.Mkdir(instrFile, 0o700); err != nil {
		t.Fatalf("mkdir instruction: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().listManagedDiffs()
	if err == nil {
		t.Fatalf("expected error from dir diff")
	}
}

func TestWriteTemplateDirCached_Success(t *testing.T) {
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "templates", "docs")
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     instrDir,
	}

	err := inst.templates().writeTemplateDirCached(dir)
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
	instrDir := filepath.Join(root, ".agent-layer", "templates", "docs")
	// Create with full permissions first
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write an existing file that differs from template
	existingFile := filepath.Join(instrDir, "CONTEXT.md")
	if err := os.WriteFile(existingFile, []byte("different content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make the directory read-only (can't create/modify files)
	if err := os.Chmod(instrDir, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfWritable(t, instrDir)

	inst := &installer{
		root:                root,
		sys:                 RealSystem{},
		overwrite:           true,
		overwriteAllDecided: true,
		overwriteAll:        true,
	}
	dir := templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     instrDir,
	}

	err := inst.templates().writeTemplateDirCached(dir)
	if err == nil {
		t.Fatalf("expected error writing to read-only dir")
	}
}

func TestTemplateDirEntries_Cached(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     filepath.Join(root, ".agent-layer", "templates", "docs"),
	}

	entries1, err := inst.templates().templateDirEntries(dir)
	if err != nil {
		t.Fatalf("templateDirEntries error: %v", err)
	}

	// Second call should use cache
	entries2, err := inst.templates().templateDirEntries(dir)
	if err != nil {
		t.Fatalf("templateDirEntries error: %v", err)
	}

	if len(entries1) != len(entries2) {
		t.Fatalf("expected cached entries to match")
	}
}

func TestTemplateFileMatches_GitignoreBlockMatchesTemplate(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write content that matches the template exactly.
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(blockPath, templateBytes, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	matches, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if !matches {
		t.Fatalf("expected gitignore.block to match")
	}
}

func TestTemplateFileMatches_GitignoreBlockMatchesMergedTrackingSettings(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte(customizeGitignoreTracking(string(templateBytes))), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	matches, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if !matches {
		t.Fatalf("expected customized tracking settings to match the merged template")
	}
}

func TestTemplateFileMatches_GitignoreBlockNoMatch(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write different content that doesn't match
	if err := os.WriteFile(blockPath, []byte("# custom content\nsome-pattern\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	matches, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err != nil {
		t.Fatalf("matchTemplate error: %v", err)
	}
	if matches {
		t.Fatalf("expected gitignore.block NOT to match custom content")
	}
}

func TestAppendTemplateFileDiffs_StatError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create config.toml as a directory to cause a stat error (not ErrNotExist)
	configPath := filepath.Join(alDir, "config.toml")
	if err := os.Mkdir(configPath, 0o000); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.

	inst := &installer{root: root, sys: RealSystem{}}
	diffs := make(map[string]struct{})
	files := []templateFile{
		{path: configPath, template: "config.toml", perm: 0o644},
	}
	err := inst.templates().appendTemplateFileDiffs(diffs, files)
	if err == nil {
		t.Fatalf("expected error from stat failure")
	}
}

func TestAppendTemplateDirDiffs_StatError_Permissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "templates", "docs")
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a file with unreadable permissions to cause stat error
	basePath := filepath.Join(instrDir, "CONTEXT.md")
	if err := os.WriteFile(basePath, []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make parent directory unreadable
	if err := os.Chmod(instrDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instrDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	testutil.SkipIfReadable(t, basePath)

	inst := &installer{root: root, sys: RealSystem{}}
	dir := templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     instrDir,
	}

	diffs := make(map[string]struct{})
	err := inst.templates().appendTemplateDirDiffs(diffs, dir)
	if err == nil {
		t.Fatalf("expected error from stat failure")
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
		templateRoot: "docs/agent-layer",
		destRoot:     filepath.Join(root, "templates", "docs"),
	}

	err := inst.templates().writeTemplateDirCached(dir)
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
		templateRoot: "docs/agent-layer",
		destRoot:     filepath.Join(root, "templates", "docs"),
	}

	_, err := inst.templates().templateDirEntries(dir)
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
		templateRoot: "docs/agent-layer",
		destRoot:     filepath.Join(root, "templates", "docs"),
	}

	_, err := inst.templates().templateDirEntries(dir)
	if err == nil {
		t.Fatalf("expected error for unexpected path")
	}
}

func TestTemplateFileMatches_ReadError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	// Write a file but make it unreadable
	if err := os.WriteFile(blockPath, []byte("content"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockPath, 0o600) })
	testutil.SkipIfReadable(t, blockPath)

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
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
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error from template read failure")
	}
}

func TestAppendTemplateFileDiffs_StatErrorInjected(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write commands.allow: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(path)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}
	if err := inst.templates().appendTemplateFileDiffs(map[string]struct{}{}, []templateFile{{
		path:     path,
		template: "commands.allow",
		perm:     0o644,
	}}); err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestAppendTemplateDirDiffs_SectionAwareErrorsAndDiffs(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	issuesPath := filepath.Join(memoryDir, "ISSUES.md")
	if err := os.Mkdir(issuesPath, 0o700); err != nil {
		t.Fatalf("mkdir issues dir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.templates().appendTemplateDirDiffs(map[string]struct{}{}, templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     memoryDir,
	})
	if err == nil {
		t.Fatalf("expected section-aware read error")
	}
}

func TestAppendTemplateDirDiffs_AddsNonSectionAwareMismatch(t *testing.T) {
	root := t.TempDir()
	instructionsDir := filepath.Join(root, ".agent-layer", "templates", "docs")
	if err := os.MkdirAll(instructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	targetPath := filepath.Join(instructionsDir, "CONTEXT.md")
	if err := os.WriteFile(targetPath, []byte("custom instructions\n"), 0o600); err != nil {
		t.Fatalf("write instruction: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs := map[string]struct{}{}
	if err := inst.templates().appendTemplateDirDiffs(diffs, templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     instructionsDir,
	}); err != nil {
		t.Fatalf("appendTemplateDirDiffs: %v", err)
	}

	if _, ok := diffs[".agent-layer/templates/docs/CONTEXT.md"]; !ok {
		t.Fatalf("expected non-section-aware mismatch to be recorded, got %v", diffs)
	}
}

func TestWriteTemplateDirCached_SectionAwareError(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "ISSUES.md"), []byte("# invalid\n"), 0o600); err != nil {
		t.Fatalf("write invalid issues: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.templates().writeTemplateDirCached(templateDir{
		templateRoot: "docs/agent-layer",
		destRoot:     memoryDir,
	})
	if err == nil {
		t.Fatalf("expected section-aware error")
	}
}

func TestWriteSectionAwareTemplateFile_ErrorPaths(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	relPath := "docs/agent-layer/ISSUES.md"
	path := filepath.Join(memoryDir, "ISSUES.md")
	inst := &installer{root: root, sys: RealSystem{}}

	t.Run("read error", func(t *testing.T) {
		if err := os.RemoveAll(path); err != nil {
			t.Fatalf("remove file: %v", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("mkdir path: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(path) })
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil || !strings.Contains(err.Error(), "failed to read") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("template read error", func(t *testing.T) {
		content := "# header\n" + ownershipMarkerEntriesStart + "\n- entry\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write issues: %v", err)
		}
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "docs/agent-layer/ISSUES.md" {
				return nil, errors.New("template read boom")
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil || !strings.Contains(err.Error(), "failed to read template") {
			t.Fatalf("expected template read error, got %v", err)
		}
	})

	t.Run("local split error", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("# no marker\n"), 0o600); err != nil {
			t.Fatalf("write issues: %v", err)
		}
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil {
			t.Fatalf("expected local split error")
		}
	})

	t.Run("template split error", func(t *testing.T) {
		content := "# header\n" + ownershipMarkerEntriesStart + "\n- local entry\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write issues: %v", err)
		}
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "docs/agent-layer/ISSUES.md" {
				return []byte("# bad template without marker\n"), nil
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil {
			t.Fatalf("expected template split error")
		}
	})
}

func TestWriteSectionAwareTemplateFile_OverwriteBranches(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	relPath := "docs/agent-layer/ISSUES.md"
	path := filepath.Join(memoryDir, "ISSUES.md")
	content := "# custom header\n" + ownershipMarkerEntriesStart + "\n- custom entry\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write issues: %v", err)
	}

	t.Run("overwrite prompt error", func(t *testing.T) {
		inst := &installer{
			root:      root,
			sys:       RealSystem{},
			overwrite: true,
			prompter: &PromptFuncs{
				OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) { return false, false, errors.New("prompt boom") },
			},
		}
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil || !strings.Contains(err.Error(), "prompt boom") {
			t.Fatalf("expected overwrite prompt error, got %v", err)
		}
	})

	t.Run("overwrite declined records diff", func(t *testing.T) {
		inst := &installer{
			root:      root,
			sys:       RealSystem{},
			overwrite: true,
			prompter: &PromptFuncs{
				OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) { return false, false, nil },
				OverwritePreviewFunc:           func(DiffPreview) (bool, error) { return false, nil },
			},
		}
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err != nil {
			t.Fatalf("writeSectionAwareTemplateFile: %v", err)
		}
		if len(inst.diffs) == 0 {
			t.Fatalf("expected diff to be recorded when overwrite is declined")
		}
	})

	t.Run("overwrite accepted write error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.writeErrs[normalizePath(path)] = errors.New("write boom")
		inst := &installer{
			root:      root,
			sys:       sys,
			overwrite: true,
			prompter: &PromptFuncs{
				OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) { return false, true, nil },
				OverwritePreviewFunc:           func(DiffPreview) (bool, error) { return true, nil },
			},
		}
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil || !strings.Contains(err.Error(), "failed to write") {
			t.Fatalf("expected write error, got %v", err)
		}
	})

	t.Run("stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(path)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}
		err := inst.templates().writeSectionAwareTemplateFile(path, "docs/agent-layer/ISSUES.md", 0o644, relPath, ownershipMarkerEntriesStart)
		if err == nil || !strings.Contains(err.Error(), "failed to stat") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})
}

func TestWriteTemplateFile_MkdirAllErrorAfterNotExist(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "config.toml")
	sys := newFaultSystem(RealSystem{})
	sys.mkdirErrs[normalizePath(filepath.Dir(path))] = errors.New("mkdir boom")
	inst := &installer{sys: sys}

	err := inst.templates().writeTemplateFile(path, "config.toml", 0o644, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to create directory for") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestWriteTemplateFiles_UnreadableSeedFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(path)] = errors.New("seed read failed")
	inst := &installer{root: root, sys: sys}
	if err := inst.templates().writeTemplateFiles(); err == nil || !strings.Contains(err.Error(), "failed to read "+path+": seed read failed") {
		t.Fatalf("expected seed read error, got %v", err)
	}
}

func TestWriteTemplateFile_OverwriteReadOrder(t *testing.T) {
	for _, templatePath := range []string{"config.toml", "gitignore.block"} {
		t.Run(templatePath, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), templatePath)
			if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			sys := &promptReadErrorSystem{System: RealSystem{}, path: path, failAt: 2, err: errors.New("second read failed")}
			inst := &installer{sys: sys}
			approve := func(string) (bool, error) { return true, nil }
			err := inst.templates().writeTemplateFile(path, templatePath, 0o644, approve, nil)
			if templatePath == "gitignore.block" {
				if err == nil || !strings.Contains(err.Error(), "failed to read "+path+": second read failed") {
					t.Fatalf("expected overwrite read error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("overwrite: %v", err)
			}
			got, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
			if err != nil {
				t.Fatalf("read updated file: %v", err)
			}
			want, err := templates.Read(templatePath)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("overwrite did not write raw template")
			}
		})
	}
}

func TestWriteTemplateFile_MissingWritesRawTemplate(t *testing.T) {
	for _, tc := range []struct {
		template string
		perm     fs.FileMode
	}{{"env", 0o600}, {"gitignore.block", 0o644}} {
		t.Run(tc.template, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", tc.template)
			inst := &installer{sys: RealSystem{}}
			unexpected := func(string) (bool, error) {
				t.Fatal("missing file should not prompt")
				return false, nil
			}
			if err := inst.templates().writeTemplateFile(path, tc.template, tc.perm, unexpected, func(string) { t.Fatal("missing file should not record a diff") }); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			want, err := templates.Read(tc.template)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			if string(got) != string(want) {
				t.Fatal("missing file should receive raw template")
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if os.PathSeparator != '\\' && info.Mode().Perm() != tc.perm {
				t.Fatalf("permissions = %o, want %o", info.Mode().Perm(), tc.perm)
			}
		})
	}
}

func TestWriteTemplateFiles_OverwritePolicyPerFileClass(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "overwrite"}[overwrite], func(t *testing.T) {
			root := t.TempDir()
			inst := &installer{root: root, sys: RealSystem{}, overwrite: overwrite, overwriteAll: true, overwriteAllDecided: true}
			tm := inst.templates()
			seed, agentOnly, managed := tm.userOwnedSeedFiles(), tm.agentOnlyFiles(), tm.managedTemplateFiles()
			for _, files := range [][]templateFile{seed, agentOnly, managed} {
				for _, file := range files {
					if err := os.MkdirAll(filepath.Dir(file.path), 0o700); err != nil {
						t.Fatalf("mkdir: %v", err)
					}
					if err := os.WriteFile(file.path, []byte("custom\n"), 0o600); err != nil {
						t.Fatalf("write %s: %v", file.path, err)
					}
				}
			}

			if err := tm.writeTemplateFiles(); err != nil {
				t.Fatalf("writeTemplateFiles: %v", err)
			}

			matchesTemplate := func(file templateFile) bool {
				t.Helper()
				matches, err := fileMatchesTemplate(RealSystem{}, file.path, file.template)
				if err != nil {
					t.Fatalf("match %s: %v", file.path, err)
				}
				return matches
			}
			for _, file := range seed {
				if matchesTemplate(file) {
					t.Fatalf("seed file %s was overwritten", file.path)
				}
			}
			for _, file := range agentOnly {
				if !matchesTemplate(file) {
					t.Fatalf("agent-only file %s was not restored", file.path)
				}
			}
			var wantDiffs []string
			for _, file := range managed {
				if matchesTemplate(file) != overwrite {
					t.Fatalf("managed file %s overwritten = %v, want %v", file.path, !overwrite, overwrite)
				}
				if !overwrite {
					wantDiffs = append(wantDiffs, file.path)
				}
			}
			if strings.Join(inst.diffs, "\n") != strings.Join(wantDiffs, "\n") {
				t.Fatalf("diffs = %v, want %v", inst.diffs, wantDiffs)
			}
		})
	}
}

func TestCheckDiffEvidence_UsesSlashNormalizedTemplateMapping(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom allowlist\n"), 0o600); err != nil {
		t.Fatalf("write commands.allow: %v", err)
	}

	inst := &installer{
		root: root,
		sys:  RealSystem{},
	}
	err := inst.templates().checkDiffEvidence(
		[]string{".agent-layer\\commands.allow"},
		map[string]string{
			".agent-layer/commands.allow": "missing-template",
		},
	)
	if err == nil {
		t.Fatal("expected template-read error when slash-normalized mapping is used")
	}
	if !strings.Contains(err.Error(), "missing-template") {
		t.Fatalf("expected missing-template error, got %v", err)
	}
}
