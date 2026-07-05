package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// preflightAndConfirmSkillsMigration runs BEFORE any disk mutations to give the
// user a clear, up-front warning about the breaking skills format change. It
// scans for flat-format skills, detects conflicts, prints the full warning
// banner, and obtains explicit user confirmation. Call this from Run() between
// prepareUpgradeMigrations() and createUpgradeSnapshot().
func (inst *installer) preflightAndConfirmSkillsMigration() error {
	// Only relevant when a migrate_skills_format operation is pending.
	var migrateOp *upgradeMigrationOperation
	for i := range inst.pendingMigrationOps {
		if inst.pendingMigrationOps[i].Kind == upgradeMigrationKindMigrateSkillsFormat {
			migrateOp = &inst.pendingMigrationOps[i]
			break
		}
	}
	if migrateOp == nil {
		return nil
	}

	// Resolve the skills directory. Before migration execution, the directory
	// might still be at the legacy path (.agent-layer/slash-commands/) if the
	// preceding rename operation hasn't run yet. Check both locations.
	postRenamePath, err := snapshotEntryAbsPath(inst.root, filepath.FromSlash(migrateOp.Path))
	if err != nil {
		return err
	}
	absSkillsDir := postRenamePath

	if _, statErr := inst.sys.Stat(absSkillsDir); statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallFailedStatFmt, absSkillsDir, statErr)
		}
		// Try the legacy pre-rename path.
		legacyPath := filepath.Join(inst.root, ".agent-layer", "slash-commands")
		if _, legacyStatErr := inst.sys.Stat(legacyPath); legacyStatErr != nil {
			if errors.Is(legacyStatErr, os.ErrNotExist) {
				// Neither directory exists — no skills to migrate.
				return nil
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, legacyPath, legacyStatErr)
		}
		absSkillsDir = legacyPath
	}

	flatCount, conflicts, preErr := preflightSkillsMigration(inst.sys, absSkillsDir)
	if preErr != nil {
		return preErr
	}
	if flatCount == 0 {
		return nil // all skills already in directory format
	}

	flatSkills, scanErr := listFlatSkillNames(inst.sys, absSkillsDir)
	if scanErr != nil {
		return scanErr
	}

	// ── Warning banner (shown BEFORE any disk mutations) ──
	out := inst.warnOutput()
	ew := &errWriter{w: out}
	ew.println()
	ew.println(messages.InstallSkillsMigrationBannerRule)
	ew.println(messages.InstallSkillsMigrationBannerTitle)
	ew.println(messages.InstallSkillsMigrationBannerRule)
	ew.println()
	ew.println(messages.InstallSkillsMigrationBannerBody1)
	ew.println(messages.InstallSkillsMigrationBannerBody2)
	ew.println(messages.InstallSkillsMigrationBannerBody3)
	ew.println()
	ew.printf(messages.InstallSkillsMigrationFoundFlatFmt, len(flatSkills))
	ew.println()
	for _, name := range flatSkills {
		ew.printf(messages.InstallSkillsMigrationFlatToDirFmt, name, name)
	}
	if ew.err != nil {
		return ew.err
	}

	if len(conflicts) > 0 {
		ew.println()
		ew.println(messages.InstallSkillsMigrationBlockedHeader)
		ew.println()
		ew.println(messages.InstallSkillsMigrationBlockedBody1)
		ew.println(messages.InstallSkillsMigrationBlockedBody2)
		ew.println(messages.InstallSkillsMigrationBlockedBody3)
		ew.println()
		for _, c := range conflicts {
			ew.printf(messages.InstallSkillsMigrationConflictSkillFmt, c.SkillName)
			ew.printf(messages.InstallSkillsMigrationConflictFlatFmt, c.FlatPath)
			ew.printf(messages.InstallSkillsMigrationConflictDirFmt, c.DirPath)
			ew.println()
		}
		ew.println(messages.InstallSkillsMigrationFixHint1)
		ew.println(messages.InstallSkillsMigrationFixHint2)
		ew.println()
		ew.println(messages.InstallSkillsMigrationFixKeepDir)
		ew.println(messages.InstallSkillsMigrationFixKeepFlat)
		ew.println()
		ew.println(messages.InstallSkillsMigrationFixRerun)
		ew.println()
		if ew.err != nil {
			return ew.err
		}
		return fmt.Errorf(messages.InstallSkillsMigrationBlockedErrFmt, len(conflicts))
	}

	ew.println()
	ew.println(messages.InstallSkillsMigrationNoConflicts)
	ew.println()
	if ew.err != nil {
		return ew.err
	}

	// Prompt for confirmation (before any mutations happen).
	resp, promptErr := inst.promptRouter().route(promptRequest{
		kind:       promptKindConfirmSkillsMigration,
		flatSkills: flatSkills,
		conflicts:  conflicts,
	})
	if promptErr != nil {
		return fmt.Errorf(messages.InstallSkillsMigrationPromptErrFmt, promptErr)
	}
	if !resp.approved {
		return fmt.Errorf(messages.InstallSkillsMigrationDeclinedErr)
	}

	inst.skillsMigrationConfirmed = true
	return nil
}

// executeMigrateSkillsFormat migrates all flat-format skills (<name>.md) to
// directory format (<name>/SKILL.md) under relSkillsDir. The user-facing
// warning and confirmation have already been handled by
// preflightAndConfirmSkillsMigration() before any disk mutations began.
func (inst *installer) executeMigrateSkillsFormat(relSkillsDir string) (bool, error) {
	absSkillsDir, err := snapshotEntryAbsPath(inst.root, filepath.FromSlash(relSkillsDir))
	if err != nil {
		return false, err
	}
	if _, statErr := inst.sys.Stat(absSkillsDir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil // no skills directory — no-op
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, absSkillsDir, statErr)
	}

	flatSkills, scanErr := listFlatSkillNames(inst.sys, absSkillsDir)
	if scanErr != nil {
		return false, scanErr
	}
	if len(flatSkills) == 0 {
		return false, nil // no flat files — no-op
	}

	// Safety check: confirmation must have been obtained during pre-flight.
	// If not (e.g., tests calling executeMigrateSkillsFormat directly), fall
	// back to prompting here.
	if !inst.skillsMigrationConfirmed {
		_, conflicts, preErr := preflightSkillsMigration(inst.sys, absSkillsDir)
		if preErr != nil {
			return false, preErr
		}
		if len(conflicts) > 0 {
			return false, fmt.Errorf(messages.InstallSkillsMigrationBlockedErrFmt, len(conflicts))
		}
		resp, promptErr := inst.promptRouter().route(promptRequest{
			kind:       promptKindConfirmSkillsMigration,
			flatSkills: flatSkills,
			conflicts:  conflicts,
		})
		if promptErr != nil {
			return false, fmt.Errorf(messages.InstallSkillsMigrationPromptErrFmt, promptErr)
		}
		if !resp.approved {
			return false, fmt.Errorf(messages.InstallSkillsMigrationDeclinedErr)
		}
	}

	// Execute migration, tracking which skills were actually migrated (moved)
	// vs. duplicates that were just cleaned up.
	changed := false
	var migratedNames []string
	for _, name := range flatSkills {
		flatPath := filepath.Join(absSkillsDir, name+".md")
		destDir := filepath.Join(absSkillsDir, name)
		destPath := filepath.Join(destDir, "SKILL.md")

		// Check if destination already exists (duplicate cleanup case).
		destInfo, destStatErr := inst.sys.Stat(destPath)
		destExisted := destStatErr == nil && !destInfo.IsDir()
		if destStatErr != nil && !errors.Is(destStatErr, os.ErrNotExist) {
			return false, fmt.Errorf(messages.InstallFailedStatFmt, destPath, destStatErr)
		}

		migrated, migErr := migrateSingleFlatSkill(inst.sys, flatPath, destDir, destPath)
		if migErr != nil {
			return false, fmt.Errorf("migrate skill %s: %w", name, migErr)
		}
		if migrated {
			changed = true
			if !destExisted {
				migratedNames = append(migratedNames, name)
			}
		}
	}

	// Print post-migration success summary.
	if changed {
		out := inst.warnOutput()
		ew := &errWriter{w: out}
		ew.println()
		if len(migratedNames) > 0 {
			ew.printf(messages.InstallSkillsMigrationMigratedCountFmt, len(migratedNames))
			ew.println()
			for _, name := range migratedNames {
				ew.printf(messages.InstallSkillsMigrationFlatToDirFmt, name, name)
			}
			ew.println()
		}
		ew.println(messages.InstallSkillsMigrationComplete)
		ew.println()
		if ew.err != nil {
			return false, ew.err
		}
	}

	return changed, nil
}

// preflightSkillsMigration scans absSkillsDir for flat .md files and checks for
// conflicts with existing directory-format skills.
func preflightSkillsMigration(sys System, absSkillsDir string) (flatCount int, conflicts []SkillsMigrationConflict, err error) {
	entries, readErr := readSkillsDirEntries(sys, absSkillsDir)
	if readErr != nil {
		return 0, nil, readErr
	}

	for _, entry := range entries {
		if entry.isDir || !strings.HasSuffix(entry.name, ".md") || strings.HasPrefix(entry.name, ".") {
			continue
		}
		flatCount++
		name := strings.TrimSuffix(entry.name, ".md")
		flatPath := filepath.Join(absSkillsDir, entry.name)
		destPath := filepath.Join(absSkillsDir, name, "SKILL.md")

		destInfo, statErr := sys.Stat(destPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue // no conflict
			}
			return 0, nil, fmt.Errorf(messages.InstallFailedStatFmt, destPath, statErr)
		}
		if destInfo.IsDir() {
			continue
		}

		// Both exist — check content.
		flatData, flatReadErr := sys.ReadFile(flatPath)
		if flatReadErr != nil {
			return 0, nil, fmt.Errorf(messages.InstallFailedReadFmt, flatPath, flatReadErr)
		}
		destData, destReadErr := sys.ReadFile(destPath)
		if destReadErr != nil {
			return 0, nil, fmt.Errorf(messages.InstallFailedReadFmt, destPath, destReadErr)
		}
		if normalizeTemplateContent(string(flatData)) != normalizeTemplateContent(string(destData)) {
			conflicts = append(conflicts, SkillsMigrationConflict{
				SkillName: name,
				FlatPath:  flatPath,
				DirPath:   destPath,
				Reason:    fmt.Sprintf(messages.InstallSkillsMigrationConflictReasonFmt, name, name),
			})
		}
	}
	return flatCount, conflicts, nil
}

// listFlatSkillNames returns sorted names (without .md suffix) of flat-format
// skill files at the root of absSkillsDir.
func listFlatSkillNames(sys System, absSkillsDir string) ([]string, error) {
	entries, err := readSkillsDirEntries(sys, absSkillsDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.isDir || !strings.HasSuffix(entry.name, ".md") || strings.HasPrefix(entry.name, ".") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.name, ".md"))
	}
	sort.Strings(names)
	return names, nil
}

// skillsDirEntry mirrors the info needed from a directory scan.
type skillsDirEntry struct {
	name  string
	isDir bool
}

// readSkillsDirEntries performs a shallow directory scan of dir (no recursion).
func readSkillsDirEntries(sys System, dir string) ([]skillsDirEntry, error) {
	info, statErr := sys.Stat(dir)
	if statErr != nil {
		return nil, fmt.Errorf(messages.InstallFailedStatFmt, dir, statErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	var entries []skillsDirEntry
	err := sys.WalkDir(dir, func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filepath.Clean(walkPath) == filepath.Clean(dir) {
			return nil
		}
		entries = append(entries, skillsDirEntry{name: d.Name(), isDir: d.IsDir()})
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills directory %s: %w", dir, err)
	}
	return entries, nil
}

// migrateSingleFlatSkill moves a flat skill file to directory format. If the
// destination already exists with the same content, the flat file is removed.
func migrateSingleFlatSkill(sys System, flatPath string, destDir string, destPath string) (bool, error) {
	if _, statErr := sys.Stat(flatPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, flatPath, statErr)
	}

	destInfo, destStatErr := sys.Stat(destPath)
	if destStatErr != nil && !errors.Is(destStatErr, os.ErrNotExist) {
		return false, fmt.Errorf(messages.InstallFailedStatFmt, destPath, destStatErr)
	}
	if destStatErr == nil && !destInfo.IsDir() {
		// Destination exists — check for same content (duplicate cleanup).
		flatData, readErr := sys.ReadFile(flatPath)
		if readErr != nil {
			return false, fmt.Errorf(messages.InstallFailedReadFmt, flatPath, readErr)
		}
		destData, readErr := sys.ReadFile(destPath)
		if readErr != nil {
			return false, fmt.Errorf(messages.InstallFailedReadFmt, destPath, readErr)
		}
		if normalizeTemplateContent(string(flatData)) == normalizeTemplateContent(string(destData)) {
			// Same content — remove flat file.
			if removeErr := sys.RemoveAll(flatPath); removeErr != nil {
				return false, fmt.Errorf("remove duplicate flat skill %s: %w", flatPath, removeErr)
			}
			return true, nil
		}
		// Different content should have been caught by preflight.
		return false, fmt.Errorf("conflict: %s and %s have different content", flatPath, destPath)
	}

	// Create destination directory and move.
	if mkErr := sys.MkdirAll(destDir, 0o755); mkErr != nil {
		return false, fmt.Errorf(messages.InstallFailedCreateDirForFmt, destPath, mkErr)
	}
	if renameErr := sys.Rename(flatPath, destPath); renameErr != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", flatPath, destPath, renameErr)
	}
	return true, nil
}
