package run

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRunDir(t *testing.T) {
	root := t.TempDir()
	info, err := Create(root)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if info.ID == "" || info.Dir == "" {
		t.Fatalf("expected run info to be set")
	}
	if !strings.Contains(info.Dir, info.ID) {
		t.Fatalf("expected run dir to include id, got %s", info.Dir)
	}
	if _, err := os.Stat(info.Dir); err != nil {
		t.Fatalf("expected run dir to exist: %v", err)
	}
	expectedRoot := filepath.Join(root, ".agent-layer", "tmp", "runs")
	if !strings.HasPrefix(info.Dir, expectedRoot) {
		t.Fatalf("unexpected run dir: %s", info.Dir)
	}
}

func TestCreateRunDirMissingRoot(t *testing.T) {
	_, err := Create("")
	if err == nil {
		t.Fatalf("expected error for missing root")
	}
	if !strings.Contains(err.Error(), "root path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateRunDirMkdirError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := Create(file); err == nil {
		t.Fatalf("expected error")
	}
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestRandomSuffixError(t *testing.T) {
	original := rand.Reader
	rand.Reader = errReader{}
	defer func() {
		rand.Reader = original
	}()

	_, err := randomSuffix(4)
	if err == nil {
		t.Fatalf("expected error")
	}
}
