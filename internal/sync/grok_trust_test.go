package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

func TestWriteGrokTrustedFoldersSeedsMissingEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := writeGrokTrustedFolders(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokTrustedFolders: %v", err)
	}

	absRoot, err := grokTrustedFolderRoot(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	path := filepath.Join(root, ".grok-config", "trusted_folders.toml")
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "[folders."+tomlpatch.FormatKey(absRoot)+"]") {
		t.Fatalf("expected folder entry for %s, got:\n%s", absRoot, got)
	}
	if !strings.Contains(got, "trusted = true") {
		t.Fatalf("expected trusted = true, got:\n%s", got)
	}
	homeInfo, err := os.Stat(filepath.Join(root, ".grok-config"))
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := homeInfo.Mode().Perm(); gotMode != 0o700 {
		t.Fatalf("grok home mode = %04o, want 0700", gotMode)
	}
}

func TestGrokTrustedFolderRootResolvesSymlinks(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(parent, "linked")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}

	got, err := grokTrustedFolderRoot(linkedRoot)
	if err != nil {
		t.Fatalf("grokTrustedFolderRoot: %v", err)
	}
	want, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("trusted root = %q, want canonical path %q", got, want)
	}
}

func TestWriteGrokTrustedFoldersFailsLoudly(t *testing.T) {
	if err := writeGrokTrustedFolders(RealSystem{}, ""); err == nil || !strings.Contains(err.Error(), "repo root required") {
		t.Fatalf("empty root error = %v", err)
	}

	t.Run("malformed existing trust", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, ".grok-config")
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "trusted_folders.toml"), []byte("[folders\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokTrustedFolders(RealSystem{}, root); err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("malformed trust error = %v", err)
		}
	})

	t.Run("filesystem failures", func(t *testing.T) {
		injected := errors.New("injected")
		root := t.TempDir()

		readSystem := &MockSystem{Fallback: RealSystem{}, ReadFileFunc: func(string) ([]byte, error) { return nil, injected }}
		if err := writeGrokTrustedFolders(readSystem, root); !errors.Is(err, injected) {
			t.Fatalf("read error = %v", err)
		}

		writeSystem := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(string, []byte, os.FileMode) error { return injected }}
		if err := writeGrokTrustedFolders(writeSystem, root); !errors.Is(err, injected) {
			t.Fatalf("write error = %v", err)
		}
	})

	t.Run("symlinked home", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		outsideTrust := filepath.Join(outside, "trusted_folders.toml")
		const existing = "[folders.\"/outside\"]\ntrusted = true\n"
		if err := os.WriteFile(outsideTrust, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".grok-config")); err != nil {
			t.Fatal(err)
		}

		err := writeGrokTrustedFolders(RealSystem{}, root)
		if err == nil || !strings.Contains(err.Error(), "must be a real directory") {
			t.Fatalf("symlinked home error = %v", err)
		}
		data, readErr := os.ReadFile(outsideTrust) // #nosec G304 -- test-controlled path.
		if readErr != nil || string(data) != existing {
			t.Fatalf("outside trust changed: %q, %v", data, readErr)
		}
	})

	t.Run("broad home permissions", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, ".grok-config")
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(home, 0o755); err != nil { // #nosec G302 -- fixture starts too open so production can tighten it.
			t.Fatal(err)
		}

		if err := writeGrokTrustedFolders(RealSystem{}, root); err != nil {
			t.Fatalf("writeGrokTrustedFolders: %v", err)
		}
		homeInfo, err := os.Lstat(home)
		if err != nil {
			t.Fatal(err)
		}
		if gotMode := homeInfo.Mode().Perm(); gotMode != 0o700 {
			t.Fatalf("grok home mode = %04o, want 0700", gotMode)
		}
		if _, err := os.Stat(filepath.Join(home, "trusted_folders.toml")); err != nil {
			t.Fatalf("trust file missing after tightening home permissions: %v", err)
		}
	})

	t.Run("home is a file", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, ".grok-config")
		if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := writeGrokTrustedFolders(RealSystem{}, root)
		if err == nil || !strings.Contains(err.Error(), "must be a real directory") {
			t.Fatalf("file home error = %v", err)
		}
	})
}

func TestWriteGrokTrustedFoldersPreservesExistingEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absRoot, err := grokTrustedFolderRoot(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	home := filepath.Join(root, ".grok-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "[folders." + tomlpatch.FormatKey(absRoot) + "]\ntrusted = false\ndecided_at = 1\n"
	path := filepath.Join(home, "trusted_folders.toml")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	if err := writeGrokTrustedFolders(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokTrustedFolders: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}
	if string(data) != existing {
		t.Fatalf("expected existing trust entry to be preserved, got:\n%s", data)
	}
}

func TestWriteGrokTrustedFoldersAppendsOtherFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".grok-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(home, "trusted_folders.toml")
	existing := "[folders.\"/other/repo\"]\ntrusted = true\ndecided_at = 9\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	if err := writeGrokTrustedFolders(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokTrustedFolders: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `[folders."/other/repo"]`) || !strings.Contains(got, "trusted = true") {
		t.Fatalf("expected existing other folder preserved, got:\n%s", got)
	}
	absRoot, err := grokTrustedFolderRoot(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if !strings.Contains(got, "[folders."+tomlpatch.FormatKey(absRoot)+"]") {
		t.Fatalf("expected seeded repo root, got:\n%s", got)
	}
}

func TestWriteGrokHomeClaudeCompatSeedsMissingFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokHomeClaudeCompat: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".grok-config", "config.toml")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read home config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "[compat.claude]") || !strings.Contains(got, "agents = false") {
		t.Fatalf("expected Claude agents compat off, got:\n%s", got)
	}
	disabled, err := grokClaudeAgentsDisabled(data)
	if err != nil || !disabled {
		t.Fatalf("compat not applied: disabled=%v err=%v", disabled, err)
	}
}

func TestWriteGrokHomeClaudeCompatSetsExistingTrue(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".grok-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# keep me\n[ui]\ncompact_mode = true\n\n[compat.claude]\nskills = true\nagents = true\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokHomeClaudeCompat: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "# keep me") || !strings.Contains(got, "compact_mode = true") || !strings.Contains(got, "skills = true") {
		t.Fatalf("expected existing keys preserved, got:\n%s", got)
	}
	if strings.Contains(got, "agents = true") {
		t.Fatalf("expected agents = true to be replaced, got:\n%s", got)
	}
	disabled, err := grokClaudeAgentsDisabled(data)
	if err != nil || !disabled {
		t.Fatalf("compat not applied: disabled=%v err=%v\n%s", disabled, err, got)
	}
}

func TestWriteGrokHomeClaudeCompatUpdatesExistingInlineAndDottedForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name:     "inline table",
			existing: "# keep me\ncompat = { claude = { agents = true, skills = true } }\n\n[ui]\ncompact_mode = true\n",
			want:     []string{"skills = true"},
		},
		{
			name:     "dotted keys",
			existing: "# keep me\ncompat.claude.agents = true\ncompat.claude.skills = true\n\n[ui]\ncompact_mode = true\n",
			want:     []string{"skills = true"},
		},
		{
			name:     "dotted sibling without agents",
			existing: "# keep me\ncompat.claude.skills = true\n\n[ui]\ncompact_mode = true\n",
			want:     []string{"skills = true"},
		},
		{
			name:     "inline compat without claude",
			existing: "# keep me\ncompat = { other = true }\n\n[ui]\ncompact_mode = true\n",
			want:     []string{"other = true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			home := filepath.Join(root, ".grok-config")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "config.toml")
			if err := os.WriteFile(path, []byte(tt.existing), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err != nil {
				t.Fatalf("writeGrokHomeClaudeCompat: %v", err)
			}
			data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			if strings.Contains(got, "[compat.claude]") {
				t.Fatalf("appended [compat.claude] onto an existing compat.claude path, got:\n%s", got)
			}
			if !strings.Contains(got, "# keep me") || !strings.Contains(got, "compact_mode = true") {
				t.Fatalf("expected unrelated keys preserved, got:\n%s", got)
			}
			for _, fragment := range tt.want {
				if !strings.Contains(got, fragment) {
					t.Fatalf("expected %q preserved, got:\n%s", fragment, got)
				}
			}
			if strings.Contains(got, "agents = true") {
				t.Fatalf("expected agents = true to be replaced, got:\n%s", got)
			}
			disabled, err := grokClaudeAgentsDisabled(data)
			if err != nil || !disabled {
				t.Fatalf("compat not applied: disabled=%v err=%v\n%s", disabled, err, got)
			}
		})
	}
}

func TestWriteGrokHomeClaudeCompatAppendsWhenPathMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".grok-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# keep me\n[ui]\ncompact_mode = true\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err != nil {
		t.Fatalf("writeGrokHomeClaudeCompat: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "# keep me") || !strings.Contains(got, "compact_mode = true") {
		t.Fatalf("expected existing keys preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "[compat.claude]") || !strings.Contains(got, "agents = false") {
		t.Fatalf("expected appended Claude agents compat, got:\n%s", got)
	}
	disabled, err := grokClaudeAgentsDisabled(data)
	if err != nil || !disabled {
		t.Fatalf("compat not applied: disabled=%v err=%v\n%s", disabled, err, got)
	}
}

func TestWriteGrokHomeClaudeCompatNoopWhenAlreadyFalse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".grok-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "[compat.claude]\nagents = false\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected")
	sys := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(string, []byte, os.FileMode) error { return injected }}
	if err := writeGrokHomeClaudeCompat(sys, root); err != nil {
		t.Fatalf("expected no write when already false, got %v", err)
	}
}

func TestWriteGrokHomeClaudeCompatFailsLoudly(t *testing.T) {
	t.Parallel()
	t.Run("malformed existing config", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, ".grok-config")
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[compat.claude\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("malformed config error = %v", err)
		}
	})

	t.Run("symlink config", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, ".grok-config")
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(target, []byte("[compat.claude]\nagents = true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(home, "config.toml")); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokHomeClaudeCompat(RealSystem{}, root); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("symlink config error = %v", err)
		}
	})
}
