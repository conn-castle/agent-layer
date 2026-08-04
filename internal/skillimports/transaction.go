package skillimports

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	// stagingDirName and journalFileName are sourced from internal/config, the
	// package that owns the .agent-layer layout contract, so the paths this
	// package creates and the paths install/upgrade treat as known can never
	// drift.
	stagingDirName  = config.SkillImportStagingDirName
	journalFileName = config.SkillImportJournalFileName
	// journalVersion is the only journal schema this release understands.
	journalVersion = 1
)

// Journal phases.
const (
	// phasePending means the publish had not reached its commit point.
	phasePending = "pending"
	// phaseCommitted means every mutation landed and only cleanup remains.
	phaseCommitted = "committed"
)

// publishRecord is one skill directory mutation inside a transaction.
type publishRecord struct {
	// SkillName is the local directory name being published or retired.
	SkillName string `json:"skill_name"`
	// Target is the live directory path.
	Target string `json:"target"`
	// Staged is the prepared replacement, empty when the skill is being retired.
	Staged string `json:"staged"`
	// Backup is where an existing target is moved before the replacement lands.
	Backup string `json:"backup"`
	// TargetExisted records whether the live directory existed when the
	// transaction was prepared, so rollback knows whether to restore or remove.
	TargetExisted bool `json:"target_existed"`
}

// fileRecord is one whole-file snapshot mutation (config or lock).
type fileRecord struct {
	// Path is the live file.
	Path string `json:"path"`
	// Staged is the prepared replacement.
	Staged string `json:"staged"`
	// Backup is where the existing file is copied before replacement.
	Backup string `json:"backup"`
	// Existed records whether the live file existed when the transaction was
	// prepared.
	Existed bool `json:"existed"`
}

// journal is the durable description of an in-flight publish.
type journal struct {
	Version    int             `json:"version"`
	Phase      string          `json:"phase"`
	StagingDir string          `json:"staging_dir"`
	Publishes  []publishRecord `json:"publishes"`
	Config     *fileRecord     `json:"config,omitempty"`
	Lock       *fileRecord     `json:"lock,omitempty"`
}

// Transaction stages a complete change to imported skill trees, configuration,
// and lock state, then publishes it so an interruption never leaves a partial
// skill or a lock that has advanced past unpublished content.
type Transaction struct {
	root        string
	stagingDir  string
	journalPath string
	publishes   []publishRecord
	config      *fileRecord
	lock        *fileRecord
}

// StagingRoot returns the staging directory for a repo root.
func StagingRoot(root string) string {
	return filepath.Join(root, ".agent-layer", stagingDirName)
}

// JournalPath returns the transaction journal path for a repo root.
func JournalPath(root string) string {
	return filepath.Join(root, ".agent-layer", journalFileName)
}

// ImportedSkillsRoot returns the managed imported-skills directory for a repo root.
func ImportedSkillsRoot(root string) string {
	return filepath.Join(root, ".agent-layer", config.ImportedSkillsDirName)
}

// NewTransaction prepares a clean staging area. Any leftover staging content is
// removed only after RecoverTransaction has resolved a previous interruption, so
// callers must recover first.
func NewTransaction(root string) (*Transaction, error) {
	stagingRoot := StagingRoot(root)
	if err := os.MkdirAll(stagingRoot, DirectoryMode); err != nil {
		return nil, fmt.Errorf("create staging root %s: %w", stagingRoot, err)
	}
	staging, err := os.MkdirTemp(stagingRoot, "transaction-")
	if err != nil {
		return nil, fmt.Errorf("create transaction staging directory under %s: %w", stagingRoot, err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "staged"), DirectoryMode); err != nil {
		_ = os.RemoveAll(staging)
		return nil, fmt.Errorf("create staging directory %s: %w", staging, err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "backup"), DirectoryMode); err != nil {
		_ = os.RemoveAll(staging)
		return nil, fmt.Errorf("create staging directory %s: %w", staging, err)
	}
	return &Transaction{
		root:        root,
		stagingDir:  staging,
		journalPath: JournalPath(root),
	}, nil
}

// StageSkill stages a validated tree to replace or create one imported skill.
func (t *Transaction) StageSkill(name string, tree *Tree) error {
	staged := filepath.Join(t.stagingDir, "staged", name)
	if err := os.RemoveAll(staged); err != nil {
		return fmt.Errorf("clear staged skill %s: %w", staged, err)
	}
	if err := WriteTree(tree, staged); err != nil {
		return err
	}
	return t.record(name, staged)
}

// StageSkillRemoval stages the retirement of one imported skill directory.
func (t *Transaction) StageSkillRemoval(name string) error {
	return t.record(name, "")
}

func (t *Transaction) record(name string, staged string) error {
	if !config.IsSafeSkillImportName(name) {
		return fmt.Errorf("skill name %q is not safe for a managed path", name)
	}
	target := filepath.Join(ImportedSkillsRoot(t.root), name)
	existed, err := pathExists(target)
	if err != nil {
		return err
	}
	for _, existing := range t.publishes {
		if existing.SkillName == name {
			return fmt.Errorf("skill %q is staged twice in one transaction", name)
		}
	}
	t.publishes = append(t.publishes, publishRecord{
		SkillName:     name,
		Target:        target,
		Staged:        staged,
		Backup:        filepath.Join(t.stagingDir, "backup", name),
		TargetExisted: existed,
	})
	return nil
}

// StageConfig stages replacement bytes for .agent-layer/config.toml.
func (t *Transaction) StageConfig(path string, data []byte) error {
	record, err := t.stageFile("config.toml", path, data)
	if err != nil {
		return err
	}
	t.config = record
	return nil
}

// StageLock stages replacement bytes for the skill import lock.
func (t *Transaction) StageLock(path string, data []byte) error {
	record, err := t.stageFile("lock.json", path, data)
	if err != nil {
		return err
	}
	t.lock = record
	return nil
}

func (t *Transaction) stageFile(name string, path string, data []byte) (*fileRecord, error) {
	staged := filepath.Join(t.stagingDir, name+".new")
	// #nosec G703 -- staged is built from an internal constant name inside the
	// staging directory this transaction just created; no caller input reaches it.
	if err := os.WriteFile(staged, data, RegularFileMode); err != nil {
		return nil, fmt.Errorf("stage %s: %w", staged, err)
	}
	existed, err := pathExists(path)
	if err != nil {
		return nil, err
	}
	return &fileRecord{
		Path:    path,
		Staged:  staged,
		Backup:  filepath.Join(t.stagingDir, name+".backup"),
		Existed: existed,
	}, nil
}

// Empty reports whether the transaction would change nothing.
func (t *Transaction) Empty() bool {
	return len(t.publishes) == 0 && t.config == nil && t.lock == nil
}

// Commit publishes every staged change. Skill trees land first, then the
// configuration snapshot, then the lock. A failure at any point rolls the whole
// transaction back to its pre-publish state, so a lock never advances past a
// tree that was not published.
func (t *Transaction) Commit() error {
	if t.Empty() {
		return t.discard()
	}
	sort.Slice(t.publishes, func(i, j int) bool { return t.publishes[i].SkillName < t.publishes[j].SkillName })
	record := &journal{
		Version:    journalVersion,
		Phase:      phasePending,
		StagingDir: t.stagingDir,
		Publishes:  t.publishes,
		Config:     t.config,
		Lock:       t.lock,
	}
	if err := validateJournal(t.root, record); err != nil {
		return fmt.Errorf("refuse unsafe transaction: %w", err)
	}
	if _, err := os.Lstat(t.journalPath); err == nil {
		return fmt.Errorf("transaction journal %s already exists; recover it before publishing another transaction", t.journalPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect transaction journal %s: %w", t.journalPath, err)
	}
	if err := writeJournal(t.journalPath, record); err != nil {
		return err
	}
	if err := applyJournal(record); err != nil {
		if rollbackErr := rollbackJournal(record); rollbackErr != nil {
			return fmt.Errorf("%w; rolling back also failed: %v", err, rollbackErr)
		}
		if removeErr := finishJournal(t.journalPath, record); removeErr != nil {
			return fmt.Errorf("%w; clearing the transaction journal also failed: %v", err, removeErr)
		}
		return err
	}

	record.Phase = phaseCommitted
	if err := writeJournal(t.journalPath, record); err != nil {
		return err
	}
	return finishJournal(t.journalPath, record)
}

func (t *Transaction) discard() error {
	if err := os.RemoveAll(t.stagingDir); err != nil {
		return fmt.Errorf("clear staging directory %s: %w", t.stagingDir, err)
	}
	removeEmptyStagingRoot(filepath.Dir(t.stagingDir))
	return nil
}

// applyJournal performs the journal's mutations in commit order.
func applyJournal(record *journal) error {
	for i := range record.Publishes {
		publish := record.Publishes[i]
		if publish.TargetExisted {
			if err := os.Rename(publish.Target, publish.Backup); err != nil {
				return fmt.Errorf("move %s aside: %w", publish.Target, err)
			}
		}
		if publish.Staged == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(publish.Target), DirectoryMode); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(publish.Target), err)
		}
		if err := os.Rename(publish.Staged, publish.Target); err != nil {
			return fmt.Errorf("publish %s: %w", publish.Target, err)
		}
	}
	if err := applyFileRecord(record.Config); err != nil {
		return err
	}
	return applyFileRecord(record.Lock)
}

func applyFileRecord(record *fileRecord) error {
	if record == nil {
		return nil
	}
	if record.Existed {
		data, err := os.ReadFile(record.Path) // #nosec G304 -- record.Path is an Agent Layer-owned file under .agent-layer.
		if err != nil {
			return fmt.Errorf("read %s before replacing it: %w", record.Path, err)
		}
		// #nosec G703 -- record.Backup is an Agent Layer-owned path inside the
		// staging directory, written by stageFile above.
		if err := os.WriteFile(record.Backup, data, RegularFileMode); err != nil {
			return fmt.Errorf("back up %s: %w", record.Path, err)
		}
	}
	if err := os.Rename(record.Staged, record.Path); err != nil {
		return fmt.Errorf("publish %s: %w", record.Path, err)
	}
	return nil
}

// rollbackJournal restores every mutation the journal describes. It is safe to
// run repeatedly: each step checks the current state before acting.
func rollbackJournal(record *journal) error {
	var failures []error
	if err := rollbackFileRecord(record.Lock); err != nil {
		failures = append(failures, err)
	}
	if err := rollbackFileRecord(record.Config); err != nil {
		failures = append(failures, err)
	}
	for i := len(record.Publishes) - 1; i >= 0; i-- {
		publish := record.Publishes[i]
		backupExists, err := pathExists(publish.Backup)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if backupExists {
			if err := os.RemoveAll(publish.Target); err != nil {
				failures = append(failures, fmt.Errorf("clear %s during rollback: %w", publish.Target, err))
				continue
			}
			if err := os.Rename(publish.Backup, publish.Target); err != nil {
				failures = append(failures, fmt.Errorf("restore %s during rollback: %w", publish.Target, err))
			}
			continue
		}
		if !publish.TargetExisted {
			if err := os.RemoveAll(publish.Target); err != nil {
				failures = append(failures, fmt.Errorf("remove %s during rollback: %w", publish.Target, err))
			}
		}
	}
	return errors.Join(failures...)
}

func rollbackFileRecord(record *fileRecord) error {
	if record == nil {
		return nil
	}
	backupExists, err := pathExists(record.Backup)
	if err != nil {
		return err
	}
	if backupExists {
		data, err := os.ReadFile(record.Backup) // #nosec G304 -- record.Backup is inside the Agent Layer-owned staging directory.
		if err != nil {
			return fmt.Errorf("read %s during rollback: %w", record.Backup, err)
		}
		if err := fsutil.WriteFileAtomic(record.Path, data, RegularFileMode); err != nil {
			return fmt.Errorf("restore %s during rollback: %w", record.Path, err)
		}
		return nil
	}
	if !record.Existed {
		if err := os.Remove(record.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s during rollback: %w", record.Path, err)
		}
	}
	return nil
}

// finishJournal removes staged content and the journal itself.
func finishJournal(journalPath string, record *journal) error {
	if err := os.RemoveAll(record.StagingDir); err != nil {
		return fmt.Errorf("clear staging directory %s: %w", record.StagingDir, err)
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove transaction journal %s: %w", journalPath, err)
	}
	removeEmptyStagingRoot(filepath.Dir(record.StagingDir))
	return nil
}

func removeEmptyStagingRoot(path string) {
	_ = os.Remove(path) // Removal succeeds only when no other transaction is staging there.
}

// RecoveryOutcome describes what RecoverTransaction did.
type RecoveryOutcome struct {
	// Recovered is true when an interrupted publish was found.
	Recovered bool
	// RolledForward is true when the interrupted publish had already committed and
	// only cleanup remained.
	RolledForward bool
}

// RecoverTransaction deterministically finishes or undoes an interrupted
// publish. It must run before any command reads or mutates import state.
//
// A journal that has not reached its commit point is rolled back completely,
// including the lock, so local content and lock state stay consistent with each
// other. A journal that reached its commit point only needs its staging area
// cleared.
func RecoverTransaction(root string) (RecoveryOutcome, error) {
	journalPath := JournalPath(root)
	data, err := os.ReadFile(journalPath) // #nosec G304 -- journalPath is Agent Layer-owned state under .agent-layer.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RecoveryOutcome{}, nil
		}
		return RecoveryOutcome{}, fmt.Errorf("read transaction journal %s: %w", journalPath, err)
	}
	var record journal
	if err := json.Unmarshal(data, &record); err != nil {
		return RecoveryOutcome{}, fmt.Errorf(
			"transaction journal %s is unreadable (%v); resolve it by hand rather than letting Agent Layer guess which half of a publish landed",
			journalPath, err,
		)
	}
	if record.Version != journalVersion {
		return RecoveryOutcome{}, fmt.Errorf(
			"transaction journal %s has unsupported version %d", journalPath, record.Version,
		)
	}
	if err := validateJournal(root, &record); err != nil {
		return RecoveryOutcome{}, fmt.Errorf("transaction journal %s is unsafe: %w", journalPath, err)
	}
	switch record.Phase {
	case phaseCommitted:
		if err := finishJournal(journalPath, &record); err != nil {
			return RecoveryOutcome{}, err
		}
		return RecoveryOutcome{Recovered: true, RolledForward: true}, nil
	case phasePending:
		if err := rollbackJournal(&record); err != nil {
			return RecoveryOutcome{}, fmt.Errorf("roll back interrupted skill import publish: %w", err)
		}
		if err := finishJournal(journalPath, &record); err != nil {
			return RecoveryOutcome{}, err
		}
		return RecoveryOutcome{Recovered: true}, nil
	default:
		return RecoveryOutcome{}, fmt.Errorf(
			"transaction journal %s has unknown phase %q", journalPath, record.Phase,
		)
	}
}

func validateJournal(root string, record *journal) error {
	stagingRoot := filepath.Clean(StagingRoot(root))
	stagingDir := filepath.Clean(record.StagingDir)
	if filepath.Dir(stagingDir) != stagingRoot || !strings.HasPrefix(filepath.Base(stagingDir), "transaction-") {
		return fmt.Errorf("staging_dir is not a transaction directory under %s", stagingRoot)
	}
	seen := make(map[string]struct{}, len(record.Publishes))
	for _, publish := range record.Publishes {
		if !config.IsSafeSkillImportName(publish.SkillName) {
			return fmt.Errorf("skill_name %q is not safe", publish.SkillName)
		}
		if _, exists := seen[publish.SkillName]; exists {
			return fmt.Errorf("skill_name %q is recorded twice", publish.SkillName)
		}
		seen[publish.SkillName] = struct{}{}
		if filepath.Clean(publish.Target) != filepath.Join(ImportedSkillsRoot(root), publish.SkillName) {
			return fmt.Errorf("target for %q escapes the managed imported-skills root", publish.SkillName)
		}
		wantBackup := filepath.Join(stagingDir, "backup", publish.SkillName)
		if filepath.Clean(publish.Backup) != wantBackup {
			return fmt.Errorf("backup for %q is outside its transaction", publish.SkillName)
		}
		if publish.Staged != "" && filepath.Clean(publish.Staged) != filepath.Join(stagingDir, "staged", publish.SkillName) {
			return fmt.Errorf("staged path for %q is outside its transaction", publish.SkillName)
		}
	}
	paths := config.DefaultPaths(root)
	if err := validateJournalFile(record.Config, paths.ConfigPath, filepath.Join(stagingDir, "config.toml.new"), filepath.Join(stagingDir, "config.toml.backup")); err != nil {
		return fmt.Errorf("config record: %w", err)
	}
	if err := validateJournalFile(record.Lock, paths.SkillImportLockPath, filepath.Join(stagingDir, "lock.json.new"), filepath.Join(stagingDir, "lock.json.backup")); err != nil {
		return fmt.Errorf("lock record: %w", err)
	}
	return nil
}

func validateJournalFile(record *fileRecord, path string, staged string, backup string) error {
	if record == nil {
		return nil
	}
	if filepath.Clean(record.Path) != filepath.Clean(path) ||
		filepath.Clean(record.Staged) != filepath.Clean(staged) ||
		filepath.Clean(record.Backup) != filepath.Clean(backup) {
		return fmt.Errorf("contains a path outside the expected project state files")
	}
	return nil
}

func writeJournal(path string, record *journal) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction journal: %w", err)
	}
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), RegularFileMode); err != nil {
		return fmt.Errorf("write transaction journal %s: %w", path, err)
	}
	return nil
}

func pathExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	return true, nil
}
