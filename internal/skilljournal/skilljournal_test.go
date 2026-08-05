package skilljournal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scene is an imported skill tier with an interrupted transaction staged in it.
type scene struct {
	t       *testing.T
	targets Targets
	staging string
}

func newScene(t *testing.T) *scene {
	t.Helper()
	root := t.TempDir()
	imported := filepath.Join(root, "imported-skills")
	if err := os.MkdirAll(StagingRoot(imported), 0o750); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	s := &scene{
		t: t,
		targets: Targets{
			ImportedSkillsDir: imported,
			ConfigPath:        filepath.Join(root, "config.toml"),
			SkillsLockPath:    filepath.Join(root, "skills.lock.json"),
		},
		staging: StagingRoot(imported),
	}
	return s
}

func (s *scene) writeTree(dir string, content string) {
	s.t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		s.t.Fatalf("write %s: %v", dir, err)
	}
}

func (s *scene) writeFile(path string, content string) {
	s.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		s.t.Fatalf("write %s: %v", path, err)
	}
}

func (s *scene) read(path string) string {
	s.t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		s.t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func (s *scene) skill(name string) string { return filepath.Join(s.targets.ImportedSkillsDir, name) }

// TestRecoverRollsBackAnInterruptedTransaction proves recovery restores every
// kind of change a transaction can be interrupted in the middle of, so a
// killed process never leaves trees, configuration, and lock state describing
// different generations.
func TestRecoverRollsBackAnInterruptedTransaction(t *testing.T) {
	t.Parallel()
	s := newScene(t)

	// replaced: previous tree parked in its backup, new content published.
	s.writeTree(filepath.Join(s.staging, WriteBackupPrefix+"replaced"), "previous replaced")
	s.writeTree(s.skill("replaced"), "interrupted replaced")
	// created: no backup, because the skill did not exist before.
	s.writeTree(s.skill("created"), "interrupted created")
	// deleted: moved aside, nothing at the live path.
	s.writeTree(filepath.Join(s.staging, DeleteBackupPrefix+"deleted"), "previous deleted")
	// untouched: the transaction intended to write it but never got there.
	s.writeTree(s.skill("untouched"), "previous untouched")

	s.writeFile(filepath.Join(s.staging, ConfigBackupName), "previous config")
	s.writeFile(s.targets.ConfigPath, "interrupted config")
	s.writeFile(s.targets.SkillsLockPath, "interrupted lock")

	if err := Write(s.staging, Document{
		Writes: []WriteIntent{
			{Name: "replaced", Existed: true},
			{Name: "created", Existed: false},
			{Name: "untouched", Existed: true},
		},
		Deletes:     []string{"deleted"},
		Config:      true,
		LockExisted: false,
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := Recover(s.targets); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	if got := s.read(filepath.Join(s.skill("replaced"), "SKILL.md")); got != "previous replaced" {
		t.Fatalf("replaced skill = %q, want its pre-transaction content", got)
	}
	if _, err := os.Stat(s.skill("created")); !os.IsNotExist(err) {
		t.Fatalf("a skill the transaction created survived rollback: %v", err)
	}
	if got := s.read(filepath.Join(s.skill("deleted"), "SKILL.md")); got != "previous deleted" {
		t.Fatalf("deleted skill = %q, want it restored", got)
	}
	if got := s.read(filepath.Join(s.skill("untouched"), "SKILL.md")); got != "previous untouched" {
		t.Fatalf("an intended write the transaction never reached was destroyed: %q", got)
	}
	if got := s.read(s.targets.ConfigPath); got != "previous config" {
		t.Fatalf("configuration = %q, want its pre-transaction content", got)
	}
	if _, err := os.Stat(s.targets.SkillsLockPath); !os.IsNotExist(err) {
		t.Fatalf("a lockfile that did not exist before the transaction survived: %v", err)
	}
	if _, err := os.Stat(s.staging); !os.IsNotExist(err) {
		t.Fatalf("recovery left the staging directory behind: %v", err)
	}
}

// TestRecoverKeepsACommittedTransaction proves a transaction whose final
// durable write already succeeded is not undone by a crash that happened before
// its staging directory could be cleared.
func TestRecoverKeepsACommittedTransaction(t *testing.T) {
	t.Parallel()
	s := newScene(t)
	s.writeTree(filepath.Join(s.staging, WriteBackupPrefix+"alpha"), "previous alpha")
	s.writeTree(s.skill("alpha"), "committed alpha")
	s.writeFile(filepath.Join(s.staging, ConfigBackupName), "previous config")
	s.writeFile(s.targets.ConfigPath, "committed config")
	s.writeFile(s.targets.SkillsLockPath, "committed lock")

	if err := Write(s.staging, Document{
		Writes:      []WriteIntent{{Name: "alpha", Existed: true}},
		Config:      true,
		LockExisted: false,
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := MarkCommitted(s.staging); err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}

	if err := Recover(s.targets); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got := s.read(filepath.Join(s.skill("alpha"), "SKILL.md")); got != "committed alpha" {
		t.Fatalf("alpha = %q, want the committed content", got)
	}
	if got := s.read(s.targets.ConfigPath); got != "committed config" {
		t.Fatalf("configuration = %q, want the committed content", got)
	}
	if _, err := os.Stat(s.staging); !os.IsNotExist(err) {
		t.Fatalf("recovery left the staging directory behind: %v", err)
	}
}

// TestRecoverClearsStagingWithNoRecordedIntent proves a staging directory that
// never got a journal is discarded rather than treated as a transaction: no
// intent was recorded, so nothing live had been touched.
func TestRecoverClearsStagingWithNoRecordedIntent(t *testing.T) {
	t.Parallel()
	s := newScene(t)
	s.writeTree(s.skill("alpha"), "live alpha")
	s.writeTree(filepath.Join(s.staging, StagedTreePrefix+"alpha"), "staged alpha")

	if err := Recover(s.targets); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got := s.read(filepath.Join(s.skill("alpha"), "SKILL.md")); got != "live alpha" {
		t.Fatalf("alpha = %q, want the untouched live content", got)
	}
	if _, err := os.Stat(s.staging); !os.IsNotExist(err) {
		t.Fatalf("recovery left the staging directory behind: %v", err)
	}
}

// TestRecoverIsANoOpWithoutAStagingDirectory proves the normal case costs
// nothing and reports no error.
func TestRecoverIsANoOpWithoutAStagingDirectory(t *testing.T) {
	t.Parallel()
	s := newScene(t)
	if err := os.RemoveAll(s.staging); err != nil {
		t.Fatalf("remove staging: %v", err)
	}
	if err := Recover(s.targets); err != nil {
		t.Fatalf("Recover: %v", err)
	}
}

// TestRecoverRejectsAJournalItCannotTrust proves recovery never guesses. A
// journal it cannot read exactly would otherwise decide which content to
// restore or delete on bad information.
func TestRecoverRejectsAJournalItCannotTrust(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "unreadable document", data: "{", want: "malformed"},
		{name: "unknown schema version", data: `{"version":99}`, want: "unsupported schema version"},
		{name: "unknown field", data: `{"version":1,"surprise":true}`, want: "unknown field"},
		{name: "escaping write name", data: `{"version":1,"writes":[{"name":"../escape","existed":true}]}`, want: "not a directory name"},
		{name: "escaping delete name", data: `{"version":1,"deletes":["a/b"]}`, want: "not a directory name"},
		{name: "empty name", data: `{"version":1,"deletes":[" "]}`, want: "is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newScene(t)
			s.writeTree(s.skill("alpha"), "live alpha")
			s.writeFile(filepath.Join(s.staging, FileName), tt.data)

			err := Recover(s.targets)
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
			if got := s.read(filepath.Join(s.skill("alpha"), "SKILL.md")); got != "live alpha" {
				t.Fatalf("a rejected journal still changed live content: %q", got)
			}
		})
	}
}

// TestRecoverReportsAnIncompleteRollback proves a rollback that cannot restore
// prior state fails loudly instead of leaving the caller to read half-reverted
// state as if it were clean.
func TestRecoverReportsAnIncompleteRollback(t *testing.T) {
	t.Parallel()
	s := newScene(t)
	s.writeTree(s.skill("alpha"), "interrupted alpha")
	// The journal claims a configuration backup that is not there.
	if err := Write(s.staging, Document{
		Writes:      []WriteIntent{{Name: "alpha", Existed: true}},
		Config:      true,
		LockExisted: false,
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := Recover(s.targets)
	if err == nil {
		t.Fatal("expected an incomplete rollback to be reported")
	}
	if !strings.Contains(err.Error(), "could not be fully rolled back") {
		t.Fatalf("error %q does not report the incomplete rollback", err)
	}
	if errors.Is(err, ErrMalformed) {
		t.Fatalf("an incomplete rollback was reported as a malformed journal: %v", err)
	}
}
