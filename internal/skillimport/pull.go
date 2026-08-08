package skillimport

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Pull fetches every configured source, reconciles it with local state, commits
// each independently successful skill, and projects the results.
//
// It is the only command that advances tracked imports. Pinned imports stay at
// their locked commits unless the configured ref itself changed.
func (s *Service) Pull(ctx context.Context) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		return s.pullLocked(ctx, st, report)
	})
	report.Sort()
	return report, err
}

func (s *Service) pullLocked(ctx context.Context, st *state, report *Report) error {
	if err := failOnOrphans(st); err != nil {
		return err
	}

	txn := newTransaction(pathSetFor(st), st.lock)
	adoptMissingImports(st, txn, report)

	if len(st.cfg.Skills.Imports) > 0 {
		runner, err := s.newRunner(st.env)
		if err != nil {
			return err
		}
		workRoot, err := os.MkdirTemp("", "al-skill-pull-")
		if err != nil {
			return fmt.Errorf("failed to create a git working directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(workRoot) }()

		for index, block := range st.cfg.Skills.Imports {
			s.pullBlock(ctx, runner, workRoot, st, txn, index, block, report)
		}
	}

	retireUnconfigured(st, txn, report)

	if txn.NeedsCommit(st.lock, st.lockPresent) {
		if err := txn.Commit(); err != nil {
			return err
		}
	}
	s.project(report)
	return nil
}

// pullBlock reconciles one import block. A source-level failure blocks every
// skill in the block and leaves other sources unaffected.
func (s *Service) pullBlock(ctx context.Context, runner *gitrepo.Runner, workRoot string, st *state, txn *transaction, index int, block config.SkillImport, report *Report) {
	repository := config.NormalizeSkillRepository(block.Repository)
	// A source-level failure blocks every skill in the block, so each blocked
	// skill is reported alongside the source line rather than left unaccounted
	// for.
	blocked := func(err error) {
		report.AddSourceFailure(repository, block.Ref, err)
		reportBlockedBlockSkills(report, st, txn, block, err)
	}

	blockCtx, err := s.openBlock(ctx, runner, workRoot, index, block)
	if err != nil {
		blocked(err)
		return
	}

	// Lock entries are read from the pending transaction, not the loaded
	// snapshot, so earlier blocks' independent results are visible here.
	lockedEntries := txnEntriesForBlock(st, txn, block)

	// Membership is always resolved at the operation's current source target.
	// That lets a wildcard discover a new skill immediately without making an
	// existing pinned sibling move off its own independently recorded commit.
	desired, failures, err := resolveBlock(ctx, blockCtx.Source, index, block, blockCtx.Resolution.Commit)
	if err != nil {
		blocked(err)
		return
	}
	desiredByPath := make(map[string]desiredSkill, len(desired))
	for _, skill := range desired {
		desiredByPath[skill.SelectedPath] = skill
	}
	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	failedByPath := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		failedByPath[failure.Path] = struct{}{}
	}

	// Validate the actual prospective set. Existing pins that stay on their own
	// commits and existing skills preserved after an upstream validation failure
	// remain owners even when the current source tree does not provide a usable
	// desiredSkill for them.
	prospective := make([]desiredSkill, 0, len(desired)+len(lockedEntries))
	for _, skill := range desired {
		entry, locked := lockedByPath[skill.SelectedPath]
		if locked && targetCommitForEntry(blockCtx, entry) != blockCtx.Resolution.Commit {
			continue
		}
		prospective = append(prospective, skill)
	}
	for _, entry := range lockedEntries {
		_, failed := failedByPath[entry.SelectedPath]
		if targetCommitForEntry(blockCtx, entry) == blockCtx.Resolution.Commit && !failed {
			continue
		}
		selector, selected := selectingPositiveSelector(block, entry.SelectedPath)
		if !selected {
			continue
		}
		prospective = append(prospective, desiredSkill{BlockIndex: index, Block: block, Selector: selector, SelectedPath: entry.SelectedPath, Name: entry.Name})
	}
	if err := validateDesiredSet(withOtherPendingEntries(st, txn, block, prospective)); err != nil {
		blocked(err)
		return
	}

	// A skill that cannot be read or validated at the current target fails on
	// its own. Its local directory and independent lock entry are preserved.
	failedPaths := reportCandidateFailures(report, repository, failures)

	for _, entry := range lockedEntries {
		if _, selected := selectingPositiveSelector(block, entry.SelectedPath); !selected {
			retire(st, txn, entry, report)
			continue
		}
		targetCommit := targetCommitForEntry(blockCtx, entry)
		if targetCommit == blockCtx.Resolution.Commit {
			if _, invalid := failedPaths[entry.SelectedPath]; invalid {
				continue
			}
			skill, still := desiredByPath[entry.SelectedPath]
			if !still {
				retire(st, txn, entry, report)
				continue
			}
			entryCtx := *blockCtx
			entryCtx.TargetCommit = targetCommit
			s.advanceExisting(ctx, runner, st, txn, &entryCtx, skill, entry, report)
			continue
		}

		// This pin remains at its own commit. Read and validate that exact path so
		// its lock evidence, rather than another member's generation, is the only
		// merge base involved.
		skill, skillErr := desiredSkillAtCommit(ctx, blockCtx.Source, index, block, entry, targetCommit)
		if skillErr != nil {
			report.Add(SkillResult{Name: entry.Name, Repository: entry.Repository, SelectedPath: entry.SelectedPath, Outcome: OutcomeFailed, Err: skillErr})
			continue
		}
		entryCtx := *blockCtx
		entryCtx.TargetCommit = targetCommit
		s.advanceExisting(ctx, runner, st, txn, &entryCtx, skill, entry, report)
	}

	for _, skill := range desired {
		if _, locked := lockedByPath[skill.SelectedPath]; locked {
			continue
		}
		s.importNew(st, txn, blockCtx, skill, report)
	}
}

// reportCandidateFailures records one failed skill result per unusable selector
// match and returns the selected paths they cover.
func reportCandidateFailures(report *Report, repository string, failures []candidateFailure) map[string]struct{} {
	paths := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		paths[failure.Path] = struct{}{}
		report.Add(SkillResult{
			Name:         failure.Name(),
			Repository:   repository,
			SelectedPath: failure.Path,
			Outcome:      OutcomeFailed,
			Err:          failure.Err,
		})
	}
	return paths
}

// reportBlockedBlockSkills records the failed result every skill a block-level
// failure blocked. Recorded members come from the lock; a newly configured
// exact selector has no lock entry yet and would otherwise be missing from the
// report entirely.
func reportBlockedBlockSkills(report *Report, st *state, txn *transaction, block config.SkillImport, err error) {
	entries := txnEntriesForBlock(st, txn, block)
	reportBlockedSkills(report, entries, err)

	covered := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		covered[entry.SelectedPath] = struct{}{}
	}
	repository := config.NormalizeSkillRepository(block.Repository)
	for _, selector := range block.PositiveSelectors() {
		normalized := config.NormalizeSkillSelector(selector)
		if strings.ContainsAny(normalized, "*?[") {
			// A wildcard's membership is only knowable from the source that
			// just failed, so there is no skill to name.
			continue
		}
		if _, ok := covered[normalized]; ok {
			continue
		}
		report.Add(SkillResult{
			Name:         path.Base(normalized),
			Repository:   repository,
			SelectedPath: normalized,
			Outcome:      OutcomeFailed,
			Err:          err,
		})
	}
}

// desiredSkillAtCommit reconstructs one existing entry from its own locked
// commit. It is used only when an unchanged pin deliberately stays behind the
// operation's current source target.
func desiredSkillAtCommit(ctx context.Context, source treeReader, blockIndex int, block config.SkillImport, entry skilllock.Entry, commit string) (desiredSkill, error) {
	selector, selected := selectingPositiveSelector(block, entry.SelectedPath)
	if !selected {
		return desiredSkill{}, fmt.Errorf("selected path %s is no longer selected", entry.SelectedPath)
	}
	tree, err := source.ReadTree(ctx, commit, entry.SelectedPath)
	if err != nil {
		return desiredSkill{}, fmt.Errorf("locked source commit %s could not be read for %s: %w", shortCommit(commit), entry.SelectedPath, err)
	}
	info, err := skilltree.ValidateSkill(tree, entry.SelectedPath)
	if err != nil {
		return desiredSkill{}, fmt.Errorf("locked source commit %s no longer provides a valid merge base for %s: %w", shortCommit(commit), entry.SelectedPath, err)
	}
	return desiredSkill{BlockIndex: blockIndex, Block: block, Selector: selector, SelectedPath: entry.SelectedPath, Name: info.Name, Tree: tree}, nil
}

// importNew imports a newly desired skill at the block's target commit.
func (s *Service) importNew(st *state, txn *transaction, blockCtx *blockContext, skill desiredSkill, report *Report) {
	s.importNewAt(st, txn, blockCtx, skill, blockCtx.TargetCommit, report)
}

// importNewAt imports a skill whose upstream tree was resolved at commit.
func (s *Service) importNewAt(st *state, txn *transaction, blockCtx *blockContext, skill desiredSkill, commit string, report *Report) {
	result := SkillResult{Name: skill.Name, Repository: config.NormalizeSkillRepository(skill.Block.Repository), SelectedPath: skill.SelectedPath}
	if dir, blocked := blockedByUserSkill(st, skill.Name); blocked {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("user-managed skill %s already owns the name %q; delete it to let this import take ownership, or narrow the selector that matches %s to keep the user-managed skill",
			relativeTo(st.paths.Root, dir), skill.Name, skill.SelectedPath)
		report.Add(result)
		return
	}
	if observed := effectiveLocal(st, txn, skill.Name); observed.Present {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("%s already exists but is not recorded for this selector; resolve the conflicting directory before importing", relativeTo(st.paths.Root, observed.Dir))
		report.Add(result)
		return
	}
	txn.WriteSkill(skill.Name, skill.Tree)
	txn.SetLockEntry(lockEntryFor(skill, blockCtx, commit, skill.Tree))
	result.Outcome = OutcomeImported
	report.Add(result)
}

// advanceExisting reconciles one already-imported skill against its new
// upstream tree.
func (s *Service) advanceExisting(ctx context.Context, runner *gitrepo.Runner, st *state, txn *transaction, blockCtx *blockContext, skill desiredSkill, entry skilllock.Entry, report *Report) {
	result := SkillResult{Name: skill.Name, Repository: entry.Repository, SelectedPath: skill.SelectedPath}
	observed := effectiveLocal(st, txn, skill.Name)

	if !observed.Present {
		// A desired skill whose local directory is gone is restored from the
		// selected source. Adoption is handled before any block runs, so a
		// same-name user-managed skill never reaches this path.
		txn.WriteSkill(skill.Name, skill.Tree)
		txn.SetLockEntry(preservePublication(lockEntryFor(skill, blockCtx, blockCtx.TargetCommit, skill.Tree), entry))
		result.Outcome = OutcomeRestored
		report.Add(result)
		return
	}
	if observed.Err != nil {
		// A locally invalid import is preserved and fails only itself.
		result.Outcome = OutcomeFailed
		result.Err = observed.Err
		report.Add(result)
		return
	}

	base, err := blockCtx.Source.ReadTree(ctx, entry.Commit, entry.SelectedPath)
	if err != nil {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("locked source commit %s could not be read, so no merge base exists: %w", shortCommit(entry.Commit), err)
		report.Add(result)
		return
	}
	if base.Hash() != entry.TreeHash {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("locked upstream state for %s does not match commit %s; local content is preserved", skill.Name, shortCommit(entry.Commit))
		report.Add(result)
		return
	}

	// Every field the lock records for this skill is compared, not just the
	// content: a configuration change that only moves the tracking mode or the
	// resolved ref kind must still be written through, or status, push
	// freshness checks, and lock advancement would keep applying the superseded
	// policy indefinitely.
	nextEntry := preservePublication(lockEntryFor(skill, blockCtx, blockCtx.TargetCommit, skill.Tree), entry)
	if skill.Tree.Equal(base) && entry.Equal(nextEntry) {
		result.Outcome = OutcomeUnchanged
		report.Add(result)
		return
	}

	merged, conflicts, err := skilltree.Merge(base, observed.Tree, skill.Tree, runner.TextMerger(ctx))
	if err != nil {
		result.Outcome = OutcomeFailed
		result.Err = err
		report.Add(result)
		return
	}
	if len(conflicts) > 0 {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("upstream and local changes conflict in %s", describeConflicts(conflicts))
		report.Add(result)
		return
	}
	if _, err := skilltree.ValidateSkill(merged, skill.SelectedPath); err != nil {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("merged result is not a valid skill: %w", err)
		report.Add(result)
		return
	}

	txn.WriteSkill(skill.Name, merged)
	// The lock advances to the new upstream commit and hash even when the
	// merged tree still carries local modifications.
	txn.SetLockEntry(nextEntry)
	if merged.Equal(observed.Tree) && entry.Commit == blockCtx.TargetCommit {
		result.Outcome = OutcomeUnchanged
	} else {
		result.Outcome = OutcomeUpdated
		if !merged.Equal(skill.Tree) {
			result.Detail = "local modifications retained"
		}
	}
	report.Add(result)
}

// adoptMissingImports prunes the lock entry of an imported skill that was moved
// into the user-managed tier. The same-name user-managed skill takes precedence
// over restoration so adoption never recreates the collision.
func adoptMissingImports(st *state, txn *transaction, report *Report) {
	for _, entry := range st.lock.Skills {
		if effectiveLocal(st, txn, entry.Name).Present {
			continue
		}
		dir, adopted := blockedByUserSkill(st, entry.Name)
		if !adopted {
			continue
		}
		txn.RemoveLockEntry(entry.Name)
		report.Add(SkillResult{
			Name:         entry.Name,
			Repository:   entry.Repository,
			SelectedPath: entry.SelectedPath,
			Outcome:      OutcomePruned,
			Detail:       "adopted as user-managed at " + relativeTo(st.paths.Root, dir),
		})
	}
}

// retireUnconfigured applies the retirement rule to locked skills whose
// repository and selector pair is no longer configured at all.
func retireUnconfigured(st *state, txn *transaction, report *Report) {
	// Iterate a snapshot: retiring an entry mutates the transaction's lock.
	pending := append([]skilllock.Entry{}, txn.lock.Skills...)
	reported := reportedSkillKeys(report)
	for _, entry := range pending {
		if _, stillReported := reported[skillKey(entry.Repository, entry.SelectedPath)]; stillReported {
			continue
		}
		// Configuration has no block ID. One current block that still selects the
		// recorded path keeps the independent entry owned when a source failure
		// prevented its selector evidence from being refreshed. Multiple matches
		// are an ownership error: blocks may use different refs, so neither an
		// update nor retirement can choose one block's observation as authoritative.
		selectionCount := st.configuredSelectionCount(entry)
		if selectionCount > 1 {
			report.Add(SkillResult{
				Name:         entry.Name,
				Repository:   entry.Repository,
				SelectedPath: entry.SelectedPath,
				Outcome:      OutcomeFailed,
				Err:          fmt.Errorf("selected by multiple configured blocks; make selector ownership unambiguous before pulling"),
			})
			continue
		}
		if selectionCount == 1 {
			continue
		}
		retire(st, txn, entry, report)
	}
}

// reportedSkillKeys returns the skills already accounted for in a report.
//
// Skills are keyed by repository and selected path rather than by name: two
// blocks can resolve distinct paths to the same name, and a name-keyed skip
// would let one block's failure suppress retirement of an entirely different
// block's unconfigured entry.
func reportedSkillKeys(report *Report) map[string]struct{} {
	keys := make(map[string]struct{}, len(report.Skills))
	for _, skill := range report.Skills {
		keys[skillKey(skill.Repository, skill.SelectedPath)] = struct{}{}
	}
	return keys
}

// skillKey renders the repository and path pair that identifies one managed
// skill independently of the name it declares.
//
// Both components are normalized, so a key never depends on how a repository or
// selector happened to be spelled. path is either a selected path or the
// selector that produced it; the two are normalized the same way, and callers
// keep them in separate maps.
func skillKey(repository string, path string) string {
	return config.NormalizeSkillRepository(repository) + "\x00" + config.NormalizeSkillSelector(path)
}

// txnEntriesForBlock returns a block's lock entries from the pending
// transaction so a later stage sees membership reconciliation results.
func txnEntriesForBlock(st *state, txn *transaction, block config.SkillImport) []skilllock.Entry {
	var entries []skilllock.Entry
	for _, entry := range txn.lock.Skills {
		if entryBelongsToBlock(st.cfg, block, entry) {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// failOnOrphans refuses to operate while `.agent-layer/skills-imported/`
// contains a directory Agent Layer does not own.
func failOnOrphans(st *state) error {
	orphans := st.orphanDirectories()
	if len(orphans) == 0 {
		return nil
	}
	paths := make([]string, 0, len(orphans))
	for _, name := range orphans {
		paths = append(paths, relativeTo(st.paths.Root, st.skill(name).Dir))
	}
	return fmt.Errorf("imported skill directories have no entry in %s: %s; move each one into %s to adopt it as user-managed, or delete it",
		relativeTo(st.paths.Root, st.paths.SkillsLockPath), strings.Join(paths, ", "), relativeTo(st.paths.Root, st.paths.SkillsDir))
}

// describeConflicts renders a deterministic conflict list.
func describeConflicts(conflicts []skilltree.Conflict) string {
	rendered := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		rendered = append(rendered, conflict.Error())
	}
	return strings.Join(rendered, ", ")
}

// pathSetFor extracts the paths a transaction writes.
func pathSetFor(st *state) pathSet {
	return pathSet{
		ConfigPath:        st.paths.ConfigPath,
		SkillsLockPath:    st.paths.SkillsLockPath,
		ImportedSkillsDir: st.paths.ImportedSkillsDir,
	}
}
