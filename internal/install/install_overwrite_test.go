package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
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

func TestShouldOverwrite_OverwriteAllDecided(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:                root,
		overwrite:           true,
		overwriteAllDecided: true,
		overwriteAll:        true,
		sys:                 RealSystem{},
	}

	ok, err := inst.shouldOverwrite(filepath.Join(root, "any-path"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when overwrite-all is accepted")
	}
}

func TestShouldOverwrite_ManagedPathUsesOverwriteAll(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")

	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPath, []byte("# custom header\n<!-- ENTRIES START -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unifiedCalled := false

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(managed, memory []DiffPreview) (bool, bool, error) {
				unifiedCalled = true
				if len(managed) == 0 {
					t.Fatal("expected managed previews")
				}
				return true, false, nil
			},
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) { return false, nil },
		},
	}

	ok, err := inst.shouldOverwrite(managedPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when overwriteAll returns true")
	}
	if !unifiedCalled {
		t.Fatal("expected unified callback to be called")
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
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) { return false, false, nil },
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

func TestShouldOverwrite_UsesUnifiedOverwritePrompter(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("# local override\n"), 0o600); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	unifiedCalled := 0
	perFilePromptCalled := false

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(managed []DiffPreview, memory []DiffPreview) (bool, bool, error) {
				unifiedCalled++
				if len(managed) == 0 {
					t.Fatal("expected managed previews in unified callback")
				}
				return true, false, nil
			},
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
				perFilePromptCalled = true
				return false, nil
			},
		},
	}

	ok, err := inst.shouldOverwrite(managedPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected true when unified managed decision is true")
	}
	if unifiedCalled != 1 {
		t.Fatalf("expected unified callback once, got %d", unifiedCalled)
	}
	if perFilePromptCalled {
		t.Fatalf("did not expect per-file prompt when overwrite-all managed is true")
	}
	if !inst.overwriteAllDecided {
		t.Fatalf("expected the overwrite-all decision to be cached")
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

	_, err := inst.shouldOverwrite(filepath.Join(root, ".agent-layer", "unknown.file"))
	if err == nil || err.Error() != messages.InstallOverwritePromptRequired {
		t.Fatalf("expected missing prompter error before preview lookup, got %v", err)
	}
}

func TestResolveOverwriteAllDecisions_AlreadyDecided(t *testing.T) {
	inst := &installer{
		overwriteAllDecided: true,
	}
	if err := inst.resolveOverwriteAllDecisions(); err != nil {
		t.Fatalf("expected nil error when already decided: %v", err)
	}
}

func TestResolveOverwriteAllDecisions_MissingPrompter(t *testing.T) {
	for _, p := range []*PromptFuncs{nil, {}} {
		// A missing callback must fail before diff listing, even without a System.
		inst := &installer{prompter: p}
		if err := inst.resolveOverwriteAllDecisions(); err == nil || err.Error() != messages.InstallOverwritePromptRequired {
			t.Fatalf("expected missing unified prompt error before listing diffs, got %v", err)
		}
	}
}

func TestResolveOverwriteAllDecisions_NoDiffsSkipsPrompt(t *testing.T) {
	root := t.TempDir()
	unifiedCalls := 0
	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				unifiedCalls++
				return true, true, nil
			},
		},
	}

	if err := inst.resolveOverwriteAllDecisions(); err != nil {
		t.Fatalf("resolveOverwriteAllDecisions: %v", err)
	}
	if unifiedCalls != 0 {
		t.Fatalf("expected no unified prompt calls when there are no diffs, got %d", unifiedCalls)
	}
	if !inst.overwriteAllDecided {
		t.Fatalf("expected overwrite-all decisions to be marked decided")
	}
	if inst.overwriteAll || inst.overwriteMemoryAll {
		t.Fatalf("expected overwrite-all defaults to remain false when no diffs exist")
	}
}

func TestResolveOverwriteAllDecisions_UnifiedPromptError(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o600); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				return false, false, errors.New("unified prompt failed")
			},
		},
	}

	if err := inst.resolveOverwriteAllDecisions(); err == nil {
		t.Fatalf("expected unified prompt error")
	}
}

func TestLookupDiffPreview_FallbackPinPreview(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.1.0\n"), 0o600); err != nil {
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
	if preview.Path != pinVersionRelPath {
		t.Fatalf("preview path = %q, want %s", preview.Path, pinVersionRelPath)
	}
}

func TestLookupDiffPreview_PathRequired(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview(""); err == nil {
		t.Fatalf("expected path-required error")
	}
}

func TestLookupDiffPreview_UsesManagedPreviewCache(t *testing.T) {
	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
		managedDiffPreviews: map[string]DiffPreview{
			".agent-layer/commands.allow": {
				Path: ".agent-layer/commands.allow",
			},
		},
	}

	preview, err := inst.lookupDiffPreview(".agent-layer/commands.allow")
	if err != nil {
		t.Fatalf("lookupDiffPreview: %v", err)
	}
	if preview.Path != ".agent-layer/commands.allow" {
		t.Fatalf("preview path = %q, want managed cache preview", preview.Path)
	}
}

func TestLookupDiffPreview_UsesMemoryPreviewCache(t *testing.T) {
	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
		memoryDiffPreviews: map[string]DiffPreview{
			"docs/agent-layer/ISSUES.md": {
				Path: "docs/agent-layer/ISSUES.md",
			},
		},
	}

	preview, err := inst.lookupDiffPreview("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("lookupDiffPreview: %v", err)
	}
	if preview.Path != "docs/agent-layer/ISSUES.md" {
		t.Fatalf("preview path = %q, want memory cache preview", preview.Path)
	}
}

func TestLookupDiffPreview_MissingTemplateMapping(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	path := ".agent-layer/unknown.file"
	sys.readErrs[filepath.Join(root, filepath.FromSlash(path))] = errors.New("must report mapping error before reading")
	inst := &installer{root: root, sys: sys}
	_, err := inst.lookupDiffPreview(path)
	if err == nil || err.Error() != fmt.Sprintf(messages.InstallMissingTemplatePathMappingFmt, path) {
		t.Fatalf("expected missing-template-mapping error before read, got %v", err)
	}
}

func TestLookupDiffPreview_NotExistFallback(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	preview, err := inst.lookupDiffPreview(".agent-layer/commands.allow")
	if err != nil {
		t.Fatalf("lookupDiffPreview: %v", err)
	}
	if preview != (DiffPreview{Path: commandsAllowRelPath}) {
		t.Fatalf("expected bare preview, got %#v", preview)
	}
}

func TestLookupDiffPreview_BuildPreviewReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir as file target: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview(".agent-layer/commands.allow"); err == nil {
		t.Fatalf("expected read error when destination path is a directory")
	}
}

func TestLookupDiffPreview_MemoryTemplateMappingError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk failed")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview("docs/agent-layer/ISSUES.md"); err == nil {
		t.Fatalf("expected memory template mapping error")
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
	if err := os.MkdirAll(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create commands.allow as a directory to cause stat error
	allowPath := filepath.Join(alDir, "commands.allow")
	if err := os.Mkdir(allowPath, 0o700); err != nil {
		t.Fatalf("mkdir allow: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().listManagedDiffs()
	if err == nil {
		t.Fatalf("expected error from template file diff")
	}
}

func TestListMemoryDiffs_Success(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a file that differs from template
	issuesPath := filepath.Join(memoryDir, "ISSUES.md")
	if err := os.WriteFile(issuesPath, []byte("# Custom issues"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs, err := inst.templates().listMemoryDiffs()
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
	_, err := inst.templates().listMemoryDiffs()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestAppendTemplateDirDiffs_StatError(t *testing.T) {
	root := t.TempDir()
	instrDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(instrDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create file that will cause stat to succeed initially but contain a directory
	// where a file should be to cause read error during comparison

	// Create instruction file as directory to trigger error path in matchTemplate
	instrFile := filepath.Join(instrDir, "00_rules.md")
	if err := os.Mkdir(instrFile, 0o700); err != nil {
		t.Fatalf("mkdir instruction: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs := make(map[string]struct{})
	err := inst.templates().appendTemplateDirDiffs(diffs, templateDir{
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
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create gitignore.block as a directory to cause read error
	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.Mkdir(blockPath, 0o700); err != nil {
		t.Fatalf("mkdir block: %v", err)
	}

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error reading gitignore.block")
	}
}

func TestTemplateFileMatches_ReadTemplateError(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.WriteFile(blockPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		return nil, errors.New("template read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	info, _ := os.Stat(blockPath)
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.templates().matchTemplate(blockPath, "gitignore.block", info)
	if err == nil {
		t.Fatalf("expected error from template read")
	}
}

func TestShouldOverwrite_UnifiedDecisionCachedAcrossManagedAndMemory(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		".agent-layer/commands.allow",
		".agent-layer/gitignore.block",
		"docs/agent-layer/ISSUES.md",
		"docs/agent-layer/BACKLOG.md",
	}
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# custom header\n<!-- ENTRIES START -->\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sys := newFaultSystem(RealSystem{})
	unifiedCalls := 0
	perFileCalls := 0
	inst := &installer{root: root, overwrite: true, sys: sys}
	inst.prompter = &PromptFuncs{
		OverwriteAllUnifiedPreviewFunc: func(managed, memory []DiffPreview) (bool, bool, error) {
			unifiedCalls++
			if len(managed) != 2 || len(memory) != 2 {
				t.Fatalf("expected two managed and two memory previews, got %d and %d", len(managed), len(memory))
			}
			// Any later file read fails, so per-file prompts must use the caches.
			for _, rel := range paths {
				sys.readErrs[normalizePath(filepath.Join(root, filepath.FromSlash(rel)))] = errors.New("unexpected preview rebuild")
			}
			return false, false, nil
		},
		OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
			wantPath := paths[perFileCalls]
			perFileCalls++
			cache := inst.managedDiffPreviews
			if inst.isMemoryPath(filepath.Join(root, filepath.FromSlash(wantPath))) {
				cache = inst.memoryDiffPreviews
			}
			cached, ok := cache[wantPath]
			if !ok || preview != cached || preview.Path != wantPath || preview.UnifiedDiff == "" {
				t.Fatalf("per-file preview did not reuse populated cache for %s: %#v", wantPath, preview)
			}
			return true, nil
		},
	}
	for _, rel := range paths {
		if ok, err := inst.shouldOverwrite(filepath.Join(root, filepath.FromSlash(rel))); !ok || err != nil {
			t.Fatalf("shouldOverwrite(%s) = (%v, %v)", rel, ok, err)
		}
	}
	if unifiedCalls != 1 || perFileCalls != len(paths) {
		t.Fatalf("unified calls=%d, per-file calls=%d", unifiedCalls, perFileCalls)
	}
}

func TestResolveOverwriteAllDecisions_PreviewErrorsBeforePrompt(t *testing.T) {
	for _, rel := range []string{".agent-layer/commands.allow", "docs/agent-layer/ISSUES.md"} {
		t.Run(rel, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("# custom header\n<!-- ENTRIES START -->\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wantErr := errors.New("preview read failed")
			sys := newFaultSystem(RealSystem{})
			sys.readErrs[normalizePath(path)] = wantErr
			inst := &installer{
				root: root, sys: sys,
				prompter: &PromptFuncs{
					OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
						t.Fatal("prompt must not run after preview error")
						return false, false, nil
					},
				},
			}
			if err := inst.resolveOverwriteAllDecisions(); !errors.Is(err, wantErr) {
				t.Fatalf("resolve error = %v, want %v", err, wantErr)
			}
			if inst.overwriteAllDecided {
				t.Fatal("failed preview must not cache a decision")
			}
		})
	}
}

func TestResolveOverwriteAllDecisions_PassesManagedPreviews(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom allow\n"), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	var promptPreviews []DiffPreview
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(previews, memory []DiffPreview) (bool, bool, error) {
				promptPreviews = append(promptPreviews, previews...)
				return false, false, nil
			},
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) { return false, nil },
		},
	}

	if err := inst.resolveOverwriteAllDecisions(); err != nil {
		t.Fatalf("resolveOverwriteAllDecisions: %v", err)
	}
	if len(promptPreviews) == 0 {
		t.Fatalf("expected prompt previews")
	}
	if promptPreviews[0].Path != commandsAllowRelPath || promptPreviews[0].UnifiedDiff == "" {
		t.Fatalf("expected commands.allow diff preview, got %#v", promptPreviews[0])
	}
}

func TestResolveOverwriteAllDecisions_PassesMemoryPreviews(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o700); err != nil {
		t.Fatalf("mkdir baseline docs: %v", err)
	}
	content := []byte("# ISSUES\n\nLegacy header\n\n<!-- ENTRIES START -->\n")
	docPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.WriteFile(docPath, content, 0o600); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
	if err := os.WriteFile(baselinePath, content, 0o600); err != nil {
		t.Fatalf("write baseline doc: %v", err)
	}

	var promptPreviews []DiffPreview
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: &PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(managed, previews []DiffPreview) (bool, bool, error) {
				promptPreviews = append(promptPreviews, previews...)
				return false, false, nil
			},
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) { return false, nil },
		},
	}

	if err := inst.resolveOverwriteAllDecisions(); err != nil {
		t.Fatalf("resolveOverwriteAllDecisions: %v", err)
	}
	if len(promptPreviews) == 0 {
		t.Fatalf("expected prompt previews")
	}
	if promptPreviews[0].Path != "docs/agent-layer/ISSUES.md" || promptPreviews[0].UnifiedDiff == "" {
		t.Fatalf("expected ISSUES.md diff preview, got %#v", promptPreviews[0])
	}
}
