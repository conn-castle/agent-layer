package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// writeSkillSource creates a minimal valid editable skill source directory.
func writeSkillSource(t *testing.T, root string, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	manifest := "---\nname: " + name + "\ndescription: Source skill.\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// TestWriteClaudeSkillsProjectsEveryRegularResource proves both source tiers
// project the same exhaustive resource set: hidden files are included, the
// executable bit survives, nested trees are preserved, and only the three
// named filesystem artifacts are ignored.
func TestWriteClaudeSkillsProjectsEveryRegularResource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "alpha")

	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- the executable bit is the behavior under test.
		t.Fatalf("write script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ".hidden-config"), []byte("hidden"), 0o600); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ".DS_Store"), []byte("junk"), 0o600); err != nil {
		t.Fatalf("write .DS_Store: %v", err)
	}

	skill := config.Skill{Name: "alpha", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteClaudeSkills: %v", err)
	}

	projected := filepath.Join(root, ".claude", "skills", "alpha")
	if data, err := os.ReadFile(filepath.Join(projected, ".hidden-config")); err != nil || string(data) != "hidden" { // #nosec G304 -- path is built from test-controlled temporary directories.
		t.Fatalf("hidden resource not projected: data=%q err=%v", data, err)
	}
	info, err := os.Stat(filepath.Join(projected, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("stat projected script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("projected script lost its executable bit: %v", info.Mode())
	}
	for _, ignored := range []string{".git", ".DS_Store"} {
		if _, err := os.Lstat(filepath.Join(projected, ignored)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be ignored, stat err = %v", ignored, err)
		}
	}
}

// TestWriteClaudeSkillsRejectsUnsafeImportedNode proves the imported tier is
// held to the strict node policy: an imported symlink fails projection instead
// of being silently skipped or dereferenced.
func TestWriteClaudeSkillsRejectsUnsafeImportedNode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "imported")
	if err := os.Symlink("/etc/hosts", filepath.Join(sourceDir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	skill := config.Skill{Name: "imported", Description: "Source skill.", SourceDir: sourceDir, Imported: true}
	err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill})
	if err == nil {
		t.Fatal("expected an imported symlink to fail projection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error %q does not name the rejected node type", err)
	}
}

// TestWriteClaudeSkillsSkipsUserManagedSymlink proves an existing user-managed
// source keeps the historical symlink skip so adding imports does not make
// ordinary sync newly fail for projects that already contain one.
func TestWriteClaudeSkillsSkipsUserManagedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "local")
	if err := os.Symlink("/etc/hosts", filepath.Join(sourceDir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	skill := config.Skill{Name: "local", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteClaudeSkills: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "skills", "local", "link")); !os.IsNotExist(err) {
		t.Fatalf("expected the user-managed symlink to be skipped, stat err = %v", err)
	}
}

// TestWriteClaudeSkillsPublicationRollsBackOnRenameFailure proves a failed
// publication leaves the previous complete tree in place rather than an empty
// or half-written skill directory.
func TestWriteClaudeSkillsPublicationRollsBackOnRenameFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "alpha")
	if err := os.WriteFile(filepath.Join(sourceDir, "note.md"), []byte("v1"), 0o600); err != nil {
		t.Fatalf("write resource: %v", err)
	}

	skill := config.Skill{Name: "alpha", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "note.md"), []byte("v2"), 0o600); err != nil {
		t.Fatalf("update resource: %v", err)
	}
	publishErr := errors.New("publication rename failed")
	target := filepath.Join(root, ".claude", "skills", "alpha")
	sys := &MockSystem{
		Fallback: RealSystem{},
		RenameFunc: func(oldpath string, newpath string) error {
			if newpath == target && strings.Contains(oldpath, stagingPrefix) {
				return publishErr
			}
			return os.Rename(oldpath, newpath)
		},
	}

	err := WriteClaudeSkills(sys, root, []config.Skill{skill})
	if !errors.Is(err, publishErr) {
		t.Fatalf("error = %v, want the publication failure", err)
	}
	data, readErr := os.ReadFile(filepath.Join(target, "note.md")) // #nosec G304 -- path is built from test-controlled temporary directories.
	if readErr != nil {
		t.Fatalf("previous projection was not restored: %v", readErr)
	}
	if string(data) != "v1" {
		t.Fatalf("restored resource = %q, want the previous complete tree", data)
	}
}

// TestWriteClaudeSkillsReplacesStaleResources proves publication swaps in the
// complete new tree, so a resource removed from the source disappears from the
// projection.
func TestWriteClaudeSkillsReplacesStaleResources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "alpha")
	if err := os.WriteFile(filepath.Join(sourceDir, "old.md"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	skill := config.Skill{Name: "alpha", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}
	if err := os.Remove(filepath.Join(sourceDir, "old.md")); err != nil {
		t.Fatalf("remove resource: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("stale resource survived publication, stat err = %v", err)
	}
}

// TestWriteClaudeSkillsRecoversAnInterruptedPublication proves projection is
// restart-safe. A process killed between the two publication renames leaves the
// only copy of the skill in its backup directory; clearing that backup before
// inspecting it would delete a complete projected skill and leave the reader
// with nothing.
func TestWriteClaudeSkillsRecoversAnInterruptedPublication(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "alpha")
	if err := os.WriteFile(filepath.Join(sourceDir, "note.md"), []byte("v1"), 0o600); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	skill := config.Skill{Name: "alpha", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}

	// Reproduce the crash state: the previous tree is parked in its backup and
	// nothing is at the target.
	skillsDir := filepath.Join(root, ".claude", "skills")
	target := filepath.Join(skillsDir, "alpha")
	backup := filepath.Join(skillsDir, backupPrefix+"alpha")
	if err := os.Rename(target, backup); err != nil {
		t.Fatalf("simulate interrupted publication: %v", err)
	}

	// A source that cannot be staged forces the next publication to stop before
	// it could replace the target, so only recovery can restore the skill.
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	if err := os.WriteFile(sourceDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace source with a file: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err == nil {
		t.Fatal("expected an unreadable source to fail projection")
	}

	data, err := os.ReadFile(filepath.Join(target, "note.md")) // #nosec G304 -- path is built from test-controlled temporary directories.
	if err != nil {
		t.Fatalf("the interrupted publication was not recovered: %v", err)
	}
	if string(data) != "v1" {
		t.Fatalf("recovered resource = %q, want the previous complete tree", data)
	}
}

// TestWriteClaudeSkillsDiscardsABackupWhenTheSwapCompleted proves recovery
// distinguishes an interrupted swap from an interrupted cleanup. A backup left
// beside a live target means publication already finished, so restoring it
// would replace the current projection with a stale tree.
func TestWriteClaudeSkillsDiscardsABackupWhenTheSwapCompleted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := writeSkillSource(t, t.TempDir(), "alpha")
	if err := os.WriteFile(filepath.Join(sourceDir, "note.md"), []byte("current"), 0o600); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	skill := config.Skill{Name: "alpha", Description: "Source skill.", SourceDir: sourceDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}

	skillsDir := filepath.Join(root, ".claude", "skills")
	backup := filepath.Join(skillsDir, backupPrefix+"alpha")
	if err := os.MkdirAll(backup, 0o750); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backup, "note.md"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale backup: %v", err)
	}

	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("second projection: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(skillsDir, "alpha", "note.md")) // #nosec G304 -- path is built from test-controlled temporary directories.
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if string(data) != "current" {
		t.Fatalf("projection = %q, want the current tree rather than the stale backup", data)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("the stale backup survived: %v", err)
	}
}
