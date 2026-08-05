package skillimport

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// atomicWriter publishes a file's complete content. It is injected so tests can
// reproduce WriteFileAtomic's documented post-rename failure mode, where the
// new bytes are already visible but the operation still reports failure.
type atomicWriter func(path string, data []byte, perm os.FileMode) error

// transaction accumulates a complete desired local state and applies it in one
// recoverable step.
//
// Imported trees are materialized in a staging directory first, then swapped in
// with rename plus backup so an interruption leaves either the previous
// complete tree or the new complete tree. Configuration and lock state are
// written last, after every tree is in place, so recorded state never claims
// content that is not on disk.
//
// Because that sequence spans several renames, the transaction records its
// intent and a durable copy of every file it replaces in a
// skilljournal.Document before touching anything live. An interrupted process
// therefore leaves enough evidence for the next operation to roll the whole
// transaction back, and an in-process failure restores the same state
// immediately, surfacing any rollback failure rather than hiding it.
type transaction struct {
	paths       pathSet
	writes      map[string]skilltree.Tree
	deletes     map[string]struct{}
	lock        *skilllock.File
	configRaw   *string
	stagingRoot string
	// writeFile publishes configuration and lock content.
	writeFile atomicWriter
}

// pathSet is the subset of resolved paths a transaction writes.
type pathSet struct {
	ConfigPath        string
	SkillsLockPath    string
	ImportedSkillsDir string
}

func newTransaction(paths pathSet, lock *skilllock.File) *transaction {
	return &transaction{
		paths:     paths,
		writes:    map[string]skilltree.Tree{},
		deletes:   map[string]struct{}{},
		lock:      lock.Clone(),
		writeFile: fsutil.WriteFileAtomic,
	}
}

// WriteSkill records the complete desired content of one imported skill.
func (t *transaction) WriteSkill(name string, tree skilltree.Tree) {
	delete(t.deletes, name)
	t.writes[name] = tree
}

// DeleteSkill records that an imported directory must be removed.
func (t *transaction) DeleteSkill(name string) {
	delete(t.writes, name)
	t.deletes[name] = struct{}{}
}

// SetLockEntry records a lock entry to write.
func (t *transaction) SetLockEntry(entry skilllock.Entry) { t.lock.Upsert(entry) }

// RemoveLockEntry drops a lock entry.
func (t *transaction) RemoveLockEntry(name string) { t.lock.Remove(name) }

// SetConfig records replacement configuration content.
func (t *transaction) SetConfig(content string) { t.configRaw = &content }

// Empty reports whether the transaction would change nothing on disk.
func (t *transaction) Empty() bool {
	return len(t.writes) == 0 && len(t.deletes) == 0 && t.configRaw == nil
}

// PendingTree returns content this transaction has already staged for a skill.
// Later stages of one operation read through it so they observe the state the
// operation is building rather than the state it started from.
func (t *transaction) PendingTree(name string) (skilltree.Tree, bool) {
	tree, ok := t.writes[name]
	return tree, ok
}

// PendingDelete reports whether this transaction removes a skill.
func (t *transaction) PendingDelete(name string) bool {
	_, ok := t.deletes[name]
	return ok
}

// NeedsCommit reports whether applying the transaction would change any file,
// including creating the lockfile for the first time.
func (t *transaction) NeedsCommit(original *skilllock.File, lockPresent bool) bool {
	if !t.Empty() {
		return true
	}
	if !lockPresent {
		return len(t.lock.Skills) > 0
	}
	next, nextErr := t.lock.Marshal()
	previous, previousErr := original.Marshal()
	if nextErr != nil || previousErr != nil {
		return true
	}
	return !bytes.Equal(next, previous)
}

// Commit applies the transaction.
//
// Every file the transaction will replace is first copied into the staging
// directory and recorded in a journal. Only then are the trees published,
// followed by configuration and lock state. Any failure — including a
// durability failure reported after the new bytes are already visible — rolls
// every published path back to its recorded content, and a failed rollback is
// surfaced alongside the original error instead of being discarded.
func (t *transaction) Commit() (err error) {
	staging := skilljournal.StagingRoot(t.paths.ImportedSkillsDir)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return fmt.Errorf("failed to create %s: %w", staging, err)
	}
	t.stagingRoot = staging
	// The staging directory holds the journal and the only copies of every file
	// this transaction replaces. It is cleared once the outcome is settled — a
	// complete commit or a clean rollback — but deliberately kept when rollback
	// itself failed, because those backups are then the only way the next
	// operation's recovery can repair the mixed state left on disk.
	stagingSettled := true
	defer func() {
		if stagingSettled {
			_ = os.RemoveAll(staging)
		}
	}()

	if err := t.prepareJournal(); err != nil {
		return err
	}

	published := &publishedState{}
	fail := func(cause error) error {
		rollbackErr := t.rollback(published)
		stagingSettled = rollbackErr == nil
		return joinRollback(cause, rollbackErr)
	}

	for _, name := range sortedKeys(t.writes) {
		applied, publishErr := t.publishTree(name, t.writes[name])
		if applied.Name != "" {
			published.Writes = append(published.Writes, applied)
		}
		if publishErr != nil {
			return fail(publishErr)
		}
	}
	for _, name := range sortedKeys(t.deletes) {
		applied, removeErr := t.removeTree(name)
		if applied.Name != "" {
			published.Deletes = append(published.Deletes, applied)
		}
		if removeErr != nil {
			return fail(removeErr)
		}
	}
	// The renames above are metadata changes; flushing the directory makes the
	// published trees durable before recorded state starts to claim them.
	if syncErr := fsutil.SyncDir(t.paths.ImportedSkillsDir); syncErr != nil {
		return fail(syncErr)
	}

	if t.configRaw != nil {
		published.Config = true
		if writeErr := t.writeFile(t.paths.ConfigPath, []byte(*t.configRaw), 0o644); writeErr != nil {
			return fail(fmt.Errorf("failed to write %s: %w", t.paths.ConfigPath, writeErr))
		}
	}

	lockData, marshalErr := t.lock.Marshal()
	if marshalErr != nil {
		return fail(marshalErr)
	}
	published.Lock = true
	if lockErr := t.writeFile(t.paths.SkillsLockPath, lockData, 0o644); lockErr != nil {
		return fail(fmt.Errorf("failed to write skill lock %s: %w", t.paths.SkillsLockPath, lockErr))
	}

	// Everything is durable. Recording that fact stops a crash before the
	// staging directory is cleared from reverting a complete transaction.
	if commitErr := skilljournal.MarkCommitted(staging); commitErr != nil {
		return fail(commitErr)
	}
	return nil
}

// publishedTree records one tree change Commit already applied to a live path,
// so rollback knows whether a previous version has to come back. A zero Name
// means the live path was never touched and must be left alone.
type publishedTree struct {
	Name        string
	HadPrevious bool
}

// publishedState is what Commit has already applied to live paths.
type publishedState struct {
	Writes  []publishedTree
	Deletes []publishedTree
	Config  bool
	Lock    bool
}

// prepareJournal copies every file the transaction replaces into staging and
// records the transaction's intent, so an interrupted process can be recovered.
// It runs before any live path changes.
func (t *transaction) prepareJournal() error {
	doc := skilljournal.Document{
		Deletes: sortedKeys(t.deletes),
		Config:  t.configRaw != nil,
	}
	for _, name := range sortedKeys(t.writes) {
		existed, err := pathExists(filepath.Join(t.paths.ImportedSkillsDir, name))
		if err != nil {
			return err
		}
		doc.Writes = append(doc.Writes, skilljournal.WriteIntent{Name: name, Existed: existed})
	}

	if t.configRaw != nil {
		previous, readErr := os.ReadFile(t.paths.ConfigPath) // #nosec G304 -- resolved repository configuration path.
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", t.paths.ConfigPath, readErr)
		}
		if err := t.stageBackup(skilljournal.ConfigBackupName, previous); err != nil {
			return err
		}
	}

	previousLock, lockErr := os.ReadFile(t.paths.SkillsLockPath) // #nosec G304 -- resolved repository skill lock path.
	switch {
	case lockErr == nil:
		doc.LockExisted = true
		if err := t.stageBackup(skilljournal.LockBackupName, previousLock); err != nil {
			return err
		}
	case errors.Is(lockErr, os.ErrNotExist):
		doc.LockExisted = false
	default:
		return fmt.Errorf("failed to read %s: %w", t.paths.SkillsLockPath, lockErr)
	}

	return skilljournal.Write(t.stagingRoot, doc)
}

// stageBackup writes one pre-transaction file copy into the staging directory.
func (t *transaction) stageBackup(name string, data []byte) error {
	path := filepath.Join(t.stagingRoot, name)
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to preserve prior state in %s: %w", path, err)
	}
	return nil
}

// rollback restores every live path this transaction already changed, using the
// same backups recovery would use after an interruption. It returns every
// problem it hit so a partial rollback is never reported as a clean revert.
func (t *transaction) rollback(published *publishedState) error {
	var problems []string
	note := func(err error) {
		if err != nil {
			problems = append(problems, err.Error())
		}
	}

	for i := len(published.Deletes) - 1; i >= 0; i-- {
		entry := published.Deletes[i]
		if !entry.HadPrevious {
			continue
		}
		note(restoreBackup(filepath.Join(t.stagingRoot, skilljournal.DeleteBackupPrefix+entry.Name),
			filepath.Join(t.paths.ImportedSkillsDir, entry.Name)))
	}
	for i := len(published.Writes) - 1; i >= 0; i-- {
		entry := published.Writes[i]
		target := filepath.Join(t.paths.ImportedSkillsDir, entry.Name)
		if !entry.HadPrevious {
			if err := os.RemoveAll(target); err != nil {
				note(fmt.Errorf("failed to remove %s: %w", target, err))
			}
			continue
		}
		note(restoreBackup(filepath.Join(t.stagingRoot, skilljournal.WriteBackupPrefix+entry.Name), target))
	}
	if published.Config {
		note(t.restoreStagedFile(filepath.Join(t.stagingRoot, skilljournal.ConfigBackupName), t.paths.ConfigPath))
	}
	if published.Lock {
		backup := filepath.Join(t.stagingRoot, skilljournal.LockBackupName)
		if _, statErr := os.Lstat(backup); statErr == nil {
			note(t.restoreStagedFile(backup, t.paths.SkillsLockPath))
		} else if err := os.Remove(t.paths.SkillsLockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			note(fmt.Errorf("failed to remove %s: %w", t.paths.SkillsLockPath, err))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// restoreBackup moves a staged backup tree back over its live path.
func restoreBackup(backup string, target string) error {
	if _, err := os.Lstat(backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to inspect %s: %w", backup, err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove %s: %w", target, err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return nil
}

// restoreStagedFile rewrites a live file with its staged pre-transaction copy.
func (t *transaction) restoreStagedFile(backup string, target string) error {
	data, err := os.ReadFile(backup) // #nosec G304 -- backup is inside the staging directory this transaction owns.
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", backup, err)
	}
	if err := t.writeFile(target, data, 0o644); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return nil
}

// joinRollback reports a rollback failure alongside the failure that triggered
// it, because a silent rollback failure is the one outcome that can strand
// mixed generations on disk.
func joinRollback(cause error, rollbackErr error) error {
	if rollbackErr == nil {
		return cause
	}
	return fmt.Errorf("%w; rolling the change back also failed: %v", cause, rollbackErr)
}

// publishTree materializes one skill tree in staging and swaps it into the
// imported tier. It reports whether a previous tree was moved aside, which is
// what rollback and recovery need to restore the prior state.
func (t *transaction) publishTree(name string, tree skilltree.Tree) (publishedTree, error) {
	target := filepath.Join(t.paths.ImportedSkillsDir, name)
	staged := filepath.Join(t.stagingRoot, skilljournal.StagedTreePrefix+name)
	backup := filepath.Join(t.stagingRoot, skilljournal.WriteBackupPrefix+name)

	if err := os.RemoveAll(staged); err != nil {
		return publishedTree{}, fmt.Errorf("failed to clear %s: %w", staged, err)
	}
	if err := os.MkdirAll(staged, 0o750); err != nil {
		return publishedTree{}, fmt.Errorf("failed to create %s: %w", staged, err)
	}
	if err := skilltree.Materialize(tree, staged); err != nil {
		return publishedTree{}, err
	}

	hadPrevious, err := moveAside(target, backup)
	if err != nil {
		// The live path was never touched, so nothing about it is rolled back.
		return publishedTree{}, err
	}
	applied := publishedTree{Name: name, HadPrevious: hadPrevious}
	if renameErr := os.Rename(staged, target); renameErr != nil {
		return applied, fmt.Errorf("failed to publish %s: %w", target, renameErr)
	}
	return applied, nil
}

// removeTree moves an imported directory aside, reporting the change rollback
// would have to undo.
func (t *transaction) removeTree(name string) (publishedTree, error) {
	target := filepath.Join(t.paths.ImportedSkillsDir, name)
	backup := filepath.Join(t.stagingRoot, skilljournal.DeleteBackupPrefix+name)
	hadPrevious, err := moveAside(target, backup)
	if err != nil {
		return publishedTree{}, err
	}
	return publishedTree{Name: name, HadPrevious: hadPrevious}, nil
}

// pathExists reports whether a filesystem node is present without following
// links.
func pathExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect %s: %w", path, err)
	}
	return true, nil
}

// moveAside relocates an existing path to backup, reporting whether anything
// was there.
func moveAside(target string, backup string) (bool, error) {
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect %s: %w", target, err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return false, fmt.Errorf("failed to clear %s: %w", backup, err)
	}
	if err := os.Rename(target, backup); err != nil {
		return false, fmt.Errorf("failed to move %s aside: %w", target, err)
	}
	return true, nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
