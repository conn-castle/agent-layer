package sync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// LoadSources reads the one combined skill-source snapshot every projection is
// built from: configuration, environment, instructions, the user-managed skill
// tier, and the imported skill tier.
//
// It enforces the ownership invariants that only apply once both tiers exist:
// an imported directory with no lock entry is an actionable error, and one
// normalized skill name may not exist in both tiers.
//
// Callers must already hold the project lock; use WithLockedProject unless the
// lock is held for a larger operation.
func LoadSources(fsys fs.FS, root string) (*config.ProjectConfig, error) {
	project, err := config.LoadProjectConfigFS(fsys, root)
	if err != nil {
		return nil, err
	}
	paths := config.DefaultPaths(root)

	imported, err := config.LoadImportedSkillsFS(fsys, root, paths.ImportedSkillsDir)
	if err != nil {
		return nil, err
	}
	if err := verifyImportedOwnership(fsys, root, paths); err != nil {
		return nil, err
	}
	combined, err := combineSkillTiers(project.Skills, imported)
	if err != nil {
		return nil, err
	}
	project.Skills = combined
	return project, nil
}

// LoadLockedSources acquires the project lock and returns the combined source
// snapshot. It is the entry point for callers that need the validated snapshot
// but do not perform their own writes under the lock.
func LoadLockedSources(sys System, root string) (*config.ProjectConfig, error) {
	var project *config.ProjectConfig
	err := WithLockedProject(sys, root, func(loaded *config.ProjectConfig) error {
		project = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return project, nil
}

// WithLockedProject acquires the project lock, loads the combined source
// snapshot inside the critical section, and runs fn with it.
func WithLockedProject(sys System, root string, fn func(*config.ProjectConfig) error) error {
	if sys == nil {
		return fmt.Errorf(messages.SyncSystemRequired)
	}
	_, err := withProjectSyncLock(sys, root, func() (*Result, error) {
		project, loadErr := LoadSources(os.DirFS(root), root)
		if loadErr != nil {
			return nil, loadErr
		}
		return &Result{}, fn(project)
	})
	return err
}

// verifyImportedOwnership rejects imported directories Agent Layer does not
// own. `.agent-layer/imported-skills/` is fully managed, so a directory without
// a lock entry means recorded state and local state disagree and no import
// operation can safely reconcile it.
func verifyImportedOwnership(fsys fs.FS, root string, paths config.Paths) error {
	present, err := importedDirectoryNames(fsys, root, paths.ImportedSkillsDir)
	if err != nil {
		return err
	}
	if len(present) == 0 {
		return nil
	}

	locked, err := loadSkillLockNames(fsys, root, paths.SkillsLockPath)
	if err != nil {
		return err
	}
	var orphans []string
	for _, name := range present {
		if _, ok := locked[name]; !ok {
			orphans = append(orphans, filepath.Join(paths.ImportedSkillsDir, name))
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return fmt.Errorf(messages.SyncImportedSkillOrphanFmt, strings.Join(orphans, ", "))
	}
	return nil
}

// importedDirectoryNames lists the directory entries under the imported tier.
func importedDirectoryNames(fsys fs.FS, root string, dir string) ([]string, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, dir, err)
	}
	entries, err := fs.ReadDir(fsys, filepath.ToSlash(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		// Hidden entries are Agent Layer's own transaction scratch space, not
		// imported skills; skill loading skips them for the same reason.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// loadSkillLockNames returns the locked skill names. A missing lockfile yields
// an empty set so every present imported directory is reported as an orphan; a
// malformed lockfile fails loudly rather than being treated as empty.
func loadSkillLockNames(fsys fs.FS, root string, lockPath string) (map[string]struct{}, error) {
	rel, err := filepath.Rel(root, lockPath)
	if err != nil {
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, lockPath, err)
	}
	data, err := fs.ReadFile(fsys, filepath.ToSlash(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]struct{}{}, nil
		}
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, lockPath, err)
	}
	file, err := skilllock.Parse(data, lockPath)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(file.Skills))
	for _, entry := range file.Skills {
		names[entry.Name] = struct{}{}
	}
	return names, nil
}

// combineSkillTiers merges the user-managed and imported tiers into one sorted
// projection set, failing on a normalized-name collision with both paths so
// neither source is ever silently shadowed.
func combineSkillTiers(user []config.Skill, imported []config.Skill) ([]config.Skill, error) {
	byName := make(map[string]config.Skill, len(user)+len(imported))
	combined := make([]config.Skill, 0, len(user)+len(imported))
	for _, skill := range append(append([]config.Skill{}, user...), imported...) {
		key := skilltree.NormalizeName(skill.Name)
		if existing, clash := byName[key]; clash {
			return nil, fmt.Errorf(messages.SyncSkillTierCollisionFmt, skill.Name, existing.SourceDir, skill.SourceDir)
		}
		byName[key] = skill
		combined = append(combined, skill)
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].Name < combined[j].Name })
	return combined, nil
}

// ProjectLocked regenerates every output from the combined source snapshot.
//
// Callers must already hold the project lock; skill import operations use it to
// project committed source state without releasing and reacquiring the lock,
// which would let another writer interleave.
func ProjectLocked(sys System, root string) (*Result, error) {
	if sys == nil {
		return nil, fmt.Errorf(messages.SyncSystemRequired)
	}
	project, err := LoadSources(os.DirFS(root), root)
	if err != nil {
		return nil, err
	}
	return runWithProjectLocked(sys, root, project)
}
