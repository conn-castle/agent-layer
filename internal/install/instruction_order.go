package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// instructionOrderPreview returns a config only when legacy ordering needs migration.
func (inst *installer) instructionOrderPreview() (string, error) {
	raw, names, err := inst.instructionOrderInputs()
	if err != nil {
		return "", err
	}
	next, err := config.MigrateInstructionOrder(string(raw), names)
	if err != nil || next == string(raw) {
		return "", err
	}
	return next, nil
}

// instructionOrderInputs reads and validates the real sources before preview or apply.
func (inst *installer) instructionOrderInputs() ([]byte, []string, error) {
	paths := config.DefaultPaths(inst.root)
	localExists := false
	for _, dir := range []string{filepath.Dir(paths.ConfigPath), paths.InstructionsDir, paths.ImportedInstructionsDir} {
		info, err := inst.sys.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("%s must be a real unlinked directory before instruction migration", dir)
		}
		localExists = localExists || dir == paths.InstructionsDir
	}
	raw, err := inst.sys.ReadFile(paths.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	dir := paths.InstructionsDir
	var names []string
	if localExists {
		err = inst.sys.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == dir {
				return nil
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			if !config.LocalInstructionEntry(d) {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("%s must be a regular unlinked instruction", p)
			}
			names = append(names, d.Name())
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return raw, names, nil
}

func (inst *installer) planInstructionOrder(effects migrationPathEffects) ([]ConfigKeyMigration, error) {
	raw, _, err := inst.instructionOrderInputs()
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	// Ordering runs after historical file migrations, so preview their final names.
	var names []string
	for path, isDir := range effects.tree {
		name, local := strings.CutPrefix(path, ".agent-layer/instructions/")
		if local && !isDir && !strings.Contains(name, "/") && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
	}
	next, err := config.MigrateInstructionOrder(string(raw), names)
	if err != nil || next == string(raw) {
		return nil, err
	}
	cfg, err := config.ParseConfigLenient([]byte(next), "config.toml")
	if err != nil {
		return nil, err
	}
	migrations := make([]ConfigKeyMigration, 0, len(cfg.Instructions.Local))
	for i, local := range cfg.Instructions.Local {
		migrations = append(migrations, ConfigKeyMigration{
			Key: fmt.Sprintf("instructions.local[%d]", i), From: unsetValue,
			To: fmt.Sprintf("selectors = [%q], order = %d", local.Selectors[0], *local.Order),
		})
	}
	return migrations, nil
}

func (inst upgradeOrchestrator) migrateInstructionOrder() error {
	next, err := inst.instructionOrderPreview()
	if err != nil {
		return err
	}
	if next == "" {
		return nil
	}
	target := filepath.Join(inst.root, ".agent-layer", "config.toml")
	info, err := inst.sys.Lstat(target)
	if err != nil {
		return err
	}
	return inst.sys.WriteFileAtomic(target, []byte(next), info.Mode().Perm())
}
