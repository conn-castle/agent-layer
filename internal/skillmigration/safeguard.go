// Package skillmigration protects converted local slots during supported
// rollback and CLI downgrade operations. It contains coordinates, never content.
package skillmigration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projectlock"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

// MinimumCLI is the first CLI release with the migration's ownership safeguard.
// This is a CLI capability boundary, not a skill-content release/version.
const MinimumCLI = "1.0.0"

// WithRecovered serializes a supported operation with imports and projection.
func WithRecovered(root string, fn func() error) error {
	if _, err := os.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return fn()
	} else if err != nil {
		return err
	}
	return projectlock.With(projectlock.RealSystem{}, root, func() error {
		paths := config.DefaultPaths(root)
		if err := skilljournal.Recover(skilljournal.Targets{ImportedSkillsDir: paths.ImportedSkillsDir, LocalSkillsDir: paths.SkillsDir, ConfigPath: paths.ConfigPath, SkillsLockPath: paths.SkillsLockPath}); err != nil {
			return err
		}
		return fn()
	})
}

// ActiveNames returns occupied imported slots with the migration's genuine
// repository/path lock identity. Bad evidence blocks mutation rather than guessing.
func ActiveNames(root string) (map[string]bool, error) {
	paths := config.DefaultPaths(root)
	lock, err := skilllock.Load(paths.SkillsLockPath)
	if errors.Is(err, skilllock.ErrMissing) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	catalog, err := templates.LoadCLISkillCatalog()
	if err != nil {
		return nil, err
	}
	allowed := map[string]string{}
	for _, entry := range catalog {
		if entry.Repository == templates.GeneralSkillsRepository {
			for i, name := range entry.SkillNames() {
				allowed[name] = entry.Selectors[i]
			}
		}
	}
	active := map[string]bool{}
	for _, entry := range lock.Skills {
		selected, ok := allowed[entry.Name]
		if !ok || selected != entry.SelectedPath || config.NormalizeSkillRepository(entry.Repository) != templates.GeneralSkillsRepository {
			continue
		}
		if _, err := os.Lstat(filepath.Join(paths.ImportedSkillsDir, entry.Name)); err == nil {
			active[entry.Name] = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return active, nil
}

// CheckVersionLocked refuses a CLI that can recreate converted local templates.
// Callers hold the project lock and have recovered imports before invoking it.
func CheckVersionLocked(root, target string) error {
	if target == "" || version.IsDev(target) {
		return nil
	}
	cmp, err := version.Compare(target, MinimumCLI)
	if err != nil {
		return err
	}
	if cmp >= 0 {
		return nil
	}
	names, err := ActiveNames(root)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("cannot select pre-migration CLI %s with active moved imports: it can recreate legacy local skills alongside imported copies; use CLI %s or newer. Imported modifications and recovery evidence are preserved", target, MinimumCLI)
	}
	return nil
}

// CheckVersion checks capability before locking and recovering unsafe handoffs.
func CheckVersion(root, target string) error {
	// A capable target must receive its own recovery evidence, including schemas
	// this reader does not yet understand. Install/rollback still use WithRecovered.
	cmp, err := version.Compare(target, MinimumCLI)
	if err != nil {
		return err
	}
	if cmp >= 0 {
		return nil
	}
	return WithRecovered(root, func() error { return CheckVersionLocked(root, target) })
}
