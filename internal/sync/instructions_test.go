package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildInstructionShim(t *testing.T) {
	t.Parallel()
	instructions := []config.InstructionFile{
		{Name: "00_base.md", Content: "base\n"},
		{Name: "10_extra.md", Content: "extra"},
	}
	content := buildInstructionShim(instructions)
	if !strings.Contains(content, "BEGIN: 00_base.md") {
		t.Fatalf("expected begin marker in content")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestWriteInstructionShims(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	if err := WriteInstructionShims(RealSystem{}, root, instructions); err != nil {
		t.Fatalf("WriteInstructionShims error: %v", err)
	}

	paths := []string{
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "GEMINI.md"),
		filepath.Join(root, ".github", "copilot-instructions.md"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestWriteCodexInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	if err := WriteCodexInstructions(RealSystem{}, root, instructions); err != nil {
		t.Fatalf("WriteCodexInstructions error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "AGENTS.md")); err != nil {
		t.Fatalf("expected codex instructions: %v", err)
	}
}

func TestWriteInstructionShimsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	if err := WriteInstructionShims(RealSystem{}, file, instructions); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCodexInstructionsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	if err := WriteCodexInstructions(RealSystem{}, file, instructions); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteInstructionShimsErrorPaths(t *testing.T) {
	t.Parallel()
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	cases := []struct {
		name  string
		setup func(root string) error
	}{
		{
			name: "agents write fails",
			setup: func(root string) error {
				return os.Mkdir(filepath.Join(root, "AGENTS.md"), 0o755)
			},
		},
		{
			name: "claude write fails",
			setup: func(root string) error {
				return os.Mkdir(filepath.Join(root, "CLAUDE.md"), 0o755)
			},
		},
		{
			name: "gemini write fails",
			setup: func(root string) error {
				return os.Mkdir(filepath.Join(root, "GEMINI.md"), 0o755)
			},
		},
		{
			name: "github mkdir fails",
			setup: func(root string) error {
				return os.WriteFile(filepath.Join(root, ".github"), []byte("x"), 0o644)
			},
		},
		{
			name: "copilot write fails",
			setup: func(root string) error {
				githubDir := filepath.Join(root, ".github")
				if err := os.Mkdir(githubDir, 0o755); err != nil {
					return err
				}
				return os.Mkdir(filepath.Join(githubDir, "copilot-instructions.md"), 0o755)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := tc.setup(root); err != nil {
				t.Fatalf("setup: %v", err)
			}
			if err := WriteInstructionShims(RealSystem{}, root, instructions); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}
