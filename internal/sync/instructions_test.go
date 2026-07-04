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
		filepath.Join(root, ".github", "copilot-instructions.md"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	// GEMINI.md must NOT be generated post-0.10.2 — the Gemini CLI is
	// retired and agy reads AGENTS.md. The v0.10.2 migration cleans up
	// any stale GEMINI.md from earlier installs.
	if _, err := os.Stat(filepath.Join(root, "GEMINI.md")); err == nil {
		t.Fatal("GEMINI.md must not be generated post-Gemini removal")
	}
}

func TestCleanCodexInstructionsRemovesGeneratedShim(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	path := filepath.Join(root, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	if err := os.WriteFile(path, []byte(buildInstructionShim(instructions)), 0o600); err != nil {
		t.Fatalf("write generated shim: %v", err)
	}

	if err := CleanCodexInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("CleanCodexInstructions error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected generated codex instructions to be removed, got %v", err)
	}
}

func TestCleanCodexInstructionsPreservesUserAuthoredFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "AGENTS.md")
	content := []byte("# Personal Codex home instructions\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write user instructions: %v", err)
	}

	if err := CleanCodexInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("CleanCodexInstructions error: %v", err)
	}
	got, err := os.ReadFile(path) // #nosec G304 -- path is test-controlled.
	if err != nil {
		t.Fatalf("read preserved user instructions: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("unexpected content after cleanup: %q", string(got))
	}
}

func TestWriteInstructionShimsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	if err := WriteInstructionShims(RealSystem{}, file, instructions); err == nil {
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
				return os.Mkdir(filepath.Join(root, "AGENTS.md"), 0o700)
			},
		},
		{
			name: "claude write fails",
			setup: func(root string) error {
				return os.Mkdir(filepath.Join(root, "CLAUDE.md"), 0o700)
			},
		},
		{
			name: "github mkdir fails",
			setup: func(root string) error {
				return os.WriteFile(filepath.Join(root, ".github"), []byte("x"), 0o600)
			},
		},
		{
			name: "copilot write fails",
			setup: func(root string) error {
				githubDir := filepath.Join(root, ".github")
				if err := os.Mkdir(githubDir, 0o700); err != nil {
					return err
				}
				return os.Mkdir(filepath.Join(githubDir, "copilot-instructions.md"), 0o700)
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
