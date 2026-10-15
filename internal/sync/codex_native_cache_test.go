package sync

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLegacyCleanupPreservesCodexCacheInstalledDuringCleanup(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, ".codex", "skills")
	legacy := filepath.Join(skills, "old", "SKILL.md")
	cache := filepath.Join(skills, ".system", "native", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("retired"), 0o600); err != nil {
		t.Fatal(err)
	}
	install := func() {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cache, []byte("native bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sys := &MockSystem{Fallback: RealSystem{}}
	sys.ReadDirFunc = func(path string) ([]os.DirEntry, error) {
		entries, err := os.ReadDir(path)
		if path == skills {
			install()
		}
		return entries, err
	}
	sys.RemoveAllFunc = func(path string) error {
		if path == skills {
			install()
			return &os.PathError{Op: "remove", Path: path, Err: syscall.ENOTEMPTY}
		}
		return os.RemoveAll(path)
	}
	if err := cleanLegacySkillOutputs(sys, root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cache) // #nosec G304 -- test-owned native cache fixture.
	if err != nil || string(got) != "native bytes" {
		t.Fatalf("native cache removed: %q %v", got, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("retired skill remains: %v", err)
	}
	if err := cleanLegacySkillOutputs(RealSystem{}, root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(cache); err != nil || string(got) != "native bytes" { // #nosec G304 -- test-owned native cache fixture.
		t.Fatalf("retry removed cache: %q %v", got, err)
	}
}

func TestLegacyCodexCleanupDoesNotFollowRootSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("owned elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".codex", "skills")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := cleanLegacySkillOutputs(RealSystem{}, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy root link remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatalf("link target touched: %v", err)
	}
}
