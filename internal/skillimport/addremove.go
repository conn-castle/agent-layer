package skillimport

import (
	"context"
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	selectorEditAdd    = "add"
	selectorEditRemove = "remove"
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
	Order          *int
	AdoptLegacy    bool
}

// importBlock carries the shared policy for either source kind.
func (o AddOptions) importBlock(exactFile bool) config.SkillImport {
	return config.SkillImport{
		Repository:     o.Repository,
		Selectors:      o.Selectors,
		ExactFile:      exactFile,
		Ref:            o.Ref,
		Tracking:       o.Tracking,
		WritePolicy:    o.WritePolicy,
		PushRepository: o.PushRepository,
		PushBranch:     o.PushBranch,
	}
}

// Add validates explicit selectors, creates or extends the one block with a
// matching policy, imports every newly desired skill, and projects the result.
//
// The entire old-to-new desired-set transition is validated before any local
// state changes; configuration, imported skills, and lock state then commit
// together. A projection failure afterwards is reported without discarding that
// valid source state.
func (s *Service) Add(ctx context.Context, opts AddOptions) (*Report, error) {
	return s.withLockedReport(func(st *state, report *Report) error {
		return s.addLocked(ctx, st, opts, report, nil)
	})
}

func (s *Service) addLocked(ctx context.Context, st *state, opts AddOptions, report *Report, legacy map[string]skilltree.Tree) error {
	if len(opts.Selectors) == 0 {
		return fmt.Errorf("at least one selector is required")
	}
	if st.instructions && (len(opts.Selectors) != 1 || opts.Order == nil) {
		return fmt.Errorf("instructions add requires one exact Markdown selector and --order")
	}
	for _, selector := range opts.Selectors {
		var err error
		if st.instructions {
			err = config.ValidateInstructionSelector(selector)
		} else {
			err = config.ValidateSkillSelectorPath(config.SkillExclusionPath(selector))
		}
		if err != nil {
			return fmt.Errorf("invalid selector %q: %w", selector, err)
		}
	}

	requested := opts.importBlock(st.instructions)
	identity := requested.Identity()
	existing, existingIndex, hasExisting := findBlockByIdentity(st.cfg, identity)
	var nextConfig string
	var err error
	if st.instructions {
		legacy = map[string]skilltree.Tree{}
		nextConfig, err = instructionAddConfig(st, opts, requested, legacy)
	} else {
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
		nextConfig, err = config.SetSkillImportSelectors(st.configRaw, identity, selectors)
	}
	if err != nil {
		return err
	}
	proposed, err := config.ParseConfig([]byte(nextConfig), st.paths.ConfigPath)
	if err != nil {
		return err
	}
	if st.instructions {
		instructionBlocks(proposed)
	}
	block, blockIndex, ok := findBlockByIdentity(proposed, identity)
	if !ok {
		return fmt.Errorf("the updated configuration does not contain the expected import block")
	}
	lockedEntries := []skilllock.Entry{}
	if hasExisting {
		blockIndex = existingIndex
		lockedEntries = st.entriesForBlock(existing)
	}

	txn := s.newTransaction(pathSetFor(st), st.lock)
	txn.SetConfig(nextConfig)
	for name, tree := range legacy {
		txn.RetireLocal(name, tree)
	}
	return s.applySelectorEdit(ctx, st, txn, selectorEdit{
		op:            selectorEditAdd,
		blockIndex:    blockIndex,
		block:         block,
		lockedEntries: lockedEntries,
		requireSkills: true,
	}, report)
}

// Remove drops one configured positive or exclusion selector, keeps each
// existing entry on its own lock evidence, imports newly revealed membership
// at the current resolved target, and projects the result.
func (s *Service) Remove(ctx context.Context, repository string, selector string) (*Report, error) {
	return s.withLockedReport(func(st *state, report *Report) error {
		return s.removeLocked(ctx, st, repository, selector, report)
	})
}

func (s *Service) removeLocked(ctx context.Context, st *state, repository string, selector string, report *Report) error {
	if err := config.ValidateSkillSelectorPath(config.SkillExclusionPath(selector)); err != nil {
		return fmt.Errorf("invalid selector %q: %w", selector, err)
	}
	block, blockIndex, ok := st.blockForSelector(repository, selector)
	if !ok {
		return fmt.Errorf("no configured %s.imports block declares selector %q for %s", importKind(st.instructions), selector, config.NormalizeSkillRepository(repository))
	}

	return s.removeSelectorsLocked(ctx, st, block, blockIndex, []string{selector}, report)
}

// RemoveCatalogSelectors rechecks whole-block eligibility for wizard removals.
// The complete retirement set commits together; absent targets are no-ops.
func (s *Service) RemoveCatalogSelectors(ctx context.Context, selectors []string) (*Report, error) {
	identity := (config.SkillImport{Repository: templates.GeneralSkillsRepository}).Identity()
	return s.withLockedReport(func(st *state, report *Report) error {
		if err := requireCatalogPolicy(st); err != nil {
			return err
		}
		block, index, ok := findBlockByIdentity(st.cfg, identity)
		if !ok {
			return nil
		}
		return s.removeSelectorsLocked(ctx, st, block, index, selectors, report)
	})
}

func (s *Service) removeSelectorsLocked(ctx context.Context, st *state, block config.SkillImport, blockIndex int, selectors []string, report *Report) error {
	targets := map[string]bool{}
	for _, selector := range selectors {
		if err := config.ValidateSkillSelectorPath(config.SkillExclusionPath(selector)); err != nil {
			return err
		}
		targets[config.NormalizeSkillSelector(selector)] = true
	}
	remaining := make([]string, 0, len(block.Selectors))
	for _, candidate := range block.Selectors {
		if !targets[config.NormalizeSkillSelector(candidate)] {
			remaining = append(remaining, config.NormalizeSkillSelector(candidate))
		}
	}
	if len(remaining) == len(block.Selectors) {
		return nil
	}
	if !hasPositiveSelector(remaining) {
		// A block with no positive selectors is removed entirely; every skill it
		// owns leaves the desired set.
		remaining = nil
	}

	var nextConfig string
	var err error
	if st.instructions {
		nextConfig, err = config.SetInstructionImport(st.configRaw, block, nil, true)
	} else {
		nextConfig, err = config.SetSkillImportSelectors(st.configRaw, block.Identity(), remaining)
	}
	if err != nil {
		return err
	}
	proposed, err := config.ParseConfig([]byte(nextConfig), st.paths.ConfigPath)
	if err != nil {
		return err
	}

	txn := s.newTransaction(pathSetFor(st), st.lock)
	txn.SetConfig(nextConfig)
	lockedEntries := st.entriesForBlock(block)

	if len(remaining) == 0 {
		for _, entry := range lockedEntries {
			retire(st, txn, entry, report)
		}
		return s.commitSelectorEdit(txn, report)
	}

	nextBlock, _, ok := findBlockByIdentity(proposed, block.Identity())
	if !ok {
		return fmt.Errorf("the updated configuration does not contain the expected skills.imports block")
	}
	return s.applySelectorEdit(ctx, st, txn, selectorEdit{
		op:            selectorEditRemove,
		blockIndex:    blockIndex,
		block:         nextBlock,
		lockedEntries: lockedEntries,
	}, report)
}

// selectorEdit describes one block's add or remove selector change.
type selectorEdit struct {
	// op names the operation in its temporary Git working directory.
	op         string
	blockIndex int
	// block is the block as the edited configuration declares it.
	block config.SkillImport
	// lockedEntries are the block's lock entries before the edit.
	lockedEntries []skilllock.Entry
	// requireSkills fails an edit whose block would select no valid skill.
	requireSkills bool
}

// applySelectorEdit resolves an edited block at its current target, keeps or
// retires each existing entry on its own lock evidence, imports newly selected
// membership, and commits configuration, imported skills, and lock state
// together.
//
// The entire old-to-new desired-set change is validated and preflighted before
// any local state changes, so one unusable newly selected match fails the
// command rather than applying part of it.
func (s *Service) applySelectorEdit(ctx context.Context, st *state, txn *transaction, edit selectorEdit, report *Report) error {
	runner, workRoot, cleanup, err := s.gitWorkspace(st, edit.op)
	if err != nil {
		return err
	}
	defer cleanup()

	block := edit.block
	blockCtx, err := s.openBlock(ctx, runner, workRoot, edit.blockIndex, block)
	if err != nil {
		return err
	}
	commit := blockCtx.Resolution.Commit
	desired, failures, err := resolveBlock(ctx, blockCtx.Source, edit.blockIndex, block, commit)
	if err != nil {
		return err
	}
	lockedByPath := make(map[string]skilllock.Entry, len(edit.lockedEntries))
	for _, entry := range edit.lockedEntries {
		lockedByPath[entry.SelectedPath] = entry
	}
	newFailures := failuresForNewPaths(failures, lockedByPath, block)
	if len(newFailures) > 0 {
		return fmt.Errorf("no local state was changed: %w", candidateFailureError(newFailures))
	}
	prospective := prospectiveSelectorEdit(edit.blockIndex, block, desired, edit.lockedEntries)
	if err := validateDesiredSet(combineWithOtherEntries(st, block, edit.lockedEntries, st.lock.Skills, prospective)); err != nil {
		return err
	}
	if edit.requireSkills && len(prospective) == 0 {
		return fmt.Errorf("the requested selectors resolve to no valid skills at %s; no local state was changed", shortCommit(commit))
	}

	for _, entry := range edit.lockedEntries {
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
		// A new path was introduced by an added selector or revealed by a
		// removed exclusion; it imports at the operation's current target.
		s.importNewAt(st, txn, blockCtx, skill, commit, report)
	}
	return s.commitSelectorEdit(txn, report)
}

// commitSelectorEdit commits a preflighted add or remove and projects the
// result, or aborts without writing when any skill failed.
func (s *Service) commitSelectorEdit(txn *transaction, report *Report) error {
	if report.Failed() {
		return abortUnapplied(report)
	}
	if err := txn.Commit(); err != nil {
		report.discardUnapplied()
		return err
	}
	s.project(report)
	return nil
}

// abortUnapplied ends an add or remove whose preflight found skill failures.
// Nothing was written, so the report keeps only the failures, and the error
// stays short because the caller prints that report.
func abortUnapplied(report *Report) error {
	report.discardUnapplied()
	return fmt.Errorf("no local state was changed because %d skill(s) failed", len(report.Skills))
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
