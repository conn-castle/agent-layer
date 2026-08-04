package skillimports

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// repoIndex is the directory and skill-root view of one repository at one commit.
// Selectors are resolved against this Git-derived index, never against a
// checked-out working tree, so untrusted content is never materialized just to
// decide what to import.
type repoIndex struct {
	// directories holds every directory path in the tree.
	directories map[string]struct{}
	// skillRoots holds every directory that directly contains a canonical SKILL.md.
	skillRoots map[string]struct{}
	// manifestVariants holds directories that contain a non-canonical manifest
	// spelling, so an exact selector can explain the real problem.
	manifestVariants map[string]struct{}
}

// buildRepoIndex lists the repository tree at a commit once and derives the
// directory and skill-root sets from it.
func buildRepoIndex(ctx context.Context, space *workspace, commit string) (*repoIndex, error) {
	out, err := space.run(ctx, "ls-tree", "-r", "-z", "--full-tree", "--name-only", commit)
	if err != nil {
		return nil, fmt.Errorf("list tree at %s: %w", commit, err)
	}
	index := &repoIndex{
		directories:      map[string]struct{}{},
		skillRoots:       map[string]struct{}{},
		manifestVariants: map[string]struct{}{},
	}
	for _, filePath := range strings.Split(string(out), "\x00") {
		if filePath == "" {
			continue
		}
		normalized, normErr := normalizeTreePath(filePath)
		if normErr != nil {
			return nil, fmt.Errorf("list tree at %s: %w", commit, normErr)
		}
		dir := path.Dir(normalized)
		if dir == "." {
			dir = ""
		}
		for current := dir; current != ""; current = parentDir(current) {
			index.directories[current] = struct{}{}
		}
		base := path.Base(normalized)
		switch {
		case base == SkillManifestName:
			if dir != "" {
				index.skillRoots[dir] = struct{}{}
			}
		case strings.EqualFold(base, SkillManifestName):
			if dir != "" {
				index.manifestVariants[dir] = struct{}{}
			}
		}
	}
	return index, nil
}

func parentDir(p string) string {
	parent := path.Dir(p)
	if parent == "." || parent == "/" {
		return ""
	}
	return parent
}

// SelectorResolution is the desired set for one import block at one commit.
type SelectorResolution struct {
	// Paths are the deduplicated selected skill roots, sorted.
	Paths []string
	// Exclusions are the block's normalized exclusion selectors, sorted.
	Exclusions []string
}

// resolveSelectors expands one block's selectors against a repository index.
//
// Exclusions are applied to whole candidate roots before exact and wildcard
// validation, so an excluded path is outside the desired set and is never
// validated as an import. Exclusions never filter files inside an included skill
// tree, and they never reach another block.
func resolveSelectors(imp config.SkillImport, index *repoIndex) (SelectorResolution, error) {
	positives := imp.PositiveSelectors()
	exclusions := imp.ExclusionSelectors()
	if len(positives) == 0 {
		return SelectorResolution{}, fmt.Errorf(
			"repository %s has no positive selector; an exclusion never imports a skill by itself",
			RedactSecrets(config.NormalizeSkillRepository(imp.Repository)),
		)
	}

	excluded := func(candidate string) bool {
		for _, exclusion := range exclusions {
			if matchSelectorPath(exclusion, candidate) {
				return true
			}
		}
		return false
	}

	selected := map[string]struct{}{}
	for _, selector := range positives {
		if config.IsSkillSelectorWildcard(selector) {
			for _, candidate := range sortedKeys(index.directories) {
				if !matchSelectorPath(selector, candidate) {
					continue
				}
				if excluded(candidate) {
					continue
				}
				if _, ok := index.skillRoots[candidate]; !ok {
					// A wildcard walks ordinary directories; only skill roots are
					// candidates. A non-canonical manifest spelling is still reported,
					// because silently skipping it would hide a real skill.
					if _, variant := index.manifestVariants[candidate]; variant {
						return SelectorResolution{}, fmt.Errorf(
							"%s matched %q, which has a skill manifest that is not named %s; rename it upstream or exclude the path",
							selector, candidate, SkillManifestName,
						)
					}
					continue
				}
				selected[candidate] = struct{}{}
			}
			continue
		}

		if excluded(selector) {
			// An exclusion wins regardless of order and regardless of whether the
			// positive selector was exact.
			continue
		}
		if _, ok := index.directories[selector]; !ok {
			return SelectorResolution{}, fmt.Errorf("selector %q does not exist in the source repository", selector)
		}
		if _, ok := index.skillRoots[selector]; !ok {
			if _, variant := index.manifestVariants[selector]; variant {
				return SelectorResolution{}, fmt.Errorf(
					"selector %q has a skill manifest that is not named %s", selector, SkillManifestName,
				)
			}
			return SelectorResolution{}, fmt.Errorf("selector %q has no %s", selector, SkillManifestName)
		}
		selected[selector] = struct{}{}
	}

	paths := sortedKeys(selected)
	if err := rejectOverlappingPaths(paths); err != nil {
		return SelectorResolution{}, err
	}
	sort.Strings(exclusions)
	return SelectorResolution{Paths: paths, Exclusions: exclusions}, nil
}

// rejectOverlappingPaths fails when one selected path contains another. Two
// overlapping editable owners cannot both own the shared files, so this is a
// configuration error rather than a resolution choice.
func rejectOverlappingPaths(paths []string) error {
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if isPathAncestor(paths[i], paths[j]) {
				return fmt.Errorf(
					"selected paths %q and %q overlap; a skill inside another skill would create two editable owners for the same files",
					paths[i], paths[j],
				)
			}
		}
	}
	return nil
}

// isPathAncestor reports whether ancestor contains descendant.
func isPathAncestor(ancestor string, descendant string) bool {
	if ancestor == descendant {
		return true
	}
	return strings.HasPrefix(descendant, ancestor+"/")
}

// matchSelectorPath matches a selector pattern against a candidate path.
// `*` and `?` never cross a path separator; `**` matches any number of segments,
// including none.
func matchSelectorPath(pattern string, candidate string) bool {
	if pattern == candidate {
		return true
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(candidate, "/"))
}

func matchSegments(pattern []string, candidate []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// `**` matches zero or more segments; try every split point.
			for skip := 0; skip <= len(candidate); skip++ {
				if matchSegments(pattern[1:], candidate[skip:]) {
					return true
				}
			}
			return false
		}
		if len(candidate) == 0 {
			return false
		}
		matched, err := path.Match(pattern[0], candidate[0])
		if err != nil || !matched {
			return false
		}
		pattern = pattern[1:]
		candidate = candidate[1:]
	}
	return len(candidate) == 0
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
