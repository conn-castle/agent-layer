package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilltree"
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
	approved, promptErr := inst.prompter.confirmSkillsMigration(flatSkills, conflicts)
	if promptErr != nil {
		return fmt.Errorf(messages.InstallSkillsMigrationPromptErrFmt, promptErr)
	}
	if !approved {
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
		approved, promptErr := inst.prompter.confirmSkillsMigration(flatSkills, conflicts)
		if promptErr != nil {
			return false, fmt.Errorf(messages.InstallSkillsMigrationPromptErrFmt, promptErr)
		}
		if !approved {
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
		destPath := filepath.Join(destDir, skillManifestFileName)

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
		destPath := filepath.Join(absSkillsDir, name, skillManifestFileName)

		flatInfo, flatErr := sys.Lstat(flatPath)
		if flatErr != nil {
			return 0, nil, flatErr
		}
		if !flatInfo.Mode().IsRegular() {
			return 0, nil, fmt.Errorf("flat skill %s must be a regular file", flatPath)
		}
		if dirInfo, dirErr := sys.Lstat(filepath.Dir(destPath)); dirErr == nil {
			if !dirInfo.IsDir() {
				return 0, nil, fmt.Errorf("skill destination %s must be a directory", filepath.Dir(destPath))
			}
		} else if !errors.Is(dirErr, os.ErrNotExist) {
			return 0, nil, dirErr
		}
		destInfo, statErr := sys.Lstat(destPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue // no conflict
			}
			return 0, nil, fmt.Errorf(messages.InstallFailedStatFmt, destPath, statErr)
		}
		if !destInfo.Mode().IsRegular() {
			return 0, nil, fmt.Errorf("skill destination %s must be a regular file", destPath)
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
		if !flatSkillDuplicate(flatData, destData, filepath.Dir(destPath)) {
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
	flatInfo, statErr := sys.Lstat(flatPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, flatPath, statErr)
	}
	if !flatInfo.Mode().IsRegular() {
		return false, fmt.Errorf("flat skill %s must be a regular file", flatPath)
	}

	if dirInfo, dirErr := sys.Lstat(destDir); dirErr == nil {
		if !dirInfo.IsDir() {
			return false, fmt.Errorf("skill destination %s must be a directory", destDir)
		}
	} else if !errors.Is(dirErr, os.ErrNotExist) {
		return false, fmt.Errorf(messages.InstallFailedStatFmt, destDir, dirErr)
	}

	destInfo, destStatErr := sys.Lstat(destPath)
	if destStatErr != nil && !errors.Is(destStatErr, os.ErrNotExist) {
		return false, fmt.Errorf(messages.InstallFailedStatFmt, destPath, destStatErr)
	}
	if destStatErr == nil && !destInfo.Mode().IsRegular() {
		return false, fmt.Errorf("skill destination %s must be a regular file", destPath)
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
		if flatSkillDuplicate(flatData, destData, destDir) {
			// Same content — remove flat file.
			if removeErr := sys.RemoveAll(flatPath); removeErr != nil {
				return false, fmt.Errorf("remove duplicate flat skill %s: %w", flatPath, removeErr)
			}
			return true, nil
		}
		// Different content should have been caught by preflight.
		return false, fmt.Errorf("conflict: %s and %s have different content", flatPath, destPath)
	}

	flatData, readErr := sys.ReadFile(flatPath)
	if readErr != nil {
		return false, fmt.Errorf(messages.InstallFailedReadFmt, flatPath, readErr)
	}
	migratedData := addMissingFlatSkillName(flatData, destDir)

	// Keep the original flat bytes until the augmented manifest is durable.
	// The existing upgrade snapshot also retains the pre-migration source.
	if mkErr := sys.MkdirAll(destDir, 0o755); mkErr != nil {
		return false, fmt.Errorf(messages.InstallFailedCreateDirForFmt, destPath, mkErr)
	}
	if !bytes.Equal(flatData, migratedData) {
		if writeErr := sys.WriteFileAtomic(destPath, migratedData, flatInfo.Mode().Perm()); writeErr != nil {
			return false, fmt.Errorf("write migrated skill %s: %w", destPath, writeErr)
		}
		if removeErr := sys.RemoveAll(flatPath); removeErr != nil {
			return false, fmt.Errorf("remove migrated flat skill %s: %w", flatPath, removeErr)
		}
		return true, nil
	}
	if renameErr := sys.Rename(flatPath, destPath); renameErr != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", flatPath, destPath, renameErr)
	}
	return true, nil
}

// An interrupted augmentation can leave both the raw flat source and its
// deterministic destination. Retry cleans up only that proven duplicate.
func flatSkillDuplicate(flatData, destData []byte, destDir string) bool {
	return normalizeTemplateContent(string(flatData)) == normalizeTemplateContent(string(destData)) ||
		bytes.Equal(destData, addMissingFlatSkillName(flatData, destDir))
}

const flatSkillFrontmatterDelimiter = "---"

// addMissingFlatSkillName supplies the identity implicit in historical flat
// filenames. Only a genuinely absent name in otherwise valid YAML frontmatter
// is eligible. Never reserialize metadata/body or repair explicit invalid names.
// Ineligible sources keep the historical move-only behavior: ordinary strict
// loading rejects any invalid skills remaining after all historical migrations.
// Directory manifests never pass through this function.
func addMissingFlatSkillName(raw []byte, destDir string) []byte {
	content := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	firstEnd := bytes.IndexByte(content, '\n')
	if firstEnd < 0 || strings.TrimSpace(string(content[:firstEnd])) != flatSkillFrontmatterDelimiter {
		return raw
	}
	start := len(raw) - len(content) + firstEnd + 1
	end := start
	for _, line := range bytes.SplitAfter(raw[start:], []byte("\n")) {
		if strings.TrimSpace(string(line)) == flatSkillFrontmatterDelimiter {
			break
		}
		end += len(line)
	}
	if end == len(raw) {
		return raw
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw[start:end], &root); err != nil {
		return raw
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return raw
	}
	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Kind != yaml.ScalarNode || key.Value == "name" || key.Tag == "!!merge" {
			return raw // explicit null/invalid names and uncertain merged/alias keys
		}
	}
	newline := "\n"
	if firstEnd > 0 && content[firstEnd-1] == '\r' {
		newline = "\r\n"
	}
	// Quote even numeric filenames as strings; validation below checks identity.
	addition := []byte(fmt.Sprintf("name: %q%s", filepath.Base(destDir), newline))
	result := make([]byte, 0, len(raw)+len(addition))
	result = append(result, raw[:start]...)
	result = append(result, addition...)
	result = append(result, raw[start:]...)
	if _, err := skilltree.ValidateManifest(result, filepath.ToSlash(destDir)); err != nil {
		return raw
	}
	return result
}
