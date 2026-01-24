package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCommandsAllow(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "commands.allow", `
# comment
 git status

ls
`)

	got, err := LoadCommandsAllow(path)
	if err != nil {
		t.Fatalf("LoadCommandsAllow returned error: %v", err)
	}

	want := []string{"git status", "ls"}
	if len(got) != len(want) {
		t.Fatalf("expected %d commands, got %d", len(want), len(got))
	}
	for i, cmd := range want {
		if got[i] != cmd {
			t.Fatalf("expected command %d to be %q, got %q", i, cmd, got[i])
		}
	}
}

func TestLoadCommandsAllowMissing(t *testing.T) {
	_, err := LoadCommandsAllow(filepath.Join(t.TempDir(), "missing.allow"))
	if err == nil {
		t.Fatalf("expected error for missing allowlist")
	}
	if !strings.Contains(err.Error(), "missing commands allowlist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadCommandsAllowScannerError(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("a", 70*1024)
	path := writeTempFile(t, dir, "commands.allow", longLine)

	_, err := LoadCommandsAllow(path)
	if err == nil {
		t.Fatalf("expected scanner error")
	}
	if !strings.Contains(err.Error(), "failed to read commands allowlist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTempFile(t *testing.T, dir string, name string, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
