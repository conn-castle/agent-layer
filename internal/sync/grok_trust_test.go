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
