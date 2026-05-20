package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateSingleFlatSkill_FlatNotFound(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "missing.md")
	destDir := filepath.Join(dir, "missing")
	destPath := filepath.Join(destDir, "SKILL.md")

	migrated, err := migrateSingleFlatSkill(RealSystem{}, flatPath, destDir, destPath)
	if err != nil {
		t.Fatalf("expected no error for missing flat file, got %v", err)
	}
	if migrated {
		t.Fatal("expected migrated=false for missing flat file")
	}
}

func TestMigrateSingleFlatSkill_FlatStatError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "bad.md")
	destDir := filepath.Join(dir, "bad")
	destPath := filepath.Join(destDir, "SKILL.md")

	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(flatPath)] = errors.New("stat boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected stat boom, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_DestStatError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "skill.md")
	if err := os.WriteFile(flatPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(dir, "skill")
	destPath := filepath.Join(destDir, "SKILL.md")

	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(destPath)] = errors.New("dest stat boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || !strings.Contains(err.Error(), "dest stat boom") {
		t.Fatalf("expected dest stat boom, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_SameContentDedup(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: alpha\ndescription: test\n---\nBody.\n"

	flatPath := filepath.Join(dir, "alpha.md")
	destDir := filepath.Join(dir, "alpha")
	destPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(flatPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateSingleFlatSkill(RealSystem{}, flatPath, destDir, destPath)
	if err != nil {
		t.Fatalf("expected no error for same content, got %v", err)
	}
	if !migrated {
		t.Fatal("expected migrated=true for dedup cleanup")
	}
	if _, err := os.Stat(flatPath); !os.IsNotExist(err) {
		t.Fatal("expected flat file to be removed after dedup")
	}
}

func TestMigrateSingleFlatSkill_DifferentContentConflict(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "alpha.md")
	destDir := filepath.Join(dir, "alpha")
	destPath := filepath.Join(destDir, "SKILL.md")

	if err := os.WriteFile(flatPath, []byte("flat content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("different content"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := migrateSingleFlatSkill(RealSystem{}, flatPath, destDir, destPath)
	if err == nil || err.Error() != "conflict: "+flatPath+" and "+destPath+" have different content" {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_RenameSuccess(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "beta.md")
	destDir := filepath.Join(dir, "beta")
	destPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(flatPath, []byte("skill content"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateSingleFlatSkill(RealSystem{}, flatPath, destDir, destPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !migrated {
		t.Fatal("expected migrated=true")
	}
	data, err := os.ReadFile(destPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "skill content" {
		t.Fatalf("expected 'skill content', got %q", string(data))
	}
}

func TestMigrateSingleFlatSkill_MkdirError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "gamma.md")
	destDir := filepath.Join(dir, "gamma")
	destPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(flatPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.mkdirErrs[normalizePath(destDir)] = errors.New("mkdir boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_RenameError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "delta.md")
	destDir := filepath.Join(dir, "delta")
	destPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(flatPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.renameErrs[normalizePath(flatPath)] = errors.New("rename boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected rename error, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_DedupRemoveError(t *testing.T) {
	dir := t.TempDir()
	content := "same content\n"
	flatPath := filepath.Join(dir, "epsilon.md")
	destDir := filepath.Join(dir, "epsilon")
	destPath := filepath.Join(destDir, "SKILL.md")

	if err := os.WriteFile(flatPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.removeErrs[normalizePath(flatPath)] = errors.New("remove boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected remove error, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_DedupReadFlatError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "zeta.md")
	destDir := filepath.Join(dir, "zeta")
	destPath := filepath.Join(destDir, "SKILL.md")

	if err := os.WriteFile(flatPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(flatPath)] = errors.New("read flat boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestMigrateSingleFlatSkill_DedupReadDestError(t *testing.T) {
	dir := t.TempDir()
	flatPath := filepath.Join(dir, "eta.md")
	destDir := filepath.Join(dir, "eta")
	destPath := filepath.Join(destDir, "SKILL.md")

	if err := os.WriteFile(flatPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(destPath)] = errors.New("read dest boom")

	_, err := migrateSingleFlatSkill(sys, flatPath, destDir, destPath)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected read error, got %v", err)
	}
}
