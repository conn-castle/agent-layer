package clients

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathSymlinkBeforeParentTraversal(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "child")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, leaf := range []string{"existing", "missing"} {
		want := filepath.Join(filepath.Dir(target), leaf)
		if leaf == "existing" {
			if err := os.Mkdir(want, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		value := "link/../" + leaf
		got := ResolveHome(value)
		if !filepath.IsAbs(got) || !SamePath(got, want) {
			t.Fatalf("%q resolved to %q, want %q", value, got, want)
		}
		if SamePath(got, filepath.Join(root, leaf)) {
			t.Fatalf("symlink/.. was cleaned lexically: %q", got)
		}
	}
}

func TestSamePathMissingLeafRootAlias(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if !SamePath(filepath.Join(root, ".codex"), filepath.Join(alias, ".codex")) {
		t.Fatal("missing leaf under alias did not match")
	}
	if SamePath(filepath.Join(root, ".codex"), filepath.Join(t.TempDir(), ".codex")) {
		t.Fatal("different parents matched")
	}
}

func TestSamePathCaseAliasesFollowFilesystem(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".codex")
	alias := filepath.Join(root, ".Codex")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(alias)
	if os.IsNotExist(err) {
		if SamePath(home, alias) {
			t.Fatal("missing case-only alias matched on case-sensitive filesystem")
		}
		if err := os.Mkdir(alias, 0o700); err != nil {
			t.Fatal(err)
		}
		if SamePath(home, alias) {
			t.Fatal("distinct case-only directories matched")
		}
		t.Run("case-insensitive identity", func(t *testing.T) { t.Skip("fixture filesystem is case-sensitive") })
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, info) || !SamePath(home, alias) {
		t.Fatal("filesystem case alias did not match")
	}
}

func TestSamePathMissingLeafUnderCaseAliasParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	alias := filepath.Join(root, "PARENT")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(alias)
	same := err == nil
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if got := SamePath(filepath.Join(parent, ".codex"), filepath.Join(alias, ".codex")); got != same {
		t.Fatalf("parent alias same=%t, missing leaf match=%t", same, got)
	}
}

func TestRelativeHomeFallbackDoesNotCleanParentTraversal(t *testing.T) {
	root := t.TempDir()
	caller := filepath.Join(root, "sub")
	if err := os.Mkdir(caller, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(caller)
	for _, name := range []string{".codex", ".claude-config"} {
		value := "missing/../" + name
		want := caller + "/" + value
		if got := ResolveRelativeHome(root, value, name); got != want {
			t.Fatalf("%q resolved to %q, want unresolved %q", value, got, want)
		}
	}
}
