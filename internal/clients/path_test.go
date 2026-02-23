package clients

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath_EvalSymlinksSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved := ResolvePath(path)
	if resolved == "" {
		t.Fatalf("expected resolved path")
	}
}

func TestResolvePath_EvalSymlinksFailure(t *testing.T) {
	path := "/non-existent/path/xyz"
	resolved := ResolvePath(path)
	if resolved == "" {
		t.Fatalf("expected resolved path")
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute path")
	}
}

func TestResolvePath_AbsErrorReturnsOriginalPath(t *testing.T) {
	origAbs := filepathAbs
	origEval := filepathEvalSymlinks
	filepathAbs = func(string) (string, error) { return "", errors.New("abs failed") }
	filepathEvalSymlinks = func(path string) (string, error) { return path, nil }
	t.Cleanup(func() {
		filepathAbs = origAbs
		filepathEvalSymlinks = origEval
	})

	path := "relative-path"
	resolved := ResolvePath(path)
	if resolved != path {
		t.Fatalf("expected original path on Abs error, got %q", resolved)
	}
}

func TestResolvePath_EvalSymlinksErrorReturnsAbsPath(t *testing.T) {
	origAbs := filepathAbs
	origEval := filepathEvalSymlinks
	filepathAbs = func(string) (string, error) { return "/abs/path", nil }
	filepathEvalSymlinks = func(string) (string, error) { return "", errors.New("eval failed") }
	t.Cleanup(func() {
		filepathAbs = origAbs
		filepathEvalSymlinks = origEval
	})

	resolved := ResolvePath("ignored")
	if resolved != "/abs/path" {
		t.Fatalf("expected abs path on eval error, got %q", resolved)
	}
}

func TestSamePath_Identical(t *testing.T) {
	dir := t.TempDir()
	if !SamePath(dir, dir) {
		t.Fatalf("expected identical paths to match")
	}
}

func TestSamePath_Different(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if SamePath(a, b) {
		t.Fatalf("expected different paths to not match")
	}
}
