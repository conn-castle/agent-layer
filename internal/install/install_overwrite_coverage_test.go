package install

import (
	"errors"
	"io/fs"
	"path/filepath"
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
