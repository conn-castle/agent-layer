package skillimports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// transactionFixture is a project root with one published skill, a config file,
// and a lock file, ready to be mutated by a transaction.
type transactionFixture struct {
	root       string
	configPath string
	lockPath   string
}

func newTransactionFixture(t *testing.T) *transactionFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fixture := &transactionFixture{
		root:       root,
		configPath: filepath.Join(root, ".agent-layer", "config.toml"),
		lockPath:   filepath.Join(root, ".agent-layer", config.SkillImportLockFileName),
	}
	if err := os.WriteFile(fixture.configPath, []byte("original config\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(fixture.lockPath, []byte("original lock\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	existing := tree(t, file(SkillManifestName, "original skill\n"))
	if err := WriteTree(existing, filepath.Join(ImportedSkillsRoot(root), "alpha")); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	return fixture
}

func (f *transactionFixture) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func (f *transactionFixture) skillContent(t *testing.T, name string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ImportedSkillsRoot(f.root), name, SkillManifestName)) // #nosec G304 -- test-controlled path.
	if err != nil {
		return "", false
	}
	return string(data), true
}

func TestTransactionPublishesTreesConfigAndLockTogether(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)

	transaction, err := NewTransaction(fixture.root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageSkill("alpha", tree(t, file(SkillManifestName, "replaced skill\n"))); err != nil {
		t.Fatalf("stage alpha: %v", err)
	}
	if err := transaction.StageSkill("beta", tree(t, file(SkillManifestName, "new skill\n"))); err != nil {
		t.Fatalf("stage beta: %v", err)
	}
	if err := transaction.StageConfig(fixture.configPath, []byte("new config\n")); err != nil {
		t.Fatalf("stage config: %v", err)
	}
	if err := transaction.StageLock(fixture.lockPath, []byte("new lock\n")); err != nil {
		t.Fatalf("stage lock: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got, _ := fixture.skillContent(t, "alpha"); got != "replaced skill\n" {
		t.Fatalf("alpha = %q", got)
	}
	if got, _ := fixture.skillContent(t, "beta"); got != "new skill\n" {
		t.Fatalf("beta = %q", got)
	}
	if got := fixture.read(t, fixture.configPath); got != "new config\n" {
		t.Fatalf("config = %q", got)
	}
	if got := fixture.read(t, fixture.lockPath); got != "new lock\n" {
		t.Fatalf("lock = %q", got)
	}
	// A completed transaction leaves no journal and no staging area behind.
	if _, err := os.Stat(JournalPath(fixture.root)); !os.IsNotExist(err) {
		t.Fatalf("journal survived a completed transaction")
	}
	if _, err := os.Stat(StagingRoot(fixture.root)); !os.IsNotExist(err) {
		t.Fatalf("staging survived a completed transaction")
	}
}

func TestTransactionRetiresASkillDirectory(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)

	transaction, err := NewTransaction(fixture.root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageSkillRemoval("alpha"); err != nil {
		t.Fatalf("stage removal: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, ok := fixture.skillContent(t, "alpha"); ok {
		t.Fatal("a retired skill directory must be gone after commit")
	}
}

// interruptAfterPreparing simulates a crash: it builds and journals a
// transaction, applies part of it, and leaves the journal in place without
// finishing, exactly as a killed process would.
func interruptAfterPreparing(t *testing.T, fixture *transactionFixture, applyThrough int) {
	t.Helper()
	transaction, err := NewTransaction(fixture.root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageSkill("alpha", tree(t, file(SkillManifestName, "half-published\n"))); err != nil {
		t.Fatalf("stage alpha: %v", err)
	}
	if err := transaction.StageSkill("beta", tree(t, file(SkillManifestName, "half-published\n"))); err != nil {
		t.Fatalf("stage beta: %v", err)
	}
	if err := transaction.StageConfig(fixture.configPath, []byte("new config\n")); err != nil {
		t.Fatalf("stage config: %v", err)
	}
	if err := transaction.StageLock(fixture.lockPath, []byte("new lock\n")); err != nil {
		t.Fatalf("stage lock: %v", err)
	}

	record := &journal{
		Version:    journalVersion,
		Phase:      phasePending,
		StagingDir: transaction.stagingDir,
		Publishes:  transaction.publishes,
		Config:     transaction.config,
		Lock:       transaction.lock,
	}
	if err := writeJournal(transaction.journalPath, record); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	// Apply the first applyThrough steps of the same sequence Commit performs,
	// then stop, leaving the journal behind.
	steps := []func() error{}
	for i := range record.Publishes {
		publish := record.Publishes[i]
		steps = append(steps, func() error {
			if publish.TargetExisted {
				if err := os.Rename(publish.Target, publish.Backup); err != nil {
					return err
				}
			}
			return nil
		}, func() error {
			if publish.Staged == "" {
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(publish.Target), DirectoryMode); err != nil {
				return err
			}
			return os.Rename(publish.Staged, publish.Target)
		})
	}
	steps = append(steps, func() error { return applyFileRecord(record.Config) })
	steps = append(steps, func() error { return applyFileRecord(record.Lock) })

	for i, step := range steps {
		if i >= applyThrough {
			return
		}
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
}

func TestRecoverRollsBackAnInterruptedPublishAtEveryBoundary(t *testing.T) {
	// Every prefix of the publish sequence must recover to the same consistent
	// pre-transaction state; a partially published skill or an advanced lock
	// would both be corruption.
	for applyThrough := 0; applyThrough <= 6; applyThrough++ {
		t.Run(boundaryName(applyThrough), func(t *testing.T) {
			fixture := newTransactionFixture(t)
			interruptAfterPreparing(t, fixture, applyThrough)

			outcome, err := RecoverTransaction(fixture.root)
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if !outcome.Recovered {
				t.Fatal("an interrupted publish must be detected")
			}
			if outcome.RolledForward {
				t.Fatal("a publish that never committed must roll back, not forward")
			}

			if got, ok := fixture.skillContent(t, "alpha"); !ok || got != "original skill\n" {
				t.Fatalf("alpha = %q (present=%v), want the pre-transaction content", got, ok)
			}
			if _, ok := fixture.skillContent(t, "beta"); ok {
				t.Fatal("a skill that did not exist before the transaction must not survive rollback")
			}
			if got := fixture.read(t, fixture.configPath); got != "original config\n" {
				t.Fatalf("config = %q, want the pre-transaction content", got)
			}
			// The lock is the last thing published; rolling it back too is what keeps
			// it from pointing at trees that were never published.
			if got := fixture.read(t, fixture.lockPath); got != "original lock\n" {
				t.Fatalf("lock = %q, want the pre-transaction content", got)
			}
			if _, err := os.Stat(JournalPath(fixture.root)); !os.IsNotExist(err) {
				t.Fatal("recovery must clear the journal")
			}
			if _, err := os.Stat(StagingRoot(fixture.root)); !os.IsNotExist(err) {
				t.Fatal("recovery must clear the staging area")
			}
		})
	}
}

func boundaryName(step int) string {
	names := []string{
		"nothing applied",
		"alpha moved aside",
		"alpha replaced",
		"beta moved aside",
		"beta created",
		"config published",
		"lock published",
	}
	return names[step]
}

func TestRecoverRollsForwardACommittedPublish(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)
	interruptAfterPreparing(t, fixture, 6)

	// Mark the journal committed, standing in for a crash after the lock landed
	// but before the staging area was cleared.
	journalPath := JournalPath(fixture.root)
	data, err := os.ReadFile(journalPath) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var record journal
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode journal: %v", err)
	}
	record.Phase = phaseCommitted
	if err := writeJournal(journalPath, &record); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	outcome, err := RecoverTransaction(fixture.root)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !outcome.RolledForward {
		t.Fatal("a committed publish must be completed, not undone")
	}
	if got, _ := fixture.skillContent(t, "alpha"); got != "half-published\n" {
		t.Fatalf("alpha = %q, want the committed content", got)
	}
	if got := fixture.read(t, fixture.lockPath); got != "new lock\n" {
		t.Fatalf("lock = %q, want the committed content", got)
	}
	if _, err := os.Stat(StagingRoot(fixture.root)); !os.IsNotExist(err) {
		t.Fatal("roll-forward must clear the staging area")
	}
}

func TestRecoverRefusesAnUnreadableJournal(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)
	if err := os.WriteFile(JournalPath(fixture.root), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	_, err := RecoverTransaction(fixture.root)
	if err == nil {
		t.Fatal("an unreadable journal must fail rather than let Agent Layer guess which half landed")
	}
	requireContains(t, err.Error(), "resolve it by hand")
}

func TestRecoverTransactionRejectsJournalPathsOutsideProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), DirectoryMode); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), RegularFileMode); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	record := &journal{
		Version: journalVersion, Phase: phasePending, StagingDir: t.TempDir(),
		Publishes: []publishRecord{{SkillName: "alpha", Target: victim, Backup: filepath.Join(t.TempDir(), "backup"), TargetExisted: true}},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal journal: %v", err)
	}
	if err := os.WriteFile(JournalPath(root), data, RegularFileMode); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	if _, err := RecoverTransaction(root); err == nil {
		t.Fatal("recovery accepted journal-controlled paths outside the project")
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep" { // #nosec G304 -- victim is rooted in a test-owned temporary directory.
		t.Fatalf("unsafe recovery changed victim: %q, %v", got, err)
	}
}

func TestRecoveryRunsBeforeAnyOperationReadsState(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// Simulate an interrupted publish left behind by an earlier command.
	fixture := &transactionFixture{
		root:       p.root,
		configPath: config.DefaultPaths(p.root).ConfigPath,
		lockPath:   config.DefaultPaths(p.root).SkillImportLockPath,
	}
	original, _ := p.readSkillFile("alpha", SkillManifestName)
	interruptAfterPreparing(t, fixture, 2)

	view, err := p.service(nil).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !view.Recovered {
		t.Fatal("status must resolve an interrupted publish before reading local state")
	}
	restored, _ := p.readSkillFile("alpha", SkillManifestName)
	if restored != original {
		t.Fatalf("recovery did not restore the pre-transaction skill: %q", restored)
	}
}
