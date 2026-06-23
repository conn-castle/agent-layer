package sync

import (
	"os"
	"testing"
)

func TestRealSystem_LookPath_NotFound(t *testing.T) {
	sys := RealSystem{}
	_, err := sys.LookPath("this-binary-should-not-exist-abc123xyz")
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
}

func TestRealSystem_MkdirAll(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	target := dir + "/sub/dir"
	if err := sys.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected created directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", target)
	}
}

func TestRealSystem_ReadDir(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := sys.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestRealSystem_Remove(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	path := dir + "/remove-me.txt"
	if err := os.WriteFile(path, []byte("bye"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sys.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}
}

func TestRealSystem_RemoveAll(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	sub := dir + "/sub"
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sys.RemoveAll(sub); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed")
	}
}
