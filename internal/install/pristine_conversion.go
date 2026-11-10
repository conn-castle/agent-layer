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
)

// PristineConverter imports unmodified shipped copies during al upgrade. The
// installer holds the project lock, has recovered interrupted imports, and owns
// the upgrade snapshot; implementations must not take the lock again.
type PristineConverter interface {
	// Prepare fetches and validates every import the non-DeleteOnly items need
	// without writing to the project. It plans them as the commit will see the
	// project: with the DeleteOnly items' local copies already removed.
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
	// Only new imports are a choice. A leftover copy of an intact import is
	// always removed: sync cannot load it beside its import.
	imports := pendingImports(items)
	if len(imports) > 0 {
		accepted, err := inst.prompter.convertPristineCopies(imports)
		if err != nil {
			return err
		}
		if !accepted {
			_, _ = fmt.Fprintln(inst.warnOutput(), messages.UpgradeConvertPristineDeclined)
			imports = nil
		}
	}
	var conversions []PristineConversion
	for _, item := range items {
		if item.DeleteOnly {
			conversions = append(conversions, item)
		}
	}
	conversions = append(conversions, imports...)
	if len(imports) > 0 {
		if inst.converter == nil {
			return errors.New("converting unmodified shipped copies requires an import converter")
		}
		prepared, err := inst.converter.Prepare(inst.ctx, inst.root, conversions)
		if err != nil {
			return fmt.Errorf(messages.UpgradeConvertPristineFetchFmt, len(imports), err)
		}
		inst.preparedConversion = prepared
	}
	// Announce leftover removals only once the preflight can no longer fail.
	for _, item := range conversions {
		if item.DeleteOnly {
			_, _ = fmt.Fprintf(inst.warnOutput(), messages.UpgradeConvertPristineLeftoverFmt, item.Path, item.Selector)
		}
	}
	inst.conversions = conversions
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
		// A copy replaced after the snapshot (for example by a link) fails
		// planning; it must still be moved out of the rollback's reach.
		return inst.failConversion(err)
	}
	verified := map[string]PristineConversion{}
	for _, item := range current {
		verified[item.Path] = item
	}
	for _, item := range inst.conversions {
		if verified[item.Path] != item {
			return inst.failConversion(fmt.Errorf(messages.UpgradeConvertPristineChangedFmt, item.Path))
		}
	}
	removeLocal := func(item PristineConversion) error {
		// Re-check the import right before deleting the only other copy.
		cfg, err := loadTransitionConfig(inst.root, inst.sys)
		if err != nil {
			return err
		}
		if err := intactImport(inst.root, cfg, item); err != nil {
			return fmt.Errorf(messages.UpgradeConvertPristineNotActiveFmt, item.Path, err)
		}
		return inst.removePristineCopy(item)
	}
	for _, item := range inst.conversions {
		if item.DeleteOnly {
			if err := removeLocal(item); err != nil {
				return inst.failConversion(err)
			}
		}
	}
	if imports := pendingImports(inst.conversions); len(imports) > 0 {
		// Imports edit the configuration, which a project may lack until
		// writeTemplateFiles seeds it; seed it here, inside the snapshotted step.
		configPath := config.DefaultPaths(inst.root).ConfigPath
		if err := inst.templates().writeTemplateFile(configPath, configFileName, 0o644, nil, nil); err != nil {
			return inst.failConversion(err)
		}
		if err := inst.preparedConversion.Import(inst.ctx, imports, removeLocal); err != nil {
			return inst.failConversion(err)
		}
	}
	return nil
}

// failConversion fails the step after moving every local copy edited during
// the upgrade out of the rollback's reach. The snapshot predates those edits,
// so restoring it would otherwise overwrite them. Delete-only copies are not
// rollback targets, so an edit to one is already left in place.
func (inst *installer) failConversion(cause error) error {
	var preserved []error
	for _, item := range pendingImports(inst.conversions) {
		path := inst.localCopyPath(item)
		if _, err := inst.sys.Lstat(path); err != nil || inst.verifyPristineCopy(item, path) == nil {
			continue
		}
		preserved = append(preserved, inst.preserveEditedCopy(item))
	}
	if len(preserved) == 0 {
		return cause
	}
	return errors.Join(append([]error{cause}, preserved...)...)
}

func (inst *installer) localCopyPath(item PristineConversion) string {
	return filepath.Join(inst.root, filepath.FromSlash(item.Path))
}

// removePristineCopy deletes one converted local copy. It first renames the
// copy into .agent-layer/tmp, so an interruption never leaves a partial tree
// at the original path, then re-verifies the moved copy against the shipped
// content. A copy edited during the upgrade is kept there, outside the
// rollback's reach, and the step fails. Without an atomic rename the step
// fails rather than deleting in place.
func (inst *installer) removePristineCopy(item PristineConversion) error {
	src := inst.localCopyPath(item)
	dest, err := inst.conversionRemovalPath(item)
	if err == nil {
		err = inst.sys.Rename(src, dest)
	}
	if err != nil {
		return fmt.Errorf(messages.UpgradeConvertPristineMoveFmt, item.Path, err)
	}
	if inst.verifyPristineCopy(item, dest) != nil {
		inst.conversionPreserved = true
		return fmt.Errorf(messages.UpgradeConvertPristineEditedFmt, item.Path, inst.displayPath(dest))
	}
	return inst.sys.RemoveAll(dest)
}

// preserveEditedCopy moves (or, across filesystems, copies) a local copy edited
// during the upgrade into .agent-layer/tmp, which rollback never touches, and
// returns the error that fails the step. The full rollback still runs, so when
// neither works it restores the shipped copy and the edit is lost; the error
// names the path.
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
	for _, item := range pendingImports(inst.conversions) {
		targets = append(targets, inst.localCopyPath(item))
	}
	return uniqueNormalizedPaths(targets)
}

// isDeleteOnlyConversion reports whether path is the local copy of a
// delete-only conversion. Such a copy is an unmodified leftover of an import
// that already committed, so the upgrade neither snapshots it nor lets any
// rollback recreate it beside that import.
func (inst *installer) isDeleteOnlyConversion(path string) bool {
	for _, item := range inst.conversions {
		if item.DeleteOnly && filepath.Clean(inst.localCopyPath(item)) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

func (inst *installer) closePreparedConversion() {
	if inst.preparedConversion != nil {
		inst.preparedConversion.Close()
		inst.preparedConversion = nil
	}
}

// pendingImports returns the items that still need an import; the rest are
// delete-only leftovers of imports that already committed.
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
// restores configuration (or its absence, when the conversion seeded it) and
// both lock files to their pre-conversion state and
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
	// An absent configuration was seeded by the conversion; rollback removes it.
	if kind := entries[rel(paths.ConfigPath)].Kind; kind != upgradeSnapshotEntryKindFile && kind != upgradeSnapshotEntryKindAbsent {
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
