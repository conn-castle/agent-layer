package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/sync"
)

// interruptAfterPublishing reproduces the on-disk state a process leaves when
// it dies part way through a transaction.
//
// It drives the production transaction primitives — the journal, the staged
// trees, and the rename-with-backup publication — and then simply stops,
// without the rollback, the commit marker, or the staging cleanup that a normal
// return would perform. writeConfig extends the interruption past the point
// where configuration is already published but lock state is not, which is the
// window that can strand mixed generations.
func interruptAfterPublishing(t *testing.T, proj *project, writeConfig bool, build func(*transaction)) {
	t.Helper()
	lock := proj.Lock()
	if lock == nil {
		lock = skilllock.New()
	}
	txn := newTestTransaction(proj, lock)
	build(txn)

	staging := skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	txn.stagingRoot = staging
	if err := txn.prepareJournal(); err != nil {
		t.Fatalf("prepare journal: %v", err)
	}
	for _, name := range sortedKeys(txn.writes) {
		if _, err := txn.publishTree(name, txn.writes[name]); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	for _, name := range sortedKeys(txn.deletes) {
		if _, err := txn.removeTree(name); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}
	if writeConfig && txn.configRaw != nil {
		if err := os.WriteFile(proj.paths.ConfigPath, []byte(*txn.configRaw), 0o600); err != nil {
			t.Fatalf("publish configuration: %v", err)
		}
	}
}

// seedImportedAlpha imports one skill from a hermetic repository so recovery
// tests start from real committed state.
func seedImportedAlpha(t *testing.T, body string) (*project, *gitRepo) {
	t.Helper()
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", body)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	return proj, source
}

// TestInterruptedImportIsRolledBackByTheNextOperation proves an interrupted
// transaction never leaves a replaced skill, a stale configuration, and a lock
// at different generations. The next operation restores the exact state the
// interrupted transaction started from before it reads anything.
func TestInterruptedImportIsRolledBackByTheNextOperation(t *testing.T) {
	proj, _ := seedImportedAlpha(t, "Alpha body")
	originalConfig := proj.ConfigContent()
	originalLock, _ := proj.Lock().Entry("alpha")

	interruptAfterPublishing(t, proj, true, func(txn *transaction) {
		txn.WriteSkill("alpha", mustSkillTree(t, "alpha", "Interrupted body"))
		txn.SetConfig(originalConfig + "\n# interrupted edit\n")
		advanced := originalLock
		advanced.Commit = strings.Repeat("f", 40)
		txn.SetLockEntry(advanced)
	})
	// The interruption is real: the replaced tree and configuration are live
	// while the lock still describes the previous generation.
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Interrupted body") {
		t.Fatal("the fixture did not actually publish the interrupted tree")
	}

	if _, err := proj.Service().Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}

	if got := proj.ImportedFile("alpha", "SKILL.md"); !strings.Contains(got, "Alpha body") {
		t.Fatalf("imported tree = %q, want the pre-transaction content", got)
	}
	if proj.ConfigContent() != originalConfig {
		t.Fatalf("configuration was not rolled back:\n%s", proj.ConfigContent())
	}
	if entry, _ := proj.Lock().Entry("alpha"); entry.Commit != originalLock.Commit {
		t.Fatalf("lock commit = %s, want the pre-transaction commit", entry.Commit)
	}
	if _, err := os.Stat(skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)); !os.IsNotExist(err) {
		t.Fatalf("recovery left the staging directory behind: %v", err)
	}
}

// TestInterruptedImportRestoresANewlyCreatedAndADeletedSkill proves recovery
// reverts both directions of a tree change: a skill the transaction created is
// removed again, and a skill it deleted comes back.
func TestInterruptedImportRestoresANewlyCreatedAndADeletedSkill(t *testing.T) {
	proj, _ := seedImportedAlpha(t, "Alpha body")

	interruptAfterPublishing(t, proj, false, func(txn *transaction) {
		txn.WriteSkill("beta", mustSkillTree(t, "beta", "Beta body"))
		txn.DeleteSkill("alpha")
	})
	if !proj.ImportedExists("beta") || proj.ImportedExists("alpha") {
		t.Fatal("the fixture did not actually apply the interrupted tree changes")
	}

	if _, err := proj.Service().Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if proj.ImportedExists("beta") {
		t.Fatal("a skill created by an interrupted transaction survived recovery")
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("a skill deleted by an interrupted transaction was not restored")
	}
}

// TestInterruptedImportRecoveryPrecedesOrdinaryProjection proves ordinary sync
// shares the recovery, so a projection can never be built from a half-applied
// import transaction.
func TestInterruptedImportRecoveryPrecedesOrdinaryProjection(t *testing.T) {
	proj, _ := seedImportedAlpha(t, "Alpha body")

	interruptAfterPublishing(t, proj, false, func(txn *transaction) {
		txn.DeleteSkill("alpha")
	})
	if proj.ImportedExists("alpha") {
		t.Fatal("the fixture did not actually remove the skill")
	}

	if _, err := sync.Run(proj.root); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("ordinary sync did not recover the interrupted import")
	}
	if _, ok := proj.ProjectedFile("alpha", "SKILL.md"); !ok {
		t.Fatal("the recovered skill was not projected")
	}
}

// TestMalformedRecoveryJournalFailsLoudly proves a journal that cannot be
// trusted to drive recovery stops the operation instead of guessing which
// content to restore or delete.
func TestMalformedRecoveryJournalFailsLoudly(t *testing.T) {
	proj, _ := seedImportedAlpha(t, "Alpha body")

	staging := skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, skilljournal.FileName), []byte(`{"version":1,"writes":[{"name":"../escape","existed":true}]}`), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	_, err := proj.Service().Status()
	if err == nil {
		t.Fatal("expected an unusable recovery journal to fail the operation")
	}
	if !strings.Contains(err.Error(), "journal") {
		t.Fatalf("error %q does not name the journal", err)
	}
}
