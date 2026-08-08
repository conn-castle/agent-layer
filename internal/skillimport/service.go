package skillimport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/projectlock"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/sync"
)

// Service performs skill import operations for one repository root.
type Service struct {
	root string
	sys  sync.System
	// newRunner is injected so tests can exercise reporting without git. It
	// receives the AL_-filtered `.agent-layer/.env` map every repository
	// reference resolves its `${AL_*}` placeholders from.
	newRunner func(env map[string]string) (*gitrepo.Runner, error)
}

// New returns a service bound to a repository root.
func New(root string) *Service {
	return &Service{root: root, sys: sync.RealSystem{}, newRunner: gitrepo.NewRunner}
}

// blockContext is one import block's resolved source access for an operation.
type blockContext struct {
	Index      int
	Block      config.SkillImport
	Source     *gitrepo.Source
	Resolution gitrepo.Resolution
	Tracking   string
	// TargetCommit is the commit this operation's desired set is resolved at.
	TargetCommit string
	workDir      string
}

// withLockedState runs fn inside the project lock with freshly loaded state.
//
// Any transaction an earlier process left in flight is rolled back first, so
// state is always loaded from a coherent generation of configuration, imported
// trees, and lock file.
func (s *Service) withLockedState(fn func(st *state) error) error {
	return projectlock.With(s.sys, s.root, func() error {
		if err := sync.RecoverInterruptedImport(s.root); err != nil {
			return err
		}
		st, err := loadState(s.root)
		if err != nil {
			return err
		}
		return fn(st)
	})
}

// project regenerates all outputs from the committed source state. The caller
// must already hold the project lock. A projection failure is reported without
// discarding valid source state.
func (s *Service) project(report *Report) {
	if _, err := sync.ProjectLocked(s.sys, s.root); err != nil {
		report.ProjectionErr = err
	}
}

// openBlock prepares one block's source access for a networked operation.
//
// It resolves the configured ref (or the repository's actual default branch)
// and proves the tracking mode against the resolved ref kind. Callers decide
// each existing entry's advance or retarget behavior from its own lock evidence.
func (s *Service) openBlock(ctx context.Context, runner *gitrepo.Runner, workRoot string, index int, block config.SkillImport) (*blockContext, error) {
	workDir := filepath.Join(workRoot, fmt.Sprintf("block-%d", index))
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create git working directory %s: %w", workDir, err)
	}
	// The configured reference becomes a usable URL only here, at the Git access
	// boundary. Everything recorded or reported keeps the placeholder text.
	repository, err := runner.Secrets().Resolve(config.NormalizeSkillRepository(block.Repository))
	if err != nil {
		return nil, err
	}
	source, err := gitrepo.OpenSource(ctx, runner, workDir, repository)
	if err != nil {
		return nil, err
	}
	resolution, err := source.Resolve(ctx, block.Ref)
	if err != nil {
		return nil, err
	}
	tracking, err := ensureTrackedRefKind(block, resolution)
	if err != nil {
		return nil, err
	}

	blockCtx := &blockContext{
		Index:        index,
		Block:        block,
		Source:       source,
		Resolution:   resolution,
		Tracking:     tracking,
		TargetCommit: resolution.Commit,
		workDir:      workDir,
	}

	return blockCtx, nil
}

// isRetarget reports whether the configured selection moved to a different
// version, which is reconciled rather than treated as a removal plus addition.
func isRetarget(block config.SkillImport, locked skilllock.Entry, resolution gitrepo.Resolution) bool {
	if locked.ConfiguredRef != block.Ref {
		return true
	}
	// With an omitted ref the repository's default branch is re-resolved on
	// every pull; a changed default-branch name is a retarget.
	return block.Ref == "" && locked.ResolvedRef != resolution.Ref
}

// targetCommitForEntry chooses one existing skill's target independently. A
// pinned skill stays at its own commit unless its configured selection was
// explicitly retargeted; tracked skills and retargeted pins use the operation's
// freshly resolved target.
func targetCommitForEntry(blockCtx *blockContext, entry skilllock.Entry) string {
	if !isRetarget(blockCtx.Block, entry, blockCtx.Resolution) && blockCtx.Tracking == config.SkillTrackingPinned {
		return entry.Commit
	}
	return blockCtx.Resolution.Commit
}

// lockEntryFor builds the lock entry recorded for a resolved skill.
func lockEntryFor(skill desiredSkill, blockCtx *blockContext, commit string, tree skilltree.Tree) skilllock.Entry {
	return skilllock.Entry{
		Name:          skill.Name,
		Repository:    config.NormalizeSkillRepository(skill.Block.Repository),
		Selector:      config.NormalizeSkillSelector(skill.Selector),
		SelectedPath:  skill.SelectedPath,
		ConfiguredRef: skill.Block.Ref,
		ResolvedRef:   blockCtx.Resolution.Ref,
		RefKind:       blockCtx.Resolution.Kind,
		Tracking:      blockCtx.Tracking,
		Commit:        commit,
		TreeHash:      tree.Hash(),
	}
}

// preservePublication carries destination history across source pulls and
// resets while the skill still owns the same destination path. Source state and
// publication state are independent merge bases.
func preservePublication(next skilllock.Entry, previous skilllock.Entry) skilllock.Entry {
	if next.SelectedPath == previous.SelectedPath {
		next.Publication = previous.Publication
	}
	return next
}

// retire applies the single retirement rule shared by selector removal,
// exclusion, and upstream disappearance.
//
// A clean imported directory is deleted with its lock entry. A modified one is
// preserved and fails with instructions. An already-absent directory has its
// lock entry pruned and the removal reported.
func retire(st *state, txn *transaction, entry skilllock.Entry, report *Report) {
	observed := effectiveLocal(st, txn, entry.Name)
	result := SkillResult{Name: entry.Name, Repository: entry.Repository, SelectedPath: entry.SelectedPath}
	switch {
	case !observed.Present:
		txn.RemoveLockEntry(entry.Name)
		result.Outcome = OutcomePruned
		result.Detail = "imported directory was already absent"
	case observed.Err == nil && observed.Tree.Hash() == entry.TreeHash:
		txn.DeleteSkill(entry.Name)
		txn.RemoveLockEntry(entry.Name)
		result.Outcome = OutcomeRetired
	default:
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("%s is no longer selected but has local changes; move it into %s to adopt it as user-managed, or delete it explicitly",
			relativeTo(st.paths.Root, observed.Dir), relativeTo(st.paths.Root, st.paths.SkillsDir))
	}
	report.Add(result)
}

// effectiveLocal returns a skill's local state as the current operation has
// built it so far: content this operation already staged wins over the state it
// started from, and a staged removal reads as absent.
func effectiveLocal(st *state, txn *transaction, name string) localSkill {
	observed := st.skill(name)
	if tree, staged := txn.PendingTree(name); staged {
		return localSkill{Name: name, Dir: observed.Dir, Present: true, Tree: tree}
	}
	if txn.PendingDelete(name) {
		return localSkill{Name: name, Dir: observed.Dir}
	}
	return observed
}

// relativeTo renders a path relative to the repository root for messages.
func relativeTo(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// blockedByUserSkill reports the user-managed directory that blocks importing a
// name, if any.
func blockedByUserSkill(st *state, name string) (string, bool) {
	dir, ok := st.userSkills[skilltree.NormalizeName(name)]
	return dir, ok
}
