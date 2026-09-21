package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestClaudeInstructionLinkMigrationAndUpdates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "CLAUDE.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(instructionHeader+"old generated copy"), 0o600))
	instructions := []config.InstructionFile{{Name: "rules.md", Content: "Initial guidance"}}
	require.NoError(t, writeInstructionShims(RealSystem{}, root, instructions))
	target, err := os.Readlink(path)
	require.NoError(t, err)
	require.Equal(t, "../AGENTS.md", target)
	before, err := os.Lstat(path)
	require.NoError(t, err)
	require.NoError(t, writeInstructionShims(RealSystem{}, root, instructions))
	after, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "no-op sync must preserve the link")

	for _, content := range []string{"Updated guidance", ""} {
		instructions = nil
		if content != "" {
			instructions = []config.InstructionFile{{Name: "rules.md", Content: content}}
		}
		require.NoError(t, writeInstructionShims(RealSystem{}, root, instructions))
		canonical, err := os.ReadFile(filepath.Join(root, "AGENTS.md")) // #nosec G304 -- test-owned canonical instruction path.
		require.NoError(t, err)
		claude, err := os.ReadFile(path) // #nosec G304 -- test-owned instruction path.
		require.NoError(t, err)
		require.Equal(t, canonical, claude)
		if content == "" {
			require.Empty(t, claude)
		} else {
			require.Contains(t, string(claude), content)
		}
	}
	// Older writers use an atomic regular-file replacement. It must replace
	// the link rather than write through it and corrupt canonical instructions.
	require.NoError(t, RealSystem{}.WriteFileAtomic(path, []byte("legacy copy"), 0o644))
	canonical, err := os.ReadFile(filepath.Join(root, "AGENTS.md")) // #nosec G304 -- test-owned canonical instruction path.
	require.NoError(t, err)
	require.Empty(t, canonical)
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
}

func TestClaudeInstructionsInRelocatedDirectory(t *testing.T) {
	t.Parallel()
	root, external := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(external, filepath.Join(root, ".claude")))
	require.NoError(t, writeInstructionShims(RealSystem{}, root, []config.InstructionFile{{Name: "rules.md", Content: "Project guidance"}}))
	path := filepath.Join(external, "CLAUDE.md")
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "a relative link in an external Claude tree would be broken")
	content, err := os.ReadFile(path) // #nosec G304 -- test-owned external directory.
	require.NoError(t, err)
	require.Contains(t, string(content), "Project guidance")
}

func TestClaudeInstructionPublicationFailurePreservesCopy(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"symlink", "rename"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".claude", "CLAUDE.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(instructionHeader+"previous guidance"), 0o600))
			failure := errors.New("injected publication failure")
			sys := &MockSystem{Fallback: RealSystem{}}
			if stage == "symlink" {
				sys.SymlinkFunc = func(_, _ string) error { return failure }
			} else {
				sys.RenameFunc = func(_, _ string) error { return failure }
			}
			require.ErrorIs(t, writeInstructionShims(sys, root, nil), failure)
			content, err := os.ReadFile(path) // #nosec G304 -- test-owned instruction path.
			require.NoError(t, err)
			require.Equal(t, instructionHeader+"previous guidance", string(content))
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1, "failed publication must not leave a staging link")
		})
	}
}

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
	if err := writeInstructionShims(RealSystem{}, root, instructions); err != nil {
		t.Fatalf("writeInstructionShims error: %v", err)
	}

	paths := []string{
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, ".claude", "CLAUDE.md"),
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

	if err := cleanCodexInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("cleanCodexInstructions error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected generated codex instructions to be removed, got %v", err)
	}
}

func TestCleanClaudeRootInstructionsRemovesGeneratedShim(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	instructions := []config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}
	path := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(buildInstructionShim(instructions)), 0o600); err != nil {
		t.Fatalf("write generated shim: %v", err)
	}

	if err := cleanClaudeRootInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("cleanClaudeRootInstructions error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected generated root CLAUDE.md to be removed, got %v", err)
	}
}

func TestCleanClaudeRootInstructionsPreservesUserAuthoredFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	content := []byte("# Personal Claude instructions\n\nMentions GENERATED FILE, Source: .agent-layer/, and Regenerate: al sync.\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write user instructions: %v", err)
	}

	if err := cleanClaudeRootInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("cleanClaudeRootInstructions error: %v", err)
	}
	got, err := os.ReadFile(path) // #nosec G304 -- path is test-controlled.
	if err != nil {
		t.Fatalf("read preserved user instructions: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("unexpected content after cleanup: %q", string(got))
	}
}

func TestCleanCodexInstructionsPreservesUserAuthoredFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "AGENTS.md")
	content := []byte("# Personal Codex home instructions\n\nMentions GENERATED FILE, Source: .agent-layer/, and Regenerate: al sync.\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write user instructions: %v", err)
	}

	if err := cleanCodexInstructions(RealSystem{}, root); err != nil {
		t.Fatalf("cleanCodexInstructions error: %v", err)
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
	if err := writeInstructionShims(RealSystem{}, file, instructions); err == nil {
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
			name: "claude dir mkdir fails",
			setup: func(root string) error {
				return os.WriteFile(filepath.Join(root, ".claude"), []byte("x"), 0o600)
			},
		},
		{
			name: "claude write fails",
			setup: func(root string) error {
				if err := os.Mkdir(filepath.Join(root, ".claude"), 0o700); err != nil {
					return err
				}
				return os.Mkdir(filepath.Join(root, ".claude", "CLAUDE.md"), 0o700)
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
			if err := writeInstructionShims(RealSystem{}, root, instructions); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestClaudeInstructionsPreserveUnmanagedPaths(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{"regular", "symlink", "relocated directory"} {
		for _, agentsState := range []string{"missing", "existing"} {
			t.Run(layout+"/"+agentsState, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, ".claude")
				if layout == "relocated directory" {
					require.NoError(t, os.Symlink(t.TempDir(), dir))
				} else {
					require.NoError(t, os.MkdirAll(dir, 0o700))
				}
				path := filepath.Join(dir, "CLAUDE.md")
				const guidance = "# Handwritten Claude guidance\n"
				if layout == "symlink" {
					external := filepath.Join(t.TempDir(), "instructions.md")
					require.NoError(t, os.WriteFile(external, []byte(guidance), 0o600))
					require.NoError(t, os.Symlink(external, path))
				} else {
					require.NoError(t, os.WriteFile(path, []byte(guidance), 0o600))
				}
				agentsPath := filepath.Join(root, "AGENTS.md")
				const priorAgents = "prior AGENTS.md\n"
				if agentsState == "existing" {
					require.NoError(t, os.WriteFile(agentsPath, []byte(priorAgents), 0o600))
				}
				before, err := os.Lstat(path)
				require.NoError(t, err)
				err = writeInstructionShims(RealSystem{}, root, []config.InstructionFile{{Name: "rules.md", Content: "Generated guidance"}})
				require.ErrorContains(t, err, "refusing to overwrite unmanaged Claude instructions")
				require.ErrorContains(t, err, ".agent-layer/instructions/")
				after, err := os.Lstat(path)
				require.NoError(t, err)
				require.True(t, os.SameFile(before, after))
				data, err := os.ReadFile(path) // #nosec G304 -- test-owned instruction path.
				require.NoError(t, err)
				require.Equal(t, guidance, string(data))
				if agentsState == "existing" {
					got, err := os.ReadFile(agentsPath) // #nosec G304 -- test-owned canonical instruction path.
					require.NoError(t, err)
					require.Equal(t, priorAgents, string(got))
					return
				}
				if _, err := os.Lstat(agentsPath); !os.IsNotExist(err) {
					t.Fatalf("unmanaged Claude destination must not create AGENTS.md, got %v", err)
				}
			})
		}
	}
}

func TestRemoveGeneratedInstructionShimPreservesNonRegularPaths(t *testing.T) {
	t.Parallel()
	generated := []byte(buildInstructionShim([]config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}))
	for _, name := range []string{"symlink to generated file", "directory"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "CLAUDE.md")
			if name == "directory" {
				require.NoError(t, os.Mkdir(path, 0o700))
			} else {
				target := filepath.Join(t.TempDir(), "generated.md")
				require.NoError(t, os.WriteFile(target, generated, 0o600))
				require.NoError(t, os.Symlink(target, path))
			}
			before, err := os.Lstat(path)
			require.NoError(t, err)
			require.NoError(t, removeGeneratedInstructionShim(RealSystem{}, path))
			after, err := os.Lstat(path)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			if name != "symlink to generated file" {
				return
			}
			got, err := os.ReadFile(path) // #nosec G304 -- test-owned symlink whose target is generated.
			require.NoError(t, err)
			require.Equal(t, generated, got)
		})
	}
}

func TestRemoveGeneratedInstructionShimInspectError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	content := []byte(buildInstructionShim([]config.InstructionFile{{Name: "00_base.md", Content: "base\n"}}))
	require.NoError(t, os.WriteFile(path, content, 0o600))
	failure := errors.New("injected lstat failure")
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if name == path {
				return nil, failure
			}
			return RealSystem{}.Lstat(name)
		},
	}
	err := removeGeneratedInstructionShim(sys, path)
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "inspect instruction shim")
	got, err := os.ReadFile(path) // #nosec G304 -- test-owned instruction path.
	require.NoError(t, err)
	require.Equal(t, content, got)
}
