package skillimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

var errPublicationUnavailable = errors.New("publication merge base is unavailable")

// pushCandidate is one validated skill whose local change is ready to reconcile
// against a destination.
type pushCandidate struct {
	Entry skilllock.Entry
	Block config.SkillImport
	// Base is the locked source tree, the immutable merge base for the delta.
	Base skilltree.Tree
	// Local is the current imported filesystem content.
	Local skilltree.Tree
	// CanCheckpoint is true when Local is known to be the exact destination
	// result. Only then may an unchanged push establish or advance publication
	// state without creating a commit.
	CanCheckpoint bool
	// SyncLocal records that reconciliation incorporated destination-only state.
	// It is written locally only after the whole destination group succeeds.
	SyncLocal bool
}

// pushGroup collects every candidate that shares one destination repository and
// branch, which are committed and pushed together.
type pushGroup struct {
	// Repository is the destination in both forms: its String is the configured
	// text used for grouping, comparison, and every message, while the resolved
	// value it carries reaches git alone.
	Repository gitrepo.Repository
	Branch     string
	// DefaultBranch is resolved once with Branch and reused if a missing
	// configured branch must start from the destination default.
	DefaultBranch string
	// BranchConfigured is true when the branch came from an explicit
	// `push_branch`; a missing configured branch is created from the
	// destination repository's current default-branch commit.
	BranchConfigured bool
	// SharedBySources is true when more than one source block contributes here.
	// A shared destination can never be the exact tracked source ref of any one
	// skill, so no lock advances from it.
	SharedBySources bool
	// SourceRepository and SourceRef record the single contributing source, so
	// lock advancement can require an exact match. They are meaningful only
	// while SharedBySources is false.
	SourceRepository string
	SourceRef        string
	Candidates       []pushCandidate
}

// Push performs the configured upstream writes. It never pulls first and never
// force-pushes.
func (s *Service) Push(ctx context.Context) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		return s.pushLocked(ctx, st, report)
	})
	report.Sort()
	return report, err
}

func (s *Service) pushLocked(ctx context.Context, st *state, report *Report) error {
	if err := failOnOrphans(st); err != nil {
		return err
	}

	writable := make([]config.SkillImport, 0, len(st.cfg.Skills.Imports))
	for _, block := range st.cfg.Skills.Imports {
		if block.WriteEnabled() {
			writable = append(writable, block)
		}
	}
	if len(writable) == 0 {
		return nil
	}

	runner, err := s.newRunner(st.env)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-push-")
	if err != nil {
		return fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workRoot) }()

	groups := map[string]*pushGroup{}
	var order []string
	branches := &defaultBranchCache{runner: runner, workRoot: workRoot, resolved: map[string]string{}}

	for index, block := range writable {
		repository := config.NormalizeSkillRepository(block.Repository)
		entries := st.entriesForBlock(block)
		if len(entries) == 0 {
			continue
		}
		// A block-level failure blocks every skill it owns, so each one is
		// reported rather than silently dropped from the operation report.
		blocked := func(err error) {
			report.AddSourceFailure(repository, block.Ref, err)
			reportBlockedSkills(report, entries, err)
		}

		blockWorkDir := filepath.Join(workRoot, fmt.Sprintf("push-source-%d", index))
		if mkErr := os.MkdirAll(blockWorkDir, 0o700); mkErr != nil {
			return fmt.Errorf("failed to create git working directory %s: %w", blockWorkDir, mkErr)
		}
		// Placeholders resolve here, at the Git access boundary. `repository`
		// above stays the configured text used for reporting and lock identity.
		sourceRepository, resolveErr := runner.Secrets().Resolve(repository)
		if resolveErr != nil {
			blocked(resolveErr)
			continue
		}
		source, openErr := gitrepo.OpenSource(ctx, runner, blockWorkDir, sourceRepository)
		if openErr != nil {
			blocked(openErr)
			continue
		}

		// The source ref is resolved once per block and then compared against
		// every tracked entry's own locked commit. A partial pull can leave one
		// block's entries at different commits, so checking a single entry
		// would let a stale skill through.
		sourceCommit, resolveErr := resolveTrackedSourceCommit(ctx, source, block, entries)
		if resolveErr != nil {
			blocked(resolveErr)
			continue
		}

		destination, destinationErr := runner.Secrets().Resolve(config.NormalizeSkillRepository(block.EffectivePushRepository()))
		if destinationErr != nil {
			blocked(destinationErr)
			continue
		}
		branch, defaultBranch, branchConfigured, branchErr := resolvePushBranch(ctx, branches, block, destination)
		if branchErr != nil {
			blocked(branchErr)
			continue
		}

		// Grouping keys off the configured text, so placeholder spelling is what
		// decides whether two blocks share a destination commit.
		key := destination.String() + "\x00" + branch
		group, exists := groups[key]
		if !exists {
			group = &pushGroup{
				Repository:       destination,
				Branch:           branch,
				DefaultBranch:    defaultBranch,
				BranchConfigured: branchConfigured,
				SourceRepository: repository,
				SourceRef:        entries[0].ResolvedRef,
			}
			groups[key] = group
			order = append(order, key)
		} else if group.SourceRepository != repository || group.SourceRef != entries[0].ResolvedRef {
			// Two different sources contributing to one destination group cannot
			// both claim it for lock advancement.
			group.SharedBySources = true
		}

		for _, entry := range entries {
			candidate, candidateErr := buildPushCandidate(ctx, st, source, block, entry, sourceCommit)
			if candidateErr != nil {
				report.Add(SkillResult{
					Name:         entry.Name,
					Repository:   entry.Repository,
					SelectedPath: entry.SelectedPath,
					Outcome:      OutcomeFailed,
					Err:          candidateErr,
				})
				continue
			}
			group.Candidates = append(group.Candidates, candidate)
		}
	}

	sort.Strings(order)
	txn := newTransaction(pathSetFor(st), st.lock)
	for _, key := range order {
		s.publishGroup(ctx, runner, workRoot, txn, groups[key], report)
	}

	if txn.NeedsCommit(st.lock, st.lockPresent) {
		if err := txn.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// resolveTrackedSourceCommit resolves the block's source ref once when any of
// its entries track it. An empty result means no entry tracks the ref, so no
// freshness comparison applies.
func resolveTrackedSourceCommit(ctx context.Context, source *gitrepo.Source, block config.SkillImport, entries []skilllock.Entry) (string, error) {
	tracked := false
	for _, entry := range entries {
		if entry.Tracking == config.SkillTrackingTracked {
			tracked = true
			break
		}
	}
	if !tracked {
		return "", nil
	}
	resolution, err := source.Resolve(ctx, block.Ref)
	if err != nil {
		return "", err
	}
	return resolution.Commit, nil
}

// verifySourceUnmoved refuses to push a tracked import whose own locked commit
// no longer matches the source ref, because the derived delta would be stale.
// Each entry is checked against its own commit: a partial pull advances only
// the skills it succeeded on, so entries in one block can legitimately sit at
// different commits.
func verifySourceUnmoved(entry skilllock.Entry, sourceCommit string) error {
	if entry.Tracking != config.SkillTrackingTracked || sourceCommit == "" {
		return nil
	}
	if sourceCommit != entry.Commit {
		return fmt.Errorf("source %s ref %s advanced from %s to %s; run 'al skills pull' before pushing",
			entry.Repository, entry.ResolvedRef, shortCommit(entry.Commit), shortCommit(sourceCommit))
	}
	return nil
}

// defaultBranchCache resolves each destination repository's default branch once
// per push. Both `direct` (which writes to it) and `branch` (which must refuse
// to write to it) need the answer.
type defaultBranchCache struct {
	runner   *gitrepo.Runner
	workRoot string
	resolved map[string]string
}

func (c *defaultBranchCache) get(ctx context.Context, repository gitrepo.Repository) (string, error) {
	if branch, ok := c.resolved[repository.String()]; ok {
		return branch, nil
	}
	dir, err := os.MkdirTemp(c.workRoot, "default-branch-")
	if err != nil {
		return "", fmt.Errorf("failed to create a git working directory: %w", err)
	}
	destination, err := gitrepo.OpenDestination(ctx, c.runner, dir, repository)
	if err != nil {
		return "", err
	}
	branch, err := destination.DefaultBranch(ctx)
	if err != nil {
		return "", err
	}
	c.resolved[repository.String()] = branch
	return branch, nil
}

// resolvePushBranch decides which destination branch a block writes to.
//
// `direct` writes to the destination's default branch. `branch` writes to its
// explicitly configured branch, and the destination's actual default branch is
// resolved to prove that branch is not the primary one: the configuration
// vocabulary reserves primary-branch writes for `direct`, and a repository
// whose default branch is neither `main` nor `master` cannot be recognized
// statically.
func resolvePushBranch(ctx context.Context, branches *defaultBranchCache, block config.SkillImport, destination gitrepo.Repository) (branch string, defaultBranch string, configured bool, err error) {
	defaultBranch, err = branches.get(ctx, destination)
	if err != nil {
		return "", "", false, err
	}
	configuredBranch := strings.TrimSpace(block.PushBranch)
	if configuredBranch == "" {
		return defaultBranch, defaultBranch, false, nil
	}
	if configuredBranch == defaultBranch {
		return "", "", false, fmt.Errorf("push_branch %q is the default branch of %s; write_policy = %q never writes to a destination's primary branch, so use write_policy = %q or configure a different branch",
			configuredBranch, destination, config.SkillWritePolicyBranch, config.SkillWritePolicyDirect)
	}
	return configuredBranch, defaultBranch, true, nil
}

// reportBlockedSkills records the required failed result for every skill a
// block-level failure blocked. Reporting the source line alone would leave
// those skills unaccounted for in an operation report that must cover every
// source and skill.
func reportBlockedSkills(report *Report, entries []skilllock.Entry, err error) {
	for _, entry := range entries {
		report.Add(SkillResult{
			Name:         entry.Name,
			Repository:   entry.Repository,
			SelectedPath: entry.SelectedPath,
			Outcome:      OutcomeFailed,
			Err:          err,
		})
	}
}

// buildPushCandidate validates one imported skill and derives its file-level
// delta from the locked source tree without pulling.
func buildPushCandidate(ctx context.Context, st *state, source *gitrepo.Source, block config.SkillImport, entry skilllock.Entry, sourceCommit string) (pushCandidate, error) {
	if err := verifySourceUnmoved(entry, sourceCommit); err != nil {
		return pushCandidate{}, err
	}
	observed := st.skill(entry.Name)
	if !observed.Present {
		// A missing whole skill is never translated into upstream deletion.
		return pushCandidate{}, fmt.Errorf("imported directory %s is missing; a push requires an existing, valid imported skill", relativeTo(st.paths.Root, observed.Dir))
	}
	if observed.Err != nil {
		return pushCandidate{}, observed.Err
	}
	base, err := source.ReadTree(ctx, entry.Commit, entry.SelectedPath)
	if err != nil {
		return pushCandidate{}, fmt.Errorf("locked source commit %s could not be read, so no merge base exists: %w", shortCommit(entry.Commit), err)
	}
	if base.Hash() != entry.TreeHash {
		return pushCandidate{}, fmt.Errorf("locked upstream state does not match commit %s; run 'al skills pull' to restore a trustworthy merge base", shortCommit(entry.Commit))
	}
	return pushCandidate{Entry: entry, Block: block, Base: base, Local: observed.Tree}, nil
}

// publishGroup reconciles every candidate in one destination group against the
// destination head and, when anything changed, commits and pushes them together.
func (s *Service) publishGroup(ctx context.Context, runner *gitrepo.Runner, workRoot string, txn *transaction, group *pushGroup, report *Report) {
	if len(group.Candidates) == 0 {
		return
	}
	// The runtime group is the authority on which paths share one destination
	// commit. Desired-set validation cannot see it: configuration edited after
	// import can route blocks from different repositories to one destination,
	// and `al skills push` never pulls, so nothing else revalidates it. Publish
	// applies updates in order, so an unchecked overlap would silently let one
	// skill overwrite another.
	if err := rejectOverlappingDestinationPaths(group); err != nil {
		s.failGroup(group, report, err)
		return
	}
	groupDir, err := os.MkdirTemp(workRoot, "destination-")
	if err != nil {
		report.AddSourceFailure(group.Repository.String(), group.Branch, fmt.Errorf("failed to create a git working directory: %w", err))
		return
	}
	destination, err := gitrepo.OpenDestination(ctx, runner, groupDir, group.Repository)
	if err != nil {
		s.failGroup(group, report, err)
		return
	}

	head, branchExisted, err := destination.Head(ctx, group.Branch)
	if err != nil {
		s.failGroup(group, report, err)
		return
	}
	if !branchExisted {
		if !group.BranchConfigured {
			s.failGroup(group, report, fmt.Errorf("destination %s has no branch %s", group.Repository, group.Branch))
			return
		}
		defaultHead, defaultExists, headErr := destination.Head(ctx, group.DefaultBranch)
		if headErr != nil {
			s.failGroup(group, report, headErr)
			return
		}
		if !defaultExists {
			s.failGroup(group, report, fmt.Errorf("cannot create destination branch %s because default branch %s does not exist in %s", group.Branch, group.DefaultBranch, group.Repository))
			return
		}
		if fetchErr := destination.FetchCommit(ctx, group.Repository, defaultHead); fetchErr != nil {
			s.failGroup(group, report, fetchErr)
			return
		}
		head = defaultHead
	} else if fetchErr := destination.FetchCommit(ctx, group.Repository, head); fetchErr != nil {
		s.failGroup(group, report, fetchErr)
		return
	}

	var updates []gitrepo.Update
	var pushed []pushCandidate
	// Results for candidates that need no commit are held until the group's
	// outcome is known: publishing to a tracked source ref moves that ref for
	// every skill on it, so an unchanged sibling's lock has to advance too.
	var unchanged []SkillResult
	var unchangedCandidates []pushCandidate
	flushUnchanged := func() {
		for _, result := range unchanged {
			report.Add(result)
		}
	}
	for _, candidate := range group.Candidates {
		result := SkillResult{
			Name:         candidate.Entry.Name,
			Repository:   candidate.Entry.Repository,
			SelectedPath: candidate.Entry.SelectedPath,
		}
		destinationTree, readErr := destination.ReadTree(ctx, head, candidate.Entry.SelectedPath)
		if readErr != nil {
			result.Outcome = OutcomeFailed
			result.Err = readErr
			report.Add(result)
			continue
		}
		mergeBase := candidate.Base
		checkpointed := false
		if branchExisted {
			var baseErr error
			mergeBase, checkpointed, baseErr = publicationMergeBase(ctx, destination, group, candidate, head)
			if errors.Is(baseErr, errPublicationUnavailable) {
				candidate.Entry.Publication = nil
				txn.SetLockEntry(candidate.Entry)
				mergeBase = candidate.Base
				baseErr = nil
			}
			if baseErr != nil {
				result.Outcome = OutcomeFailed
				result.Err = baseErr
				report.Add(result)
				continue
			}
		} else if publicationMatches(group, candidate.Entry.Publication) {
			// A deleted branch has no destination history left to reconcile. Drop
			// its obsolete checkpoint before optionally creating the branch anew.
			candidate.Entry.Publication = nil
			txn.SetLockEntry(candidate.Entry)
		}
		candidate.Base = mergeBase
		candidate.CanCheckpoint = checkpointed
		// An absent destination path means two different things, and only the
		// branch's history distinguishes them.
		//
		// On a branch this publication just created from the destination's
		// default branch, a path absent from that base was never present on the
		// contribution branch: there is no common skill history to reconcile, so
		// the complete local skill is added. That is what keeps a heterogeneous
		// group coherent — merging against an empty tree would read every
		// unchanged file as a destination deletion and publish a partial skill.
		//
		// On a branch that already existed, an absent path is a destination-side
		// whole-skill deletion relative to the locked base, which is ordinary
		// three-way input: unchanged local content preserves the deletion, and
		// modified local content is a delete/modify conflict.
		merged := candidate.Local
		if branchExisted || !destinationTree.IsEmpty() {
			result, merged = mergeAgainstDestination(ctx, runner, candidate, destinationTree, result)
			if result.Outcome == OutcomeFailed {
				report.Add(result)
				continue
			}
		}
		if !checkpointed && !destinationTree.IsEmpty() && !merged.Equal(candidate.Local) && merged.Equal(destinationTree) {
			result.Outcome = OutcomeFailed
			result.Err = fmt.Errorf("no publication checkpoint exists for %s branch %s, so Agent Layer cannot distinguish a local reversion from no local change; first align the local skill with the destination and push once to establish a checkpoint, then reapply the reversion",
				group.Repository, group.Branch)
			report.Add(result)
			continue
		}
		if merged.Equal(candidate.Local) {
			candidate.CanCheckpoint = true
		}
		// A whole-skill destination deletion cannot be materialized as a valid
		// imported skill. Leaving its checkpoint unadvanced preserves that remote
		// deletion on later pushes without corrupting the managed local tier.
		if merged.IsEmpty() {
			candidate.CanCheckpoint = false
		}
		candidate.SyncLocal = !merged.IsEmpty() && !merged.Equal(candidate.Local)
		candidate.Local = merged
		// Equality is settled before validation so a preserved deletion reports
		// unchanged instead of failing validation on an empty tree.
		if merged.Equal(destinationTree) {
			result.Outcome = OutcomeUnchanged
			result.Detail = unchangedDetail(group, candidate, merged)
			unchanged = append(unchanged, result)
			unchangedCandidates = append(unchangedCandidates, candidate)
			continue
		}
		if _, validateErr := skilltree.ValidateSkill(merged, candidate.Entry.SelectedPath); validateErr != nil {
			result.Outcome = OutcomeFailed
			result.Err = fmt.Errorf("the result for %s would not be a valid skill: %w", group.Repository, validateErr)
			report.Add(result)
			continue
		}
		updates = append(updates, gitrepo.Update{Path: candidate.Entry.SelectedPath, Tree: merged})
		pushed = append(pushed, candidate)
	}

	if len(updates) == 0 {
		if branchExisted {
			advancePublications(txn, group, head, unchangedCandidates)
		}
		syncMergedLocals(txn, unchangedCandidates)
		flushUnchanged()
		return
	}

	commit, err := destination.Publish(ctx, head, group.Branch, updates, groupCommitMessage(pushed))
	if err != nil {
		// A grouped commit or push failure affects every skill in the group.
		failed := append(append([]pushCandidate{}, pushed...), unchangedCandidates...)
		for _, candidate := range failed {
			report.Add(SkillResult{
				Name:         candidate.Entry.Name,
				Repository:   candidate.Entry.Repository,
				SelectedPath: candidate.Entry.SelectedPath,
				Outcome:      OutcomeFailed,
				Err:          err,
			})
		}
		return
	}

	advanceUnchangedLocks(txn, group, head, commit, unchanged, unchangedCandidates)
	advancePublications(txn, group, commit, unchangedCandidates)
	syncMergedLocals(txn, unchangedCandidates)
	flushUnchanged()

	for _, candidate := range pushed {
		result := SkillResult{
			Name:         candidate.Entry.Name,
			Repository:   candidate.Entry.Repository,
			SelectedPath: candidate.Entry.SelectedPath,
			Outcome:      OutcomePushed,
			Detail:       fmt.Sprintf("%s %s @ %s", group.Repository, group.Branch, shortCommit(commit)),
		}
		if advancesLock(group, candidate) {
			entry := candidate.Entry
			entry.Commit = commit
			entry.TreeHash = candidate.Local.Hash()
			entry.Publication = nil
			txn.SetLockEntry(entry)
			result.Detail += "; lock advanced"
		} else {
			entry := candidate.Entry
			entry.Publication = publicationFor(group, commit, candidate.Local)
			txn.SetLockEntry(entry)
		}
		if candidate.SyncLocal {
			txn.WriteSkill(candidate.Entry.Name, candidate.Local)
		}
		report.Add(result)
	}
}

// publicationMergeBase returns the last tree this destination received from a
// successful push. A checkpoint for another destination is intentionally
// ignored; the immutable source tree remains the first-push base there.
func publicationMergeBase(ctx context.Context, destination *gitrepo.Destination, group *pushGroup, candidate pushCandidate, head string) (skilltree.Tree, bool, error) {
	publication := candidate.Entry.Publication
	if !publicationMatches(group, publication) {
		return candidate.Base, false, nil
	}
	if err := destination.FetchCommit(ctx, group.Repository, publication.Commit); err != nil {
		if errors.Is(err, gitrepo.ErrCommitUnavailable) {
			return skilltree.Tree{}, false, fmt.Errorf("%w: commit %s from %s branch %s: %v",
				errPublicationUnavailable, shortCommit(publication.Commit), group.Repository, group.Branch, err)
		}
		return skilltree.Tree{}, false, fmt.Errorf("fetch published merge base %s from %s branch %s: %w",
			shortCommit(publication.Commit), group.Repository, group.Branch, err)
	}
	ancestor, err := destination.IsAncestor(ctx, publication.Commit, head)
	if err != nil {
		return skilltree.Tree{}, false, fmt.Errorf("verify published merge base %s on %s branch %s: %w",
			shortCommit(publication.Commit), group.Repository, group.Branch, err)
	}
	if !ancestor {
		// The branch was rebased or force-pushed away from the prior publication.
		// The locked source remains a trustworthy conservative merge base.
		return skilltree.Tree{}, false, fmt.Errorf("%w: commit %s is not an ancestor of %s branch %s",
			errPublicationUnavailable, shortCommit(publication.Commit), group.Repository, group.Branch)
	}
	base, err := destination.ReadTree(ctx, publication.Commit, candidate.Entry.SelectedPath)
	if err != nil {
		return skilltree.Tree{}, false, fmt.Errorf("published merge base %s could not be read for %s: %w",
			shortCommit(publication.Commit), candidate.Entry.SelectedPath, err)
	}
	if base.Hash() != publication.TreeHash {
		return skilltree.Tree{}, false, fmt.Errorf("published merge base for %s does not match commit %s; refusing to guess at prior destination state",
			candidate.Entry.SelectedPath, shortCommit(publication.Commit))
	}
	return base, true, nil
}

func syncMergedLocals(txn *transaction, candidates []pushCandidate) {
	for _, candidate := range candidates {
		if candidate.SyncLocal {
			txn.WriteSkill(candidate.Entry.Name, candidate.Local)
		}
	}
}

func publicationMatches(group *pushGroup, publication *skilllock.Publication) bool {
	return publication != nil && publication.Repository == group.Repository.String() && publication.Branch == group.Branch
}

func publicationFor(group *pushGroup, commit string, tree skilltree.Tree) *skilllock.Publication {
	return &skilllock.Publication{
		Repository: group.Repository.String(),
		Branch:     group.Branch,
		Commit:     commit,
		TreeHash:   tree.Hash(),
	}
}

// advancePublications moves trustworthy unchanged checkpoints to the group's
// resulting commit. This matters for grouped pushes: one skill's update moves
// the branch commit shared by every unchanged sibling.
func advancePublications(txn *transaction, group *pushGroup, commit string, candidates []pushCandidate) {
	for _, candidate := range candidates {
		if !candidate.CanCheckpoint || advancesLock(group, candidate) {
			continue
		}
		entry := candidate.Entry
		entry.Publication = publicationFor(group, commit, candidate.Local)
		txn.SetLockEntry(entry)
	}
}

// advanceUnchangedLocks moves the locks of skills a grouped publication left
// untouched but whose tracked source ref the publication itself moved.
//
// Publishing to the exact tracked source ref advances that ref for every skill
// recorded against it, not only the ones this commit changed. Leaving an
// unchanged sibling at the previous commit would make the very next push reject
// it as a stale source and demand a pull that has nothing to do.
//
// The lock is only advanced when the destination head this publication built on
// is the skill's own locked commit. That proves the commit did not alter this
// skill's path — its updates are disjoint by construction — so the recorded
// tree hash still describes the tree at the new commit.
func advanceUnchangedLocks(txn *transaction, group *pushGroup, head string, commit string, results []SkillResult, candidates []pushCandidate) {
	for i, candidate := range candidates {
		if !advancesLock(group, candidate) || candidate.Entry.Commit != head {
			continue
		}
		entry := candidate.Entry
		entry.Commit = commit
		txn.SetLockEntry(entry)
		results[i].Detail += "; lock advanced"
	}
}

// mergeAgainstDestination reconciles one candidate's local change with the
// content the destination already carries. A failure is reported on the
// returned result, which the caller detects through OutcomeFailed.
func mergeAgainstDestination(ctx context.Context, runner *gitrepo.Runner, candidate pushCandidate, destinationTree skilltree.Tree, result SkillResult) (SkillResult, skilltree.Tree) {
	merged, conflicts, mergeErr := skilltree.Merge(candidate.Base, candidate.Local, destinationTree, runner.TextMerger(ctx))
	if mergeErr != nil {
		result.Outcome = OutcomeFailed
		result.Err = mergeErr
		return result, skilltree.Tree{}
	}
	if len(conflicts) > 0 {
		result.Outcome = OutcomeFailed
		result.Err = fmt.Errorf("local and destination changes conflict in %s", describeConflicts(conflicts))
		return result, skilltree.Tree{}
	}
	return result, merged
}

// unchangedDetail explains why a skill needs no commit. A merged result that is
// empty is not "already present"; it is a destination-side removal the local
// tree agreed with, and saying so keeps the report honest about what the
// destination actually holds.
func unchangedDetail(group *pushGroup, candidate pushCandidate, merged skilltree.Tree) string {
	if merged.IsEmpty() {
		return fmt.Sprintf("%s is not present on %s %s; the destination removed it and the local tree matches the locked base, so the removal is preserved",
			candidate.Entry.SelectedPath, group.Repository, group.Branch)
	}
	return fmt.Sprintf("%s already contains this result on %s", group.Repository, group.Branch)
}

// rejectOverlappingDestinationPaths refuses a group whose skills would write to
// duplicate or nested destination paths, because one update would then silently
// overwrite another inside the same commit.
func rejectOverlappingDestinationPaths(group *pushGroup) error {
	paths := make([]string, 0, len(group.Candidates))
	for _, candidate := range group.Candidates {
		paths = append(paths, candidate.Entry.SelectedPath)
	}
	sort.Strings(paths)
	accepted := make(map[string]struct{}, len(paths))
	for _, current := range paths {
		for ancestor := current; ancestor != "." && ancestor != "/" && ancestor != ""; ancestor = path.Dir(ancestor) {
			if _, exists := accepted[ancestor]; !exists {
				continue
			}
			return fmt.Errorf("destination paths %s and %s overlap in %s branch %s; narrow one selector or route the blocks to different destinations",
				ancestor, current, group.Repository, group.Branch)
		}
		accepted[current] = struct{}{}
	}
	return nil
}

// advancesLock reports whether a successful push updated the exact tracked
// source repository and ref this skill is locked to. A pinned lock and a push
// to any other repository or ref never advance.
func advancesLock(group *pushGroup, candidate pushCandidate) bool {
	if group.SharedBySources {
		return false
	}
	// A tracked entry always names a branch: the lock parser rejects any other
	// combination, so no separate ref-kind check is needed here.
	if candidate.Entry.Tracking != config.SkillTrackingTracked {
		return false
	}
	// The lock records configured text, so the comparison is against the
	// group's configured text rather than anything resolved.
	if group.Repository.String() != candidate.Entry.Repository {
		return false
	}
	return group.Branch == candidate.Entry.ResolvedRef
}

// failGroup records a destination-level failure against every candidate.
func (s *Service) failGroup(group *pushGroup, report *Report, err error) {
	for _, candidate := range group.Candidates {
		report.Add(SkillResult{
			Name:         candidate.Entry.Name,
			Repository:   candidate.Entry.Repository,
			SelectedPath: candidate.Entry.SelectedPath,
			Outcome:      OutcomeFailed,
			Err:          err,
		})
	}
}

// groupCommitMessage renders the deterministic message for a grouped
// contribution.
func groupCommitMessage(candidates []pushCandidate) string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Entry.Name)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return "Update skill " + names[0]
	}
	return "Update skills " + strings.Join(names, ", ")
}
