package skillimports

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// ResolvedRef is one configured ref resolved against a real remote.
type ResolvedRef struct {
	// Name is the branch name, tag name, or commit id.
	Name string
	// Type is config.SkillRefBranch, config.SkillRefTag, or config.SkillRefCommit.
	Type string
	// Commit is the commit the ref points at.
	Commit string
	// FullRef is the fully qualified ref (refs/heads/... or refs/tags/...), empty
	// for a directly named commit.
	FullRef string
}

// Tracked reports whether this ref kind advances on pull when the block leaves
// tracking unset. Only branches move; tags and commits are pinned by nature.
func (r ResolvedRef) Tracked() bool {
	return r.Type == config.SkillRefBranch
}

// remoteRefs is the parsed output of one `git ls-remote --symref` call.
type remoteRefs struct {
	// defaultBranch is the branch HEAD points at, empty when the remote does not
	// advertise a symref.
	defaultBranch string
	// branches maps branch name to commit.
	branches map[string]string
	// tags maps tag name to the commit it resolves to, following annotated tags.
	tags map[string]string
}

// Ref namespaces a configured ref may name explicitly. A short name is resolved
// against both namespaces; a qualified name resolves against exactly one, which
// is how an import states which of an ambiguous branch and tag it means.
const (
	qualifiedRefPrefix = "refs/"
	branchRefPrefix    = "refs/heads/"
	tagRefPrefix       = "refs/tags/"
)

var commitIDPattern = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

// isCommitID reports whether a configured ref is a full object id. Abbreviated
// ids are deliberately rejected: they cannot be resolved without fetching, and
// silently expanding one would record state the user did not write.
func isCommitID(ref string) bool {
	return commitIDPattern.MatchString(strings.ToLower(ref))
}

// listRemoteRefs asks the remote for every ref plus the HEAD symref in one call.
func listRemoteRefs(ctx context.Context, space *workspace, repository string) (*remoteRefs, error) {
	out, err := space.run(ctx, "ls-remote", "--symref", "--", repository)
	if err != nil {
		return nil, fmt.Errorf("list refs for %s: %w", RedactSecrets(repository), err)
	}
	return parseRemoteRefs(string(out)), nil
}

// parseRemoteRefs parses `git ls-remote --symref` output. Peeled annotated tag
// lines (`<sha> refs/tags/x^{}`) override the tag object id so a tag always maps
// to the commit it names.
func parseRemoteRefs(output string) *remoteRefs {
	refs := &remoteRefs{branches: map[string]string{}, tags: map[string]string{}}
	peeled := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		left, right, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		if strings.HasPrefix(left, "ref: ") {
			if right == "HEAD" {
				refs.defaultBranch = strings.TrimPrefix(strings.TrimPrefix(left, "ref: "), "refs/heads/")
			}
			continue
		}
		switch {
		case strings.HasPrefix(right, "refs/heads/"):
			refs.branches[strings.TrimPrefix(right, "refs/heads/")] = left
		case strings.HasSuffix(right, "^{}") && strings.HasPrefix(right, "refs/tags/"):
			peeled[strings.TrimSuffix(strings.TrimPrefix(right, "refs/tags/"), "^{}")] = left
		case strings.HasPrefix(right, "refs/tags/"):
			refs.tags[strings.TrimPrefix(right, "refs/tags/")] = left
		}
	}
	for name, commit := range peeled {
		refs.tags[name] = commit
	}
	return refs
}

// resolveRef resolves a configured ref against a remote. An empty configured ref
// resolves to the repository's actual default branch name, which is recorded so
// a later rename is recognized as a retarget.
func resolveRef(ctx context.Context, space *workspace, repository string, configuredRef string) (ResolvedRef, error) {
	refs, err := listRemoteRefs(ctx, space, repository)
	if err != nil {
		return ResolvedRef{}, err
	}
	ref := strings.TrimSpace(configuredRef)
	if ref == "" {
		if refs.defaultBranch == "" {
			return ResolvedRef{}, fmt.Errorf(
				"%s does not advertise a default branch; set an explicit ref for this import",
				RedactSecrets(repository),
			)
		}
		commit, ok := refs.branches[refs.defaultBranch]
		if !ok {
			return ResolvedRef{}, fmt.Errorf(
				"%s advertises default branch %q but does not publish it",
				RedactSecrets(repository), refs.defaultBranch,
			)
		}
		return ResolvedRef{
			Name:    refs.defaultBranch,
			Type:    config.SkillRefBranch,
			Commit:  commit,
			FullRef: "refs/heads/" + refs.defaultBranch,
		}, nil
	}

	if strings.HasPrefix(ref, qualifiedRefPrefix) {
		return resolveQualifiedRef(refs, repository, ref)
	}

	branchCommit, isBranch := refs.branches[ref]
	tagCommit, isTag := refs.tags[ref]
	if isBranch && isTag {
		return ResolvedRef{}, fmt.Errorf(
			"%s has both a branch and a tag named %q; set ref to %q or %q to say which one this import means",
			RedactSecrets(repository), ref, branchRefPrefix+ref, tagRefPrefix+ref,
		)
	}
	switch {
	case isBranch:
		return ResolvedRef{Name: ref, Type: config.SkillRefBranch, Commit: branchCommit, FullRef: "refs/heads/" + ref}, nil
	case isTag:
		return ResolvedRef{Name: ref, Type: config.SkillRefTag, Commit: tagCommit, FullRef: "refs/tags/" + ref}, nil
	case isCommitID(ref):
		return ResolvedRef{Name: ref, Type: config.SkillRefCommit, Commit: strings.ToLower(ref)}, nil
	default:
		return ResolvedRef{}, fmt.Errorf(
			"%s has no branch or tag named %q, and %q is not a full commit id",
			RedactSecrets(repository), ref, ref,
		)
	}
}

// resolveQualifiedRef resolves a configured ref that names its namespace
// explicitly (refs/heads/x or refs/tags/x). The recorded Name stays the short
// name so the lock records the same ref identity however it was spelled, while
// the namespace decides the ref type and therefore whether the block tracks.
func resolveQualifiedRef(refs *remoteRefs, repository string, ref string) (ResolvedRef, error) {
	var kind, name string
	var commits map[string]string
	switch {
	case strings.HasPrefix(ref, branchRefPrefix):
		kind, name, commits = config.SkillRefBranch, strings.TrimPrefix(ref, branchRefPrefix), refs.branches
	case strings.HasPrefix(ref, tagRefPrefix):
		kind, name, commits = config.SkillRefTag, strings.TrimPrefix(ref, tagRefPrefix), refs.tags
	default:
		return ResolvedRef{}, fmt.Errorf(
			"ref %q names a ref namespace Agent Layer does not import; use %s<branch>, %s<tag>, a short name, or a full commit id",
			ref, branchRefPrefix, tagRefPrefix,
		)
	}
	if name == "" {
		return ResolvedRef{}, fmt.Errorf("ref %q names no %s", ref, kind)
	}
	commit, ok := commits[name]
	if !ok {
		return ResolvedRef{}, fmt.Errorf("%s has no %s named %q", RedactSecrets(repository), kind, name)
	}
	return ResolvedRef{Name: name, Type: kind, Commit: commit, FullRef: ref}, nil
}

// resolveTracking turns a block's configured tracking mode into the recorded
// mode. An omitted mode follows the resolved ref kind; an explicit "tracked" on a
// tag or commit is rejected rather than silently downgraded.
func resolveTracking(configured string, ref ResolvedRef) (string, error) {
	switch strings.TrimSpace(configured) {
	case "":
		if ref.Tracked() {
			return config.SkillTrackingTracked, nil
		}
		return config.SkillTrackingPinned, nil
	case config.SkillTrackingPinned:
		return config.SkillTrackingPinned, nil
	case config.SkillTrackingTracked:
		if !ref.Tracked() {
			return "", fmt.Errorf(
				"tracking = %q requires a branch, but %q resolves to a %s",
				config.SkillTrackingTracked, ref.Name, ref.Type,
			)
		}
		return config.SkillTrackingTracked, nil
	default:
		return "", fmt.Errorf("tracking must be %q or %q", config.SkillTrackingTracked, config.SkillTrackingPinned)
	}
}

// fetchCommit makes a commit available in the workspace. It first asks the
// remote for that exact object, which most servers allow and which stays cheap.
// Servers that refuse object-id fetches need the reachable refs instead, so the
// second attempt fetches branches and tags and then verifies the object is
// present. Both attempts demand the same object; neither substitutes a
// different commit, and a still-missing object is a hard failure.
func fetchCommit(ctx context.Context, space *workspace, repository string, commit string) error {
	if commit == "" {
		return fmt.Errorf("a commit id is required to fetch from %s", RedactSecrets(repository))
	}
	if _, err := space.run(ctx, "fetch", "--quiet", "--depth=1", "--", repository, commit); err == nil {
		if has, checkErr := hasCommit(ctx, space, commit); checkErr == nil && has {
			return nil
		}
	}
	if _, err := space.run(ctx, "fetch", "--quiet", "--tags", "--", repository,
		"+refs/heads/*:refs/remotes/import/*"); err != nil {
		return fmt.Errorf("fetch %s from %s: %w", commit, RedactSecrets(repository), err)
	}
	has, err := hasCommit(ctx, space, commit)
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf(
			"commit %s is not available in %s; it may have been rewritten or removed upstream",
			commit, RedactSecrets(repository),
		)
	}
	return nil
}

// fetchRef makes a resolved ref's commit available in the workspace.
func fetchRef(ctx context.Context, space *workspace, repository string, ref ResolvedRef) error {
	if ref.FullRef == "" {
		return fetchCommit(ctx, space, repository, ref.Commit)
	}
	if _, err := space.run(ctx, "fetch", "--quiet", "--depth=1", "--", repository,
		"+"+ref.FullRef+":refs/import/"+ref.Type+"/"+ref.Name); err != nil {
		return fmt.Errorf("fetch %s from %s: %w", ref.FullRef, RedactSecrets(repository), err)
	}
	has, err := hasCommit(ctx, space, ref.Commit)
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf(
			"fetched %s from %s but commit %s is missing; the ref moved during the fetch, so nothing was changed locally",
			ref.FullRef, RedactSecrets(repository), ref.Commit,
		)
	}
	return nil
}

// hasCommit reports whether the workspace already contains a commit object.
func hasCommit(ctx context.Context, space *workspace, commit string) (bool, error) {
	if _, err := space.run(ctx, "cat-file", "-e", commit+"^{commit}"); err != nil {
		// A missing object is the answer to the question, not a failure; any other
		// git failure (a broken workspace, a killed process) still propagates.
		var gitErr *GitError
		if errors.As(err, &gitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
