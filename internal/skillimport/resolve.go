package skillimport

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// desiredSkill is one resolved member of a block's desired set.
type desiredSkill struct {
	// BlockIndex is the configuration index of the owning block.
	BlockIndex int
	Block      config.SkillImport
	// Selector is the positive selector that produced this path. When several
	// positive selectors in the block match one path, the first in
	// configuration order wins so the recorded identity is deterministic.
	Selector     string
	SelectedPath string
	Name         string
	Tree         skilltree.Tree
}

// treeReader reads a repository path's exact content at a commit.
type treeReader interface {
	ReadTree(ctx context.Context, commit string, repoPath string) (skilltree.Tree, error)
	ListDirectories(ctx context.Context, commit string) ([]string, error)
	PathExists(ctx context.Context, commit string, repoPath string) (bool, bool, error)
}

// candidateFailure is one selector match that cannot be accepted as an import.
//
// It is kept separate from a source-level error because the two have different
// blast radii: a fetch, authentication, or ref failure blocks a whole block,
// while an unreadable or invalid individual skill blocks only itself and leaves
// the block's other skills free to import, update, restore, or retire.
type candidateFailure struct {
	Selector string
	Path     string
	Err      error
}

// Name is the local skill name the failed path would have owned. A valid skill
// always declares the name of its own directory, so the base name identifies
// the affected skill even when the manifest could not be validated.
func (f candidateFailure) Name() string { return path.Base(f.Path) }

// resolveBlock expands one block's selectors at a commit into its desired set.
//
// Positive selectors resolve first, every block-local exclusion is applied
// before exact and wildcard validation, ordinary wildcard directories without a
// manifest are ignored, and a path matched by several positive selectors
// becomes one entry. An unreadable or invalid matched skill root is returned as
// a per-candidate failure rather than an error, so callers decide whether it
// blocks only that skill (pull) or the whole change (add and remove).
func resolveBlock(ctx context.Context, source treeReader, blockIndex int, block config.SkillImport, commit string) ([]desiredSkill, []candidateFailure, error) {
	candidates, failures, err := expandPositiveSelectors(ctx, source, block, commit)
	if err != nil {
		return nil, nil, err
	}
	exclusions := block.ExclusionSelectors()

	resolved := make([]desiredSkill, 0, len(candidates))
	for _, candidate := range candidates {
		if matchesAnySelector(candidate.path, exclusions) {
			// Excluded paths are outside the desired set and are never
			// validated as imports.
			continue
		}
		tree, readErr := source.ReadTree(ctx, commit, candidate.path)
		if readErr != nil {
			failures = append(failures, candidateFailure{Selector: candidate.selector, Path: candidate.path,
				Err: fmt.Errorf("selector %q does not resolve to a readable skill at %s: %w", candidate.selector, shortCommit(commit), readErr)})
			continue
		}
		if !skilltree.HasManifest(tree) {
			if candidate.wildcard {
				// A wildcard ignores ordinary directories.
				continue
			}
			failures = append(failures, candidateFailure{Selector: candidate.selector, Path: candidate.path,
				Err: fmt.Errorf("selector %q resolves to %s, which has no %s", candidate.selector, candidate.path, skilltree.SkillManifestName)})
			continue
		}
		info, validateErr := skilltree.ValidateSkill(tree, candidate.path)
		if validateErr != nil {
			failures = append(failures, candidateFailure{Selector: candidate.selector, Path: candidate.path, Err: validateErr})
			continue
		}
		resolved = append(resolved, desiredSkill{
			BlockIndex:   blockIndex,
			Block:        block,
			Selector:     candidate.selector,
			SelectedPath: candidate.path,
			Name:         info.Name,
			Tree:         tree,
		})
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].SelectedPath < resolved[j].SelectedPath })
	sort.Slice(failures, func(i, j int) bool { return failures[i].Path < failures[j].Path })
	return resolved, failures, nil
}

// candidateFailureError renders per-candidate failures as one actionable error
// for the commands that change nothing unless the whole desired set resolves.
func candidateFailureError(failures []candidateFailure) error {
	rendered := make([]string, 0, len(failures))
	for _, failure := range failures {
		rendered = append(rendered, failure.Err.Error())
	}
	return fmt.Errorf("%s", strings.Join(rendered, "; "))
}

// selectorCandidate is one positive-selector match before exclusions apply.
type selectorCandidate struct {
	path     string
	selector string
	wildcard bool
}

// expandPositiveSelectors resolves every positive selector, deduplicating a
// path matched by several selectors into the first match in configuration
// order. An exact selector that names nothing is a failure of that selector
// alone; only a failure to read the source at all blocks the block.
func expandPositiveSelectors(ctx context.Context, source treeReader, block config.SkillImport, commit string) ([]selectorCandidate, []candidateFailure, error) {
	var directories []string
	seen := make(map[string]struct{})
	var candidates []selectorCandidate
	var failures []candidateFailure

	for _, selector := range block.PositiveSelectors() {
		normalized := config.NormalizeSkillSelector(selector)
		if !strings.ContainsAny(normalized, "*?[") {
			exists, isDir, err := source.PathExists(ctx, commit, normalized)
			if err != nil {
				return nil, nil, err
			}
			if !exists || !isDir {
				failures = append(failures, candidateFailure{Selector: selector, Path: normalized,
					Err: fmt.Errorf("selector %q does not exist as a directory at %s", selector, shortCommit(commit))})
				continue
			}
			if _, duplicate := seen[normalized]; duplicate {
				continue
			}
			seen[normalized] = struct{}{}
			candidates = append(candidates, selectorCandidate{path: normalized, selector: selector})
			continue
		}

		if directories == nil {
			listed, err := source.ListDirectories(ctx, commit)
			if err != nil {
				return nil, nil, err
			}
			directories = listed
			if directories == nil {
				directories = []string{}
			}
		}
		for _, dir := range directories {
			matched, matchErr := path.Match(normalized, dir)
			if matchErr != nil {
				return nil, nil, fmt.Errorf("selector %q is not a valid path pattern: %w", selector, matchErr)
			}
			if !matched {
				continue
			}
			if _, duplicate := seen[dir]; duplicate {
				continue
			}
			seen[dir] = struct{}{}
			candidates = append(candidates, selectorCandidate{path: dir, selector: selector, wildcard: true})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })
	return candidates, failures, nil
}

// matchesAnySelector reports whether a path is matched by any of the given
// selector patterns. Exclusions match skill roots exactly or by wildcard; they
// never filter files inside an included skill tree.
func matchesAnySelector(candidate string, selectors []string) bool {
	for _, selector := range selectors {
		normalized := config.NormalizeSkillSelector(selector)
		if normalized == candidate {
			return true
		}
		if matched, err := path.Match(normalized, candidate); err == nil && matched {
			return true
		}
	}
	return false
}

// selectingPositiveSelector returns the first configured positive selector
// that selects candidate after block exclusions are applied. Configuration
// validation guarantees every pattern is syntactically valid before this
// helper is reached.
func selectingPositiveSelector(block config.SkillImport, candidate string) (string, bool) {
	normalizedCandidate := config.NormalizeSkillSelector(candidate)
	if matchesAnySelector(normalizedCandidate, block.ExclusionSelectors()) {
		return "", false
	}
	for _, selector := range block.PositiveSelectors() {
		normalized := config.NormalizeSkillSelector(selector)
		if normalized == normalizedCandidate {
			return normalized, true
		}
		matched, _ := path.Match(normalized, normalizedCandidate)
		if matched {
			return normalized, true
		}
	}
	return "", false
}

// validateDesiredSet enforces the identity rules that only hold across a
// complete desired set: one normalized name per import, no ancestor or
// descendant selected paths within a repository, and no overlapping
// destination paths inside a push group.
func validateDesiredSet(desired []desiredSkill) error {
	byName := make(map[string]desiredSkill, len(desired))
	for _, skill := range desired {
		key := skilltree.NormalizeName(skill.Name)
		if existing, clash := byName[key]; clash {
			return fmt.Errorf("selected paths %s and %s both resolve to skill name %q; narrow one selector so each import owns a distinct name",
				existing.SelectedPath, skill.SelectedPath, skill.Name)
		}
		byName[key] = skill
	}

	byRepository := make(map[string][]desiredSkill)
	for _, skill := range desired {
		repository := config.NormalizeSkillRepository(skill.Block.Repository)
		byRepository[repository] = append(byRepository[repository], skill)
	}
	for repository, skills := range byRepository {
		if err := rejectOverlappingPaths(skills, fmt.Sprintf("source repository %s", repository)); err != nil {
			return err
		}
	}

	byPushGroup := make(map[string][]desiredSkill)
	for _, skill := range desired {
		if !skill.Block.WriteEnabled() {
			continue
		}
		key := skill.Block.EffectivePushRepository() + "\x00" + strings.TrimSpace(skill.Block.PushBranch)
		byPushGroup[key] = append(byPushGroup[key], skill)
	}
	for key, skills := range byPushGroup {
		repository, branch, _ := strings.Cut(key, "\x00")
		label := fmt.Sprintf("push destination %s", repository)
		if branch != "" {
			label += " branch " + branch
		}
		if err := rejectOverlappingPaths(skills, label); err != nil {
			return err
		}
	}
	return nil
}

// rejectOverlappingPaths fails when one selected path is an ancestor or
// descendant of another, because that would create overlapping editable owners.
//
// Comparing sorted neighbours is not enough: every byte below '/' sorts ahead
// of it, so "a/b-x" lands between "a/b" and "a/b/c" and would hide that pair.
// Each path is instead checked against every path already accepted, by walking
// its own ancestor prefixes. Sorting guarantees an ancestor is accepted before
// any of its descendants, because an ancestor is a strict prefix.
func rejectOverlappingPaths(skills []desiredSkill, scope string) error {
	paths := make([]string, 0, len(skills))
	for _, skill := range skills {
		paths = append(paths, skill.SelectedPath)
	}
	sort.Strings(paths)
	accepted := make(map[string]struct{}, len(paths))
	for _, current := range paths {
		for ancestor := current; ancestor != "." && ancestor != "/" && ancestor != ""; ancestor = path.Dir(ancestor) {
			if _, exists := accepted[ancestor]; exists {
				return fmt.Errorf("selected paths %s and %s overlap within %s; overlapping editable owners are not supported",
					ancestor, current, scope)
			}
		}
		accepted[current] = struct{}{}
	}
	return nil
}

// shortCommit renders a commit id for messages without losing identity.
func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

// ensureTrackedRefKind enforces the tracking rules that only remote resolution
// can prove: a branch may track or pin, while a tag or commit can only pin.
func ensureTrackedRefKind(block config.SkillImport, resolution gitrepo.Resolution) (string, error) {
	configured := strings.TrimSpace(block.Tracking)
	if configured == config.SkillTrackingTracked && resolution.Kind != skilllock.RefKindBranch {
		return "", fmt.Errorf("tracking = \"tracked\" requires a branch, but ref %q resolves to a %s; use tracking = \"pinned\" or configure a branch",
			resolution.Ref, resolution.Kind)
	}
	if configured != "" {
		return configured, nil
	}
	if resolution.Kind == skilllock.RefKindBranch {
		return config.SkillTrackingTracked, nil
	}
	return config.SkillTrackingPinned, nil
}
