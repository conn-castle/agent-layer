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

func TestRunCreatesVSCodeLaunchers(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify VS Code launchers are created during init
	expectLaunchers := []string{
		filepath.Join(root, ".agent-layer", "open-vscode.command"),
		filepath.Join(root, ".agent-layer", "open-vscode.bat"),
		filepath.Join(root, ".agent-layer", "open-vscode.desktop"),
		filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents", "Info.plist"),
		filepath.Join(root, ".agent-layer", "open-vscode.app", "Contents", "MacOS", "open-vscode"),
	}
	for _, path := range expectLaunchers {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected VS Code launcher %s to exist: %v", path, err)
		}
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

	// Modify a file to differ from template.
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	// Second run without overwrite - should complete but record diff.
	if err := Run(root, Options{Overwrite: false, System: RealSystem{}}); err != nil {
		t.Fatalf("second Run error: %v", err)
	}

	// Verify file was not overwritten.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "# custom config" {
		t.Fatalf("expected custom config to remain, got %q", string(data))
	}
}

func TestRunWithOverwrite(t *testing.T) {
	root := t.TempDir()

	// First run to create structure.
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first Run error: %v", err)
	}

	// Modify a file to differ from template.
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	// Run with overwrite - should replace the file.
	if err := Run(root, Options{Overwrite: true, Force: true, System: RealSystem{}}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	// Verify file was overwritten with template content.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) == "# custom config" {
		t.Fatalf("expected config to be overwritten")
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
			OverwriteAllFunc:       func([]string) (bool, error) { return true, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return true, nil },
			OverwriteFunc:          func(string) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:   promptDeleteAll,
			DeleteUnknownFunc:      promptDelete,
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
			OverwriteAllFunc:       func([]string) (bool, error) { return true, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return true, nil },
			OverwriteFunc:          func(string) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:   func([]string) (bool, error) { return true, nil },
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
			OverwriteAllFunc:       func([]string) (bool, error) { return true, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:   func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:      func(string) (bool, error) { return true, nil },
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
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	var prompted []string
	prompt := func(path string) (bool, error) {
		prompted = append(prompted, path)
		return false, nil
	}

	if err := Run(root, Options{
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllFunc:       func([]string) (bool, error) { return false, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return false, nil },
			OverwriteFunc:          prompt,
			DeleteUnknownAllFunc:   func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:      func(string) (bool, error) { return true, nil },
		},
		System: RealSystem{},
	}); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "# custom config" {
		t.Fatalf("expected config to remain after declining prompt")
	}
	if len(prompted) != 1 || prompted[0] != filepath.Join(".agent-layer", "config.toml") {
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
			OverwriteAllFunc:       func([]string) (bool, error) { managedCalled = true; return false, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { memoryCalled = true; return false, nil },
			OverwriteFunc:          func(string) (bool, error) { return false, nil },
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

func TestListManagedDiffs_IgnoresGitignoreBlockHash(t *testing.T) {
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

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.WriteFile(configPath, []byte("# custom config"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	diffs, err := inst.listManagedDiffs()
	if err != nil {
		t.Fatalf("listManagedDiffs: %v", err)
	}
	if len(diffs) != 1 || diffs[0] != filepath.Join(".agent-layer", "config.toml") {
		t.Fatalf("unexpected diffs: %v", diffs)
	}
}

func TestWriteVersionFile_ExistingEmpty(t *testing.T) {
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
	if err == nil {
		t.Fatalf("expected error for empty existing pin file")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("unexpected error: %v", err)
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
		OverwriteFunc: func(string) (bool, error) {
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
			OverwriteFunc: func(string) (bool, error) {
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
			OverwriteFunc: func(string) (bool, error) {
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
			OverwriteAllFunc:       func([]string) (bool, error) { return true, nil },
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return true, nil },
			OverwriteFunc:          func(string) (bool, error) { return true, nil },
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
		PinVersion: "1.0.0",
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
