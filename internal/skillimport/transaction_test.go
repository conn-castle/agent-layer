package skillimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
)

// newTestTransaction returns a transaction bound to a project's real paths.
func newTestTransaction(proj *project, lock *skilllock.File) *transaction {
	return newTransaction(pathSetFor(&state{paths: proj.paths}), lock)
}

// lockEntry returns a valid lock entry for a skill name so fixtures exercise
// the same state the lock parser accepts.
func lockEntry(name string) skilllock.Entry {
	return skilllock.Entry{
		Name:         name,
		Repository:   "https://example.invalid/skills.git",
		Selector:     "skills/" + name,
		SelectedPath: "skills/" + name,
		ResolvedRef:  "main",
		RefKind:      skilllock.RefKindBranch,
		Tracking:     skilllock.TrackingTracked,
		Commit:       strings.Repeat("a", 40),
		TreeHash:     "sha256:" + strings.Repeat("b", 64),
	}
}

// TestTransactionAppliesTreesConfigurationAndLockTogether proves one commit
// publishes imported content, rewrites configuration, and records lock state as
// a single observable change.
func TestTransactionAppliesTreesConfigurationAndLockTogether(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.WriteSkill("alpha", mustSkillTree(t, "alpha", "Alpha body"))
	txn.SetConfig(baseConfigTOML + "\n# edited by the transaction\n")
	txn.SetLockEntry(lockEntry("alpha"))

	if !txn.NeedsCommit(mustEmptyLock(), false) {
		t.Fatal("a transaction with pending work reported nothing to commit")
	}
	if err := txn.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("the skill tree was not published")
	}
	if !strings.Contains(proj.ConfigContent(), "edited by the transaction") {
		t.Fatal("configuration was not written")
	}
	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("lock state was not written")
	}
	// The staging area never survives a commit.
	if _, err := os.Stat(skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)); !os.IsNotExist(err) {
		t.Fatalf("staging directory survived the commit: %v", err)
	}
}

// TestTransactionRemovesAndRestoresTrees proves a recorded deletion is applied
// on commit and that the removal is reverted when a later step fails.
func TestTransactionRemovesAndRestoresTrees(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Alpha body"))

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.DeleteSkill("alpha")
	if err := txn.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("the recorded deletion was not applied")
	}

	// A deletion whose commit later fails is restored.
	proj.WriteImportedFile("beta", "SKILL.md", skillManifest("beta", "Beta body"))
	failing := newTestTransaction(proj, mustEmptyLock())
	failing.DeleteSkill("beta")
	failing.SetConfig("replacement")
	failing.writeFile = failingWriter(proj.paths.ConfigPath, false)

	if err := failing.Commit(); err == nil {
		t.Fatal("expected the commit to fail")
	}
	if !proj.ImportedExists("beta") {
		t.Fatal("a failed commit did not restore the removed skill")
	}
}

// TestTransactionRestoresConfigurationWhenLockWritingFails proves the last step
// failing does not leave a configuration change stranded without its lock.
func TestTransactionRestoresConfigurationWhenLockWritingFails(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := proj.ConfigContent()

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.SetConfig(original + "\n# edited\n")
	txn.SetLockEntry(lockEntry("alpha"))
	txn.writeFile = failingWriter(proj.paths.SkillsLockPath, false)

	if err := txn.Commit(); err == nil {
		t.Fatal("expected the lock write to fail")
	}
	if proj.ConfigContent() != original {
		t.Fatalf("configuration was not restored:\n%s", proj.ConfigContent())
	}
}

// TestTransactionRollsBackAPostRenameConfigurationDurabilityFailure proves the
// documented WriteFileAtomic failure window is handled: the new configuration
// is already visible when the write reports failure, so restoring it is the
// only thing that keeps configuration, trees, and lock at one generation.
func TestTransactionRollsBackAPostRenameConfigurationDurabilityFailure(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Original body"))
	original := proj.ConfigContent()

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.WriteSkill("alpha", mustSkillTree(t, "alpha", "Replacement body"))
	txn.SetConfig(original + "\n# edited\n")
	txn.SetLockEntry(lockEntry("alpha"))
	// The bytes land, then the durability step fails.
	txn.writeFile = failingWriter(proj.paths.ConfigPath, true)

	if err := txn.Commit(); err == nil {
		t.Fatal("expected a post-rename durability failure to fail the commit")
	}
	if proj.ConfigContent() != original {
		t.Fatalf("published configuration was not restored:\n%s", proj.ConfigContent())
	}
	if got := proj.ImportedFile("alpha", "SKILL.md"); !strings.Contains(got, "Original body") {
		t.Fatalf("the imported tree was not rolled back with configuration: %q", got)
	}
	if proj.Lock() != nil {
		t.Fatal("a rolled-back transaction left lock state behind")
	}
}

// TestTransactionRollsBackAPostRenameLockDurabilityFailure proves a lock file
// that is already published when its write reports failure is removed again,
// so no lock can describe trees the transaction rolled back.
func TestTransactionRollsBackAPostRenameLockDurabilityFailure(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Original body"))

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.WriteSkill("alpha", mustSkillTree(t, "alpha", "Replacement body"))
	txn.SetLockEntry(lockEntry("alpha"))
	txn.writeFile = failingWriter(proj.paths.SkillsLockPath, true)

	if err := txn.Commit(); err == nil {
		t.Fatal("expected a post-rename lock durability failure to fail the commit")
	}
	if proj.Lock() != nil {
		t.Fatal("a published lock survived a rolled-back transaction")
	}
	if got := proj.ImportedFile("alpha", "SKILL.md"); !strings.Contains(got, "Original body") {
		t.Fatalf("the imported tree was not rolled back with the lock: %q", got)
	}
}

// TestTransactionSurfacesARollbackFailure proves a rollback that cannot restore
// prior state is reported rather than reducing the operation to its original
// error, which would hide mixed generations on disk.
func TestTransactionSurfacesARollbackFailure(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := proj.ConfigContent()

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.SetConfig(original + "\n# edited\n")
	txn.SetLockEntry(lockEntry("alpha"))
	// The configuration write succeeds, the lock write fails, and restoring
	// configuration then fails too.
	txn.writeFile = func(path string, data []byte, perm os.FileMode) error {
		if path == proj.paths.SkillsLockPath {
			return fmt.Errorf("injected lock failure")
		}
		if path == proj.paths.ConfigPath && strings.Contains(string(data), "# edited") {
			return os.WriteFile(path, data, perm) // #nosec G306,G703 -- path is a fixture path this test owns; the mode mirrors the production permission.
		}
		return fmt.Errorf("injected rollback failure")
	}

	err := txn.Commit()
	if err == nil {
		t.Fatal("expected the commit to fail")
	}
	if !strings.Contains(err.Error(), "injected lock failure") {
		t.Fatalf("error %q does not report the original failure", err)
	}
	if !strings.Contains(err.Error(), "rolling the change back also failed") {
		t.Fatalf("error %q does not surface the rollback failure", err)
	}
}

// TestTransactionPendingStateIsVisibleToLaterStages proves an operation reads
// through the state it is building rather than the state it started from.
func TestTransactionPendingStateIsVisibleToLaterStages(t *testing.T) {
	t.Parallel()
	txn := newTransaction(pathSet{}, mustEmptyLock())
	tree := mustSkillTree(t, "alpha", "Alpha body")

	txn.WriteSkill("alpha", tree)
	if staged, ok := txn.PendingTree("alpha"); !ok || !staged.Equal(tree) {
		t.Fatal("a staged write is not visible")
	}
	if txn.PendingDelete("alpha") {
		t.Fatal("a staged write reported as a deletion")
	}

	txn.DeleteSkill("alpha")
	if _, ok := txn.PendingTree("alpha"); ok {
		t.Fatal("a deletion did not supersede the staged write")
	}
	if !txn.PendingDelete("alpha") {
		t.Fatal("the deletion is not visible")
	}

	txn.WriteSkill("alpha", tree)
	if txn.PendingDelete("alpha") {
		t.Fatal("a write did not supersede the staged deletion")
	}
}

// TestNeedsCommitDetectsLockOnlyChanges proves an operation that changes only
// recorded state still writes, and that a genuine no-op writes nothing.
func TestNeedsCommitDetectsLockOnlyChanges(t *testing.T) {
	t.Parallel()
	original := mustEmptyLock()
	entry := lockEntry("alpha")
	original.Upsert(entry)

	unchanged := newTransaction(pathSet{}, original)
	if unchanged.NeedsCommit(original, true) {
		t.Fatal("an unchanged transaction reported work to commit")
	}

	advanced := newTransaction(pathSet{}, original)
	moved := entry
	moved.Commit = strings.Repeat("c", 40)
	advanced.SetLockEntry(moved)
	if !advanced.NeedsCommit(original, true) {
		t.Fatal("a lock-only change reported nothing to commit")
	}

	// A project with no lockfile yet must write one as soon as it has entries.
	fresh := newTransaction(pathSet{}, mustEmptyLock())
	if fresh.NeedsCommit(mustEmptyLock(), false) {
		t.Fatal("an empty transaction created a lockfile")
	}
	fresh.SetLockEntry(entry)
	if !fresh.NeedsCommit(mustEmptyLock(), false) {
		t.Fatal("the first recorded import did not create a lockfile")
	}
	fresh.RemoveLockEntry("alpha")
	if fresh.NeedsCommit(mustEmptyLock(), false) {
		t.Fatal("removing the only entry still created a lockfile")
	}
}

// TestTransactionRefusesToStageAnUnwritableTree proves a tree that cannot be
// materialized fails the commit before anything is published.
func TestTransactionRefusesToStageAnUnwritableTree(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Original body"))

	txn := newTestTransaction(proj, mustEmptyLock())
	// A path that escapes its skill root can never be materialized.
	txn.writes["alpha"] = mustEscapingTree(t)

	if err := txn.Commit(); err == nil {
		t.Fatal("expected staging an unsafe tree to fail")
	}
	if got := proj.ImportedFile("alpha", "SKILL.md"); !strings.Contains(got, "Original body") {
		t.Fatalf("the existing skill was disturbed: %q", got)
	}
}

// TestTransactionReportsAMissingConfigurationFile proves a configuration change
// whose target vanished fails rather than recreating a file the user deleted.
func TestTransactionReportsAMissingConfigurationFile(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	txn := newTestTransaction(proj, mustEmptyLock())
	txn.SetConfig("replacement")
	if err := os.Remove(proj.paths.ConfigPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	if err := txn.Commit(); err == nil {
		t.Fatal("expected a missing configuration file to fail the commit")
	}
}

// failingWriter returns an atomic writer that fails for one path. When
// afterPublishing is true the new bytes land first, reproducing
// fsutil.WriteFileAtomic's window where the rename already succeeded and only
// the durability step failed.
func failingWriter(failFor string, afterPublishing bool) atomicWriter {
	return func(path string, data []byte, perm os.FileMode) error {
		if path != failFor {
			return os.WriteFile(path, data, perm) // #nosec G306,G703 -- path is a fixture path this test owns; the mode mirrors the production permission.
		}
		if afterPublishing {
			if err := os.WriteFile(path, data, perm); err != nil { // #nosec G306,G703 -- path is a fixture path this test owns; the mode mirrors the production permission.
				return err
			}
		}
		return fmt.Errorf("injected durability failure for %s", filepath.Base(path))
	}
}

// TestTransactionRemovesANewSkillWhenTheCommitFails proves a failed import
// leaves nothing behind. The skill did not exist before, so rolling back means
// deleting what was published rather than restoring a previous version.
func TestTransactionRemovesANewSkillWhenTheCommitFails(t *testing.T) {
	proj := newProject(t)
	if err := os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	txn := newTestTransaction(proj, mustEmptyLock())
	txn.WriteSkill("alpha", mustSkillTree(t, "alpha", "Alpha body"))
	txn.SetLockEntry(lockEntry("alpha"))
	txn.writeFile = failingWriter(proj.paths.SkillsLockPath, false)

	if err := txn.Commit(); err == nil {
		t.Fatal("expected the lock write to fail")
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("a failed commit left a newly imported skill on disk")
	}
	if proj.Lock() != nil {
		t.Fatal("a failed commit left lock state behind")
	}
}
