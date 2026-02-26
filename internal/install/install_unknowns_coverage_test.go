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

func TestScanUnknowns_BuildKnownPathsError_Propagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
	}
	if err := inst.scanUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestScanUnknownRoot_RootDoesNotExist_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root: root,
		sys:  RealSystem{},
	}

	err := inst.scanUnknownRoot(filepath.Join(root, ".agent-layer"), map[string]struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleUnknowns_MissingPrompter_ReturnsError(t *testing.T) {
	inst := &installer{
		overwrite: true,
		unknowns:  []string{"a"},
		prompter:  nil,
		sys:       RealSystem{},
	}

	if err := inst.handleUnknowns(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleUnknowns_DeleteAllTrue_DeletesUnknowns(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	unknownPath := filepath.Join(alDir, "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{
		root:      root,
		overwrite: true,
		unknowns:  []string{unknownPath},
		sys:       RealSystem{},
		prompter: PromptFuncs{
			DeleteUnknownAllFunc: func([]string) (bool, error) { return true, nil },
		},
	}

	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(unknownPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unknown path to be deleted, got %v", err)
	}
}

func TestBuildKnownPaths_SkillsWalkCallbackError_Propagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		switch root {
		case "instructions":
			return nil
		case "skills":
			return fn(root+"/bad", nil, errors.New("walk boom"))
		default:
			return nil
		}
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
	}
	_, err := inst.buildKnownPaths()
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected walk boom error, got %v", err)
	}
}

func TestBuildKnownPaths_DocsAgentLayerWalkError_Propagates(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		if root == "docs/agent-layer" {
			return errors.New("docs boom")
		}
		return nil
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
	}
	_, err := inst.buildKnownPaths()
	if err == nil || !strings.Contains(err.Error(), "docs boom") {
		t.Fatalf("expected docs boom error, got %v", err)
	}
}
