package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// unknownScanRoots returns the directories scanned for untracked files.
func (inst *installer) unknownScanRoots() []string {
	return []string{
		filepath.Join(inst.root, ".agent-layer"),
		filepath.Join(inst.root, "docs", "agent-layer"),
	}
}

func (inst *installer) scanUnknowns() error {
	known, err := inst.buildKnownPaths()
	if err != nil {
		return err
	}

	for _, root := range inst.unknownScanRoots() {
		if err := inst.scanUnknownRoot(root, known); err != nil {
			return err
		}
	}
	inst.sortUnknowns()
	return nil
}

func (inst *installer) scanUnknownRoot(root string, known map[string]struct{}) error {
	sys := inst.sys
	if _, err := sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, root, err)
	}
	return sys.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if clean == filepath.Clean(root) {
			return nil
		}
		if _, ok := known[clean]; ok {
			return nil
		}
		inst.recordUnknown(clean)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

// handleUnknowns prompts the user about files under .agent-layer/ and
// docs/agent-layer/ that are not tracked by Agent Layer. It performs a fresh
// scan at call time so the list reflects the actual post-migration state (the
// early scanUnknowns call captures unknowns for snapshot/rollback safety but
// may include paths that migrations have since moved or deleted).
func (inst *installer) handleUnknowns() error {
	if !inst.overwrite {
		return nil
	}
	unknowns, err := inst.scanCurrentUnknowns()
	if err != nil {
		return err
	}
	if len(unknowns) == 0 {
		return nil
	}
	if inst.prompter == nil {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	rel := inst.relativePathList(unknowns)
	deleteAll, err := inst.prompter.DeleteUnknownAll(rel)
	if err != nil {
		return err
	}
	if deleteAll {
		return inst.deleteUnknowns(unknowns)
	}
	for _, path := range unknowns {
		relPath := inst.relativePath(path)
		deletePath, err := inst.prompter.DeleteUnknown(relPath)
		if err != nil {
			return err
		}
		if deletePath {
			if err := inst.sys.RemoveAll(path); err != nil {
				return fmt.Errorf(messages.InstallDeleteUnknownFailedFmt, relPath, err)
			}
		}
	}
	return nil
}

// scanCurrentUnknowns performs a fresh scan for unknown files, returning the
// list directly without modifying inst.unknowns (which is preserved for
// snapshot rollback safety).
func (inst *installer) scanCurrentUnknowns() ([]string, error) {
	known, err := inst.buildKnownPaths()
	if err != nil {
		return nil, err
	}
	var unknowns []string
	for _, root := range inst.unknownScanRoots() {
		found, walkErr := inst.walkUnknownsInRoot(root, known)
		if walkErr != nil {
			return nil, walkErr
		}
		unknowns = append(unknowns, found...)
	}
	sort.Slice(unknowns, func(i, j int) bool {
		return inst.relativePath(unknowns[i]) < inst.relativePath(unknowns[j])
	})
	return unknowns, nil
}

// walkUnknownsInRoot walks a single directory tree and returns paths not in known.
func (inst *installer) walkUnknownsInRoot(root string, known map[string]struct{}) ([]string, error) {
	sys := inst.sys
	if _, err := sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf(messages.InstallFailedStatFmt, root, err)
	}
	var unknowns []string
	walkErr := sys.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if clean == filepath.Clean(root) {
			return nil
		}
		if _, ok := known[clean]; ok {
			return nil
		}
		unknowns = append(unknowns, clean)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return unknowns, nil
}

func (inst *installer) deleteUnknowns(paths []string) error {
	sys := inst.sys
	for _, path := range paths {
		rel := inst.relativePath(path)
		if err := sys.RemoveAll(path); err != nil {
			return fmt.Errorf(messages.InstallDeleteUnknownFailedFmt, rel, err)
		}
	}
	return nil
}

func (inst *installer) sortUnknowns() {
	sort.Slice(inst.unknowns, func(i, j int) bool {
		return inst.relativePath(inst.unknowns[i]) < inst.relativePath(inst.unknowns[j])
	})
}

func (inst *installer) relativeUnknowns() []string {
	if len(inst.unknowns) == 0 {
		return nil
	}
	rel := make([]string, 0, len(inst.unknowns))
	for _, path := range inst.unknowns {
		rel = append(rel, inst.relativePath(path))
	}
	sort.Strings(rel)
	return rel
}

// relativePathList converts absolute paths to root-relative paths, sorted.
func (inst *installer) relativePathList(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	rel := make([]string, 0, len(paths))
	for _, path := range paths {
		rel = append(rel, inst.relativePath(path))
	}
	sort.Strings(rel)
	return rel
}

func (inst *installer) buildKnownPaths() (map[string]struct{}, error) {
	known := make(map[string]struct{})
	add := func(path string) {
		known[filepath.Clean(path)] = struct{}{}
	}

	root := inst.root
	add(filepath.Join(root, ".agent-layer"))
	add(filepath.Join(root, ".agent-layer", "instructions"))
	add(filepath.Join(root, ".agent-layer", "skills"))
	add(filepath.Join(root, ".agent-layer", "templates"))
	add(filepath.Join(root, ".agent-layer", "templates", "docs"))
	add(filepath.Join(root, ".agent-layer", "state"))
	add(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json"))
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	add(snapshotDir)
	add(filepath.Join(root, ".agent-layer", "tmp"))
	add(filepath.Join(root, ".agent-layer", "tmp", "runs"))

	// Root-level managed files.
	for _, file := range inst.templates().knownTemplateFiles() {
		add(file.path)
	}
	add(filepath.Join(root, ".agent-layer", "al.version"))

	// VS Code launchers generated by sync.
	for _, path := range launchers.VSCodePaths(root).All() {
		add(path)
	}

	addTemplatePaths := func(templateRoot string, destRoot string) error {
		return templates.Walk(templateRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path == templateRoot {
					add(destRoot)
					return nil
				}
				rel := strings.TrimPrefix(path, templateRoot+"/")
				if rel == path {
					return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
				}
				add(filepath.Join(destRoot, rel))
				return nil
			}
			rel := strings.TrimPrefix(path, templateRoot+"/")
			if rel == path {
				return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
			}
			add(filepath.Join(destRoot, rel))
			return nil
		})
	}

	if err := addTemplatePaths("instructions", filepath.Join(root, ".agent-layer", "instructions")); err != nil {
		return nil, err
	}
	if err := addTemplatePaths("skills", filepath.Join(root, ".agent-layer", "skills")); err != nil {
		return nil, err
	}
	if err := addTemplatePaths("docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")); err != nil {
		return nil, err
	}

	// docs/agent-layer/ output directory (memory files written outside .agent-layer).
	add(filepath.Join(root, "docs", "agent-layer"))
	if err := addTemplatePaths("docs/agent-layer", filepath.Join(root, "docs", "agent-layer")); err != nil {
		return nil, err
	}

	if err := inst.addExistingKnownPaths(snapshotDir, add); err != nil {
		return nil, err
	}

	return known, nil
}

func (inst *installer) addExistingKnownPaths(root string, add func(string)) error {
	sys := inst.sys
	if _, err := sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, root, err)
	}
	return sys.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		add(path)
		return nil
	})
}
