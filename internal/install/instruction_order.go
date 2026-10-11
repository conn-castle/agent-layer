package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
)

// instructionOrderPreview returns a config only when legacy ordering needs migration.
func (inst *installer) instructionOrderPreview() (string, error) {
	paths := config.DefaultPaths(inst.root)
	localExists := false
	for _, dir := range []string{filepath.Dir(paths.ConfigPath), paths.InstructionsDir, paths.ImportedInstructionsDir} {
		info, err := inst.sys.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s must be a real unlinked directory before instruction migration", dir)
		}
		localExists = localExists || dir == paths.InstructionsDir
	}
	raw, err := inst.sys.ReadFile(paths.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
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
			return "", err
		}
	}
	next, err := config.MigrateInstructionOrder(string(raw), names)
	if err != nil || next == string(raw) {
		return "", err
	}
	return next, nil
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
