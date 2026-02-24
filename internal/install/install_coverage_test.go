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

func TestRun_OverwriteScanUnknownsError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	fs.walkErrs[normalizePath(filepath.Join(root, ".agent-layer"))] = errors.New("walk boom")

	err := Run(root, Options{
		Overwrite: true,
		Prompter:  autoApprovePrompter(),
		System:    fs,
	})
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected scan unknowns error, got %v", err)
	}
}

func TestRun_OverwritePrepareMigrationsError_Propagates(t *testing.T) {
	root := t.TempDir()

	err := Run(root, Options{
		Overwrite:  true,
		Prompter:   autoApprovePrompter(),
		PinVersion: "9.9.9",
		System:     RealSystem{},
	})
	if err == nil || !strings.Contains(err.Error(), "missing migration manifest") {
		t.Fatalf("expected missing migration manifest error, got %v", err)
	}
}

func TestRun_OverwriteCreateUpgradeSnapshotError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	snapshotDir := filepath.Join(root, ".agent-layer", "state", "upgrade-snapshots")
	fs.mkdirErrs[normalizePath(snapshotDir)] = errors.New("snapshot mkdir boom")

	err := Run(root, Options{
		Overwrite: true,
		Prompter:  autoApprovePrompter(),
		System:    fs,
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot mkdir boom") {
		t.Fatalf("expected create snapshot error, got %v", err)
	}
}

func TestRun_NonOverwriteScanUnknownsError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	fs.walkErrs[normalizePath(filepath.Join(root, ".agent-layer"))] = errors.New("walk boom")

	err := Run(root, Options{System: fs})
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected scan unknowns error, got %v", err)
	}
}

func TestRun_NonOverwriteRunStepsError_Propagates(t *testing.T) {
	root := t.TempDir()
	fs := newFaultSystem(RealSystem{})
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	fs.writeErrs[normalizePath(configPath)] = errors.New("write config boom")

	err := Run(root, Options{System: fs})
	if err == nil || !strings.Contains(err.Error(), "write config boom") {
		t.Fatalf("expected run steps error, got %v", err)
	}
}

func TestRunWithOverwrite_AppliedSnapshotWriteFailure_Propagates(t *testing.T) {
	root := t.TempDir()
	sys := &snapshotWriteFailOnNthSystem{
		base:     RealSystem{},
		failOn:   2,
		failErr:  errors.New("snapshot write boom"),
		failRoot: root,
	}

	err := Run(root, Options{
		Overwrite: true,
		Prompter:  autoApprovePrompter(),
		System:    sys,
	})
	if err == nil || !strings.Contains(err.Error(), "mark upgrade snapshot") || !strings.Contains(err.Error(), "snapshot write boom") {
		t.Fatalf("expected applied snapshot write failure, got %v", err)
	}
}

func TestRunWithOverwrite_RollbackSucceededSnapshotWriteFailure_Propagates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	unknownPath := filepath.Join(root, ".agent-layer", "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write unknown file: %v", err)
	}

	sys := &snapshotWriteFailOnNthSystem{
		base:     RealSystem{},
		failOn:   2,
		failErr:  errors.New("snapshot write boom"),
		failRoot: root,
	}

	err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:          func([]string) (bool, error) { return false, nil },
			DeleteUnknownFunc:             func(string) (bool, error) { return false, errors.New("delete prompt boom") },
		},
		System: sys,
	})
	if err == nil || !strings.Contains(err.Error(), "rollback succeeded; failed to write snapshot state") {
		t.Fatalf("expected rollback-success snapshot write failure, got %v", err)
	}
}

func TestRunWithOverwrite_RollbackFailedSnapshotWriteFailure_Propagates(t *testing.T) {
	root := t.TempDir()
	base := newFaultSystem(RealSystem{})
	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	base.writeErrs[normalizePath(managedPath)] = errors.New("write managed boom")
	base.removeErrs[normalizePath(managedPath)] = errors.New("rollback remove boom")

	sys := &snapshotWriteFailOnNthSystem{
		base:     base,
		failOn:   2,
		failErr:  errors.New("snapshot write boom"),
		failRoot: root,
	}

	err := Run(root, Options{
		Overwrite: true,
		Prompter:  autoApprovePrompter(),
		System:    sys,
	})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "failed to write snapshot state") {
		t.Fatalf("expected rollback-failed snapshot write failure, got %v", err)
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

type snapshotWriteFailOnNthSystem struct {
	base     System
	failOn   int
	failErr  error
	failRoot string
	writes   int
}

func (s *snapshotWriteFailOnNthSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *snapshotWriteFailOnNthSystem) Lstat(name string) (os.FileInfo, error) {
	return s.base.Lstat(name)
}

func (s *snapshotWriteFailOnNthSystem) ReadFile(name string) ([]byte, error) {
	return s.base.ReadFile(name)
}

func (s *snapshotWriteFailOnNthSystem) Readlink(name string) (string, error) {
	return s.base.Readlink(name)
}

func (s *snapshotWriteFailOnNthSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
}

func (s *snapshotWriteFailOnNthSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s *snapshotWriteFailOnNthSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s *snapshotWriteFailOnNthSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
}

func (s *snapshotWriteFailOnNthSystem) Symlink(oldname string, newname string) error {
	return s.base.Symlink(oldname, newname)
}

func (s *snapshotWriteFailOnNthSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return s.base.WalkDir(root, fn)
}

func (s *snapshotWriteFailOnNthSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	snapshotPrefix := filepath.ToSlash(filepath.Join(s.failRoot, ".agent-layer", "state", "upgrade-snapshots")) + "/"
	if strings.HasPrefix(filepath.ToSlash(normalizePath(filename)), snapshotPrefix) {
		s.writes++
		if s.writes == s.failOn {
			return s.failErr
		}
	}
	return s.base.WriteFileAtomic(filename, data, perm)
}
