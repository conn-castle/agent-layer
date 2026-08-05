package skillimport

import (
	"context"
	"fmt"
	"os"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Reset permanently discards one imported skill's local edits and replaces it
// with the current configured upstream tree. It does not reconcile any other
// selector membership.
func (s *Service) Reset(ctx context.Context, name string) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		return s.resetLocked(ctx, st, name, report)
	})
	report.Sort()
	return report, err
}

func (s *Service) resetLocked(ctx context.Context, st *state, name string, report *Report) error {
	if err := failOnOrphans(st); err != nil {
		return err
	}
	entry, ok := st.lock.Entry(name)
	if !ok {
		return fmt.Errorf("imported skill %q has no lock entry; pass the exact name shown by 'al skills status --all'", name)
	}
	block, blockIndex, configured := st.configuredBlockForEntry(entry)
	if !configured {
		if st.configuredSelectionCount(entry) > 1 {
			return fmt.Errorf("imported skill %q at %s is selected by multiple configured blocks; make selector ownership unambiguous before resetting", name, entry.SelectedPath)
		}
		return fmt.Errorf("imported skill %q is no longer selected by configuration; run 'al skills pull' to apply retirement rules before resetting", name)
	}
	selector, selected := selectingPositiveSelector(block, entry.SelectedPath)
	if !selected {
		return fmt.Errorf("imported skill %q at %s is no longer selected by configuration; run 'al skills pull' before resetting", name, entry.SelectedPath)
	}
	if dir, collided := blockedByUserSkill(st, entry.Name); collided {
		return fmt.Errorf("user-managed skill %s already owns the name %q; remove the collision before resetting", relativeTo(st.paths.Root, dir), entry.Name)
	}

	runner, err := s.newRunner(st.env)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-reset-")
	if err != nil {
		return fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workRoot) }()

	blockCtx, err := s.openBlock(ctx, runner, workRoot, blockIndex, block)
	if err != nil {
		return err
	}
	tree, err := blockCtx.Source.ReadTree(ctx, blockCtx.Resolution.Commit, entry.SelectedPath)
	if err != nil {
		return fmt.Errorf("current upstream path %s could not be read; local content was preserved: %w", entry.SelectedPath, err)
	}
	info, err := skilltree.ValidateSkill(tree, entry.SelectedPath)
	if err != nil {
		return fmt.Errorf("current upstream path %s is not a valid skill; local content was preserved: %w", entry.SelectedPath, err)
	}
	if info.Name != entry.Name {
		return fmt.Errorf("current upstream path %s declares name %q instead of locked name %q; local content was preserved", entry.SelectedPath, info.Name, entry.Name)
	}

	skill := desiredSkill{
		BlockIndex:   blockIndex,
		Block:        block,
		Selector:     selector,
		SelectedPath: entry.SelectedPath,
		Name:         entry.Name,
		Tree:         tree,
	}
	txn := newTransaction(pathSetFor(st), st.lock)
	txn.WriteSkill(entry.Name, tree)
	txn.SetLockEntry(lockEntryFor(skill, blockCtx, blockCtx.Resolution.Commit, tree))
	if err := txn.Commit(); err != nil {
		return err
	}
	report.Add(SkillResult{
		Name:         entry.Name,
		Repository:   config.NormalizeSkillRepository(block.Repository),
		SelectedPath: entry.SelectedPath,
		Outcome:      OutcomeReset,
		Detail:       "local edits permanently discarded; accepted current upstream",
	})
	s.project(report)
	return nil
}
