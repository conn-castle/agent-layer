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
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// Version is the journal schema version. A journal recording a different
// version is rejected rather than guessed at, because recovering from a
// misread journal could destroy local work.
const Version = 1

// AdoptionVersion extends recovery with explicitly retired local skill slots.
const AdoptionVersion = 2

// LocalBackupPrefix distinguishes local retirements from imported writes.
const LocalBackupPrefix = "local-"

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
	Version          int      `json:"version"`
	LocalRetirements []string `json:"local_retirements,omitempty"`
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
	LocalSkillsDir    string
	ConfigPath        string
	SkillsLockPath    string
	ExactFiles        bool
}

// StagingRoot returns the staging directory for an imported skill tier.
func StagingRoot(importedSkillsDir string) string {
	return filepath.Join(importedSkillsDir, StagingDirName)
}

// Write records the transaction's intent durably, after replacement trees and
// file backups are staged and before the first live path changes. Tree backups
// are created by the subsequent live renames.
func Write(stagingRoot string, doc Document) error {
	if doc.Version == 0 {
		doc.Version = Version
	}
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
	// Every caller shares these guards. Check live roots before even inspecting
	// staging so recovery cannot consume another directory's journal/backups.
	if err := validateRecoveryRoots(targets, false); err != nil {
		return err
	}
	stagingRoot := StagingRoot(targets.ImportedSkillsDir)
	exists, err := recoveryDirectoryExists(stagingRoot)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := validateRecoveryRoots(targets, true); err != nil {
		return err
	}

	doc, err := read(stagingRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := validatePreIntentStaging(stagingRoot); err != nil {
				return err
			}
			return removeStaging(stagingRoot)
		}
		return err
	}
	if len(doc.LocalRetirements) > 0 && targets.LocalSkillsDir == "" {
		return fmt.Errorf("local recovery root is missing; preserve adoption journal %s", stagingRoot)
	}
	if err := validateRetirements(targets, doc); err != nil {
		return err
	}
	if err := validateBackups(stagingRoot, targets, doc, nil, !doc.Committed); err != nil {
		return err
	}
	if !doc.Committed {
		if err := rollback(stagingRoot, targets, doc, fsutil.WriteFileAtomic, true); err != nil {
			return err
		}
	}
	return removeStaging(stagingRoot)
}

// Progress records live paths touched by an in-process transaction. Writes
// retain whether an original moved, so missing backups cannot be mistaken for
// originals that were never changed.
type Progress struct {
	Writes           []WriteIntent
	Deletes          []string
	LocalRetirements []string
	Config           bool
	Lock             bool
}

// Rollback shares restart recovery's restore engine, but restores only touched
// paths and requires every backup the writer knows it moved. Failed validation
// leaves all live paths and recovery evidence intact.
func Rollback(targets Targets, applied Progress, writeFile func(string, []byte, os.FileMode) error) error {
	if err := validateRecoveryRoots(targets, true); err != nil {
		return err
	}
	stagingRoot := StagingRoot(targets.ImportedSkillsDir)
	if exists, err := recoveryDirectoryExists(stagingRoot); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("an interrupted skill import could not be fully rolled back: staging %s is missing; preserve live data and recovery evidence", stagingRoot)
	}
	doc, err := read(stagingRoot)
	if err != nil {
		return err
	}
	if len(doc.LocalRetirements) > 0 && targets.LocalSkillsDir == "" {
		return fmt.Errorf("local recovery root is missing; preserve adoption journal %s", stagingRoot)
	}
	var moved []string
	for _, write := range applied.Writes {
		if write.Existed {
			moved = append(moved, WriteBackupPrefix+write.Name)
		}
	}
	for _, name := range applied.Deletes {
		moved = append(moved, DeleteBackupPrefix+name)
	}
	for _, name := range applied.LocalRetirements {
		moved = append(moved, LocalBackupPrefix+name)
	}
	if err := validateRetirements(targets, doc); err != nil {
		return err
	}
	if err := validateBackups(stagingRoot, targets, doc, moved, true); err != nil {
		return err
	}
	doc.Writes, doc.Deletes, doc.LocalRetirements = applied.Writes, applied.Deletes, applied.LocalRetirements
	doc.Config = applied.Config
	return rollback(stagingRoot, targets, doc, writeFile, applied.Lock)
}

func validateRecoveryRoots(targets Targets, recovering bool) error {
	if targets.ImportedSkillsDir == "" {
		return fmt.Errorf("imported recovery root is missing; preserve recovery evidence, repair the path, then retry")
	}
	for _, root := range []string{targets.ImportedSkillsDir, targets.LocalSkillsDir} {
		// Historical v1 callers omit the local tier.
		if root == "" || (targets.ExactFiles && root == targets.LocalSkillsDir && !recovering) {
			continue
		}
		exists, err := recoveryDirectoryExists(root)
		if err != nil {
			return err
		}
		if exists && targets.ExactFiles {
			entries, err := os.ReadDir(root)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if root == targets.LocalSkillsDir && !config.LocalInstructionEntry(entry) {
					continue
				}
				if !strings.HasPrefix(entry.Name(), ".") && entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("instruction source %s must be a regular unlinked file before recovery or writes", filepath.Join(root, entry.Name()))
				}
			}
		}
	}
	return nil
}

// Missing directories are normal before the first import. Existing recovery
// roots must be real directories; Lstat never follows a linked root or staging.
func recoveryDirectoryExists(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot inspect recovery directory %s; preserve recovery evidence, repair the path, then retry: %w", dir, err)
	}
	if !info.IsDir() {
		kind := "not a directory"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "a symbolic link"
		}
		return false, fmt.Errorf("cannot recover skill imports: %s is %s; preserve the node and recovery evidence, repair the path to a real directory, then retry", dir, kind)
	}
	return true, nil
}

func read(stagingRoot string) (Document, error) {
	path := filepath.Join(stagingRoot, FileName)
	info, err := os.Lstat(path)
	if err != nil {
		return Document{}, err
	}
	if !info.Mode().IsRegular() {
		return Document{}, fmt.Errorf("%w: %s must be a regular unlinked journal; preserve recovery evidence and repair the node", ErrMalformed, path)
	}
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
	// Only whitespace may follow the single complete intent document.
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Document{}, fmt.Errorf("%w: %s: expected one complete JSON document", ErrMalformed, path)
	}
	if doc.Version != Version && doc.Version != AdoptionVersion {
		return Document{}, fmt.Errorf("%w: %s: unsupported schema version %d (this Agent Layer supports %d and %d)", ErrMalformed, path, doc.Version, Version, AdoptionVersion)
	}
	if doc.Version == Version && len(doc.LocalRetirements) > 0 {
		return Document{}, fmt.Errorf("%w: local retirements require adoption version", ErrMalformed)
	}
	seenLocal := map[string]bool{}
	for _, name := range doc.LocalRetirements {
		if (!templates.IsRetiredSkill(name) && LegacyInstruction(name) == "") || seenLocal[name] {
			return Document{}, fmt.Errorf("%w: invalid local retirement %q", ErrMalformed, name)
		}
		seenLocal[name] = true
		found := false
		for _, w := range doc.Writes {
			if w.Name == name && !w.Existed {
				found = true
			}
		}
		if !found {
			return Document{}, fmt.Errorf("%w: retirement %q requires a new imported write", ErrMalformed, name)
		}
	}
	names := append([]string{}, doc.Deletes...)
	for _, write := range doc.Writes {
		names = append(names, write.Name)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if err := validateName(name); err != nil {
			return Document{}, fmt.Errorf("%w: %s: %w", ErrMalformed, path, err)
		}
		key := strings.ToLower(skilltree.NormalizeName(name))
		if seen[key] {
			return Document{}, fmt.Errorf("%w: %s: colliding recorded skill name %q", ErrMalformed, path, name)
		}
		seen[key] = true
	}
	return doc, nil
}

// validateName rejects a recorded skill name that could address a path outside
// the imported skill tier.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("a recorded skill name is empty")
	}
	normalized := skilltree.NormalizeName(name)
	if normalized == "." || normalized == ".." || strings.ContainsAny(normalized, `/\`) {
		return fmt.Errorf("recorded skill name %q is not a directory name", name)
	}
	for _, reserved := range []string{StagingDirName, ".git", ".DS_Store", "Thumbs.db"} {
		if strings.EqualFold(normalized, reserved) {
			return fmt.Errorf("recorded skill name %q is reserved tier metadata", name)
		}
	}
	return nil
}

// Check every backup before any mutation. Rollback additionally needs each
// original: missing tree backups are valid only before a rename or after restore.
func validateBackups(stagingRoot string, targets Targets, doc Document, movedBackups []string, rollback bool) error {
	check := func(name, original string, directory, required bool) error {
		path := filepath.Join(stagingRoot, name)
		info, err := os.Lstat(path)
		if err == nil {
			if (directory && info.IsDir()) || (!directory && info.Mode().IsRegular()) {
				return nil
			}
			return fmt.Errorf("%w: backup %s has an unsafe node type; preserve recovery evidence and repair the node", ErrMalformed, path)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: cannot inspect backup %s: %w", ErrMalformed, path, err)
		}
		if !rollback || !required {
			return nil
		}
		detail := fmt.Sprintf("required backup %s is missing", path)
		if original != "" && !slices.Contains(movedBackups, name) {
			if info, err := os.Lstat(original); err == nil && ((directory && info.IsDir()) || (!directory && info.Mode().IsRegular())) {
				return nil
			}
			detail += fmt.Sprintf("; original %s is absent or not a real directory", original)
		}
		return fmt.Errorf("an interrupted skill import could not be fully rolled back: %s; preserve all live data and recovery evidence, repair the missing evidence, then retry", detail)
	}
	for _, write := range doc.Writes {
		if err := check(WriteBackupPrefix+write.Name, filepath.Join(targets.ImportedSkillsDir, write.Name), !targets.ExactFiles, write.Existed); err != nil {
			return err
		}
	}
	for _, name := range doc.Deletes {
		if err := check(DeleteBackupPrefix+name, filepath.Join(targets.ImportedSkillsDir, name), !targets.ExactFiles, true); err != nil {
			return err
		}
	}
	for _, name := range doc.LocalRetirements {
		if err := check(LocalBackupPrefix+name, targets.LocalPath(name), !targets.ExactFiles, true); err != nil {
			return err
		}
	}
	if doc.Config {
		if err := check(ConfigBackupName, "", false, true); err != nil {
			return err
		}
	}
	if doc.LockExisted {
		if err := check(LockBackupName, "", false, true); err != nil {
			return err
		}
	}
	return nil
}

// These backup names are created only by live renames after durable intent.
// Their presence without a journal contradicts harmless pre-intent staging.
// ReadDir inspects names without following even dangling backup links.
func validatePreIntentStaging(stagingRoot string) error {
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		return fmt.Errorf("cannot inspect staging %s; preserve recovery evidence: %w", stagingRoot, err)
	}
	for _, entry := range entries {
		for _, prefix := range []string{LocalBackupPrefix, WriteBackupPrefix, DeleteBackupPrefix} {
			if strings.HasPrefix(entry.Name(), prefix) {
				return fmt.Errorf("%w: missing journal in %s with post-intent backup %s; preserve all live data and recovery evidence", ErrMalformed, stagingRoot, entry.Name())
			}
		}
	}
	return nil
}

// rollback restores every path the interrupted transaction had already
// replaced. Every failure is collected so an incomplete rollback is reported
// instead of being mistaken for a clean revert.
func rollback(stagingRoot string, targets Targets, doc Document, writeFile func(string, []byte, os.FileMode) error, restoreLock bool) error {
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
	for _, name := range doc.LocalRetirements {
		note(restoreTree(filepath.Join(stagingRoot, LocalBackupPrefix+name), targets.LocalPath(name), false))
	}
	if doc.Config {
		note(restoreFile(filepath.Join(stagingRoot, ConfigBackupName), targets.ConfigPath, writeFile))
	}
	if !restoreLock {
		// The writer failed before touching the lock; preserve its bytes and mode.
	} else if doc.LockExisted {
		note(restoreFile(filepath.Join(stagingRoot, LockBackupName), targets.SkillsLockPath, writeFile))
	} else if err := os.Remove(targets.SkillsLockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		note(fmt.Errorf("failed to remove %s: %w", targets.SkillsLockPath, err))
	} else {
		note(fsutil.SyncDir(filepath.Dir(targets.SkillsLockPath)))
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
		if _, err := os.Stat(filepath.Dir(target)); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fsutil.SyncDir(filepath.Dir(target))
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove %s: %w", target, err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return errors.Join(fsutil.SyncDir(filepath.Dir(backup)), fsutil.SyncDir(filepath.Dir(target)))
}

// restoreFile rewrites target with its pre-transaction content.
func restoreFile(backup string, target string, writeFile func(string, []byte, os.FileMode) error) error {
	data, err := os.ReadFile(backup) // #nosec G304 -- backup is inside the staging directory Agent Layer owns.
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", backup, err)
	}
	if err := writeFile(target, data, 0o644); err != nil {
		return fmt.Errorf("failed to restore %s: %w", target, err)
	}
	return nil
}

func removeStaging(stagingRoot string) error {
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("failed to remove %s: %w", stagingRoot, err)
	}
	return fsutil.SyncDir(filepath.Dir(stagingRoot))
}

// LegacyInstruction is the fixed trusted adoption rename map, never journal data.
func LegacyInstruction(name string) string {
	switch name {
	case "rules.md":
		return "00_rules.md"
	case "memory.md":
		return "01_memory.md"
	}
	return ""
}

// LocalPath maps only trusted standard instruction names to their legacy slots.
func (t Targets) LocalPath(name string) string {
	if t.ExactFiles {
		if old := LegacyInstruction(name); old != "" {
			name = old
		}
	}
	return filepath.Join(t.LocalSkillsDir, name)
}
func validateRetirements(t Targets, d Document) error {
	for _, name := range d.LocalRetirements {
		if (t.ExactFiles && LegacyInstruction(name) == "") || (!t.ExactFiles && !templates.IsRetiredSkill(name)) {
			return fmt.Errorf("%w: invalid retirement for this import tier", ErrMalformed)
		}
	}
	return nil
}

// RecoverBoth uses trusted coordinates for both kinds before any source consumer.
func RecoverBoth(root string) error {
	base := filepath.Join(root, ".agent-layer")
	if _, err := recoveryDirectoryExists(base); err != nil {
		return err
	}
	var targets []Targets
	for _, kind := range []string{"skills", "instructions"} {
		target := Targets{ImportedSkillsDir: filepath.Join(base, kind+"-imported"), LocalSkillsDir: filepath.Join(base, kind), SkillsLockPath: filepath.Join(base, kind+".lock.json"), ConfigPath: filepath.Join(base, "config.toml"), ExactFiles: kind == "instructions"}
		if err := validateRecoveryRoots(target, false); err != nil {
			return err
		}
		targets = append(targets, target)
	}
	for _, target := range targets {
		if err := Recover(target); err != nil {
			return err
		}
	}
	return nil
}
