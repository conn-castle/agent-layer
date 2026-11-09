package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
)

// PristineConverter imports unmodified shipped copies during al upgrade. The
// installer holds the project lock, has recovered interrupted imports, and owns
// the upgrade snapshot; implementations must not take the lock again.
type PristineConverter interface {
	// Prepare fetches and validates every import the items need without writing
	// to the project.
	Prepare(ctx context.Context, root string, items []PristineConversion) (PreparedConversion, error)
}

// PreparedConversion commits a preflighted conversion.
type PreparedConversion interface {
	// Import commits journaled imports (each also removing replaced
	// [[instructions.local]] blocks) and calls removeLocal for every item only
	// after that item's import commits.
	Import(ctx context.Context, items []PristineConversion, removeLocal func(PristineConversion) error) error
	// Close releases the preflight workspace.
	Close()
}

// PromptConvertPristineCopiesFunc asks whether to convert unmodified shipped
// copies to Git imports. Declining keeps them as user-managed local content.
type PromptConvertPristineCopiesFunc func(items []PristineConversion) (bool, error)

// preparePristineConversions runs under the project lock after recovery and
// before the snapshot or any project write. A fetch failure aborts the upgrade
// with the project untouched.
func (inst *installer) preparePristineConversions() error {
	items, err := PlanPristineConversions(inst.root, inst.sys)
	if err != nil || len(items) == 0 {
		return err
	}
	accepted, err := inst.prompter.convertPristineCopies(items)
	if err != nil {
		return err
	}
	if !accepted {
		_, _ = fmt.Fprintln(inst.warnOutput(), messages.UpgradeConvertPristineDeclined)
		return nil
	}
	imports := pendingImports(items)
	if len(imports) > 0 {
		if inst.converter == nil {
			return errors.New("converting unmodified shipped copies requires an import converter")
		}
		prepared, err := inst.converter.Prepare(inst.ctx, inst.root, imports)
		if err != nil {
			return fmt.Errorf(messages.UpgradeConvertPristineFetchFmt, len(imports), err)
		}
		inst.preparedConversion = prepared
	}
	inst.conversions = items
	return nil
}

// convertPristineCopies runs inside the upgrade transaction after migrations
// and before the pin advances. Each import commits before its local copy is
// removed, so an interruption leaves the content local, imported, or both; the
// next al upgrade completes a leftover local copy as delete-only.
func (inst *installer) convertPristineCopies() error {
	if len(inst.conversions) == 0 {
		return nil
	}
	defer func() {
		// Keep the removal directory only when it holds a preserved edited copy.
		if inst.conversionRemovalDir != "" && !inst.conversionPreserved {
			_ = inst.sys.RemoveAll(inst.conversionRemovalDir)
		}
	}()
	current, err := PlanPristineConversions(inst.root, inst.sys)
	if err != nil {
		return err
	}
	verified := map[string]PristineConversion{}
	for _, item := range current {
		verified[item.Path] = item
	}
	for _, item := range inst.conversions {
		if verified[item.Path] != item {
			// The snapshot predates the edit, so move the edited copy out of
			// the rollback's reach before failing.
			if _, err := inst.sys.Lstat(inst.localCopyPath(item)); err == nil && inst.verifyPristineCopy(item, inst.localCopyPath(item)) != nil {
				return inst.preserveEditedCopy(item)
			}
			return fmt.Errorf(messages.UpgradeConvertPristineChangedFmt, item.Path)
		}
	}
	removeLocal := func(item PristineConversion) error {
		active, err := skillmigration.ActiveNames(inst.root)
		if err != nil {
			return err
		}
		if !active[item.Name] {
			return fmt.Errorf(messages.UpgradeConvertPristineNotActive, item.Path)
		}
		return inst.removePristineCopy(item)
	}
	for _, item := range inst.conversions {
		if item.DeleteOnly {
			if err := removeLocal(item); err != nil {
				return err
			}
		}
	}
	if imports := pendingImports(inst.conversions); len(imports) > 0 {
		if err := inst.preparedConversion.Import(inst.ctx, imports, removeLocal); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) localCopyPath(item PristineConversion) string {
	return filepath.Join(inst.root, filepath.FromSlash(item.Path))
}

// removePristineCopy deletes one converted local copy. It first renames the
// copy into .agent-layer/tmp, so an interruption never leaves a partial tree
// at the original path, then re-verifies the moved copy against the shipped
// content. A copy edited during the upgrade is kept there, outside the
// rollback's reach, and the step fails. If the rename fails, the copy is
// verified and deleted in place.
func (inst *installer) removePristineCopy(item PristineConversion) error {
	src := inst.localCopyPath(item)
	dest, err := inst.conversionRemovalPath(item)
	if err == nil {
		err = inst.sys.Rename(src, dest)
	}
	if err != nil {
		if inst.verifyPristineCopy(item, src) != nil {
			return inst.preserveEditedCopy(item)
		}
		return inst.sys.RemoveAll(src)
	}
	if inst.verifyPristineCopy(item, dest) != nil {
		inst.conversionPreserved = true
		return fmt.Errorf(messages.UpgradeConvertPristineEditedFmt, item.Path, inst.displayPath(dest))
	}
	return inst.sys.RemoveAll(dest)
}

// preserveEditedCopy moves (or, across filesystems, copies) a local copy edited
// during the upgrade into .agent-layer/tmp, which rollback never touches, and
// returns the error that fails the step.
func (inst *installer) preserveEditedCopy(item PristineConversion) error {
	src := inst.localCopyPath(item)
	dest, err := inst.conversionRemovalPath(item)
	if err == nil {
		if err = inst.sys.Rename(src, dest); err != nil {
			err = copyLocalTree(inst.sys, src, dest)
		}
	}
	if err != nil {
		return fmt.Errorf(messages.UpgradeConvertPristineEditedUnpreservedFmt, item.Path, err)
	}
	inst.conversionPreserved = true
	return fmt.Errorf(messages.UpgradeConvertPristineEditedFmt, item.Path, inst.displayPath(dest))
}

// conversionRemovalPath returns item's location in this run's removal
// directory under .agent-layer/tmp, on the project's filesystem.
func (inst *installer) conversionRemovalPath(item PristineConversion) (string, error) {
	if inst.conversionRemovalDir == "" {
		inst.conversionRemovalDir = filepath.Join(inst.agentLayerTmpDir(), "upgrade-conversion-"+newUpgradeSnapshotID(time.Now()))
	}
	dest := filepath.Join(inst.conversionRemovalDir, filepath.FromSlash(item.Path))
	if err := inst.sys.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	return dest, nil
}

// verifyPristineCopy re-checks that path still holds item's shipped content.
func (inst *installer) verifyPristineCopy(item PristineConversion, path string) error {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return err
	}
	pristine := false
	if item.Kind == PristineConversionSkill {
		pristine, err = pristineSkillDir(path, item.Path, inst.sys, manifests)
	} else {
		var info os.FileInfo
		if info, err = inst.sys.Lstat(path); err == nil && info.Mode().IsRegular() {
			var data []byte
			if data, err = inst.sys.ReadFile(path); err == nil {
				pristine = pristineFile(manifests, item.Path, data)
			}
		}
	}
	if err != nil {
		return err
	}
	if !pristine {
		return errors.New("local copy no longer matches the shipped content")
	}
	return nil
}

func (inst *installer) displayPath(path string) string {
	if rel, err := inst.repoRelativePath(path); err == nil {
		return rel
	}
	return path
}

// copyLocalTree copies a file or directory tree, preserving symlinks.
func copyLocalTree(sys System, src, dest string) error {
	return sys.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := sys.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return sys.MkdirAll(target, 0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := sys.Readlink(path)
			if err != nil {
				return err
			}
			return sys.Symlink(link, target)
		default:
			data, err := sys.ReadFile(path)
			if err != nil {
				return err
			}
			return sys.WriteFileAtomic(target, data, info.Mode().Perm())
		}
	})
}

// convertPristineCopiesTargetPaths covers every path a conversion writes:
// configuration, local copies, lock files, and both imported tiers whole. An
// import can materialize other exact selectors already configured in the same
// catalog block, and its journal stages under the tier, so rollback restores
// each tier exactly rather than only the converted names.
func (inst *installer) convertPristineCopiesTargetPaths() []string {
	if len(inst.conversions) == 0 {
		return nil
	}
	paths := config.DefaultPaths(inst.root)
	targets := []string{paths.ConfigPath}
	if len(pendingImports(inst.conversions)) > 0 {
		targets = append(targets, paths.SkillsLockPath, paths.InstructionsLockPath, paths.ImportedSkillsDir, paths.ImportedInstructionsDir)
	}
	for _, item := range inst.conversions {
		targets = append(targets, inst.localCopyPath(item))
	}
	return uniqueNormalizedPaths(targets)
}

func (inst *installer) closePreparedConversion() {
	if inst.preparedConversion != nil {
		inst.preparedConversion.Close()
		inst.preparedConversion = nil
	}
}

func pendingImports(items []PristineConversion) []PristineConversion {
	var imports []PristineConversion
	for _, item := range items {
		if !item.DeleteOnly {
			imports = append(imports, item)
		}
	}
	return imports
}

// conversionRestore reports which imports a snapshot fully reverses.
type conversionRestore struct {
	// tiers maps each imported tier the snapshot captured whole (present or
	// absent) to the import names it held at snapshot time.
	tiers map[string]map[string]bool
}

// covers reports whether rollback removes the import name with its ownership:
// its tier is restored whole and did not hold the name at snapshot time.
func (r conversionRestore) covers(name string) bool {
	paths := config.DefaultPaths("")
	tier := filepath.ToSlash(paths.ImportedSkillsDir)
	if skilljournal.LegacyInstruction(name) != "" {
		tier = filepath.ToSlash(paths.ImportedInstructionsDir)
	}
	held, captured := r.tiers[tier]
	return captured && !held[name]
}

// conversionRestoredImports reports the imports a snapshot fully reverses: it
// restores configuration and both lock files to their pre-conversion bytes and
// captures the import's whole tier, so rollback removes every import committed
// after it. Only these imports may have their local slots recreated by a
// manual rollback.
func conversionRestoredImports(snapshot upgradeSnapshot) conversionRestore {
	paths := config.DefaultPaths("")
	rel := filepath.ToSlash
	entries := map[string]upgradeSnapshotEntry{}
	for _, entry := range snapshot.Entries {
		entries[normalizeRelPath(filepath.Clean(filepath.FromSlash(entry.Path)))] = entry
	}
	restore := conversionRestore{tiers: map[string]map[string]bool{}}
	if entries[rel(paths.ConfigPath)].Kind != upgradeSnapshotEntryKindFile {
		return restore
	}
	for _, lock := range []string{paths.SkillsLockPath, paths.InstructionsLockPath} {
		if _, ok := entries[rel(lock)]; !ok {
			return restore
		}
	}
	for _, tier := range []string{rel(paths.ImportedSkillsDir), rel(paths.ImportedInstructionsDir)} {
		entry, ok := entries[tier]
		if !ok || (entry.Kind != upgradeSnapshotEntryKindDir && entry.Kind != upgradeSnapshotEntryKindAbsent) {
			continue
		}
		held := map[string]bool{}
		for path := range entries {
			if name, ok := strings.CutPrefix(path, tier+"/"); ok {
				held[strings.SplitN(name, "/", 2)[0]] = true
			}
		}
		restore.tiers[tier] = held
	}
	return restore
}
