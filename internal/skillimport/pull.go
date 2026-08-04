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
		reportBlockedBlockSkills(report, txn, block, err)
	}

	blockCtx, err := s.openBlock(ctx, runner, workRoot, st, index, block)
	if err != nil {
		blocked(err)
		return
	}

	// Lock entries are read from the pending transaction, not the loaded
	// snapshot, so earlier stages of this pull (adoption pruning, membership
	// reconciliation) are visible here.
	lockedEntries := txnEntriesForBlock(txn, block)

	// Manual selector membership is reconciled at the block's locked commit
	// before any advance, so an added or excluded selector takes effect without
	// advancing a tracked branch as a side effect. When the block is not moving
	// (a pinned import, or a tracked branch that has not advanced) the main
	// reconciliation below already runs at the locked commit and performs the
	// same membership work, so the extra pass would be redundant.
	if blockCtx.LockedCommit != "" && !blockCtx.Retarget && blockCtx.TargetCommit != blockCtx.LockedCommit {
		if err := s.reconcileMembership(ctx, st, txn, blockCtx, lockedEntries, report); err != nil {
			blocked(err)
			return
		}
		lockedEntries = txnEntriesForBlock(txn, block)
	}

	desired, failures, err := resolveBlock(ctx, blockCtx.Source, index, block, blockCtx.TargetCommit)
	if err != nil {
		blocked(err)
		return
	}
	// Selector overlap between blocks is remote-dependent, so configuration
	// validation cannot see it. Validating against the other blocks' pending
	// entries — as add and remove already do — keeps one block from staging an
	// import that collides with another block's skill name or selected path.
	if err := validateDesiredSet(withOtherPendingEntries(st, txn, block, desired)); err != nil {
		blocked(err)
		return
	}
	// A skill that cannot be read or validated upstream fails on its own. Its
	// local directory and lock entry are preserved: invalidity is never treated
	// as upstream removal.
	failedPaths := reportCandidateFailures(report, repository, failures)

	desiredByPath := make(map[string]desiredSkill, len(desired))
	for _, skill := range desired {
		desiredByPath[skill.SelectedPath] = skill
	}
	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}

	// Paths that disappeared upstream, or that a new exclusion removed, retire
	// under the one shared rule.
	for _, entry := range lockedEntries {
		if _, invalid := failedPaths[entry.SelectedPath]; invalid {
			continue
		}
		if _, still := desiredByPath[entry.SelectedPath]; !still {
			retire(st, txn, entry, report)
		}
	}

	for _, skill := range desired {
		entry, locked := lockedByPath[skill.SelectedPath]
		if !locked {
			s.importNew(st, txn, blockCtx, skill, report)
			continue
		}
		s.advanceExisting(ctx, runner, st, txn, blockCtx, skill, entry, report)
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
func reportBlockedBlockSkills(report *Report, txn *transaction, block config.SkillImport, err error) {
	entries := txnEntriesForBlock(txn, block)
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

// reconcileMembership imports newly desired paths and retires newly excluded or
// unmatched ones at the block's locked commit, without advancing it.
func (s *Service) reconcileMembership(ctx context.Context, st *state, txn *transaction, blockCtx *blockContext, lockedEntries []skilllock.Entry, report *Report) error {
	desired, failures, err := resolveBlock(ctx, blockCtx.Source, blockCtx.Index, blockCtx.Block, blockCtx.LockedCommit)
	if err != nil {
		return err
	}
	if err := validateDesiredSet(withOtherPendingEntries(st, txn, blockCtx.Block, desired)); err != nil {
		return err
	}
	failedPaths := reportCandidateFailures(report, config.NormalizeSkillRepository(blockCtx.Block.Repository), failures)

	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	desiredByPath := make(map[string]desiredSkill, len(desired))
	for _, skill := range desired {
		desiredByPath[skill.SelectedPath] = skill
	}

	for _, entry := range lockedEntries {
		if _, invalid := failedPaths[entry.SelectedPath]; invalid {
			continue
		}
		if _, still := desiredByPath[entry.SelectedPath]; !still {
			retire(st, txn, entry, report)
		}
	}
	for _, skill := range desired {
		if _, locked := lockedByPath[skill.SelectedPath]; locked {
			continue
		}
		lockedCtx := *blockCtx
		lockedCtx.TargetCommit = blockCtx.LockedCommit
		s.importNewAt(st, txn, &lockedCtx, skill, blockCtx.LockedCommit, report)
	}
	return nil
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
		txn.SetLockEntry(lockEntryFor(skill, blockCtx, blockCtx.TargetCommit, skill.Tree))
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
	if skill.Tree.Equal(base) && entry == lockEntryFor(skill, blockCtx, blockCtx.TargetCommit, skill.Tree) {
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
	txn.SetLockEntry(lockEntryFor(skill, blockCtx, blockCtx.TargetCommit, skill.Tree))
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
	configured := make(map[string]struct{})
	for _, block := range st.cfg.Skills.Imports {
		for _, selector := range block.PositiveSelectors() {
			configured[skillKey(block.Repository, selector)] = struct{}{}
		}
	}
	// Iterate a snapshot: retiring an entry mutates the transaction's lock.
	pending := append([]skilllock.Entry{}, txn.lock.Skills...)
	reported := reportedSkillKeys(report)
	for _, entry := range pending {
		if _, stillReported := reported[skillKey(entry.Repository, entry.SelectedPath)]; stillReported {
			continue
		}
		// Both sides of this comparison go through skillKey, so retirement can
		// never turn on an incidental difference in how a repository is spelled.
		if _, ok := configured[skillKey(entry.Repository, entry.Selector)]; ok {
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
func txnEntriesForBlock(txn *transaction, block config.SkillImport) []skilllock.Entry {
	selectors := make(map[string]struct{})
	for _, selector := range block.PositiveSelectors() {
		selectors[config.NormalizeSkillSelector(selector)] = struct{}{}
	}
	repository := config.NormalizeSkillRepository(block.Repository)
	var entries []skilllock.Entry
	for _, entry := range txn.lock.Skills {
		if entry.Repository != repository {
			continue
		}
		if _, ok := selectors[config.NormalizeSkillSelector(entry.Selector)]; ok {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// failOnOrphans refuses to operate while `.agent-layer/imported-skills/`
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
