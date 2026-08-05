package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var exclusiveSkillRootPaths = [][]string{
	{".agents", skillDirectoryName},
	{".claude", skillDirectoryName},
}

// preflightPendingExclusiveSkillRoots performs the ownership transition only
// when its version-targeted migration is pending. Once the installed version
// reaches that target, marker-free generated roots are accepted on upgrades.
func (inst *installer) preflightPendingExclusiveSkillRoots() error {
	for _, op := range inst.pendingMigrationOps {
		if op.Kind == upgradeMigrationKindClaimSkillRoots {
			return inst.preflightExclusiveSkillRoots()
		}
	}
	return nil
}

// preflightExclusiveSkillRoots rejects client-root entries that cannot be
// identified as projections produced by released Agent Layer versions. It is
// inspection-only and runs before init or upgrade mutates the repository.
func (inst *installer) preflightExclusiveSkillRoots() error {
	var blocking []string
	for _, parts := range exclusiveSkillRootPaths {
		root := filepath.Join(append([]string{inst.root}, parts...)...)
		paths, err := blockingSkillRootEntries(inst.sys, root)
		if err != nil {
			return err
		}
		blocking = append(blocking, paths...)
	}
	if len(blocking) == 0 {
		return nil
	}
	sort.Strings(blocking)
	return fmt.Errorf("cannot claim the client skill roots while these paths contain non-Agent-Layer content: %s; move each skill into .agent-layer/skills or remove it, then retry", strings.Join(blocking, ", "))
}

func blockingSkillRootEntries(sys System, root string) ([]string, error) {
	var blocking []string
	err := sys.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if path == root {
			return nil
		}
		if filepath.Dir(path) != root {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		safe := false
		if entry.IsDir() {
			manifest, err := sys.ReadFile(filepath.Join(path, "SKILL.md"))
			if err == nil {
				content := string(manifest)
				safe = strings.Contains(content, "GENERATED FILE") &&
					strings.Contains(content, "Source: .agent-layer/") &&
					strings.Contains(content, "Regenerate: al sync")
			}
		}
		if !safe {
			blocking = append(blocking, path)
		}
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect client skill root %s: %w", root, err)
	}
	return blocking, nil
}
