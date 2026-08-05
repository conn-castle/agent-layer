package skillimport

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
)

// AddOptions carries the policy an `al skills add` invocation declares.
type AddOptions struct {
	Repository     string
	Selectors      []string
	Ref            string
	Tracking       string
	WritePolicy    string
	PushRepository string
	PushBranch     string
}

// identity renders the block identity an add targets.
func (o AddOptions) identity() config.SkillImportBlockIdentity {
	return config.SkillImport{
		Repository:     o.Repository,
		Ref:            o.Ref,
		Tracking:       o.Tracking,
		WritePolicy:    o.WritePolicy,
		PushRepository: o.PushRepository,
		PushBranch:     o.PushBranch,
	}.Identity()
}

// Add validates explicit selectors, creates or extends the one block with a
// matching policy, imports every newly desired skill, and projects the result.
//
// The entire old-to-new desired-set transition is validated before any local
// state changes; configuration, imported skills, and lock state then commit
// together. A projection failure afterwards is reported without discarding that
// valid source state.
func (s *Service) Add(ctx context.Context, opts AddOptions) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		return s.addLocked(ctx, st, opts, report)
	})
	report.Sort()
	return report, err
}

func (s *Service) addLocked(ctx context.Context, st *state, opts AddOptions, report *Report) error {
	if err := failOnOrphans(st); err != nil {
		return err
	}
	if len(opts.Selectors) == 0 {
		return fmt.Errorf("at least one selector is required")
	}
	for _, selector := range opts.Selectors {
		if err := config.ValidateSkillSelectorPath(config.SkillExclusionPath(selector)); err != nil {
			return fmt.Errorf("invalid selector %q: %w", selector, err)
		}
	}

	identity := opts.identity()
	existing, existingIndex, hasExisting := findBlockByIdentity(st.cfg, identity)

	if !hasExisting && !hasPositiveSelector(opts.Selectors) {
		return fmt.Errorf("an exclusion-only addition must extend an existing block that already has a positive selector; no block matches this repository and policy")
	}

	selectors := make([]string, 0, len(opts.Selectors))
	if hasExisting {
		selectors = append(selectors, existing.Selectors...)
	}
	for _, selector := range opts.Selectors {
		normalized := config.NormalizeSkillSelector(selector)
		if containsSelector(selectors, normalized) {
			return fmt.Errorf("selector %q is already configured for %s", selector, identity.Repository)
		}
		selectors = append(selectors, normalized)
	}

	nextConfig, err := config.SetSkillImportSelectors(st.configRaw, identity, selectors)
	if err != nil {
		return err
	}
	proposed, err := config.ParseConfig([]byte(nextConfig), st.paths.ConfigPath)
	if err != nil {
		return err
	}
	block, blockIndex, ok := findBlockByIdentity(proposed, identity)
	if !ok {
		return fmt.Errorf("the updated configuration does not contain the expected skills.imports block")
	}
	if hasExisting {
		blockIndex = existingIndex
	}

	txn := newTransaction(pathSetFor(st), st.lock)
	txn.SetConfig(nextConfig)

	runner, err := s.newRunner(st.env)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-add-")
	if err != nil {
		return fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workRoot) }()

	blockCtx, err := s.openBlock(ctx, runner, workRoot, blockIndex, block)
	if err != nil {
		return err
	}
	commit := blockCtx.Resolution.Commit
	desired, failures, err := resolveBlock(ctx, blockCtx.Source, blockIndex, block, commit)
	if err != nil {
		return err
	}
	lockedEntries := []skilllock.Entry{}
	if hasExisting {
		lockedEntries = st.entriesForBlock(existing)
	}
	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	newFailures := failuresForNewPaths(failures, lockedByPath, block)
	if len(newFailures) > 0 {
		// `al skills add` validates and preflights the entire old-to-new
		// desired-set change before any local state changes, so one unusable
		// newly selected match fails the command rather than importing part of it.
		return fmt.Errorf("no local state was changed: %w", candidateFailureError(newFailures))
	}
	prospective := prospectiveSelectorEdit(blockIndex, block, desired, lockedEntries)
	if err := validateDesiredSet(combineWithOtherEntries(st, block, lockedEntries, st.lock.Skills, prospective)); err != nil {
		return err
	}
	if len(prospective) == 0 {
		return fmt.Errorf("the requested selectors resolve to no valid skills at %s; no local state was changed", shortCommit(commit))
	}

	for _, entry := range lockedEntries {
		selector, still := selectingPositiveSelector(block, entry.SelectedPath)
		if !still {
			retire(st, txn, entry, report)
			continue
		}
		updated := entry
		updated.Selector = selector
		txn.SetLockEntry(updated)
		report.Add(SkillResult{Name: entry.Name, Repository: entry.Repository, SelectedPath: entry.SelectedPath, Outcome: OutcomeUnchanged})
	}
	for _, skill := range desired {
		if _, locked := lockedByPath[skill.SelectedPath]; locked {
			continue
		}
		s.importNewAt(st, txn, blockCtx, skill, commit, report)
	}

	if report.Failed() {
		return fmt.Errorf("no local state was changed: %s", strings.TrimSpace(report.Render("al skills add")))
	}
	if err := txn.Commit(); err != nil {
		return err
	}
	s.project(report)
	return nil
}

// Remove drops one configured positive or exclusion selector, keeps each
// existing entry on its own lock evidence, imports newly revealed membership
// at the current resolved target, and projects the result.
func (s *Service) Remove(ctx context.Context, repository string, selector string) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		return s.removeLocked(ctx, st, repository, selector, report)
	})
	report.Sort()
	return report, err
}

func (s *Service) removeLocked(ctx context.Context, st *state, repository string, selector string, report *Report) error {
	if err := failOnOrphans(st); err != nil {
		return err
	}
	if err := config.ValidateSkillSelectorPath(config.SkillExclusionPath(selector)); err != nil {
		return fmt.Errorf("invalid selector %q: %w", selector, err)
	}
	block, blockIndex, ok := st.blockForSelector(repository, selector)
	if !ok {
		return fmt.Errorf("no configured skills.imports block declares selector %q for %s", selector, config.NormalizeSkillRepository(repository))
	}

	normalizedTarget := config.NormalizeSkillSelector(selector)
	remaining := make([]string, 0, len(block.Selectors))
	for _, candidate := range block.Selectors {
		normalized := config.NormalizeSkillSelector(candidate)
		if normalized == normalizedTarget {
			continue
		}
		remaining = append(remaining, normalized)
	}
	if !hasPositiveSelector(remaining) {
		// A block with no positive selectors is removed entirely; every skill it
		// owns leaves the desired set.
		remaining = nil
	}

	nextConfig, err := config.SetSkillImportSelectors(st.configRaw, block.Identity(), remaining)
	if err != nil {
		return err
	}
	proposed, err := config.ParseConfig([]byte(nextConfig), st.paths.ConfigPath)
	if err != nil {
		return err
	}

	txn := newTransaction(pathSetFor(st), st.lock)
	txn.SetConfig(nextConfig)
	lockedEntries := st.entriesForBlock(block)

	if len(remaining) == 0 {
		for _, entry := range lockedEntries {
			retire(st, txn, entry, report)
		}
		if report.Failed() {
			return fmt.Errorf("no local state was changed: %s", strings.TrimSpace(report.Render("al skills remove")))
		}
		if err := txn.Commit(); err != nil {
			return err
		}
		s.project(report)
		return nil
	}

	nextBlock, _, ok := findBlockByIdentity(proposed, block.Identity())
	if !ok {
		return fmt.Errorf("the updated configuration does not contain the expected skills.imports block")
	}

	runner, err := s.newRunner(st.env)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-remove-")
	if err != nil {
		return fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workRoot) }()

	blockCtx, err := s.openBlock(ctx, runner, workRoot, blockIndex, nextBlock)
	if err != nil {
		return err
	}
	commit := blockCtx.Resolution.Commit
	desired, failures, err := resolveBlock(ctx, blockCtx.Source, blockIndex, nextBlock, commit)
	if err != nil {
		return err
	}
	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	newFailures := failuresForNewPaths(failures, lockedByPath, nextBlock)
	if len(newFailures) > 0 {
		// Like add, remove leaves prior state unchanged when either step fails.
		return fmt.Errorf("no local state was changed: %w", candidateFailureError(newFailures))
	}
	prospective := prospectiveSelectorEdit(blockIndex, nextBlock, desired, lockedEntries)
	if err := validateDesiredSet(combineWithOtherEntries(st, nextBlock, lockedEntries, st.lock.Skills, prospective)); err != nil {
		return err
	}

	for _, entry := range lockedEntries {
		selectedBy, still := selectingPositiveSelector(nextBlock, entry.SelectedPath)
		if !still {
			retire(st, txn, entry, report)
			continue
		}
		updated := entry
		updated.Selector = selectedBy
		txn.SetLockEntry(updated)
		report.Add(SkillResult{Name: entry.Name, Repository: entry.Repository, SelectedPath: entry.SelectedPath, Outcome: OutcomeUnchanged})
	}
	for _, skill := range desired {
		if _, locked := lockedByPath[skill.SelectedPath]; locked {
			continue
		}
		// Removing an exclusion reveals a skill at the operation's current target.
		s.importNewAt(st, txn, blockCtx, skill, commit, report)
	}

	if report.Failed() {
		return fmt.Errorf("no local state was changed: %s", strings.TrimSpace(report.Render("al skills remove")))
	}
	if err := txn.Commit(); err != nil {
		return err
	}
	s.project(report)
	return nil
}

// findBlockByIdentity returns the configured block with a policy identity.
func findBlockByIdentity(cfg *config.Config, identity config.SkillImportBlockIdentity) (config.SkillImport, int, bool) {
	for i, block := range cfg.Skills.Imports {
		if block.Identity() == identity {
			return block, i, true
		}
	}
	return config.SkillImport{}, 0, false
}

func hasPositiveSelector(selectors []string) bool {
	for _, selector := range selectors {
		if !config.IsSkillExclusionSelector(selector) {
			return true
		}
	}
	return false
}

func containsSelector(selectors []string, normalized string) bool {
	for _, selector := range selectors {
		if config.NormalizeSkillSelector(selector) == normalized {
			return true
		}
	}
	return false
}

// failuresForNewPaths removes failures for existing entries that a selector
// edit leaves selected. Add/remove never advances those entries, so their own
// locked commits remain authoritative and the current source version is only
// relevant to newly introduced or newly revealed membership.
func failuresForNewPaths(failures []candidateFailure, lockedByPath map[string]skilllock.Entry, block config.SkillImport) []candidateFailure {
	filtered := make([]candidateFailure, 0, len(failures))
	for _, failure := range failures {
		if _, locked := lockedByPath[failure.Path]; locked {
			if _, selected := selectingPositiveSelector(block, failure.Path); selected {
				continue
			}
		}
		filtered = append(filtered, failure)
	}
	return filtered
}

// prospectiveSelectorEdit combines newly resolved membership with every
// existing independently locked entry the edited selectors still select. The
// existing entry's recorded name wins because selector edits do not advance or
// revalidate its upstream generation.
func prospectiveSelectorEdit(blockIndex int, block config.SkillImport, desired []desiredSkill, lockedEntries []skilllock.Entry) []desiredSkill {
	lockedByPath := make(map[string]skilllock.Entry, len(lockedEntries))
	for _, entry := range lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	prospective := make([]desiredSkill, 0, len(desired)+len(lockedEntries))
	for _, skill := range desired {
		if _, locked := lockedByPath[skill.SelectedPath]; !locked {
			prospective = append(prospective, skill)
		}
	}
	for _, entry := range lockedEntries {
		selector, selected := selectingPositiveSelector(block, entry.SelectedPath)
		if !selected {
			continue
		}
		prospective = append(prospective, desiredSkill{
			BlockIndex:   blockIndex,
			Block:        block,
			Selector:     selector,
			SelectedPath: entry.SelectedPath,
			Name:         entry.Name,
		})
	}
	return prospective
}

// withOtherPendingEntries is withOtherBlockEntries against the state an
// in-flight operation has built so far, so a multi-block pull validates each
// block against what the earlier blocks already staged rather than against the
// snapshot it started from.
func withOtherPendingEntries(st *state, txn *transaction, block config.SkillImport, desired []desiredSkill) []desiredSkill {
	return combineWithOtherEntries(st, block, txnEntriesForBlock(st, txn, block), txn.lock.Skills, desired)
}

// combineWithOtherEntries appends every recorded entry outside the block under
// change to its freshly resolved desired set.
func combineWithOtherEntries(st *state, block config.SkillImport, blockEntries []skilllock.Entry, allEntries []skilllock.Entry, desired []desiredSkill) []desiredSkill {
	changed := make(map[string]struct{}, len(blockEntries))
	for _, entry := range blockEntries {
		changed[entry.Name] = struct{}{}
	}
	combined := append([]desiredSkill{}, desired...)
	for _, entry := range allEntries {
		if _, sameBlock := changed[entry.Name]; sameBlock {
			continue
		}
		if entry.Repository == config.NormalizeSkillRepository(block.Repository) &&
			containsSelector(block.Selectors, config.NormalizeSkillSelector(entry.Selector)) {
			continue
		}
		combined = append(combined, desiredSkill{
			Block:        blockForEntry(st, entry),
			Selector:     entry.Selector,
			SelectedPath: entry.SelectedPath,
			Name:         entry.Name,
		})
	}
	return combined
}

// blockForEntry returns the configured block that owns a lock entry, or a
// minimal stand-in carrying the recorded repository when configuration no
// longer declares it.
func blockForEntry(st *state, entry skilllock.Entry) config.SkillImport {
	if block, _, ok := st.configuredBlockForEntry(entry); ok {
		return block
	}
	return config.SkillImport{Repository: entry.Repository}
}
