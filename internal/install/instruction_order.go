package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// instructionOrderPreview returns a config only when legacy ordering needs migration.
func (inst *installer) instructionOrderPreview() (string, error) {
	raw, err := inst.sys.ReadFile(config.DefaultPaths(inst.root).ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		// Missing config is a no-op only when source roots are safe before migrations.
		return "", config.ValidateInstructionRootsFS(os.DirFS(inst.root), inst.root)
	}
	if err != nil {
		return "", err
	}
	next, err := config.MigrateInstructionOrderFS(os.DirFS(inst.root), inst.root, string(raw))
	if err != nil || next == string(raw) {
		return "", err
	}
	return next, nil
}

func (inst *installer) planInstructionOrder(effects migrationPathEffects) ([]ConfigKeyMigration, error) {
	raw, err := inst.sys.ReadFile(config.DefaultPaths(inst.root).ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, config.ValidateInstructionRootsFS(os.DirFS(inst.root), inst.root)
	}
	if err != nil {
		return nil, err
	}
	if err := config.ValidateInstructionRootsFS(os.DirFS(inst.root), inst.root); err != nil {
		return nil, err
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
