package skillimports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitRollsBackImmediatelyWhenAPublishRenameFails(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)

	transaction, err := NewTransaction(fixture.root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageSkill("alpha", tree(t, file(SkillManifestName, "replacement\n"))); err != nil {
		t.Fatalf("stage alpha: %v", err)
	}
	if err := transaction.StageSkill("beta", tree(t, file(SkillManifestName, "new\n"))); err != nil {
		t.Fatalf("stage beta: %v", err)
	}
	if err := transaction.StageLock(fixture.lockPath, []byte("new lock\n")); err != nil {
		t.Fatalf("stage lock: %v", err)
	}

	// Destroy the second staged tree so its publish rename fails midway, standing
	// in for a disk or permission failure between two skills.
	for _, publish := range transaction.publishes {
		if publish.SkillName == "beta" {
			if err := os.RemoveAll(publish.Staged); err != nil {
				t.Fatalf("remove staged beta: %v", err)
			}
		}
	}

	if err := transaction.Commit(); err == nil {
		t.Fatal("a failed publish must be reported, not swallowed")
	}

	// The already-published skill is rolled back, the new one never appears, and
	// the lock never advances past trees that were not published.
	if got, ok := fixture.skillContent(t, "alpha"); !ok || got != "original skill\n" {
		t.Fatalf("alpha = %q (present=%v), want the pre-transaction content", got, ok)
	}
	if _, ok := fixture.skillContent(t, "beta"); ok {
		t.Fatal("a skill whose publish failed must not exist")
	}
	if got := fixture.read(t, fixture.lockPath); got != "original lock\n" {
		t.Fatalf("lock = %q, want the pre-transaction content", got)
	}
	if _, err := os.Stat(JournalPath(fixture.root)); !os.IsNotExist(err) {
		t.Fatal("a rolled-back transaction must clear its journal")
	}
	if _, err := os.Stat(StagingRoot(fixture.root)); !os.IsNotExist(err) {
		t.Fatal("a rolled-back transaction must clear its staging area")
	}
}

func TestRollbackRemovesAFileThatDidNotExistBeforeTheTransaction(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lockPath := filepath.Join(root, ".agent-layer", "lock.json")

	transaction, err := NewTransaction(root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageLock(lockPath, []byte("first lock\n")); err != nil {
		t.Fatalf("stage lock: %v", err)
	}
	record := &journal{
		Version: journalVersion, Phase: phasePending, StagingDir: transaction.stagingDir,
		Lock: transaction.lock,
	}
	if err := applyFileRecord(record.Lock); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the lock should exist after applying: %v", err)
	}

	if err := rollbackJournal(record); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	// A first-ever lock has no backup to restore, so rollback must remove it
	// rather than leave a lock the project never had.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("a lock created by the rolled-back transaction must be removed")
	}
	// Rollback is idempotent so a crash during recovery can be retried.
	if err := rollbackJournal(record); err != nil {
		t.Fatalf("second rollback: %v", err)
	}
}

func TestWriteTreeFailsWhenAPathIsBlockedByAFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "skill")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A regular file where a directory must go makes the tree unmaterializable;
	// silently skipping the file would publish an incomplete skill.
	if err := os.WriteFile(filepath.Join(dir, "scripts"), []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	built := tree(t, file("scripts/run.sh", "#!/bin/sh\n"))
	if err := WriteTree(built, dir); err == nil {
		t.Fatal("materializing over a blocking file must fail")
	}
}

func TestRejectUnsafeMemberPathGuardsMaterializedNames(t *testing.T) {
	t.Parallel()
	for _, unsafe := range []string{"a//b", "a/../b", "a/./b", "a/\x00b"} {
		if err := rejectUnsafeMemberPath(unsafe); err == nil {
			t.Fatalf("path %q must be rejected before it is written under the managed root", unsafe)
		}
	}
	if err := rejectUnsafeMemberPath("references/notes.md"); err != nil {
		t.Fatalf("an ordinary path must be accepted: %v", err)
	}
}

func TestReadGitTreeRejectsAGitlink(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	head := repo.commit("add alpha")
	// A gitlink entry stands in for a submodule without needing a second remote.
	runGit(t, repo.path(), "update-index", "--add", "--cacheinfo", "160000,"+head+",skills/alpha/vendor")
	runGit(t, repo.path(), "commit", "--quiet", "-m", "add gitlink")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	message := requireError(t, out, err)
	requireContains(t, message, "gitlink")
	requireContains(t, message, "only directories and regular files")
}

func TestTreeLenCountsFiles(t *testing.T) {
	t.Parallel()
	if got := tree(t, file("a", "1"), file("b", "2")).Len(); got != 2 {
		t.Fatalf("len = %d, want 2", got)
	}
}

func TestJoinLinesRendersOnePerLine(t *testing.T) {
	t.Parallel()
	if got := joinLines(nil); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := joinLines([]string{"one"}); got != "one" {
		t.Fatalf("single = %q", got)
	}
	if got := joinLines([]string{"one", "two"}); !strings.Contains(got, "one\ntwo") {
		t.Fatalf("multiple = %q", got)
	}
}
