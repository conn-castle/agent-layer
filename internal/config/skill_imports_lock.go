package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// SkillImportLockVersion is the only lock schema version this release reads or
// writes. A different version is rejected rather than guessed at.
const SkillImportLockVersion = 1

// SkillImportLockFileName is the canonical lock file under .agent-layer/.
const SkillImportLockFileName = "skill-imports.lock.json"

// ImportedSkillsDirName is the managed editable root for imported skills.
const ImportedSkillsDirName = "imported-skills"

// SkillImportJournalFileName records an in-flight skill import publish so the
// next command can finish or undo it deterministically.
const SkillImportJournalFileName = "skill-imports.journal.json"

// SkillImportStagingDirName holds staged trees and displaced backups while a
// skill import publish is in flight. It sits beside the managed skill root so
// every publish is a rename within one filesystem.
//
// These names live here, in the package that owns the .agent-layer layout
// contract, so the importer that creates them and the installer that must never
// classify them as unknown files share one source.
const SkillImportStagingDirName = "skill-imports.staging"

// Resolved ref kinds recorded in the lock.
const (
	// SkillRefBranch is a ref resolved under refs/heads/.
	SkillRefBranch = "branch"
	// SkillRefTag is a ref resolved under refs/tags/.
	SkillRefTag = "tag"
	// SkillRefCommit is a configured ref that names a commit object directly.
	SkillRefCommit = "commit"
)

// ErrSkillImportLockMalformed marks a lock file that cannot be trusted. Callers
// must preserve local content and fail the affected imports rather than
// reconstructing a merge base.
var ErrSkillImportLockMalformed = errors.New("skill import lock is malformed")

// SkillImportLock is the canonical resolved state for every managed import.
// Configuration is the desired set; this file records what was actually
// resolved, imported, and merged from.
type SkillImportLock struct {
	Version int                    `json:"version"`
	Entries []SkillImportLockEntry `json:"entries"`
}

// SkillImportLockEntry is one flattened import: exactly one source path in one
// source repository, projected to exactly one local skill directory.
type SkillImportLockEntry struct {
	// Repository is the configured source repository string, verbatim.
	Repository string `json:"repository"`
	// SourcePath is the slash-normalized repository-relative skill root.
	SourcePath string `json:"source_path"`
	// ConfiguredRef is the block's configured ref, empty when omitted.
	ConfiguredRef string `json:"configured_ref"`
	// RefOmitted records that the block deliberately omitted a ref, so a later
	// default-branch rename is recognized as a retarget rather than a mismatch.
	RefOmitted bool `json:"ref_omitted"`
	// ResolvedRefName is the actual branch or tag name, or the commit id when the
	// configured ref named a commit.
	ResolvedRefName string `json:"resolved_ref_name"`
	// ResolvedRefType is SkillRefBranch, SkillRefTag, or SkillRefCommit.
	ResolvedRefType string `json:"resolved_ref_type"`
	// SourceCommit is the upstream commit this entry was last reconciled from.
	// It is the merge base for the next pull and the base for the next push.
	SourceCommit string `json:"source_commit"`
	// UpstreamTreeHash is the canonical tree hash of the upstream skill tree at
	// SourceCommit. Local edits never replace it.
	UpstreamTreeHash string `json:"upstream_tree_hash"`
	// Tracking is SkillTrackingTracked or SkillTrackingPinned.
	Tracking string `json:"tracking"`
	// Write is the block's effective write policy.
	Write string `json:"write"`
	// PushRepository is the effective destination repository.
	PushRepository string `json:"push_repository"`
	// PushBranch is the configured destination branch for write = "branch".
	PushBranch string `json:"push_branch"`
	// SkillName is the validated skill name and the local directory name.
	SkillName string `json:"skill_name"`
}

// SkillImportEntryKey identifies a lock entry by the pair that must stay unique:
// the exact source repository string and the selected source path.
type SkillImportEntryKey struct {
	Repository string
	SourcePath string
}

// Key returns the entry's uniqueness key.
func (e SkillImportLockEntry) Key() SkillImportEntryKey {
	return SkillImportEntryKey{Repository: e.Repository, SourcePath: e.SourcePath}
}

// LoadSkillImportLock reads and validates the lock from disk. A missing file is
// an empty lock, which is the correct state for a project with no imports. Any
// other failure is reported; the caller must not invent a merge base.
func LoadSkillImportLock(path string) (*SkillImportLock, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from the resolved repo root's .agent-layer directory.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &SkillImportLock{Version: SkillImportLockVersion}, nil
		}
		return nil, fmt.Errorf("read skill import lock %s: %w", path, err)
	}
	return ParseSkillImportLock(data, path)
}

// LoadSkillImportLockFS reads and validates the lock from an fs.FS rooted at the
// repo root. root resolves absolute paths; path is used for error messages.
func LoadSkillImportLockFS(fsys fs.FS, root string, lockPath string) (*SkillImportLock, error) {
	data, err := readFileFS(fsys, root, lockPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &SkillImportLock{Version: SkillImportLockVersion}, nil
		}
		return nil, fmt.Errorf("read skill import lock %s: %w", lockPath, err)
	}
	return ParseSkillImportLock(data, lockPath)
}

// ParseSkillImportLock decodes and strictly validates lock bytes. Every failure
// wraps ErrSkillImportLockMalformed so callers can preserve local content and
// fail the affected imports.
func ParseSkillImportLock(data []byte, source string) (*SkillImportLock, error) {
	var lock SkillImportLock
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrSkillImportLockMalformed, source, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("%w: %s: trailing content after the lock object", ErrSkillImportLockMalformed, source)
	}
	if lock.Version != SkillImportLockVersion {
		return nil, fmt.Errorf(
			"%w: %s: version %d is not supported (expected %d)",
			ErrSkillImportLockMalformed, source, lock.Version, SkillImportLockVersion,
		)
	}
	if err := validateSkillImportLockEntries(lock.Entries, source); err != nil {
		return nil, err
	}
	SortSkillImportLockEntries(lock.Entries)
	return &lock, nil
}

func validateSkillImportLockEntries(entries []SkillImportLockEntry, source string) error {
	seenKeys := make(map[SkillImportEntryKey]struct{}, len(entries))
	seenNames := make(map[string]string, len(entries))
	for i, entry := range entries {
		required := []struct {
			field string
			value string
		}{
			{"repository", entry.Repository},
			{"source_path", entry.SourcePath},
			{"resolved_ref_name", entry.ResolvedRefName},
			{"resolved_ref_type", entry.ResolvedRefType},
			{"source_commit", entry.SourceCommit},
			{"upstream_tree_hash", entry.UpstreamTreeHash},
			{"tracking", entry.Tracking},
			{"write", entry.Write},
			{"push_repository", entry.PushRepository},
			{"skill_name", entry.SkillName},
		}
		for _, field := range required {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%w: %s: entries[%d].%s is required", ErrSkillImportLockMalformed, source, i, field.field)
			}
		}
		if entry.Repository != NormalizeSkillRepository(entry.Repository) {
			return fmt.Errorf("%w: %s: entries[%d].repository is not normalized", ErrSkillImportLockMalformed, source, i)
		}
		if err := validateSkillRepositoryString(entry.Repository); err != nil {
			return fmt.Errorf("%w: %s: entries[%d].repository %w", ErrSkillImportLockMalformed, source, i, err)
		}
		if entry.PushRepository != NormalizeSkillRepository(entry.PushRepository) {
			return fmt.Errorf("%w: %s: entries[%d].push_repository is not normalized", ErrSkillImportLockMalformed, source, i)
		}
		if err := validateSkillRepositoryString(entry.PushRepository); err != nil {
			return fmt.Errorf("%w: %s: entries[%d].push_repository %w", ErrSkillImportLockMalformed, source, i, err)
		}
		if !isHexIdentifier(entry.SourceCommit, 40, 64) {
			return fmt.Errorf("%w: %s: entries[%d].source_commit must be a full hexadecimal object id", ErrSkillImportLockMalformed, source, i)
		}
		if !strings.HasPrefix(entry.UpstreamTreeHash, skilltree.HashPrefix) || !isHexIdentifier(strings.TrimPrefix(entry.UpstreamTreeHash, skilltree.HashPrefix), 64) {
			return fmt.Errorf("%w: %s: entries[%d].upstream_tree_hash must be a versioned SHA-256 tree digest", ErrSkillImportLockMalformed, source, i)
		}
		switch entry.ResolvedRefType {
		case SkillRefBranch, SkillRefTag, SkillRefCommit:
		default:
			return fmt.Errorf(
				"%w: %s: entries[%d].resolved_ref_type %q is not %q, %q, or %q",
				ErrSkillImportLockMalformed, source, i, entry.ResolvedRefType, SkillRefBranch, SkillRefTag, SkillRefCommit,
			)
		}
		switch entry.Tracking {
		case SkillTrackingTracked, SkillTrackingPinned:
		default:
			return fmt.Errorf(
				"%w: %s: entries[%d].tracking %q is not %q or %q",
				ErrSkillImportLockMalformed, source, i, entry.Tracking, SkillTrackingTracked, SkillTrackingPinned,
			)
		}
		if entry.Tracking == SkillTrackingTracked && entry.ResolvedRefType != SkillRefBranch {
			return fmt.Errorf(
				"%w: %s: entries[%d] tracks a %s ref; only branches can be tracked",
				ErrSkillImportLockMalformed, source, i, entry.ResolvedRefType,
			)
		}
		switch entry.Write {
		case SkillWriteNone, SkillWriteBranch, SkillWriteDirect:
		default:
			return fmt.Errorf(
				"%w: %s: entries[%d].write %q is not %q, %q, or %q",
				ErrSkillImportLockMalformed, source, i, entry.Write, SkillWriteNone, SkillWriteBranch, SkillWriteDirect,
			)
		}
		if entry.Write == SkillWriteBranch && strings.TrimSpace(entry.PushBranch) == "" {
			return fmt.Errorf("%w: %s: entries[%d].push_branch is required for write %q", ErrSkillImportLockMalformed, source, i, SkillWriteBranch)
		}
		if entry.Write != SkillWriteBranch && strings.TrimSpace(entry.PushBranch) != "" {
			return fmt.Errorf("%w: %s: entries[%d].push_branch is only valid for write %q", ErrSkillImportLockMalformed, source, i, SkillWriteBranch)
		}
		if entry.RefOmitted && strings.TrimSpace(entry.ConfiguredRef) != "" {
			return fmt.Errorf("%w: %s: entries[%d] records both an omitted and a configured ref", ErrSkillImportLockMalformed, source, i)
		}
		if !entry.RefOmitted {
			if strings.TrimSpace(entry.ConfiguredRef) == "" {
				return fmt.Errorf("%w: %s: entries[%d].configured_ref is required when ref_omitted is false", ErrSkillImportLockMalformed, source, i)
			}
			if err := validateSkillRefString(entry.ConfiguredRef); err != nil {
				return fmt.Errorf("%w: %s: entries[%d].configured_ref %w", ErrSkillImportLockMalformed, source, i, err)
			}
		}
		if err := validateSkillRefString(entry.ResolvedRefName); err != nil {
			return fmt.Errorf("%w: %s: entries[%d].resolved_ref_name %w", ErrSkillImportLockMalformed, source, i, err)
		}
		if entry.PushBranch != "" {
			if err := validateSkillRefString(entry.PushBranch); err != nil {
				return fmt.Errorf("%w: %s: entries[%d].push_branch %w", ErrSkillImportLockMalformed, source, i, err)
			}
		}
		normalizedPath, _, err := ParseSkillSelector(entry.SourcePath)
		if err != nil {
			return fmt.Errorf("%w: %s: entries[%d].source_path: %v", ErrSkillImportLockMalformed, source, i, err)
		}
		if normalizedPath != entry.SourcePath {
			return fmt.Errorf(
				"%w: %s: entries[%d].source_path %q is not normalized (expected %q)",
				ErrSkillImportLockMalformed, source, i, entry.SourcePath, normalizedPath,
			)
		}
		if IsSkillSelectorWildcard(entry.SourcePath) {
			return fmt.Errorf(
				"%w: %s: entries[%d].source_path %q is a pattern; lock entries record resolved paths",
				ErrSkillImportLockMalformed, source, i, entry.SourcePath,
			)
		}
		key := entry.Key()
		if _, ok := seenKeys[key]; ok {
			return fmt.Errorf(
				"%w: %s: entries[%d] duplicates repository %q source path %q",
				ErrSkillImportLockMalformed, source, i, entry.Repository, entry.SourcePath,
			)
		}
		seenKeys[key] = struct{}{}

		normalizedName := NormalizeSkillImportName(entry.SkillName)
		if normalizedName != entry.SkillName || !IsSafeSkillImportName(entry.SkillName) {
			return fmt.Errorf("%w: %s: entries[%d].skill_name %q is not a safe normalized skill name", ErrSkillImportLockMalformed, source, i, entry.SkillName)
		}
		if other, ok := seenNames[normalizedName]; ok {
			return fmt.Errorf(
				"%w: %s: entries[%d] skill name %q collides with %q; two imports cannot own one local directory",
				ErrSkillImportLockMalformed, source, i, entry.SkillName, other,
			)
		}
		seenNames[normalizedName] = entry.SkillName
	}
	return nil
}

func isHexIdentifier(value string, lengths ...int) bool {
	validLength := false
	for _, length := range lengths {
		if len(value) == length {
			validLength = true
			break
		}
	}
	if !validLength {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// SortSkillImportLockEntries orders entries deterministically by repository then
// source path, so the serialized lock has no incidental diff churn.
func SortSkillImportLockEntries(entries []SkillImportLockEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repository != entries[j].Repository {
			return entries[i].Repository < entries[j].Repository
		}
		return entries[i].SourcePath < entries[j].SourcePath
	})
}

// MarshalSkillImportLock renders the lock as deterministic pretty JSON with a
// trailing newline. Entries are sorted first so repeated runs produce identical
// bytes.
func MarshalSkillImportLock(lock *SkillImportLock) ([]byte, error) {
	if lock == nil {
		return nil, fmt.Errorf("skill import lock is required")
	}
	normalized := SkillImportLock{Version: SkillImportLockVersion, Entries: append([]SkillImportLockEntry(nil), lock.Entries...)}
	if normalized.Entries == nil {
		normalized.Entries = []SkillImportLockEntry{}
	}
	SortSkillImportLockEntries(normalized.Entries)
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode skill import lock: %w", err)
	}
	return append(data, '\n'), nil
}

// FindSkillImportLockEntry returns the entry for a repository and source path.
func FindSkillImportLockEntry(lock *SkillImportLock, repository string, sourcePath string) (SkillImportLockEntry, bool) {
	if lock == nil {
		return SkillImportLockEntry{}, false
	}
	key := SkillImportEntryKey{Repository: NormalizeSkillRepository(repository), SourcePath: sourcePath}
	for _, entry := range lock.Entries {
		if entry.Key() == key {
			return entry, true
		}
	}
	return SkillImportLockEntry{}, false
}
