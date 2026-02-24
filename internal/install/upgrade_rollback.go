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
	if snapshot.Status != upgradeSnapshotStatusApplied {
		return fmt.Errorf(messages.InstallUpgradeRollbackSnapshotNotRollbackableFmt, snapshotID, snapshot.Status, upgradeSnapshotStatusApplied)
	}

	targets, err := rollbackTargetsFromSnapshotEntries(root, snapshot.Entries)
	if err != nil {
		return err
	}
	if err := rollbackUpgradeSnapshotState(root, sys, snapshot, targets); err != nil {
		snapshot.Status = upgradeSnapshotStatusRollbackFailed
		snapshot.FailureStep = manualRollbackFailureStep
		snapshot.FailureError = err.Error()
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

	targetRelPaths := make([]string, 0, len(scopedTargets))
	for _, t := range scopedTargets {
		rel, err := filepath.Rel(root, t)
		if err == nil {
			targetRelPaths = append(targetRelPaths, normalizeRelPath(rel))
		}
	}

	filteredEntries := make([]upgradeSnapshotEntry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		include := false
		for _, targetRel := range targetRelPaths {
			if entry.Path == targetRel || strings.HasPrefix(entry.Path, targetRel+"/") {
				include = true
				break
			}
		}
		if include {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	return restoreUpgradeSnapshotEntriesAtRoot(root, sys, filteredEntries)
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
	sort.Slice(dirs, func(i, j int) bool {
		if strings.Count(dirs[i].Path, "/") == strings.Count(dirs[j].Path, "/") {
			return dirs[i].Path < dirs[j].Path
		}
		return strings.Count(dirs[i].Path, "/") < strings.Count(dirs[j].Path, "/")
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
		if err := sys.MkdirAll(absPath, permFromSnapshot(entry.Perm, 0o755)); err != nil {
			return fmt.Errorf("restore directory %s: %w", entry.Path, err)
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
		if err := sys.Symlink(entry.LinkTarget, absPath); err != nil {
			return fmt.Errorf("restore symlink %s: %w", entry.Path, err)
		}
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
