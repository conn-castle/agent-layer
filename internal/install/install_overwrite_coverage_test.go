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

func TestShouldOverwrite_Memory_PropagatesShouldOverwriteAllMemoryError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: true,
		// prompter intentionally nil to force shouldOverwriteAllMemory to error.
		prompter: nil,
		sys:      RealSystem{},
	}

	_, err := inst.shouldOverwrite(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestShouldOverwriteAllManaged_MissingPrompter_Errors(t *testing.T) {
	inst := &installer{
		root:     t.TempDir(),
		prompter: nil,
		sys:      RealSystem{},
	}

	_, err := inst.shouldOverwriteAllManaged()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestShouldOverwriteAllManaged_ListManagedLabeledDiffsErrorPropagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc: func([]DiffPreview) (bool, error) {
				t.Fatalf("did not expect overwrite-all prompt to be called")
				return false, nil
			},
		},
	}

	_, err := inst.shouldOverwriteAllManaged()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestShouldOverwriteAllMemory_ListMemoryLabeledDiffsErrorPropagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) {
				t.Fatalf("did not expect overwrite-all-memory prompt to be called")
				return false, nil
			},
		},
	}

	_, err := inst.shouldOverwriteAllMemory()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestShouldOverwriteAllManaged_UsesUnifiedDecision(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatalf("mkdir managed path: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o644); err != nil {
		t.Fatalf("write managed path: %v", err)
	}

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(managed []DiffPreview, memory []DiffPreview) (bool, bool, error) {
				if len(managed) == 0 {
					t.Fatal("expected managed previews for unified prompt")
				}
				return true, false, nil
			},
		},
	}

	overwriteAll, err := inst.shouldOverwriteAllManaged()
	if err != nil {
		t.Fatalf("shouldOverwriteAllManaged: %v", err)
	}
	if !overwriteAll {
		t.Fatal("expected overwrite-all managed decision from unified prompt")
	}
	if !inst.overwriteAllDecided || !inst.overwriteMemoryAllDecided {
		t.Fatal("expected unified resolution to cache both decisions")
	}
}

func TestShouldOverwriteAllMemory_UsesUnifiedDecision(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		t.Fatalf("mkdir memory path: %v", err)
	}
	if err := os.WriteFile(memoryPath, []byte("<!-- ENTRIES START -->\nlocal memory entry\n"), 0o644); err != nil {
		t.Fatalf("write memory path: %v", err)
	}

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func(managed []DiffPreview, memory []DiffPreview) (bool, bool, error) {
				if len(memory) == 0 {
					t.Fatal("expected memory previews for unified prompt")
				}
				return false, true, nil
			},
		},
	}

	overwriteAll, err := inst.shouldOverwriteAllMemory()
	if err != nil {
		t.Fatalf("shouldOverwriteAllMemory: %v", err)
	}
	if !overwriteAll {
		t.Fatal("expected overwrite-all memory decision from unified prompt")
	}
	if !inst.overwriteAllDecided || !inst.overwriteMemoryAllDecided {
		t.Fatal("expected unified resolution to cache both decisions")
	}
}

func TestShouldOverwriteAllManaged_UnifiedResolutionErrorPropagates(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(managedPath, 0o755); err != nil {
		t.Fatalf("mkdir managed path directory: %v", err)
	}

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified callback when diff preview build fails")
				return false, false, nil
			},
		},
	}

	_, err := inst.shouldOverwriteAllManaged()
	if err == nil {
		t.Fatal("expected unified managed resolution error")
	}
}

func TestShouldOverwriteAllMemory_UnifiedResolutionErrorPropagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified callback when memory diff discovery fails")
				return false, false, nil
			},
		},
	}

	_, err := inst.shouldOverwriteAllMemory()
	if err == nil {
		t.Fatal("expected unified memory resolution error")
	}
}

func TestLookupDiffPreview_ManagedTemplateMappingError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk failed")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview(".agent-layer/commands.allow"); err == nil {
		t.Fatal("expected managed template mapping error")
	}
}

func TestShouldOverwrite_PropagatesLookupDiffPreviewError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:                root,
		overwrite:           true,
		overwriteAllDecided: true,
		overwriteAll:        false,
		sys:                 RealSystem{},
		prompter: PromptFuncs{
			OverwritePreviewFunc: func(DiffPreview) (bool, error) { return false, nil },
		},
	}

	if _, err := inst.shouldOverwrite(filepath.Join(root, ".agent-layer", "unknown.txt")); err == nil {
		t.Fatal("expected lookup diff preview error")
	}
}

func TestShouldOverwrite_PropagatesUnifiedResolutionError(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("template preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified prompt callback when managed preview build fails")
				return false, false, nil
			},
		},
	}

	if _, err := inst.shouldOverwrite(managedPath); err == nil || !strings.Contains(err.Error(), "template preview boom") {
		t.Fatalf("expected unified resolution error, got %v", err)
	}
}

func TestResolveUnifiedOverwriteAllDecisions_BuildManagedDiffPreviewsError(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("managed preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified prompt callback when managed preview build fails")
				return false, false, nil
			},
		},
	}

	if err := inst.resolveUnifiedOverwriteAllDecisions(); err == nil || !strings.Contains(err.Error(), "managed preview boom") {
		t.Fatalf("expected managed preview build error, got %v", err)
	}
}

func TestResolveUnifiedOverwriteAllDecisions_ListMemoryLabeledDiffsError(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(memoryPath, []byte("# Issues\n\n<!-- ENTRIES START -->\nlocal\n"), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	originalRead := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "docs/agent-layer/ISSUES.md" {
			return nil, errors.New("memory classify boom")
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified prompt callback when memory classification fails")
				return false, false, nil
			},
		},
	}

	if err := inst.resolveUnifiedOverwriteAllDecisions(); err == nil || !strings.Contains(err.Error(), "memory classify boom") {
		t.Fatalf("expected memory labeled diff error, got %v", err)
	}
}

func TestResolveUnifiedOverwriteAllDecisions_BuildMemoryDiffPreviewsError(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(memoryPath, []byte("# Issues\n\n<!-- ENTRIES START -->\nlocal\n"), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "docs/agent-layer/ISSUES.md" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("memory preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
				t.Fatal("did not expect unified prompt callback when memory preview build fails")
				return false, false, nil
			},
		},
	}

	if err := inst.resolveUnifiedOverwriteAllDecisions(); err == nil || !strings.Contains(err.Error(), "memory preview boom") {
		t.Fatalf("expected memory preview build error, got %v", err)
	}
}

func TestShouldOverwriteAllManaged_BuildManagedDiffPreviewsError(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("managed preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc: func([]DiffPreview) (bool, error) {
				t.Fatal("did not expect overwrite-all prompt when managed preview build fails")
				return false, nil
			},
		},
	}

	if _, err := inst.shouldOverwriteAllManaged(); err == nil || !strings.Contains(err.Error(), "managed preview boom") {
		t.Fatalf("expected managed preview build error, got %v", err)
	}
}

func TestShouldOverwriteAllMemory_BuildMemoryDiffPreviewsError(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(memoryPath, []byte("# Issues\n\n<!-- ENTRIES START -->\nlocal\n"), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "docs/agent-layer/ISSUES.md" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("memory preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) {
				t.Fatal("did not expect overwrite-all-memory prompt when memory preview build fails")
				return false, nil
			},
		},
	}

	if _, err := inst.shouldOverwriteAllMemory(); err == nil || !strings.Contains(err.Error(), "memory preview boom") {
		t.Fatalf("expected memory preview build error, got %v", err)
	}
}

func TestLookupDiffPreview_MemoryTemplatePathByRelError(t *testing.T) {
	root := t.TempDir()
	originalWalk := templates.WalkFunc
	walkCalls := 0
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		walkCalls++
		if walkCalls == 2 {
			return errors.New("memory walk boom")
		}
		return originalWalk(root, fn)
	}
	t.Cleanup(func() { templates.WalkFunc = originalWalk })

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview("docs/agent-layer/ISSUES.md"); err == nil || !strings.Contains(err.Error(), "memory walk boom") {
		t.Fatalf("expected memory template-path mapping error, got %v", err)
	}
}

func TestLookupDiffPreview_BuildSingleDiffPreviewError(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("local override\n"), 0o644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	originalRead := templates.ReadFunc
	readCalls := 0
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			readCalls++
			if readCalls == 2 {
				return nil, errors.New("managed preview boom")
			}
		}
		return originalRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.lookupDiffPreview(".agent-layer/commands.allow"); err == nil || !strings.Contains(err.Error(), "managed preview boom") {
		t.Fatalf("expected build single preview error, got %v", err)
	}
}
