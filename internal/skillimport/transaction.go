package skillimport

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	paths            pathSet
	writes           map[string]skilltree.Tree
	deletes          map[string]struct{}
	localRetirements map[string]skilltree.Tree
	lock             *skilllock.File
	configRaw        *string
	stagingRoot      string
	// writeFile publishes configuration and lock content.
	writeFile atomicWriter
	// checkpoint is a test seam for process interruption at durable boundaries.
	checkpoint func(string)
}

// pathSet is the subset of resolved paths a transaction writes.
type pathSet = skilljournal.Targets

func newTransaction(paths pathSet, lock *skilllock.File) *transaction {
	return &transaction{
		paths:            paths,
		writes:           map[string]skilltree.Tree{},
		deletes:          map[string]struct{}{},
		localRetirements: map[string]skilltree.Tree{},
		lock:             lock.Clone(),
		writeFile:        fsutil.WriteFileAtomic,
	}
}

// WriteSkill records the complete desired content of one imported skill.
func (t *transaction) WriteSkill(name string, tree skilltree.Tree) {
	delete(t.deletes, name)
	t.writes[name] = tree
}

// RetireLocal records a validated legacy source to retire during adoption.
func (t *transaction) RetireLocal(name string, tree skilltree.Tree) { t.localRetirements[name] = tree }

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
func (t *transaction) reached(step string) {
	if t.checkpoint != nil {
		t.checkpoint(step)
	}
}

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

	t.reached("journal")
	published := skilljournal.Progress{}
	fail := func(cause error) error {
		rollbackErr := skilljournal.Rollback(t.paths, published, t.writeFile)
		stagingSettled = rollbackErr == nil
		return joinRollback(cause, rollbackErr)
	}

	// Retire the old active slot before publishing its imported replacement.
	for _, name := range sortedKeys(t.localRetirements) {
		if _, err := skilltree.ReadStrict(skilltree.OSFS{}, filepath.Join(t.paths.LocalSkillsDir, name)); err != nil {
			return fail(err)
		}
		applied, moveErr := moveAside(filepath.Join(t.paths.LocalSkillsDir, name), filepath.Join(staging, skilljournal.LocalBackupPrefix+name))
		if applied {
			published.LocalRetirements = append(published.LocalRetirements, name)
		}
		if moveErr != nil {
			return fail(moveErr)
		}
		t.reached("retire:" + name)
		if !applied {
			return fail(fmt.Errorf("legacy source %s disappeared before retirement", name))
		}
	}
	if err := t.verifyLocalRetirements(staging); err != nil {
		return fail(err)
	}
	for _, name := range sortedKeys(t.writes) {
		applied, publishErr := t.publishTree(name)
		if applied.Name != "" {
			published.Writes = append(published.Writes, applied)
		}
		if publishErr != nil {
			return fail(publishErr)
		}
		t.reached("write:" + name)
	}
	for _, name := range sortedKeys(t.deletes) {
		applied, removeErr := t.removeTree(name)
		if applied.Existed {
			published.Deletes = append(published.Deletes, name)
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

	t.reached("config")
	lockData, marshalErr := t.lock.Marshal()
	if marshalErr != nil {
		return fail(marshalErr)
	}
	published.Lock = true
	if lockErr := t.writeFile(t.paths.SkillsLockPath, lockData, 0o644); lockErr != nil {
		return fail(fmt.Errorf("failed to write skill lock %s: %w", t.paths.SkillsLockPath, lockErr))
	}

	t.reached("lock")
	if err := t.verifyLocalRetirements(staging); err != nil {
		return fail(err)
	}
	// Everything is durable. Recording that fact stops a crash before the
	// staging directory is cleared from reverting a complete transaction.
	if commitErr := skilljournal.MarkCommitted(staging); commitErr != nil {
		return fail(commitErr)
	}
	t.reached("committed")
	return nil
}

// The project lock coordinates CLI operations, but editors can still change the
// source during staging or through an open file after retirement. Abort and
// restore that source if it differs from the copy being published.
func (t *transaction) verifyLocalRetirements(staging string) error {
	for _, name := range sortedKeys(t.localRetirements) {
		backup := filepath.Join(staging, skilljournal.LocalBackupPrefix+name)
		current, err := skilltree.ReadStrict(skilltree.OSFS{}, backup)
		if err != nil {
			return fmt.Errorf("verify retired legacy source %s: %w", name, err)
		}
		if current.Hash() != t.localRetirements[name].Hash() {
			return fmt.Errorf("legacy source %s changed while adoption was staged; finish the edits and retry conversion", name)
		}
	}
	return nil
}

// prepareJournal copies every file the transaction replaces into staging and
// records the transaction's intent, so an interrupted process can be recovered.
// It runs before any live path changes.
func (t *transaction) prepareJournal() error {
	doc := skilljournal.Document{
		Deletes: sortedKeys(t.deletes),
		Config:  t.configRaw != nil,
	}
	if len(t.localRetirements) > 0 {
		doc.Version = skilljournal.AdoptionVersion
		doc.LocalRetirements = sortedKeys(t.localRetirements)
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

	// All replacement trees are prepared before the durable journal and live moves.
	for _, name := range sortedKeys(t.writes) {
		staged := filepath.Join(t.stagingRoot, skilljournal.StagedTreePrefix+name)
		if err := os.MkdirAll(staged, 0o750); err != nil {
			return err
		}
		if err := skilltree.Materialize(t.writes[name], staged); err != nil {
			return err
		}
		if err := syncStagedTree(staged); err != nil {
			return err
		}
	}
	if err := fsutil.SyncDir(filepath.Dir(t.stagingRoot)); err != nil {
		return err
	}
	t.reached("before-journal")
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

// joinRollback reports a rollback failure alongside the failure that triggered
// it, because a silent rollback failure is the one outcome that can strand
// mixed generations on disk.
func joinRollback(cause error, rollbackErr error) error {
	if rollbackErr == nil {
		return cause
	}
	return fmt.Errorf("%w; rolling the change back also failed: %w", cause, rollbackErr)
}

// publishTree swaps one already materialized skill tree from staging into the
// imported tier. It reports whether a previous tree was moved aside, which is
// what rollback and recovery need to restore the prior state.
func (t *transaction) publishTree(name string) (skilljournal.WriteIntent, error) {
	target := filepath.Join(t.paths.ImportedSkillsDir, name)
	staged := filepath.Join(t.stagingRoot, skilljournal.StagedTreePrefix+name)
	backup := filepath.Join(t.stagingRoot, skilljournal.WriteBackupPrefix+name)

	hadPrevious, err := moveAside(target, backup)
	applied := skilljournal.WriteIntent{Name: name, Existed: hadPrevious}
	if err != nil {
		if hadPrevious {
			return applied, err
		}
		return skilljournal.WriteIntent{}, err
	}
	if renameErr := os.Rename(staged, target); renameErr != nil {
		return applied, fmt.Errorf("failed to publish %s: %w", target, renameErr)
	}
	return applied, errors.Join(fsutil.SyncDir(filepath.Dir(target)), fsutil.SyncDir(t.stagingRoot))
}

// removeTree moves an imported directory aside, reporting the change rollback
// would have to undo.
func (t *transaction) removeTree(name string) (skilljournal.WriteIntent, error) {
	target := filepath.Join(t.paths.ImportedSkillsDir, name)
	backup := filepath.Join(t.stagingRoot, skilljournal.DeleteBackupPrefix+name)
	hadPrevious, err := moveAside(target, backup)
	applied := skilljournal.WriteIntent{Name: name, Existed: hadPrevious}
	if err != nil {
		if hadPrevious {
			return applied, err
		}
		return skilljournal.WriteIntent{}, err
	}
	return applied, errors.Join(fsutil.SyncDir(filepath.Dir(target)), fsutil.SyncDir(t.stagingRoot))
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
	return true, errors.Join(fsutil.SyncDir(filepath.Dir(target)), fsutil.SyncDir(filepath.Dir(backup)))
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// syncStagedTree makes every new file and directory durable before publication.
func syncStagedTree(root string) error {
	scoped, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = scoped.Close() }()
	return filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := scoped.Open(relative)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		return errors.Join(syncErr, closeErr)
	})
}
