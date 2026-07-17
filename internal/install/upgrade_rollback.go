package install

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const manualRollbackFailureStep = "manual_rollback"

// RollbackUpgradeSnapshotOptions controls manual rollback behavior.
type RollbackUpgradeSnapshotOptions struct {
	System System
}

// RollbackUpgradeSnapshot restores a previously captured managed-file snapshot by ID.
func RollbackUpgradeSnapshot(root string, snapshotID string, opts RollbackUpgradeSnapshotOptions) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf(messages.InstallRootRequired)
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return fmt.Errorf(messages.InstallUpgradeRollbackSnapshotIDRequired)
	}
	// Reject path traversal: snapshotID must be a bare filename component.
	if filepath.Base(snapshotID) != snapshotID {
		return fmt.Errorf(messages.InstallUpgradeRollbackSnapshotIDInvalid, snapshotID)
	}
	sys := opts.System
	if sys == nil {
		return fmt.Errorf(messages.InstallSystemRequired)
	}

	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	snapshotPath := filepath.Join(snapshotDir, snapshotID+".json")
	if _, err := sys.Stat(snapshotPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallUpgradeRollbackSnapshotNotFoundFmt, snapshotID, snapshotDir)
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, snapshotPath, err)
	}

	snapshot, err := readUpgradeSnapshot(snapshotPath, sys)
	if err != nil {
		return err
	}
	if snapshot.Status != upgradeSnapshotStatusApplied &&
		snapshot.Status != upgradeSnapshotStatusCreated &&
		snapshot.Status != upgradeSnapshotStatusRollbackFailed {
		return fmt.Errorf(messages.InstallUpgradeRollbackSnapshotNotRollbackableFmt, snapshotID, snapshot.Status)
	}

	targets, err := rollbackTargetsForSnapshot(root, snapshot)
	if err != nil {
		return err
	}
	if len(snapshot.RollbackTargets) == 0 && len(targets) > 0 {
		snapshot.RollbackTargets, err = rollbackTargetRelativePaths(root, targets)
		if err != nil {
			return err
		}
		if err := writeUpgradeSnapshotFile(snapshotPath, snapshot, sys); err != nil {
			return fmt.Errorf("persist rollback targets before rollback snapshot %s: %w", snapshotID, err)
		}
	}
	if err := rollbackUpgradeSnapshotState(root, sys, snapshot, targets); err != nil {
		if snapshot.Status == upgradeSnapshotStatusRollbackFailed && strings.TrimSpace(snapshot.FailureError) != "" {
			snapshot.FailureError = fmt.Sprintf("%s; retry rollback failed: %v", snapshot.FailureError, err)
		} else {
			snapshot.FailureStep = manualRollbackFailureStep
			snapshot.FailureError = err.Error()
		}
		snapshot.Status = upgradeSnapshotStatusRollbackFailed
		if writeErr := writeUpgradeSnapshotFile(snapshotPath, snapshot, sys); writeErr != nil {
			return fmt.Errorf("rollback snapshot %s failed: %w; failed to persist rollback_failed state: %v", snapshotID, err, writeErr)
		}
		return fmt.Errorf(messages.InstallUpgradeRollbackFailedFmt, snapshotID, err)
	}

	snapshot.Status = upgradeSnapshotStatusManuallyRolledBack
	if err := writeUpgradeSnapshotFile(snapshotPath, snapshot, sys); err != nil {
		return fmt.Errorf("rollback snapshot %s succeeded but failed to persist manually_rolled_back state: %w", snapshotID, err)
	}
	return nil
}

func rollbackTargetsForSnapshot(root string, snapshot upgradeSnapshot) ([]string, error) {
	if snapshot.Status == upgradeSnapshotStatusRollbackFailed {
		if len(snapshot.RollbackTargets) == 0 {
			return nil, fmt.Errorf("rollback_failed snapshot %s is missing persisted rollback targets and cannot be retried safely", snapshot.SnapshotID)
		}
		return rollbackTargetsFromRelativePaths(root, snapshot.RollbackTargets)
	}
	return rollbackTargetsFromSnapshotEntries(root, snapshot.Entries)
}

func rollbackTargetsFromRelativePaths(root string, relPaths []string) ([]string, error) {
	paths := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		path, err := snapshotEntryAbsPath(root, relPath)
		if err != nil {
			return nil, fmt.Errorf("invalid rollback target: %w", err)
		}
		paths = append(paths, path)
	}
	return uniqueNormalizedPaths(paths), nil
}

func rollbackTargetRelativePaths(root string, targets []string) ([]string, error) {
	relPaths := make([]string, 0, len(targets))
	for _, target := range uniqueNormalizedPaths(targets) {
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return nil, fmt.Errorf("resolve rollback target %s relative to repo root: %w", target, err)
		}
		rel = filepath.Clean(rel)
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("rollback target %s resolves outside repo root", target)
		}
		relPaths = append(relPaths, normalizeRelPath(rel))
	}
	sort.Strings(relPaths)
	return relPaths, nil
}

func rollbackTargetsFromSnapshotEntries(root string, entries []upgradeSnapshotEntry) ([]string, error) {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		path, err := snapshotEntryAbsPath(root, entry.Path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return uniqueNormalizedPaths(paths), nil
}

func snapshotEntryAbsPath(root string, relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("snapshot entry path is required")
	}
	cleanRel := filepath.Clean(filepath.FromSlash(relPath))
	if cleanRel == "." || cleanRel == "" {
		return "", fmt.Errorf("snapshot entry path %q is invalid", relPath)
	}
	absPath := filepath.Join(root, cleanRel)
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("snapshot entry path %q resolves outside repo root", relPath)
	}
	return absPath, nil
}

func rollbackUpgradeSnapshotState(root string, sys System, snapshot upgradeSnapshot, targets []string) error {
	if err := validateUpgradeSnapshot(snapshot); err != nil {
		return err
	}
	scopedTargets := uniqueNormalizedPaths(targets)
	scopedTargets, err := ensureVersionRollbackTarget(root, snapshot.Entries, scopedTargets)
	if err != nil {
		return err
	}
	if len(scopedTargets) == 0 {
		return nil
	}
	if err := validateRollbackTargetAncestors(root, sys, scopedTargets); err != nil {
		return err
	}
	targetRelPaths, err := rollbackTargetRelativePaths(root, scopedTargets)
	if err != nil {
		return err
	}

	filteredEntries := make([]upgradeSnapshotEntry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		entryRel := normalizeRelPath(filepath.Clean(filepath.FromSlash(entry.Path)))
		include := false
		for _, targetRel := range targetRelPaths {
			if entryRel == targetRel || strings.HasPrefix(entryRel, targetRel+"/") {
				include = true
				break
			}
		}
		if include {
			filteredEntries = append(filteredEntries, entry)
		}
	}
	if err := makeRollbackDirectoriesWritable(root, sys, scopedTargets); err != nil {
		return err
	}
	sort.Slice(scopedTargets, func(i, j int) bool {
		leftRel, leftDepth := rollbackTargetRelativeDepth(root, scopedTargets[i])
		rightRel, rightDepth := rollbackTargetRelativeDepth(root, scopedTargets[j])
		if leftDepth == rightDepth {
			return leftRel > rightRel
		}
		return leftDepth > rightDepth
	})
	for _, target := range scopedTargets {
		if err := sys.RemoveAll(target); err != nil {
			rel, relErr := filepath.Rel(root, target)
			if relErr != nil {
				rel = target
			}
			return fmt.Errorf("reset path %s for rollback: %w", rel, err)
		}
	}

	return restoreUpgradeSnapshotEntriesAtRoot(root, sys, filteredEntries)
}

// makeRollbackDirectoriesWritable prepares the current target tree for a
// deepest-first reset, including directories created after snapshot capture.
func makeRollbackDirectoriesWritable(root string, sys System, targets []string) error {
	for _, target := range targets {
		info, err := sys.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect rollback target %s before reset: %w", rollbackDisplayPath(root, target), err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := sys.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if err := sys.Chmod(path, entryInfo.Mode().Perm()|0o700); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("make directories under %s writable for rollback reset: %w", rollbackDisplayPath(root, target), err)
		}
	}
	return nil
}

// validateRollbackTargetAncestors rejects targets whose deepest existing
// ancestor resolves outside the repository through a symbolic link.
func validateRollbackTargetAncestors(root string, sys System, targets []string) error {
	resolvedRoot, err := sys.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root before rollback: %w", err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)
	for _, target := range targets {
		ancestor := filepath.Dir(target)
		for {
			resolvedAncestor, resolveErr := sys.EvalSymlinks(ancestor)
			if resolveErr == nil {
				if !pathWithinRoot(resolvedRoot, resolvedAncestor) {
					return fmt.Errorf("rollback target %s has an ancestor that resolves outside repo root", rollbackDisplayPath(root, target))
				}
				break
			}
			if !errors.Is(resolveErr, os.ErrNotExist) || filepath.Clean(ancestor) == filepath.Clean(root) {
				return fmt.Errorf("resolve rollback target ancestor %s: %w", rollbackDisplayPath(root, ancestor), resolveErr)
			}
			ancestor = filepath.Dir(ancestor)
		}
	}
	return nil
}

// pathWithinRoot reports whether path is root or one of its descendants.
func pathWithinRoot(root string, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// rollbackDisplayPath returns a repository-relative path when possible.
func rollbackDisplayPath(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func ensureVersionRollbackTarget(root string, entries []upgradeSnapshotEntry, targets []string) ([]string, error) {
	versionEntryPath := ""
	for _, entry := range entries {
		if entry.Path == pinVersionRelPath {
			versionEntryPath = entry.Path
			break
		}
	}
	if versionEntryPath == "" {
		return targets, nil
	}

	versionAbsPath, err := snapshotEntryAbsPath(root, versionEntryPath)
	if err != nil {
		return nil, err
	}
	return uniqueNormalizedPaths(append(targets, versionAbsPath)), nil
}

func restoreUpgradeSnapshotEntriesAtRoot(root string, sys System, entries []upgradeSnapshotEntry) error {
	dirs := make([]upgradeSnapshotEntry, 0)
	files := make([]upgradeSnapshotEntry, 0)
	symlinks := make([]upgradeSnapshotEntry, 0)
	for _, entry := range entries {
		switch entry.Kind {
		case upgradeSnapshotEntryKindDir:
			dirs = append(dirs, entry)
		case upgradeSnapshotEntryKindFile:
			files = append(files, entry)
		case upgradeSnapshotEntryKindSymlink:
			symlinks = append(symlinks, entry)
		case upgradeSnapshotEntryKindAbsent:
			// Absent entries are intentionally no-op on restore because the reset phase
			// already removed all rollback targets, leaving these paths absent again.
			continue
		}
	}
	directorySortKey := func(entry upgradeSnapshotEntry) (string, int) {
		rel := normalizeRelPath(filepath.Clean(filepath.FromSlash(entry.Path)))
		return rel, strings.Count(rel, "/")
	}
	sort.Slice(dirs, func(i, j int) bool {
		leftRel, leftDepth := directorySortKey(dirs[i])
		rightRel, rightDepth := directorySortKey(dirs[j])
		if leftDepth == rightDepth {
			return leftRel < rightRel
		}
		return leftDepth < rightDepth
	})
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	sort.Slice(symlinks, func(i, j int) bool {
		return symlinks[i].Path < symlinks[j].Path
	})

	for _, entry := range dirs {
		absPath, err := snapshotEntryAbsPath(root, entry.Path)
		if err != nil {
			return err
		}
		temporaryMode := permFromSnapshot(entry.Perm, 0o755) | 0o700
		if err := sys.MkdirAll(absPath, temporaryMode); err != nil {
			return fmt.Errorf("restore directory %s: %w", entry.Path, err)
		}
		if err := validateRestoreDirectory(sys, absPath, entry.Path); err != nil {
			return err
		}
		// MkdirAll preserves an existing directory's mode. Explicitly make every
		// captured directory traversable and owner-writable so a retry can repair
		// descendants even when an earlier attempt already applied a read-only mode.
		if err := sys.Chmod(absPath, temporaryMode); err != nil {
			return fmt.Errorf("make directory %s writable for restore: %w", entry.Path, err)
		}
	}
	for _, entry := range files {
		absPath, err := snapshotEntryAbsPath(root, entry.Path)
		if err != nil {
			return err
		}
		content, err := base64.StdEncoding.DecodeString(entry.ContentBase64)
		if err != nil {
			return fmt.Errorf("decode content for %s: %w", entry.Path, err)
		}
		if err := sys.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf(messages.InstallFailedCreateDirForFmt, absPath, err)
		}
		if err := sys.WriteFileAtomic(absPath, content, permFromSnapshot(entry.Perm, 0o644)); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, absPath, err)
		}
	}
	for _, entry := range symlinks {
		absPath, err := snapshotEntryAbsPath(root, entry.Path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(entry.LinkTarget) == "" {
			return fmt.Errorf("symlink snapshot entry %s requires link_target", entry.Path)
		}
		if err := sys.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf(messages.InstallFailedCreateDirForFmt, absPath, err)
		}
		// Defensively remove any pre-existing file/symlink at the target path.
		// The rollback reset phase should have already cleared it, but this
		// prevents EEXIST if the function is called outside a full rollback flow.
		_ = sys.RemoveAll(absPath)
		if err := sys.Symlink(entry.LinkTarget, absPath); err != nil {
			return fmt.Errorf(messages.InstallFailedRestoreSymlinkFmt, entry.Path, err)
		}
	}
	// Apply final directory modes only after every descendant is restored. The
	// reverse of the shallow-first ordering above is deepest-first, which avoids
	// making a parent non-traversable before its children have their final modes.
	for i := len(dirs) - 1; i >= 0; i-- {
		entry := dirs[i]
		absPath, err := snapshotEntryAbsPath(root, entry.Path)
		if err != nil {
			return err
		}
		if err := validateRestoreDirectory(sys, absPath, entry.Path); err != nil {
			return err
		}
		if err := sys.Chmod(absPath, permFromSnapshot(entry.Perm, 0o755)); err != nil {
			return fmt.Errorf("restore directory mode %s: %w", entry.Path, err)
		}
	}
	return nil
}

// validateRestoreDirectory prevents mode changes from following an unexpected
// symlink or applying a captured directory mode to another filesystem object.
func validateRestoreDirectory(sys System, absPath string, entryPath string) error {
	info, err := sys.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("inspect restored directory %s: %w", entryPath, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("restored directory %s is not a real directory", entryPath)
	}
	return nil
}

func rollbackTargetRelativeDepth(root string, target string) (string, int) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		normalized := normalizeRelPath(target)
		return normalized, strings.Count(normalized, "/")
	}
	rel = normalizeRelPath(filepath.Clean(rel))
	return rel, strings.Count(rel, "/")
}
