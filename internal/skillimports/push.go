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
)

// pushCandidate is one write-enabled import prepared for an upstream write.
type pushCandidate struct {
	entry config.SkillImportLockEntry
	local *Tree
}

// destinationKey groups candidates that share one commit and push.
type destinationKey struct {
	Repository string
	Branch     string
}

// destinationFetchRef is the private ref the destination head is fetched into
// before it is checked out under its real branch name.
const destinationFetchRef = "refs/al/destination"

// Push performs the configured upstream writes using current imported-skill
// filesystem content, whether or not that content is committed in the consuming
// project. It never pulls first and never force-pushes.
func (s *Service) Push(ctx context.Context) error {
	state, err := s.loadState()
	if err != nil {
		return err
	}
	report := &Report{}
	candidates := pushEntriesMatchingConfig(state.config, state.lock, report)
	if len(candidates) == 0 {
		report.Note("no imports are configured to write upstream")
		return s.finish(report)
	}

	advanced := map[config.SkillImportEntryKey]config.SkillImportLockEntry{}
	err = s.withWorkspace(ctx, "push", func(space *workspace) error {
		ready := s.preflightPushCandidates(candidates, report)
		groups := s.groupPushCandidates(ctx, space, ready, report)
		keys := make([]destinationKey, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Repository != keys[j].Repository {
				return keys[i].Repository < keys[j].Repository
			}
			return keys[i].Branch < keys[j].Branch
		})
		for _, key := range keys {
			s.pushGroup(ctx, key, groups[key], report, advanced)
		}
		return nil
	})
	if err != nil {
		if s.Out != nil {
			report.Write(s.Out)
		}
		return err
	}

	if len(advanced) > 0 {
		if err := s.applyLockAdvances(state, advanced); err != nil {
			if s.Out != nil {
				report.Write(s.Out)
			}
			return err
		}
		s.runProjection(report)
	}
	return s.finish(report)
}

// pushEntriesMatchingConfig binds every write to current desired configuration.
// A stale lock policy can never authorize an upstream mutation.
func pushEntriesMatchingConfig(cfg *config.Config, lock *config.SkillImportLock, report *Report) []config.SkillImportLockEntry {
	var candidates []config.SkillImportLockEntry
	for _, entry := range lock.Entries {
		imp, ok := configuredBlockForEntry(cfg.Skills.Imports, entry)
		if !ok || !lockPolicyMatchesConfig(entry, imp) {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, fmt.Errorf(
				"configuration no longer matches this locked import; run 'al skills pull' to reconcile before pushing",
			))
			continue
		}
		if imp.EffectiveWrite() == config.SkillWriteNone {
			report.AddSkill(SkillResult{Repository: entry.Repository, SourcePath: entry.SourcePath, SkillName: entry.SkillName, Action: ActionSkipped, Detail: "write = " + config.SkillWriteNone})
			continue
		}
		candidates = append(candidates, entry)
	}
	return candidates
}

func configuredBlockForEntry(imports []config.SkillImport, entry config.SkillImportLockEntry) (config.SkillImport, bool) {
	for _, imp := range imports {
		if config.NormalizeSkillRepository(imp.Repository) != entry.Repository {
			continue
		}
		if entrySelectedByImport(imp, entry.SourcePath) {
			return imp, true
		}
	}
	return config.SkillImport{}, false
}

func entrySelectedByImport(imp config.SkillImport, sourcePath string) bool {
	selected := false
	for _, selector := range imp.PositiveSelectors() {
		if matchSelectorPath(selector, sourcePath) {
			selected = true
			break
		}
	}
	for _, exclusion := range imp.ExclusionSelectors() {
		if matchSelectorPath(exclusion, sourcePath) {
			return false
		}
	}
	return selected
}

func lockPolicyMatchesConfig(entry config.SkillImportLockEntry, imp config.SkillImport) bool {
	configuredRef := strings.TrimSpace(imp.Ref)
	if entry.ConfiguredRef != configuredRef || entry.RefOmitted != (configuredRef == "") {
		return false
	}
	tracking := strings.TrimSpace(imp.Tracking)
	if tracking != "" && tracking != entry.Tracking {
		return false
	}
	return entry.Write == imp.EffectiveWrite() &&
		entry.PushRepository == imp.EffectivePushRepository() &&
		entry.PushBranch == strings.TrimSpace(imp.PushBranch)
}

// preflightPushCandidates validates each candidate independently from current
// filesystem content. A candidate without an existing, valid skill and
// SKILL.md is refused: whole-skill deletion is never propagated upstream.
func (s *Service) preflightPushCandidates(entries []config.SkillImportLockEntry, report *Report) []pushCandidate {
	var ready []pushCandidate
	for _, entry := range entries {
		local := s.readLocalSkill(entry)
		if local.Err != nil {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, local.Err)
			continue
		}
		if !local.Present {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, fmt.Errorf(
				"%s does not exist; a push requires an existing valid skill, and Agent Layer never propagates a whole-skill deletion upstream",
				s.localSkillDir(entry.SkillName),
			))
			continue
		}
		if _, err := ValidateImportedTree(local.Tree, entry.SourcePath); err != nil {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, err)
			continue
		}
		ready = append(ready, pushCandidate{entry: entry, local: local.Tree})
	}
	return ready
}

// groupPushCandidates resolves each candidate's destination and groups the
// survivors by exact destination repository and branch.
func (s *Service) groupPushCandidates(
	ctx context.Context,
	space *workspace,
	candidates []pushCandidate,
	report *Report,
) map[destinationKey][]pushCandidate {
	defaultBranches := map[string]string{}
	groups := map[destinationKey][]pushCandidate{}

	for _, candidate := range candidates {
		entry := candidate.entry
		branch, err := s.resolveDestinationBranch(ctx, space, entry, defaultBranches)
		if err != nil {
			report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, err)
			continue
		}
		if entry.Write == config.SkillWriteBranch {
			primary, err := defaultBranchFor(ctx, space, entry.PushRepository, defaultBranches)
			if err != nil {
				report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, err)
				continue
			}
			if branch == primary {
				report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, fmt.Errorf(
					"push_branch %q is the default branch of %s; write = %q requires a non-primary branch",
					branch, RedactSecrets(entry.PushRepository), config.SkillWriteBranch,
				))
				continue
			}
		}
		if entry.Tracking == config.SkillTrackingTracked {
			if err := s.verifySourceUnmoved(ctx, space, entry); err != nil {
				report.Failf(entry.Repository, entry.SourcePath, entry.SkillName, err)
				continue
			}
		}
		key := destinationKey{Repository: entry.PushRepository, Branch: branch}
		groups[key] = append(groups[key], candidate)
	}

	for key, members := range groups {
		paths := make([]string, 0, len(members))
		for _, member := range members {
			paths = append(paths, member.entry.SourcePath)
		}
		sort.Strings(paths)
		if err := rejectOverlappingPaths(paths); err != nil {
			failGroup(report, members, fmt.Errorf(
				"destination %s branch %s: %w", RedactSecrets(key.Repository), key.Branch, err,
			))
			delete(groups, key)
		}
	}
	return groups
}

// resolveDestinationBranch returns the branch a candidate writes to.
func (s *Service) resolveDestinationBranch(
	ctx context.Context,
	space *workspace,
	entry config.SkillImportLockEntry,
	cache map[string]string,
) (string, error) {
	switch entry.Write {
	case config.SkillWriteBranch:
		branch := strings.TrimSpace(entry.PushBranch)
		if branch == "" {
			return "", fmt.Errorf("write = %q requires an explicit push_branch", config.SkillWriteBranch)
		}
		return branch, nil
	case config.SkillWriteDirect:
		return defaultBranchFor(ctx, space, entry.PushRepository, cache)
	default:
		return "", fmt.Errorf("write policy %q does not write upstream", entry.Write)
	}
}

func defaultBranchFor(ctx context.Context, space *workspace, repository string, cache map[string]string) (string, error) {
	if branch, ok := cache[repository]; ok {
		return branch, nil
	}
	refs, err := listRemoteRefs(ctx, space, repository)
	if err != nil {
		return "", err
	}
	if refs.defaultBranch == "" {
		return "", fmt.Errorf("%s does not advertise a default branch", RedactSecrets(repository))
	}
	cache[repository] = refs.defaultBranch
	return refs.defaultBranch, nil
}

// verifySourceUnmoved refuses to push a tracked import whose source ref has
// advanced past the locked commit, because the local change would be derived
// from a stale base.
func (s *Service) verifySourceUnmoved(ctx context.Context, space *workspace, entry config.SkillImportLockEntry) error {
	refs, err := listRemoteRefs(ctx, space, entry.Repository)
	if err != nil {
		return err
	}
	current, ok := refs.branches[entry.ResolvedRefName]
	if !ok {
		return fmt.Errorf(
			"branch %q no longer exists in %s; run 'al skills pull' to reconcile before pushing",
			entry.ResolvedRefName, RedactSecrets(entry.Repository),
		)
	}
	if current != entry.SourceCommit {
		return fmt.Errorf(
			"%s has advanced to %s since %s was locked at %s; run 'al skills pull' first",
			entry.ResolvedRefName, shortCommit(current), entry.SkillName, shortCommit(entry.SourceCommit),
		)
	}
	return nil
}

func failGroup(report *Report, members []pushCandidate, err error) {
	for _, member := range members {
		report.Failf(member.entry.Repository, member.entry.SourcePath, member.entry.SkillName, err)
	}
}

// pushGroup reconciles and writes every candidate for one destination as a
// single commit. A commit or push failure fails the whole group.
func (s *Service) pushGroup(
	ctx context.Context,
	key destinationKey,
	members []pushCandidate,
	report *Report,
	advanced map[config.SkillImportEntryKey]config.SkillImportLockEntry,
) {
	sort.Slice(members, func(i, j int) bool { return members[i].entry.SkillName < members[j].entry.SkillName })

	err := s.withWorkspace(ctx, "push-"+sanitizeLabel(key.Branch), func(space *workspace) error {
		created, err := checkoutDestination(ctx, space, key, members)
		if err != nil {
			return err
		}
		merger := GitTextMerger{Runner: s.Runner, TempDir: space.dir}
		results := make(map[string]*Tree, len(members))
		changed := false

		for _, member := range members {
			entry := member.entry
			if err := fetchCommit(ctx, space, entry.Repository, entry.SourceCommit); err != nil {
				return fmt.Errorf("read the locked base for %s: %w", entry.SkillName, err)
			}
			base, err := ReadGitTree(ctx, space, entry.SourceCommit, entry.SourcePath)
			if err != nil {
				return fmt.Errorf("read the locked base for %s: %w", entry.SkillName, err)
			}
			destination, _, err := readDestinationTree(ctx, space, entry.SourcePath)
			if err != nil {
				return err
			}
			labels := MergeLabels{
				Base:  "locked " + shortCommit(entry.SourceCommit),
				Local: "local " + entry.SkillName,
				Other: "destination " + key.Branch,
			}
			merged, conflicts, err := MergeTrees(ctx, base, member.local, destination, labels, merger)
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return fmt.Errorf(
					"local changes to %s conflict with %s on %s:\n%s",
					entry.SkillName, RedactSecrets(key.Repository), key.Branch, FormatConflicts(conflicts),
				)
			}
			if _, err := ValidateImportedTree(merged, entry.SourcePath); err != nil {
				return fmt.Errorf("the merged result for %s is not a valid skill, so nothing was pushed: %w", entry.SkillName, err)
			}
			results[entry.SourcePath] = merged
			if !merged.Equal(destination) {
				changed = true
			}
		}

		if !changed && !created {
			for _, member := range members {
				report.AddSkill(SkillResult{
					Repository: member.entry.Repository, SourcePath: member.entry.SourcePath,
					SkillName: member.entry.SkillName, Action: ActionUnchanged,
					Detail: "destination already has this content",
				})
			}
			return nil
		}

		for _, member := range members {
			if err := stageTreeInIndex(ctx, space, member.entry.SourcePath, results[member.entry.SourcePath]); err != nil {
				return err
			}
		}
		if _, err := space.run(ctx, "-c", "core.hooksPath=/dev/null", "commit", "-m", pushCommitMessage(members)); err != nil {
			return fmt.Errorf("commit the pushed skills: %w", err)
		}
		// Push the branch explicitly and never with --force: a rejected
		// non-fast-forward is reported, not overwritten.
		if _, err := space.run(ctx, "push", "--", key.Repository,
			"refs/heads/"+key.Branch+":refs/heads/"+key.Branch); err != nil {
			return fmt.Errorf("push to %s: %w", RedactSecrets(key.Repository), err)
		}
		head, err := space.run(ctx, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		commit := strings.TrimSpace(string(head))

		for _, member := range members {
			entry := member.entry
			detail := key.Branch + " at " + shortCommit(commit)
			report.AddSkill(SkillResult{
				Repository: entry.Repository, SourcePath: entry.SourcePath, SkillName: entry.SkillName,
				Action: ActionPushed, Detail: detail,
			})
			if !pushUpdatedTrackedSource(entry, key) {
				continue
			}
			updated := entry
			updated.SourceCommit = commit
			updated.UpstreamTreeHash = results[entry.SourcePath].Hash()
			advanced[entry.Key()] = updated
		}
		return nil
	})
	if err != nil {
		failGroup(report, members, err)
	}
}

// pushUpdatedTrackedSource reports whether a successful push updated the exact
// tracked source repository and ref this entry follows. A pinned entry, a fork
// push, or a push to a different ref never advances the source lock.
func pushUpdatedTrackedSource(entry config.SkillImportLockEntry, key destinationKey) bool {
	return entry.Tracking == config.SkillTrackingTracked &&
		entry.PushRepository == entry.Repository &&
		key.Repository == entry.Repository &&
		entry.ResolvedRefType == config.SkillRefBranch &&
		key.Branch == entry.ResolvedRefName
}

// checkoutDestination prepares the destination branch in the workspace. It
// reports whether the branch had to be created from the locked source commit.
func checkoutDestination(ctx context.Context, space *workspace, key destinationKey, members []pushCandidate) (bool, error) {
	// The destination head lands on a private ref first. Fetching straight into
	// refs/heads/<branch> would fail whenever the workspace already has that
	// branch checked out, which happens as soon as the destination branch shares
	// a name with the workspace's initial branch.
	_, err := space.run(ctx, "fetch", "--quiet", "--depth=1", "--", key.Repository,
		"+refs/heads/"+key.Branch+":"+destinationFetchRef)
	if err == nil {
		if err := prepareIndexBranch(ctx, space, key.Branch, destinationFetchRef); err != nil {
			return false, err
		}
		return false, nil
	}

	refs, listErr := listRemoteRefs(ctx, space, key.Repository)
	if listErr != nil {
		return false, listErr
	}
	if _, exists := refs.branches[key.Branch]; exists {
		return false, fmt.Errorf("fetch %s from %s: %w", key.Branch, RedactSecrets(key.Repository), err)
	}
	// The configured branch does not exist yet. Create it from the locked source
	// commit rather than from an unrelated destination head, so the new branch
	// carries the history the local skills were derived from.
	base := members[0].entry
	if fetchErr := fetchCommit(ctx, space, base.Repository, base.SourceCommit); fetchErr != nil {
		return false, fmt.Errorf(
			"create %s from the locked source commit %s: %w", key.Branch, shortCommit(base.SourceCommit), fetchErr,
		)
	}
	if err := prepareIndexBranch(ctx, space, key.Branch, base.SourceCommit); err != nil {
		return false, err
	}
	return true, nil
}

func prepareIndexBranch(ctx context.Context, space *workspace, branch string, commit string) error {
	ref := "refs/heads/" + branch
	if _, err := space.run(ctx, "update-ref", ref, commit); err != nil {
		return fmt.Errorf("prepare %s: %w", branch, err)
	}
	if _, err := space.run(ctx, "symbolic-ref", "HEAD", ref); err != nil {
		return fmt.Errorf("select %s: %w", branch, err)
	}
	if _, err := space.run(ctx, "read-tree", commit); err != nil {
		return fmt.Errorf("read %s into the push index: %w", branch, err)
	}
	return nil
}

// readDestinationTree reads the current destination content for one skill path.
// It reports whether the path exists at the destination head at all, which the
// caller needs to tell "the destination never had this skill" apart from "the
// destination changed this skill".
func readDestinationTree(ctx context.Context, space *workspace, sourcePath string) (*Tree, bool, error) {
	if _, err := space.run(ctx, "cat-file", "-e", "HEAD:"+sourcePath); err != nil {
		var gitErr *GitError
		if errors.As(err, &gitErr) {
			empty, buildErr := NewTree(nil)
			return empty, false, buildErr
		}
		return nil, false, err
	}
	tree, err := ReadGitTree(ctx, space, "HEAD", sourcePath)
	if err != nil {
		return nil, false, err
	}
	return tree, true, nil
}

// stageTreeInIndex writes blobs and index entries through Git plumbing. Remote
// content is never checked out, so destination symlinks, hooks, attributes, and
// clean/smudge filters cannot redirect or transform local filesystem writes.
func stageTreeInIndex(ctx context.Context, space *workspace, sourcePath string, tree *Tree) error {
	tracked, err := space.run(ctx, "ls-files", "-z", "--", sourcePath)
	if err != nil {
		return fmt.Errorf("list destination index path %s: %w", sourcePath, err)
	}
	for _, existing := range strings.Split(string(tracked), "\x00") {
		if existing == "" {
			continue
		}
		if _, err := space.run(ctx, "update-index", "--force-remove", "--", existing); err != nil {
			return fmt.Errorf("remove %s from the destination index: %w", existing, err)
		}
	}
	blobDir := filepath.Join(space.dir, ".agent-layer-push-blobs")
	if err := os.RemoveAll(blobDir); err != nil {
		return fmt.Errorf("clear push blob staging: %w", err)
	}
	if err := os.MkdirAll(blobDir, DirectoryMode); err != nil {
		return fmt.Errorf("create push blob staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(blobDir) }()
	for index, file := range tree.Files() {
		blobPath := filepath.Join(blobDir, fmt.Sprintf("%d", index))
		if err := os.WriteFile(blobPath, file.Data, RegularFileMode); err != nil { // #nosec G306 -- disposable private workspace content.
			return fmt.Errorf("stage blob for %s: %w", file.Path, err)
		}
		object, err := space.run(ctx, "hash-object", "-w", "--", blobPath)
		if err != nil {
			return fmt.Errorf("write blob for %s: %w", file.Path, err)
		}
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		indexPath := path.Join(sourcePath, file.Path)
		if _, err := space.run(ctx, "update-index", "--add", "--cacheinfo", mode+","+strings.TrimSpace(string(object))+","+indexPath); err != nil {
			return fmt.Errorf("stage %s in destination index: %w", indexPath, err)
		}
	}
	return nil
}

// pushCommitMessage renders a deterministic commit message for a destination group.
func pushCommitMessage(members []pushCandidate) string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.entry.SkillName)
	}
	sort.Strings(names)
	subject := fmt.Sprintf("Update %s", strings.Join(names, ", "))
	if len(subject) > 72 {
		subject = fmt.Sprintf("Update %d skills", len(names))
	}
	return subject + "\n\nPushed by Agent Layer from managed skill imports.\n"
}

// applyLockAdvances publishes lock advances produced by a successful push.
func (s *Service) applyLockAdvances(state *projectState, advanced map[config.SkillImportEntryKey]config.SkillImportLockEntry) error {
	entries := make([]config.SkillImportLockEntry, 0, len(state.lock.Entries))
	for _, entry := range state.lock.Entries {
		if updated, ok := advanced[entry.Key()]; ok {
			entries = append(entries, updated)
			continue
		}
		entries = append(entries, entry)
	}
	plan := newReconcilePlan()
	plan.entries = entries
	return s.applyPlan(state, plan, "")
}
