package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestShouldOverwrite_OverwriteFalse(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: false,
		sys:       RealSystem{},
	}

	ok, err := inst.shouldOverwrite(filepath.Join(root, "any-path"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected false when overwrite is false")
	}
}

func TestShouldOverwrite_Force(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		force:     true,
		sys:       RealSystem{},
	}

	ok, err := inst.shouldOverwrite(filepath.Join(root, "any-path"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when force is true")
	}
}

func TestShouldOverwrite_ManagedPathUsesOverwriteAll(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")

	overwriteAllCalled := false
	overwriteAllMemoryCalled := false

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { overwriteAllCalled = true; return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { overwriteAllMemoryCalled = true; return false, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return false, nil },
		},
	}

	ok, err := inst.shouldOverwrite(managedPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when overwriteAll returns true")
	}
	if !overwriteAllCalled {
		t.Fatalf("expected managed OverwriteAll prompt to be called")
	}
	if overwriteAllMemoryCalled {
		t.Fatalf("did not expect memory OverwriteAll prompt to be called for managed path")
	}
}

func TestShouldOverwrite_MemoryPathUsesOverwriteAllMemory(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")

	overwriteAllCalled := false
	overwriteAllMemoryCalled := false

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { overwriteAllCalled = true; return false, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { overwriteAllMemoryCalled = true; return true, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return false, nil },
		},
	}

	ok, err := inst.shouldOverwrite(memoryPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when memory overwriteAll returns true")
	}
	if overwriteAllCalled {
		t.Fatalf("did not expect managed OverwriteAll prompt to be called for memory path")
	}
	if !overwriteAllMemoryCalled {
		t.Fatalf("expected memory OverwriteAll prompt to be called")
	}
}

func TestShouldOverwrite_MemoryPathPromptsPerFile(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")

	perFilePromptCalled := false
	perFilePath := ""

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return false, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return false, nil },
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
				perFilePromptCalled = true
				perFilePath = preview.Path
				return true, nil
			},
		},
	}

	ok, err := inst.shouldOverwrite(memoryPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when per-file prompt returns true")
	}
	if !perFilePromptCalled {
		t.Fatalf("expected per-file prompt to be called")
	}
	if perFilePath != filepath.Join("docs", "agent-layer", "ISSUES.md") {
		t.Fatalf("expected relative path, got %q", perFilePath)
	}
}

func TestShouldOverwrite_MissingPrompter(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:                root,
		overwrite:           true,
		overwriteAllDecided: true,
		overwriteAll:        false,
		sys:                 RealSystem{},
		prompter:            nil,
	}

	_, err := inst.shouldOverwrite(filepath.Join(root, ".agent-layer", "commands.allow"))
	if err == nil {
		t.Fatalf("expected error when prompter is nil")
	}
}

func TestLookupDiffPreview_FallbackPinUsesUpstreamOwnership(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	inst := &installer{
		root:       root,
		pinVersion: "0.2.0",
		sys:        RealSystem{},
	}
	preview, err := inst.lookupDiffPreview(pinVersionRelPath)
	if err != nil {
		t.Fatalf("lookupDiffPreview: %v", err)
	}
	if preview.Ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("preview ownership = %q, want %q", preview.Ownership, OwnershipUpstreamTemplateDelta)
	}
	if preview.Path != pinVersionRelPath {
		t.Fatalf("preview path = %q, want %s", preview.Path, pinVersionRelPath)
	}
}

func TestShouldOverwriteAllManaged_Error(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc: func([]DiffPreview) (bool, error) { return false, errors.New("prompt error") },
		},
	}

	_, err := inst.shouldOverwriteAllManaged()
	if err == nil {
		t.Fatalf("expected error from prompt")
	}
}

func TestShouldOverwriteAllMemory_Error(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return false, errors.New("prompt error") },
		},
	}

	_, err := inst.shouldOverwriteAllMemory()
	if err == nil {
		t.Fatalf("expected error from prompt")
	}
}

func TestShouldOverwriteAllMemory_Cached(t *testing.T) {
	root := t.TempDir()
	promptCount := 0
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) {
				promptCount++
				return true, nil
			},
		},
	}

	// First call should prompt.
	ok, err := inst.shouldOverwriteAllMemory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true")
	}
	if promptCount != 1 {
		t.Fatalf("expected 1 prompt call, got %d", promptCount)
	}

	// Second call should use cache.
	ok, err = inst.shouldOverwriteAllMemory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true")
	}
	if promptCount != 1 {
		t.Fatalf("expected prompt to be cached, got %d calls", promptCount)
	}
}

func TestIsMemoryPath_EmptyRoot(t *testing.T) {
	inst := &installer{root: "", sys: RealSystem{}}
	if inst.isMemoryPath("/any/path") {
		t.Fatalf("expected false when root is empty")
	}
}

func TestIsMemoryPath_RelError(t *testing.T) {
	// On Unix, filepath.Rel with relative root and absolute path can fail
	inst := &installer{root: "relative/path", sys: RealSystem{}}
	if inst.isMemoryPath("/absolute/path") {
		t.Fatalf("expected false when Rel fails")
	}
}

func TestIsMemoryPath_ExactMatch(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	memoryRoot := filepath.Join(root, "docs", "agent-layer")
	if !inst.isMemoryPath(memoryRoot) {
		t.Fatalf("expected true for exact memory root")
	}
}

func TestIsMemoryPath_Subpath(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	subpath := filepath.Join(root, "docs", "agent-layer", "BACKLOG.md")
	if !inst.isMemoryPath(subpath) {
		t.Fatalf("expected true for memory subpath")
	}
}

func TestIsMemoryPath_NotUnder(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	nonMemory := filepath.Join(root, "other", "path")
	if inst.isMemoryPath(nonMemory) {
		t.Fatalf("expected false for non-memory path")
	}
}

func TestListManagedDiffs_TemplateFileDiffError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create commands.allow as a directory to cause stat error
	allowPath := filepath.Join(alDir, "commands.allow")
	if err := os.Mkdir(allowPath, 0o755); err != nil {
		t.Fatalf("mkdir allow: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.listManagedDiffs()
	if err == nil {
		t.Fatalf("expected error from template file diff")
	}
}

func TestListMemoryDiffs_Success(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a file that differs from template
	issuesPath := filepath.Join(memoryDir, "ISSUES.md")
	if err := os.WriteFile(issuesPath, []byte("# Custom issues"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs, err := inst.listMemoryDiffs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 1 || diffs[0] != filepath.Join("docs", "agent-layer", "ISSUES.md") {
		t.Fatalf("unexpected diffs: %v", diffs)
	}
}

func TestListMemoryDiffs_TemplateWalkError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk error")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	_, err := inst.listMemoryDiffs()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestAppendPinnedVersionDiff_ReadError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a directory where the version file should be
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.Mkdir(versionPath, 0o755); err != nil {
		t.Fatalf("mkdir version: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err == nil {
		t.Fatalf("expected error reading version file")
	}
}

func TestAppendPinnedVersionDiff_EmptyContent(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.WriteFile(versionPath, []byte("  \n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf("expected no diff for empty version file")
	}
}

func TestAppendPinnedVersionDiff_VersionMatches(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.WriteFile(versionPath, []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf("expected no diff when versions match")
	}
}

func TestAppendPinnedVersionDiff_VersionDiffers(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionPath := filepath.Join(alDir, "al.version")
	if err := os.WriteFile(versionPath, []byte("0.9.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendPinnedVersionDiff(diffs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected diff when versions differ")
	}
}

func TestAppendTemplateDirDiffs_StatError(t *testing.T) {
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(instrDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create file that will cause stat to succeed initially but contain a directory
	// where a file should be to cause read error during comparison

	// Create instruction file as directory to trigger error path in matchTemplate
	instrFile := filepath.Join(instrDir, "00_base.md")
	if err := os.Mkdir(instrFile, 0o755); err != nil {
		t.Fatalf("mkdir instruction: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.appendTemplateDirDiffs(diffs, templateDir{
		templateRoot: "instructions",
		destRoot:     instrDir,
	})
	if err == nil {
		t.Fatalf("expected error from matchTemplate")
	}
}

func TestTemplateFileMatches_ReadFileError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("skipping permissions test on windows")
	}
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create gitignore.block as a directory to cause read error
	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.Mkdir(blockPath, 0o755); err != nil {
		t.Fatalf("mkdir block: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error reading gitignore.block")
	}
}

func TestTemplateFileMatches_ReadTemplateError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		return nil, errors.New("template read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.matchTemplate(inst.sys, blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error from template read")
	}
}

func TestPrompterOverwriteAllMemory_NilFunc(t *testing.T) {
	p := PromptFuncs{}
	_, err := p.OverwriteAllMemory(nil)
	if err == nil {
		t.Fatalf("expected error when func is nil")
	}
}

func TestPrompterOverwrite_NilFunc(t *testing.T) {
	p := PromptFuncs{}
	_, err := p.Overwrite(DiffPreview{})
	if err == nil {
		t.Fatalf("expected error when func is nil")
	}
}

func TestPrompterDeleteUnknownAll_NilFunc(t *testing.T) {
	p := PromptFuncs{}
	_, err := p.DeleteUnknownAll(nil)
	if err == nil {
		t.Fatalf("expected error when func is nil")
	}
}
