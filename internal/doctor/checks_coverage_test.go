package doctor

import (
	"path/filepath"
	"testing"
)

func TestRelPathForDoctor_EmptyRoot(t *testing.T) {
	got := relPathForDoctor("", "/some/path")
	if got != "/some/path" {
		t.Fatalf("expected /some/path, got %s", got)
	}
}

func TestRelPathForDoctor_WhitespaceRoot(t *testing.T) {
	got := relPathForDoctor("   ", "/some/path")
	if got != "/some/path" {
		t.Fatalf("expected /some/path, got %s", got)
	}
}

func TestRelPathForDoctor_SuccessfulRelPath(t *testing.T) {
	got := relPathForDoctor("/root/project", filepath.Join("/root/project", "sub", "file.md"))
	if got != "sub/file.md" {
		t.Fatalf("expected sub/file.md, got %s", got)
	}
}

func TestRelPathForDoctor_UnrelatedPathFallback(t *testing.T) {
	// On Unix filepath.Rel can still compute a relative path even for
	// seemingly unrelated paths (../../../...), so there is no error case
	// on Unix. Verify that the function at least returns without error.
	got := relPathForDoctor("/root/a", "/other/b")
	if got == "" {
		t.Fatal("expected non-empty result")
	}
}
