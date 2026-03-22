package sync

import (
	"errors"
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

func TestTrustReadError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	e := &trustReadError{err: inner}

	if e.Error() != "inner error" {
		t.Fatalf("Error() = %q, want 'inner error'", e.Error())
	}
	if !errors.Is(e, inner) {
		t.Fatal("expected Unwrap to return inner error")
	}
	unwrapped := e.Unwrap()
	if unwrapped != inner {
		t.Fatalf("Unwrap() = %v, want %v", unwrapped, inner)
	}
}

func TestRealSystem_MkdirAll(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	if err := sys.MkdirAll(dir+"/sub/dir", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}

func TestRealSystem_ReadDir(t *testing.T) {
	sys := RealSystem{}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0o644); err != nil {
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
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
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
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sys.RemoveAll(sub); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed")
	}
}
