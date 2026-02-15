package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestRun_NilSystem_ReturnsError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_RunStepsError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	fs.mkdirErrs[normalizePath(filepath.Join(root, ".agent-layer", "instructions"))] = errors.New("mkdir boom")

	if err := Run(root, Options{System: fs}); err == nil || !strings.Contains(err.Error(), "mkdir boom") {
		t.Fatalf("expected mkdir boom error, got %v", err)
	}
}

func TestRun_WriteManagedBaselineIfConsistentError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	fs.writeErrs[normalizePath(statePath)] = errors.New("write boom")

	if err := Run(root, Options{System: fs}); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("expected write boom error, got %v", err)
	}
}

func TestValidatePrompter_MissingOverwriteAll_ReturnsError(t *testing.T) {
	if err := validatePrompter(PromptFuncs{}, true); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidatePrompter_MissingOverwriteAllMemory_ReturnsError(t *testing.T) {
	if err := validatePrompter(PromptFuncs{OverwriteAllPreviewFunc: func([]DiffPreview) (bool, error) { return false, nil }}, true); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteTemplateFiles_AgentOnlyAlwaysOverwriteCalled_AndErrorPropagates(t *testing.T) {
	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Ensure the agent-only file exists and differs so the alwaysOverwrite closure is invoked.
	gitignorePath := filepath.Join(alDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("# custom\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fs := newFaultSystem(RealSystem{})
	fs.writeErrs[normalizePath(gitignorePath)] = errors.New("write boom")

	inst := &installer{
		root: root,
		sys:  fs,
	}

	err := inst.writeTemplateFiles()
	if err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("expected write boom error, got %v", err)
	}
}

func TestWriteTemplateFiles_ManagedTemplateReadError_Propagates(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == "commands.allow" {
			return nil, errors.New("template boom")
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	inst := &installer{
		root: t.TempDir(),
		sys:  RealSystem{},
	}

	err := inst.writeTemplateFiles()
	if err == nil || !strings.Contains(err.Error(), "template boom") {
		t.Fatalf("expected template boom error, got %v", err)
	}
}

func TestWriteVersionFile_ExistingNormalizedEqualsTarget_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{
		root:       root,
		pinVersion: "1.0.0",
		sys:        RealSystem{},
	}

	if err := inst.writeVersionFile(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteVersionFile_MkdirAllError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	fs.mkdirErrs[normalizePath(filepath.Join(root, ".agent-layer"))] = errors.New("mkdir boom")

	inst := &installer{
		root:       root,
		pinVersion: "1.0.0",
		sys:        fs,
	}

	err := inst.writeVersionFile()
	if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
		t.Fatalf("expected mkdir boom error, got %v", err)
	}
}
