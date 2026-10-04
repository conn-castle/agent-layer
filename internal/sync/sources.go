package sync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// LoadSources reads the one combined skill-source snapshot every projection is
// built from: configuration, environment, instructions, the user-managed skill
// tier, and the imported skill tier.
//
// It enforces the ownership invariants that only apply once both tiers exist:
// an imported directory with no lock entry is an actionable error, and one
// normalized skill name may not exist in both tiers.
//
// Locked production loaders prune empty immediate children of
// `.agent-layer/skills/` before calling LoadSources so sync, launch, dispatch,
// and skill-import share that cleanup. Direct LoadSources callers still reject
// empty skill directories.
//
// Callers must already hold the project lock; use WithLockedProject unless the
// lock is held for a larger operation.
func LoadSources(fsys fs.FS, root string) (*config.ProjectConfig, error) {
	project, err := config.LoadProjectConfigFS(fsys, root)
	if err != nil {
		return nil, err
	}
	combined, err := config.CombineImportedSkillsFS(fsys, root, project.Skills)
	if err != nil {
		return nil, err
	}
	project.Skills = combined
	return project, nil
}

// loadLockedProjectSources prunes empty user-managed skill directories and then
// loads the combined source snapshot. Callers must already hold the project lock.
func loadLockedProjectSources(sys System, fsys fs.FS, root string) (*config.ProjectConfig, error) {
	if err := removeEmptyDeployedSkillDirs(sys, root); err != nil {
		return nil, err
	}
	return LoadSources(fsys, root)
}

// removeEmptyDeployedSkillDirs removes empty immediate children from the
// user-managed skill source directory before source loading validates them.
// Nonempty directories and every other entry type remain subject to the normal
// strict skill validation path.
func removeEmptyDeployedSkillDirs(sys System, root string) error {
	skillsRoot := filepath.Join(root, ".agent-layer", "skills")
	entries, err := sys.ReadDir(skillsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, skillsRoot, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(skillsRoot, entry.Name())
		children, err := sys.ReadDir(skillDir)
		if err != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, skillDir, err)
		}
		if len(children) != 0 {
			continue
		}
		if err := sys.Remove(skillDir); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, skillDir, err)
		}
	}
	return nil
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
		project, loadErr := loadLockedProjectSources(sys, os.DirFS(root), root)
		if loadErr != nil {
			return nil, loadErr
		}
		return &Result{}, fn(project)
	})
	return err
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
	project, err := loadLockedProjectSources(sys, os.DirFS(root), root)
	if err != nil {
		return nil, err
	}
	return runWithProjectLocked(sys, root, project)
}
