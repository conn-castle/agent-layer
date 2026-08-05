// Package skilljournal makes a skill import transaction restart-safe.
//
// A transaction publishes several imported trees, the configuration file, and
// the lockfile with independent renames. No filesystem makes that sequence one
// atomic step, so the transaction instead records its intent — and a durable
// copy of everything it is about to replace — in a journal inside its staging
// directory before it touches anything live.
//
// If the process dies part way through, the journal survives. The next
// operation to enter the project lock calls Recover, which rolls the whole
// transaction back to the exact state it started from: no skill is left
// missing or half-published, and configuration, imported trees, and lock state
// can never be stranded at different generations.
package skilljournal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// Version is the journal schema version. A journal recording a different
// version is rejected rather than guessed at, because recovering from a
// misread journal could destroy local work.
const Version = 1

const (
	// StagingDirName is the transaction staging directory inside the imported
	// skill tier. Keeping it on the same filesystem guarantees rename-based
	// publication rather than a cross-device copy. It is hidden so skill
	// enumeration never mistakes it for an imported skill.
	StagingDirName = ".staging"
	// FileName is the journal document inside the staging directory.
	FileName = "journal.json"

	// StagedTreePrefix names a fully materialized replacement tree.
	StagedTreePrefix = "new-"
	// WriteBackupPrefix names the previous tree of a replaced skill.
	WriteBackupPrefix = "old-"
	// DeleteBackupPrefix names the previous tree of a deleted skill.
	DeleteBackupPrefix = "removed-"
	// ConfigBackupName is the pre-transaction configuration file copy.
	ConfigBackupName = "config.backup"
	// LockBackupName is the pre-transaction lockfile copy.
	LockBackupName = "lock.backup"
)

// ErrMalformed reports a journal that exists but cannot be trusted to drive
// recovery. Recovery fails loudly rather than deleting or restoring content on
// a guess.
var ErrMalformed = errors.New("skill import journal is malformed")

// WriteIntent is one skill the transaction replaces or creates.
type WriteIntent struct {
	Name string `json:"name"`
	// Existed records whether the imported directory was present before the
	// transaction started. Recovery needs it because a write that was never
	// reached and a newly created skill both leave no backup behind: the first
	// must be left alone, the second must be removed.
	Existed bool `json:"existed"`
}

// Document is the recorded intent of one in-flight transaction.
type Document struct {
	Version int `json:"version"`
	// Committed marks a transaction whose final durable write already
	// succeeded. Recovery then only removes the staging directory.
	Committed bool `json:"committed"`
	// Writes are the skills the transaction replaces or creates.
	Writes []WriteIntent `json:"writes"`
	// Deletes are the skill names the transaction removes.
	Deletes []string `json:"deletes"`
	// Config records that the transaction replaces the configuration file, so
	// recovery knows a configuration backup must be restored.
	Config bool `json:"config"`
	// LockExisted records whether a lockfile was present before the
	// transaction. Recovery restores the backup when it was, and removes the
	// published lockfile when it was not.
	LockExisted bool `json:"lock_existed"`
}

// Targets are the resolved live paths a transaction publishes to. They are
// supplied by the caller rather than read from the journal so a tampered
// journal can never direct recovery at a path outside the project.
type Targets struct {
	ImportedSkillsDir string
	ConfigPath        string
	SkillsLockPath    string
}

// StagingRoot returns the staging directory for an imported skill tier.
func StagingRoot(importedSkillsDir string) string {
	return filepath.Join(importedSkillsDir, StagingDirName)
}

// Write records the transaction's intent durably. It must be called after
// every backup exists in stagingRoot and before the first live path changes.
func Write(stagingRoot string, doc Document) error {
	doc.Version = Version
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the skill import journal: %w", err)
	}
	path := filepath.Join(stagingRoot, FileName)
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write the skill import journal %s: %w", path, err)
	}
	return nil
}

// MarkCommitted records that every live write already succeeded, so recovery
// keeps the new state instead of rolling it back.
func MarkCommitted(stagingRoot string) error {
	doc, err := read(stagingRoot)
	if err != nil {
		return err
	}
	doc.Committed = true
	return Write(stagingRoot, doc)
}

// Recover completes any interrupted transaction for a project.
//
// It is a no-op when no transaction is in flight. An uncommitted transaction is
// rolled back to its pre-transaction state; a committed one only has its
// staging directory cleared. Callers must already hold the project lock.
func Recover(targets Targets) error {
	stagingRoot := StagingRoot(targets.ImportedSkillsDir)
	if _, err := os.Lstat(stagingRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to inspect %s: %w", stagingRoot, err)
	}

	doc, err := read(stagingRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Staging exists but no intent was ever recorded, so nothing live
			// was touched. Clearing it restores the normal state.
			return removeStaging(stagingRoot)
		}
		return err
	}
	if !doc.Committed {
		if err := rollback(stagingRoot, targets, doc); err != nil {
			return err
		}
	}
	return removeStaging(stagingRoot)
}

func read(stagingRoot string) (Document, error) {
	path := filepath.Join(stagingRoot, FileName)
	data, err := os.ReadFile(path) // #nosec G304 -- path is the staging directory Agent Layer owns inside the resolved project root.
	if err != nil {
		return Document{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("%w: %s: %w", ErrMalformed, path, err)
	}
	if doc.Version != Version {
		return Document{}, fmt.Errorf("%w: %s: unsupported schema version %d (this Agent Layer supports %d)", ErrMalformed, path, doc.Version, Version)
	}
	names := append([]string{}, doc.Deletes...)
	for _, write := range doc.Writes {
		names = append(names, write.Name)
	}
	for _, name := range names {
		if err := validateName(name); err != nil {
			return Document{}, fmt.Errorf("%w: %s: %w", ErrMalformed, path, err)
		}
	}
	return doc, nil
}

// validateName rejects a recorded skill name that could address a path outside
// the imported skill tier.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("a recorded skill name is empty")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("recorded skill name %q is not a directory name", name)
	}
	return nil
}

// rollback restores every path the interrupted transaction had already
// replaced. Every failure is collected so an incomplete rollback is reported
// instead of being mistaken for a clean revert.
func rollback(stagingRoot string, targets Targets, doc Document) error {
	var problems []string
	note := func(err error) {
		if err != nil {
			problems = append(problems, err.Error())
		}
	}

	for _, write := range doc.Writes {
		target := filepath.Join(targets.ImportedSkillsDir, write.Name)
		backup := filepath.Join(stagingRoot, WriteBackupPrefix+write.Name)
		// A rename is atomic, so a skill that existed before is at exactly one
		// of the two paths: restore the backup when it is there, and otherwise
		// leave the untouched original alone. A skill that did not exist before
		// is reverted by removing whatever was published.
		note(restoreTree(backup, target, !write.Existed))
	}
	for _, name := range doc.Deletes {
		target := filepath.Join(targets.ImportedSkillsDir, name)
		backup := filepath.Join(stagingRoot, DeleteBackupPrefix+name)
		note(restoreTree(backup, target, false))
	}
	if doc.Config {
		note(restoreFile(filepath.Join(stagingRoot, ConfigBackupName), targets.ConfigPath))
	}
	if doc.LockExisted {
		note(restoreFile(filepath.Join(stagingRoot, LockBackupName), targets.SkillsLockPath))
	} else if err := os.Remove(targets.SkillsLockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		note(fmt.Errorf("failed to remove %s: %w", targets.SkillsLockPath, err))
	}

	if len(problems) > 0 {
		return fmt.Errorf("an interrupted skill import could not be fully rolled back: %s", strings.Join(problems, "; "))
	}
	return nil
}

// restoreTree puts a backup tree back at target. removeWhenAbsent asks for the
// target to be cleared when no backup exists, which reverts a newly created
// skill.
func restoreTree(backup string, target string, removeWhenAbsent bool) error {
	if _, err := os.Lstat(backup); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to inspect %s: %w", backup, err)
		}
		if !removeWhenAbsent {
			return nil
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("failed to remove %s: %w", target, err)
		}
		return nil
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove %s: %w", target, err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return nil
}

// restoreFile rewrites target with its pre-transaction content.
func restoreFile(backup string, target string) error {
	data, err := os.ReadFile(backup) // #nosec G304 -- backup is inside the staging directory Agent Layer owns.
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", backup, err)
	}
	if err := fsutil.WriteFileAtomic(target, data, 0o644); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return nil
}

func removeStaging(stagingRoot string) error {
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("failed to remove %s: %w", stagingRoot, err)
	}
	return nil
}
