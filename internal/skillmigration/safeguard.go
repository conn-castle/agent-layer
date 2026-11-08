// Package skillmigration retains capability and ownership guards for CLI
// handoffs and incompatible snapshot recovery. It contains coordinates, never content.
package skillmigration

import (
	"context"
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

// MinimumCLI is the first CLI that completed the skill and instruction transition
// and the minimum supported installed-project source for this CLI.
const MinimumCLI = "1.0.0"

// WithRecoveredContext recovers under the project lock unless ctx is canceled.
func WithRecoveredContext(ctx context.Context, root string, fn func() error) error {
	return WithCheckedRecovery(ctx, root, nil, fn)
}

// WithCheckedRecovery checks before lock creation and again under the lock
// before recovery. The check must be read-only.
func WithCheckedRecovery(ctx context.Context, root string, check func() error, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return fn()
	} else if err != nil {
		return err
	}
	return projectlock.WithContext(ctx, projectlock.RealSystem{}, root, func() error {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
		if err := skilljournal.RecoverBoth(root); err != nil {
			return err
		}
		return fn()
	})
}

// ActiveNames returns occupied imported slots with the migration's genuine
// repository/path lock identity. Bad evidence blocks mutation rather than guessing.
func ActiveNames(root string) (map[string]bool, error) {
	paths := config.DefaultPaths(root)
	active := map[string]bool{}
	instructions, err := skilllock.LoadInstructions(paths.InstructionsLockPath)
	if err != nil && !errors.Is(err, skilllock.ErrMissing) {
		return nil, err
	}
	if err == nil {
		for _, entry := range instructions.Skills {
			if skilljournal.LegacyInstruction(entry.Name) != "" && entry.Repository == templates.GeneralSkillsRepository && entry.SelectedPath == "instructions/"+entry.Name {
				if _, err := os.Lstat(filepath.Join(paths.ImportedInstructionsDir, entry.Name)); err == nil {
					active[entry.Name] = true
				} else if !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
			}
		}
	}
	lock, err := skilllock.Load(paths.SkillsLockPath)
	if errors.Is(err, skilllock.ErrMissing) {
		return active, nil
	}
	if err != nil {
		return nil, err
	}
	allowed, err := movedSkillSelectors()
	if err != nil {
		return nil, err
	}
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

func movedSkillSelectors() (map[string]string, error) {
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
	return allowed, nil
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
	cfg, err := config.LoadConfigLenient(config.DefaultPaths(root).ConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if cfg != nil && len(cfg.Instructions.Imports)+len(cfg.Instructions.Local) > 0 {
		return fmt.Errorf("cannot select pre-migration CLI %s with explicitly ordered instructions; use CLI %s or newer. Instruction configuration and source files are preserved", target, MinimumCLI)
	}
	names, err := ActiveNames(root)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("cannot select pre-migration CLI %s with active moved imports: it can recreate legacy local sources alongside imported copies; use CLI %s or newer. Imported modifications and recovery evidence are preserved", target, MinimumCLI)
	}
	return nil
}

// CheckVersion checks capability before locking and recovering unsafe handoffs.
func CheckVersion(ctx context.Context, root, target string) error {
	// A capable target must receive its own recovery evidence, including schemas
	// this reader does not yet understand. Install/rollback still use WithRecoveredContext.
	cmp, err := version.Compare(target, MinimumCLI)
	if err != nil {
		return err
	}
	if cmp >= 0 {
		return nil
	}
	return WithRecoveredContext(ctx, root, func() error { return CheckVersionLocked(root, target) })
}

// LocalSlot is the trusted path a converted import must not recreate.
func LocalSlot(name string) string {
	if legacy := skilljournal.LegacyInstruction(name); legacy != "" {
		return ".agent-layer/instructions/" + legacy
	}
	return ".agent-layer/skills/" + name
}

// CheckConfig preserves import ownership when a rollback restores configuration.
func CheckConfig(raw []byte, active map[string]bool) error {
	cfg, err := config.ParseConfigLenient(raw, "rollback config")
	if err != nil {
		return err
	}
	allowed, err := movedSkillSelectors()
	if err != nil {
		return err
	}
	for name := range active {
		covered := false
		if skilljournal.LegacyInstruction(name) != "" {
			for _, imp := range cfg.Instructions.Imports {
				if imp.Order != nil && *imp.Order >= 0 && len(imp.Selectors) == 1 && imp.Selectors[0] == "instructions/"+name && config.NormalizeSkillRepository(imp.Repository) == templates.GeneralSkillsRepository {
					covered = true
				}
			}
		} else {
			for _, imp := range cfg.Skills.Imports {
				if _, selected := imp.SelectingPositiveSelector(allowed[name]); selected && config.NormalizeSkillRepository(imp.Repository) == templates.GeneralSkillsRepository {
					covered = true
				}
			}
		}
		if !covered {
			return fmt.Errorf("rollback configuration would discard ownership of converted import %s; preserve the snapshot and imported modifications", name)
		}
	}
	return nil
}
