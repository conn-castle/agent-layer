package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

var exclusiveSkillRootPaths = [][]string{
	{".agents", skillDirectoryName},
	{".claude", skillDirectoryName},
}

// projectionSourceTierPaths are the skill source tiers a client root is
// projected from.
var projectionSourceTierPaths = [][]string{
	{agentLayerDirName, skillDirectoryName},
	{agentLayerDirName, config.ImportedSkillsDirName},
}

const releasedSkillProjectionMarkerPrefix = "<!--\n  GENERATED FILE\n  Source: .agent-layer/"

const releasedSkillProjectionMarkerSuffix = "\n  Regenerate: al sync\n-->\n"

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
// identified as Agent Layer projections, either by a released version's
// generated header or by naming a skill the current source tiers project. It
// is inspection-only and runs before init or upgrade mutates the repository.
func (inst *installer) preflightExclusiveSkillRoots() error {
	projected, err := projectionSourceSkillNames(inst.sys, inst.root)
	if err != nil {
		return err
	}
	var blocking []string
	for _, parts := range exclusiveSkillRootPaths {
		root := filepath.Join(append([]string{inst.root}, parts...)...)
		paths, err := blockingSkillRootEntries(inst.sys, root, projected)
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

// projectionSourceSkillNames returns the normalized names of every skill the
// source tiers project. A client-root directory carrying one of these names is
// Agent Layer output rather than user content: every release that projects
// skills rewrites the client directory of every source skill on each sync, so
// nothing a user placed under such a name has survived to this point.
//
// This is the ownership evidence for projections written since skill trees
// became byte-exact copies of their source and stopped carrying a generated
// header. Released projections that still carry that header are recognized by
// hasReleasedSkillProjectionMarker instead.
func projectionSourceSkillNames(sys System, root string) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	for _, parts := range projectionSourceTierPaths {
		tier := filepath.Join(append([]string{root}, parts...)...)
		if err := collectSourceSkillNames(sys, tier, names); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func collectSourceSkillNames(sys System, tier string, into map[string]struct{}) error {
	err := sys.WalkDir(tier, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == tier && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if path == tier {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		// Only a directory holding a canonical regular manifest can be a
		// projectable skill source. Anything else in the tier cannot launder a
		// client-root name. A missing manifest means non-skill content; any
		// other inspection failure must surface rather than silently drop the
		// name and misreport its projection as user content.
		manifestPath := filepath.Join(path, skillManifestFileName)
		manifest, err := sys.Lstat(manifestPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect skill manifest %s: %w", manifestPath, err)
		}
		if err == nil && manifest.Mode().IsRegular() {
			into[skilltree.NormalizeName(entry.Name())] = struct{}{}
		}
		return fs.SkipDir
	})
	if err != nil {
		return fmt.Errorf("inspect skill source tier %s: %w", tier, err)
	}
	return nil
}

func blockingSkillRootEntries(sys System, root string, projected map[string]struct{}) ([]string, error) {
	var blocking []string
	err := sys.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if path == root {
			if !entry.IsDir() {
				blocking = append(blocking, path)
			}
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
			if _, projectedName := projected[skilltree.NormalizeName(entry.Name())]; projectedName {
				safe = true
			} else if manifest, err := sys.ReadFile(filepath.Join(path, skillManifestFileName)); err == nil {
				safe = hasReleasedSkillProjectionMarker(manifest)
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

func hasReleasedSkillProjectionMarker(manifest []byte) bool {
	content := string(manifest)
	if !strings.HasPrefix(content, releasedSkillProjectionMarkerPrefix) {
		return false
	}
	sourceEnd := strings.IndexByte(content[len(releasedSkillProjectionMarkerPrefix):], '\n')
	if sourceEnd <= 0 {
		return false
	}
	source := content[len(releasedSkillProjectionMarkerPrefix) : len(releasedSkillProjectionMarkerPrefix)+sourceEnd]
	if !isReleasedSkillProjectionSource(source) {
		return false
	}
	headerRemainder := content[len(releasedSkillProjectionMarkerPrefix)+sourceEnd:]
	return strings.HasPrefix(headerRemainder, releasedSkillProjectionMarkerSuffix)
}

func isReleasedSkillProjectionSource(source string) bool {
	parts := strings.Split(source, "/")
	if len(parts) != 3 || parts[0] != skillDirectoryName || parts[1] == "" {
		return false
	}
	return parts[2] == skillManifestFileName || parts[2] == "skill.md"
}
