package skillimports

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
	"github.com/conn-castle/agent-layer/internal/sync"
)

// reconcileOptions controls how far one reconciliation is allowed to move.
type reconcileOptions struct {
	// AdvanceTracked allows tracked branch heads to advance. Only `al skills pull`
	// sets it; add and remove reconcile selector changes at the locked commit.
	AdvanceTracked bool
}

// blockResolution is one import block resolved against its remote.
type blockResolution struct {
	index      int
	imp        config.SkillImport
	repository string
	resolved   ResolvedRef
	tracking   string
	// commit is the upstream commit this block's desired paths are taken from.
	commit string
	// retarget records that the configured ref or the resolved default-branch
	// name changed, so existing entries reconcile from their locked base to this
	// block's newly resolved commit.
	retarget bool
	// baseUndefined records that the block's existing entries disagree about
	// their locked commit, so a newly desired path has no single base to import
	// from without advancing the block.
	baseUndefined bool
	paths         []string
	exclusions    []string
}

// desiredTarget is one (repository, source path) the configuration wants.
type desiredTarget struct {
	block     *blockResolution
	entry     config.SkillImportLockEntry
	skillName string
}

// reconcilePlan is the fully preflighted change to local source state.
type reconcilePlan struct {
	// staged maps skill name to the tree that should replace it.
	staged map[string]*Tree
	// removals are skill names whose directories should be deleted.
	removals []string
	// entries is the complete resulting lock entry set.
	entries []config.SkillImportLockEntry
	// blocks are the successfully resolved import blocks, in configuration order.
	blocks []*blockResolution
}

// blockByIndex returns the resolved block at a configuration index.
func (p *reconcilePlan) blockByIndex(index int) (*blockResolution, bool) {
	for _, block := range p.blocks {
		if block.index == index {
			return block, true
		}
	}
	return nil, false
}

func newReconcilePlan() *reconcilePlan {
	return &reconcilePlan{staged: map[string]*Tree{}}
}

// changed reports whether the plan mutates anything.
func (p *reconcilePlan) changed(current []config.SkillImportLockEntry) bool {
	if len(p.staged) > 0 || len(p.removals) > 0 {
		return true
	}
	if len(p.entries) != len(current) {
		return true
	}
	left := append([]config.SkillImportLockEntry(nil), p.entries...)
	right := append([]config.SkillImportLockEntry(nil), current...)
	config.SortSkillImportLockEntries(left)
	config.SortSkillImportLockEntries(right)
	for i := range left {
		if left[i] != right[i] {
			return true
		}
	}
	return false
}

// reconcile resolves every import block, computes the desired set, and produces
// a fully preflighted plan. Source-level failures block only their own block;
// skill-level failures block only their own skill.
func (s *Service) reconcile(
	ctx context.Context,
	space *workspace,
	state *projectState,
	opts reconcileOptions,
	report *Report,
) (*reconcilePlan, error) {
	blocks, blockedBlocks := s.resolveBlocks(ctx, space, state, opts, report)

	desired, err := buildDesiredTargets(blocks)
	if err != nil {
		return nil, err
	}
	var blockedEntries []config.SkillImportLockEntry
	for index := range blockedBlocks {
		for _, entry := range state.lock.Entries {
			if lockEntryBelongsToBlock(entry, state.config.Skills.Imports[index]) {
				blockedEntries = append(blockedEntries, entry)
			}
		}
	}
	if err := preflightDesiredSet(desired, blockedEntries); err != nil {
		return nil, err
	}

	userNames, err := s.userManagedSkillNames()
	if err != nil {
		return nil, err
	}

	plan := newReconcilePlan()
	plan.blocks = blocks
	merger := GitTextMerger{Runner: s.Runner, TempDir: space.dir}

	keys := make([]config.SkillImportEntryKey, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Repository != keys[j].Repository {
			return keys[i].Repository < keys[j].Repository
		}
		return keys[i].SourcePath < keys[j].SourcePath
	})

	for _, key := range keys {
		target := desired[key]
		locked, hasLocked := config.FindSkillImportLockEntry(state.lock, key.Repository, key.SourcePath)
		entry, err := s.reconcileOne(ctx, space, target, locked, hasLocked, userNames, merger, plan, report)
		if err != nil {
			report.Failf(key.Repository, key.SourcePath, target.skillName, err)
			// A failed skill keeps whatever the lock already recorded, so its merge
			// base and local content survive untouched.
			if hasLocked {
				plan.entries = append(plan.entries, locked)
			}
			continue
		}
		plan.entries = append(plan.entries, entry)
	}

	s.reconcileRetirements(state, desired, blockedBlocks, plan, report)
	config.SortSkillImportLockEntries(plan.entries)
	return plan, nil
}

// resolveBlocks resolves each block's ref and desired paths. It returns the
// resolved blocks and the set of repositories that had a source-level failure,
// whose locked entries must be left alone.
func (s *Service) resolveBlocks(
	ctx context.Context,
	space *workspace,
	state *projectState,
	opts reconcileOptions,
	report *Report,
) ([]*blockResolution, map[int]struct{}) {
	blocked := map[int]struct{}{}
	var blocks []*blockResolution

	for i, imp := range state.config.Skills.Imports {
		repository := config.NormalizeSkillRepository(imp.Repository)
		block, err := s.resolveBlock(ctx, space, state, i, imp, opts)
		if err != nil {
			report.AddSource(SourceResult{Repository: repository, Ref: sourceLabel(imp), Err: err})
			blocked[i] = struct{}{}
			for _, entry := range state.lock.Entries {
				if lockEntryBelongsToBlock(entry, imp) {
					report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, fmt.Errorf("source block failed: %w", err))
				}
			}
			continue
		}
		detail := fmt.Sprintf("%s %s at %s", block.resolved.Type, block.resolved.Name, shortCommit(block.commit))
		if block.retarget {
			detail += " (retargeted)"
		}
		report.AddSource(SourceResult{Repository: repository, Ref: sourceLabel(imp), Detail: detail})
		blocks = append(blocks, block)
	}
	return blocks, blocked
}

// resolveBlock resolves one block's ref, decides which commit its desired paths
// come from, and expands its selectors at that commit.
func (s *Service) resolveBlock(
	ctx context.Context,
	space *workspace,
	state *projectState,
	index int,
	imp config.SkillImport,
	opts reconcileOptions,
) (*blockResolution, error) {
	repository := config.NormalizeSkillRepository(imp.Repository)
	resolved, err := resolveRef(ctx, space, repository, imp.Ref)
	if err != nil {
		return nil, err
	}
	tracking, err := resolveTracking(imp.Tracking, resolved)
	if err != nil {
		return nil, err
	}
	if err := fetchRef(ctx, space, repository, resolved); err != nil {
		return nil, err
	}

	block := &blockResolution{
		index:      index,
		imp:        imp,
		repository: repository,
		resolved:   resolved,
		tracking:   tracking,
		commit:     resolved.Commit,
	}

	baseCommit, baseRefName, agreed := lockedBaseFor(state.lock, imp)
	switch {
	case baseCommit == "":
		// Either a brand-new block or one whose configured ref just changed. Both
		// reconcile from the newly resolved commit; retarget is only reported when
		// the repository already had entries that this block now owns.
		block.retarget = repositoryHasEntries(state.lock, repository)
	case !agreed:
		block.baseUndefined = true
	case baseRefName != resolved.Name:
		// The configured ref was omitted and the repository's default branch was
		// renamed; that is a retarget, not a mismatch.
		block.retarget = true
	case opts.AdvanceTracked && tracking == config.SkillTrackingTracked:
		// Tracked branches advance only during pull.
	default:
		// Pinned blocks never move, and add/remove reconcile selector changes
		// against the locked commit without advancing anything.
		block.commit = baseCommit
	}

	if block.commit != resolved.Commit {
		if err := fetchCommit(ctx, space, repository, block.commit); err != nil {
			return nil, err
		}
	}

	index2, err := buildRepoIndex(ctx, space, block.commit)
	if err != nil {
		return nil, err
	}
	resolution, err := resolveSelectors(imp, index2)
	if err != nil {
		return nil, err
	}
	block.paths = resolution.Paths
	block.exclusions = resolution.Exclusions
	return block, nil
}

// lockedBaseFor returns the commit and resolved ref name shared by the lock
// entries that already belong to a block, plus whether they agree. An empty
// commit means the block has no entries under its current configured ref.
func lockedBaseFor(lock *config.SkillImportLock, imp config.SkillImport) (commit string, refName string, agreed bool) {
	repository := config.NormalizeSkillRepository(imp.Repository)
	configuredRef := strings.TrimSpace(imp.Ref)
	refOmitted := configuredRef == ""
	agreed = true
	for _, entry := range lock.Entries {
		if entry.Repository != repository || entry.ConfiguredRef != configuredRef || entry.RefOmitted != refOmitted {
			continue
		}
		if commit == "" {
			commit = entry.SourceCommit
			refName = entry.ResolvedRefName
			continue
		}
		if entry.SourceCommit != commit || entry.ResolvedRefName != refName {
			agreed = false
		}
	}
	return commit, refName, agreed
}

func repositoryHasEntries(lock *config.SkillImportLock, repository string) bool {
	for _, entry := range lock.Entries {
		if entry.Repository == repository {
			return true
		}
	}
	return false
}

// buildDesiredTargets flattens every block's resolved paths into target lock
// entries and rejects two blocks claiming the same source path.
func buildDesiredTargets(blocks []*blockResolution) (map[config.SkillImportEntryKey]desiredTarget, error) {
	desired := make(map[config.SkillImportEntryKey]desiredTarget)
	for _, block := range blocks {
		for _, sourcePath := range block.paths {
			key := config.SkillImportEntryKey{Repository: block.repository, SourcePath: sourcePath}
			if existing, ok := desired[key]; ok {
				return nil, fmt.Errorf(
					"skills.imports[%d] and skills.imports[%d] both select %s:%s; one path cannot have two policies",
					existing.block.index, block.index, RedactSecrets(block.repository), sourcePath,
				)
			}
			desired[key] = desiredTarget{
				block:     block,
				skillName: config.NormalizeSkillImportName(path.Base(sourcePath)),
				entry: config.SkillImportLockEntry{
					Repository:      block.repository,
					SourcePath:      sourcePath,
					ConfiguredRef:   strings.TrimSpace(block.imp.Ref),
					RefOmitted:      strings.TrimSpace(block.imp.Ref) == "",
					ResolvedRefName: block.resolved.Name,
					ResolvedRefType: block.resolved.Type,
					SourceCommit:    block.commit,
					Tracking:        block.tracking,
					Write:           block.imp.EffectiveWrite(),
					PushRepository:  block.imp.EffectivePushRepository(),
					PushBranch:      strings.TrimSpace(block.imp.PushBranch),
					SkillName:       config.NormalizeSkillImportName(path.Base(sourcePath)),
				},
			}
		}
	}
	return desired, nil
}

// preflightDesiredSet rejects configurations that cannot be materialized before
// any local state changes: two imports that would own one local directory, and
// overlapping selected paths in one repository. Entries kept because their
// source failed are included so a blocked repository cannot hide a collision.
func preflightDesiredSet(
	desired map[config.SkillImportEntryKey]desiredTarget,
	blockedEntries []config.SkillImportLockEntry,
) error {
	entries := make([]config.SkillImportLockEntry, 0, len(desired))
	for _, target := range desired {
		entries = append(entries, target.entry)
	}
	for _, entry := range blockedEntries {
		if _, alsoDesired := desired[entry.Key()]; alsoDesired {
			continue
		}
		entries = append(entries, entry)
	}
	if err := rejectDuplicateSkillNames(entries); err != nil {
		return err
	}
	return rejectOverlappingSourcePaths(entries)
}

// reconcileOne brings a single desired import to its target state.
func (s *Service) reconcileOne(
	ctx context.Context,
	space *workspace,
	target desiredTarget,
	locked config.SkillImportLockEntry,
	hasLocked bool,
	userNames map[string]string,
	merger TextMerger,
	plan *reconcilePlan,
	report *Report,
) (config.SkillImportLockEntry, error) {
	entry := target.entry
	repository := entry.Repository
	sourcePath := entry.SourcePath

	if hasLocked && target.block.baseUndefined {
		// Existing entries still reconcile against their own recorded base.
	} else if !hasLocked && target.block.baseUndefined {
		return entry, fmt.Errorf(
			"%s:%s is newly selected, but this import block's existing skills are locked to different commits; resolve those first so the block has one locked commit to import from",
			RedactSecrets(repository), sourcePath,
		)
	}

	upstream, err := ReadGitTree(ctx, space, entry.SourceCommit, sourcePath)
	if err != nil {
		return entry, err
	}
	identity, err := ValidateImportedTree(upstream, sourcePath)
	if err != nil {
		return entry, err
	}
	if identity.Name != entry.SkillName {
		return entry, fmt.Errorf(
			"%s/%s declares name %q but the selected directory is %q",
			sourcePath, SkillManifestName, identity.Name, path.Base(sourcePath),
		)
	}
	entry.UpstreamTreeHash = upstream.Hash()

	userPath, userCollision := userNames[entry.SkillName]

	if !hasLocked {
		if userCollision {
			return entry, fmt.Errorf(
				"a user-managed skill already owns the name %q (%s); remove or rename it, or narrow the selector, before importing %s:%s",
				entry.SkillName, userPath, RedactSecrets(repository), sourcePath,
			)
		}
		local := s.readLocalSkill(entry)
		if local.Present {
			return entry, fmt.Errorf(
				"%s already exists but has no lock entry; adopt it by moving it into .agent-layer/skills/, or remove it, before importing %s:%s",
				s.localSkillDir(entry.SkillName), RedactSecrets(repository), sourcePath,
			)
		}
		plan.staged[entry.SkillName] = upstream
		report.AddSkill(SkillResult{
			Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
			Action: ActionImported, Detail: shortCommit(entry.SourceCommit),
		})
		return entry, nil
	}

	local := s.readLocalSkill(locked)
	if local.Err != nil {
		return entry, local.Err
	}
	if !local.Present {
		if userCollision {
			return entry, fmt.Errorf(
				"%s is missing and a user-managed skill now owns the name %q (%s); narrow or remove the selector for %s:%s to finish adopting it",
				s.localSkillDir(entry.SkillName), entry.SkillName, userPath, RedactSecrets(repository), sourcePath,
			)
		}
		plan.staged[entry.SkillName] = upstream
		report.AddSkill(SkillResult{
			Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
			Action: ActionRestored, Detail: "local directory was missing",
		})
		return entry, nil
	}
	if userCollision {
		return entry, fmt.Errorf(
			"skill name %q exists in both %s and %s; move or rename one source, or narrow the import selector",
			entry.SkillName, userPath, s.localSkillDir(entry.SkillName),
		)
	}

	clean := local.Tree.Hash() == locked.UpstreamTreeHash
	if clean {
		if local.Tree.Equal(upstream) {
			report.AddSkill(SkillResult{
				Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
				Action: ActionUnchanged,
			})
			return entry, nil
		}
		plan.staged[entry.SkillName] = upstream
		report.AddSkill(SkillResult{
			Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
			Action: ActionUpdated, Detail: shortCommit(entry.SourceCommit),
		})
		return entry, nil
	}

	if locked.SourceCommit == entry.SourceCommit {
		report.AddSkill(SkillResult{
			Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
			Action: ActionUnchanged, Detail: "locally modified",
		})
		return entry, nil
	}

	if err := fetchCommit(ctx, space, repository, locked.SourceCommit); err != nil {
		return entry, fmt.Errorf(
			"the merge base for %s is unavailable, so local changes were preserved and nothing was imported: %w",
			entry.SkillName, err,
		)
	}
	base, err := ReadGitTree(ctx, space, locked.SourceCommit, locked.SourcePath)
	if err != nil {
		return entry, fmt.Errorf(
			"the merge base for %s could not be read, so local changes were preserved: %w", entry.SkillName, err,
		)
	}

	labels := MergeLabels{
		Base:  "locked " + shortCommit(locked.SourceCommit),
		Local: "local " + entry.SkillName,
		Other: "upstream " + shortCommit(entry.SourceCommit),
	}
	merged, conflicts, err := MergeTrees(ctx, base, local.Tree, upstream, labels, merger)
	if err != nil {
		return entry, err
	}
	if len(conflicts) > 0 {
		return entry, fmt.Errorf(
			"local changes conflict with %s:\n%s\nresolve the listed paths in %s, then run 'al skills pull' again",
			shortCommit(entry.SourceCommit), FormatConflicts(conflicts), s.localSkillDir(entry.SkillName),
		)
	}
	if _, err := ValidateImportedTree(merged, sourcePath); err != nil {
		return entry, fmt.Errorf("the merged result is not a valid skill, so nothing was published: %w", err)
	}
	if !merged.Equal(local.Tree) {
		plan.staged[entry.SkillName] = merged
	}
	report.AddSkill(SkillResult{
		Repository: repository, SourcePath: sourcePath, SkillName: entry.SkillName,
		Action: ActionUpdated, Detail: "merged with local changes",
	})
	return entry, nil
}

// reconcileRetirements applies the single retirement rule to every locked entry
// that is no longer in its block's desired set, whatever removed it: a deleted
// selector, a disappeared upstream path, or an exclusion.
func (s *Service) reconcileRetirements(
	state *projectState,
	desired map[config.SkillImportEntryKey]desiredTarget,
	blocked map[int]struct{},
	plan *reconcilePlan,
	report *Report,
) {
	for _, entry := range state.lock.Entries {
		if _, stillDesired := desired[entry.Key()]; stillDesired {
			continue
		}
		isBlocked := false
		for index := range blocked {
			if lockEntryBelongsToBlock(entry, state.config.Skills.Imports[index]) {
				isBlocked = true
				break
			}
		}
		if isBlocked {
			// The source failed; nothing was learned about this entry, so its local
			// tree and lock entry are preserved untouched.
			plan.entries = append(plan.entries, entry)
			continue
		}
		if planHasEntry(plan.entries, entry.Key()) {
			continue
		}
		local := s.readLocalSkill(entry)
		if local.Err != nil {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, local.Err)
			plan.entries = append(plan.entries, entry)
			continue
		}
		if !local.Present {
			report.AddSkill(SkillResult{
				Repository: entry.Repository, SourcePath: entry.SourcePath, SkillName: entry.SkillName,
				Action: ActionPruned, Detail: "local directory was already gone",
			})
			continue
		}
		if local.Modified {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, fmt.Errorf(
				"%s is no longer selected but has local changes; adopt it by moving it into .agent-layer/skills/, or delete it, then run the command again",
				s.localSkillDir(entry.SkillName),
			))
			plan.entries = append(plan.entries, entry)
			continue
		}
		plan.removals = append(plan.removals, entry.SkillName)
		report.AddSkill(SkillResult{
			Repository: entry.Repository, SourcePath: entry.SourcePath, SkillName: entry.SkillName,
			Action: ActionRetired,
		})
	}
	sort.Strings(plan.removals)
}

func lockEntryBelongsToBlock(entry config.SkillImportLockEntry, imp config.SkillImport) bool {
	configuredRef := strings.TrimSpace(imp.Ref)
	return entry.Repository == config.NormalizeSkillRepository(imp.Repository) &&
		entry.ConfiguredRef == configuredRef && entry.RefOmitted == (configuredRef == "") &&
		entrySelectedByImport(imp, entry.SourcePath)
}

func planHasEntry(entries []config.SkillImportLockEntry, key config.SkillImportEntryKey) bool {
	for _, entry := range entries {
		if entry.Key() == key {
			return true
		}
	}
	return false
}

// applyPlan stages every planned mutation and publishes it as one recoverable
// transaction. configText is staged only when it differs from what is on disk.
//
// Staging happens outside the project lock because it can be slow; the publish
// itself runs under the lock, so ordinary projection never reads a state where
// only some of the trees, the configuration, and the lock have landed. The
// snapshot the plan was built from is rechecked after the lock is taken, and a
// concurrent change fails the publish rather than clobbering it.
func (s *Service) applyPlan(state *projectState, plan *reconcilePlan, newConfigText string) error {
	transaction, err := NewTransaction(s.Root)
	if err != nil {
		return err
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = transaction.discard()
		}
	}()
	names := make([]string, 0, len(plan.staged))
	for name := range plan.staged {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := transaction.StageSkill(name, plan.staged[name]); err != nil {
			return err
		}
	}
	for _, name := range plan.removals {
		if err := transaction.StageSkillRemoval(name); err != nil {
			return err
		}
	}
	if newConfigText != "" && newConfigText != state.configText {
		if err := transaction.StageConfig(state.configPath, []byte(newConfigText)); err != nil {
			return err
		}
	}
	lockBytes, err := config.MarshalSkillImportLock(&config.SkillImportLock{
		Version: config.SkillImportLockVersion,
		Entries: plan.entries,
	})
	if err != nil {
		return err
	}
	currentLockBytes, err := config.MarshalSkillImportLock(state.lock)
	if err != nil {
		return err
	}
	if string(lockBytes) != string(currentLockBytes) {
		if err := transaction.StageLock(state.lockPath, lockBytes); err != nil {
			return err
		}
	}
	cleanupStaging = false
	return s.publish(state, transaction)
}

// publish takes the project lock, verifies the snapshot the plan was built from
// is still current, and commits the transaction.
func (s *Service) publish(state *projectState, transaction *Transaction) error {
	if transaction.Empty() {
		// Nothing to publish, so there is nothing for a concurrent projection to
		// observe half-done and no reason to block one.
		return transaction.Commit()
	}
	commitStarted := false
	err := s.lockProject(s.Root, func() error {
		if err := verifySnapshotUnchanged(state); err != nil {
			return err
		}
		commitStarted = true
		return transaction.Commit()
	})
	if err != nil && !commitStarted {
		if cleanupErr := transaction.discard(); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
	}
	return err
}

// lockProject runs fn under the project sync lock. It is a field so tests can
// exercise publish behavior without the real lock.
func (s *Service) lockProject(root string, fn func() error) error {
	if s.WithProjectLock != nil {
		return s.WithProjectLock(root, fn)
	}
	return sync.WithProjectLock(root, fn)
}

// verifySnapshotUnchanged fails when configuration or lock state changed after
// the plan was computed. Publishing over a concurrent edit would silently
// discard it.
func verifySnapshotUnchanged(state *projectState) error {
	currentConfig, err := os.ReadFile(state.configPath) // #nosec G304 -- state.configPath is the resolved project configuration.
	if err != nil {
		return fmt.Errorf("re-read %s before publishing: %w", state.configPath, err)
	}
	if string(currentConfig) != state.configText {
		return fmt.Errorf(
			"%s changed while this command was running; nothing was published, run the command again",
			state.configPath,
		)
	}
	currentLock, err := config.LoadSkillImportLock(state.lockPath)
	if err != nil {
		return err
	}
	expected, err := config.MarshalSkillImportLock(state.lock)
	if err != nil {
		return err
	}
	actual, err := config.MarshalSkillImportLock(currentLock)
	if err != nil {
		return err
	}
	if string(expected) != string(actual) {
		return fmt.Errorf(
			"%s changed while this command was running; nothing was published, run the command again",
			state.lockPath,
		)
	}
	service := &Service{Root: filepath.Dir(filepath.Dir(state.configPath))}
	currentFingerprint, err := service.skillSourceFingerprint(currentLock)
	if err != nil {
		return err
	}
	if currentFingerprint != state.sourceFingerprint {
		return fmt.Errorf("local skill sources changed while this command was running; nothing was published, run the command again")
	}
	return nil
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}
