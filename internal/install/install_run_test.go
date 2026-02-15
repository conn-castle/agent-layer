package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestRunCreatesStructure(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	expectFiles := []string{
		filepath.Join(root, ".agent-layer", "config.toml"),
		filepath.Join(root, ".agent-layer", "commands.allow"),
		filepath.Join(root, ".agent-layer", ".env"),
		filepath.Join(root, ".agent-layer", ".gitignore"),
		filepath.Join(root, ".agent-layer", "gitignore.block"),
		filepath.Join(root, "docs", "agent-layer", "BACKLOG.md"),
		filepath.Join(root, "docs", "agent-layer", "ISSUES.md"),
	}
	for _, path := range expectFiles {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(data), gitignoreStart) {
		t.Fatalf("expected gitignore block to be present")
	}
}

func TestRunWritesPinVersion(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{PinVersion: "0.5.0", System: RealSystem{}}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	path := filepath.Join(root, ".agent-layer", "al.version")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin file: %v", err)
	}
	if string(data) != "0.5.0\n" {
		t.Fatalf("unexpected pin content: %q", string(data))
	}
}

func TestRunPinVersionDoesNotOverwriteWithoutFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("0.4.0\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}
	if err := Run(root, Options{PinVersion: "0.5.0", System: RealSystem{}}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin file: %v", err)
	}
	if string(data) != "0.4.0\n" {
		t.Fatalf("expected pin to remain unchanged")
	}
}

func TestRunRejectsInvalidPinVersion(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{PinVersion: "dev", System: RealSystem{}}); err == nil {
		t.Fatalf("expected error for invalid pin version")
	}
}

func TestCreateDirsError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	inst := &installer{root: file, sys: RealSystem{}}
	if err := inst.createDirs(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunStepsError(t *testing.T) {
	err := runSteps([]func() error{
		func() error { return fmt.Errorf("boom") },
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunMissingRoot(t *testing.T) {
	if err := Run("", Options{}); err == nil {
		t.Fatalf("expected error for missing root")
	}
}

func TestRunWithExistingDifferentFiles(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify a managed file to differ from template.
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("# custom allow\n"), 0o644); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	// Second run without overwrite - should complete but record diff.
	if err := Run(root, Options{Overwrite: false, System: RealSystem{}}); err != nil {
		t.Fatalf("second Run error: %v", err)
	}

	// Verify file was not overwritten.
	data, err := os.ReadFile(allowPath)
	if err != nil {
		t.Fatalf("read commands.allow: %v", err)
	}
	if string(data) != "# custom allow\n" {
		t.Fatalf("expected custom allowlist to remain, got %q", string(data))
	}
}

func TestRunWithOverwrite(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify user-owned files to differ from templates.
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}
	envPath := filepath.Join(root, ".agent-layer", ".env")
	if err := os.WriteFile(envPath, []byte("AL_EXAMPLE=custom\n"), 0o600); err != nil {
		t.Fatalf("write custom env: %v", err)
	}

	// Modify a managed file to differ from template.
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("# custom allow\n"), 0o644); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	// Run with overwrite - managed files should be replaced, user-owned files preserved.
	if err := Run(root, Options{Overwrite: true, Force: true, System: RealSystem{}}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	// Verify user-owned files were not overwritten.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "# custom config" {
		t.Fatalf("expected config to remain unchanged")
	}

	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if string(envData) != "AL_EXAMPLE=custom\n" {
		t.Fatalf("expected env to remain unchanged")
	}

	// Verify managed file was overwritten with template content.
	allowData, err := os.ReadFile(allowPath)
	if err != nil {
		t.Fatalf("read commands.allow: %v", err)
	}
	if string(allowData) == "# custom allow\n" {
		t.Fatalf("expected commands.allow to be overwritten")
	}
}

func TestRunWithOverwriteForceDeletesUnknowns(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	unknownPath := filepath.Join(root, ".agent-layer", "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	if err := Run(root, Options{Overwrite: true, Force: true, System: RealSystem{}}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	if _, err := os.Stat(unknownPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unknown file to be deleted, got %v", err)
	}
}

func TestRunWithOverwritePromptsUnknownDeletion(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	unknownPath := filepath.Join(root, ".agent-layer", "custom.txt")
	if err := os.WriteFile(unknownPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	var deleteAllPaths []string
	var deletePrompted []string
	promptDeleteAll := func(paths []string) (bool, error) {
		deleteAllPaths = append([]string(nil), paths...)
		return false, nil
	}
	promptDelete := func(path string) (bool, error) {
		deletePrompted = append(deletePrompted, path)
		return true, nil
	}

	if err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:          promptDeleteAll,
			DeleteUnknownFunc:             promptDelete,
		},
		System: RealSystem{},
	}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	if _, err := os.Stat(unknownPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unknown file to be deleted, got %v", err)
	}
	if len(deleteAllPaths) != 1 || deleteAllPaths[0] != filepath.Join(".agent-layer", "custom.txt") {
		t.Fatalf("unexpected delete-all paths: %v", deleteAllPaths)
	}
	if len(deletePrompted) != 1 || deletePrompted[0] != filepath.Join(".agent-layer", "custom.txt") {
		t.Fatalf("unexpected delete prompt paths: %v", deletePrompted)
	}
}

func TestRunWithOverwriteMissingPrompt(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{Overwrite: true, System: RealSystem{}}); err == nil {
		t.Fatalf("expected error when overwrite prompt handler is missing")
	}
}

func TestRunWithOverwriteMissingDeletePrompt(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
		},
		System: RealSystem{},
	}); err == nil {
		t.Fatalf("expected error when delete prompt handler is missing")
	}
}

func TestRunWithOverwriteMissingPerFilePrompt(t *testing.T) {
	root := t.TempDir()

	if err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
		},
		System: RealSystem{},
	}); err == nil {
		t.Fatalf("expected error when overwrite prompt handler is missing")
	}
}

func TestRunWithOverwritePromptDecline(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify a file to differ from template.
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("# custom allowlist\n"), 0o644); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	var prompted []string
	prompt := func(preview DiffPreview) (bool, error) {
		prompted = append(prompted, preview.Path)
		return false, nil
	}

	if err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return false, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return false, nil },
			OverwritePreviewFunc:          prompt,
			DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
		},
		System: RealSystem{},
	}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	data, err := os.ReadFile(allowPath)
	if err != nil {
		t.Fatalf("read commands.allow: %v", err)
	}
	if string(data) != "# custom allowlist\n" {
		t.Fatalf("expected allowlist to remain after declining prompt")
	}
	if len(prompted) != 1 || prompted[0] != filepath.Join(".agent-layer", "commands.allow") {
		t.Fatalf("unexpected prompt paths: %v", prompted)
	}
}

func TestShouldOverwrite_UsesMemoryPrompt(t *testing.T) {
	root := t.TempDir()
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")

	managedCalled := false
	memoryCalled := false
	inst := &installer{
		root:      root,
		overwrite: true,
		prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { managedCalled = true; return false, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { memoryCalled = true; return false, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return false, nil },
		},
		sys: RealSystem{},
	}

	_, err := inst.shouldOverwrite(memoryPath)
	if err != nil {
		t.Fatalf("shouldOverwrite error: %v", err)
	}
	if managedCalled {
		t.Fatalf("expected managed prompt not to be called for memory paths")
	}
	if !memoryCalled {
		t.Fatalf("expected memory prompt to be called for memory paths")
	}
}

func TestListManagedDiffs_ReportsGitignoreBlockHash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	rendered := renderGitignoreBlock(normalizeGitignoreBlock(string(templateBytes)))
	gitignorePath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(gitignorePath, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs, err := inst.listManagedDiffs()
	if err != nil {
		t.Fatalf("listManagedDiffs: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("unexpected diffs: %v", diffs)
	}
	expected := []string{
		filepath.Join(".agent-layer", "gitignore.block"),
	}
	for i, diff := range diffs {
		if diff != expected[i] {
			t.Fatalf("unexpected diffs: %v", diffs)
		}
	}
}

func TestWriteVersionFile_ExistingEmpty_AutoRepairs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write empty file
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	err := inst.writeVersionFile()
	if err != nil {
		t.Fatalf("expected auto-repair success, got error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if string(data) != "1.0.0\n" {
		t.Fatalf("expected 1.0.0, got %q", string(data))
	}
}

func TestWriteVersionFile_ExistingCorrupt_AutoRepairs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write corrupt (non-semver) content
	if err := os.WriteFile(path, []byte("not-a-version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	err := inst.writeVersionFile()
	if err != nil {
		t.Fatalf("expected auto-repair success, got error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if string(data) != "1.0.0\n" {
		t.Fatalf("expected 1.0.0, got %q", string(data))
	}
}

func TestWriteVersionFile_ExistingValid_RequiresOverwrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a valid but different version
	if err := os.WriteFile(path, []byte("0.9.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	err := inst.writeVersionFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without overwrite, pin should NOT be changed; should record diff instead
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if string(data) != "0.9.0\n" {
		t.Fatalf("expected pin to remain unchanged, got %q", string(data))
	}
	if len(inst.diffs) != 1 {
		t.Fatalf("expected 1 diff recorded, got %d", len(inst.diffs))
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

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
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
	inst.prompter = PromptFuncs{
		OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
			return true, nil
		},
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
		sys:        RealSystem{},
		prompter: PromptFuncs{
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
				return false, errors.New("prompt failed")
			},
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

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
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
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o755) })

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

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

	inst := &installer{root: root, pinVersion: "1.0.0", sys: RealSystem{}}
	err := inst.writeVersionFile()
	if err == nil {
		t.Fatalf("expected error for read failure")
	}
}

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
		sys:                 RealSystem{},
		prompter: PromptFuncs{
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
				return false, nil // Don't overwrite
			},
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

func TestRun_DeleteUnknownPromptRequired(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			OverwritePreviewFunc:          func(preview DiffPreview) (bool, error) { return true, nil },
			// Missing DeleteUnknownAllFunc
		},
		System: RealSystem{},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_InvalidPinVersion(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		PinVersion: "invalid-version",
		System:     RealSystem{},
	})
	if err == nil {
		t.Fatalf("expected error for invalid pin version")
	}
}

func TestRun_SuccessfulWithAllOptions(t *testing.T) {
	root := t.TempDir()
	err := Run(root, Options{
		Overwrite:  true,
		Force:      true,
		PinVersion: "0.7.0",
		System:     RealSystem{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_OverwriteExisting(t *testing.T) {
	root := t.TempDir()
	// First installation
	err := Run(root, Options{System: RealSystem{}})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second installation with overwrite
	err = Run(root, Options{
		Overwrite: true,
		Force:     true,
		System:    RealSystem{},
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestRun_SectionAwareOverwritePreservesUserEntries(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	issuesPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	initial, err := os.ReadFile(issuesPath)
	if err != nil {
		t.Fatalf("read issues: %v", err)
	}

	updated := strings.Replace(string(initial), "# Issues", "# Issues (local old header)", 1)
	updated += "\n- Issue 2099-01-01 localtest: keep me\n    Priority: Low. Area: tests.\n    Description: local entry.\n    Next step: keep.\n"
	if err := os.WriteFile(issuesPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write custom issues: %v", err)
	}

	if err := Run(root, Options{Overwrite: true, Force: true, System: RealSystem{}}); err != nil {
		t.Fatalf("overwrite run: %v", err)
	}

	finalContent, err := os.ReadFile(issuesPath)
	if err != nil {
		t.Fatalf("read final issues: %v", err)
	}
	finalText := string(finalContent)
	if strings.Contains(finalText, "# Issues (local old header)") {
		t.Fatalf("expected managed header to be restored from template")
	}
	if !strings.Contains(finalText, "localtest: keep me") {
		t.Fatalf("expected local user entry to be preserved below marker:\n%s", finalText)
	}
}

func TestRun_OverwriteAllDeclineFallsBackToPerFileDiffPreview(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	original, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed file: %v", err)
	}
	updated := string(original) + "\n# local customization\n"
	if err := os.WriteFile(managedPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write managed customization: %v", err)
	}

	batchPromptCalled := false
	perFilePromptCalled := false
	sawManagedInBatch := false

	opts := Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc: func(previews []DiffPreview) (bool, error) {
				batchPromptCalled = true
				for _, preview := range previews {
					if preview.Path != ".agent-layer/commands.allow" {
						continue
					}
					sawManagedInBatch = true
					if strings.TrimSpace(preview.UnifiedDiff) == "" {
						t.Fatalf("expected non-empty batch diff preview for %s", preview.Path)
					}
				}
				return false, nil
			},
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) {
				return false, nil
			},
			OverwritePreviewFunc: func(preview DiffPreview) (bool, error) {
				perFilePromptCalled = true
				if preview.Path != ".agent-layer/commands.allow" {
					t.Fatalf("unexpected per-file diff path: %s", preview.Path)
				}
				if strings.TrimSpace(preview.UnifiedDiff) == "" {
					t.Fatalf("expected non-empty per-file diff preview for %s", preview.Path)
				}
				return false, nil
			},
			DeleteUnknownAllFunc: func([]string) (bool, error) { return false, nil },
			DeleteUnknownFunc:    func(string) (bool, error) { return false, nil },
		},
		System: RealSystem{},
	}

	if err := Run(root, opts); err != nil {
		t.Fatalf("run with prompts: %v", err)
	}
	if !batchPromptCalled {
		t.Fatal("expected overwrite-all batch prompt to be called")
	}
	if !sawManagedInBatch {
		t.Fatal("expected overwrite-all batch prompt to include managed file preview")
	}
	if !perFilePromptCalled {
		t.Fatal("expected per-file prompt after declining overwrite-all batch prompt")
	}

	finalContent, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read final managed file: %v", err)
	}
	if string(finalContent) != updated {
		t.Fatalf("expected managed file to remain unchanged after declining prompts")
	}
}
