package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Git object type names reported by `git ls-tree` and `git cat-file -t`.
const (
	gitObjectBlob   = "blob"
	gitObjectTree   = "tree"
	gitObjectCommit = "commit"
)

// Resolution is remote-resolved evidence about one configured ref.
type Resolution struct {
	// Ref is the resolved ref name: a branch name, a tag name, or the object id
	// when the configured ref was an object id.
	Ref string
	// Kind is the ref kind proven by resolution, never guessed.
	Kind string
	// Commit is the resolved commit object id.
	Commit string
}

// Source is an isolated local mirror of one remote repository. It lives inside
// a caller-owned temporary directory and is discarded with it.
type Source struct {
	runner     *Runner
	dir        string
	repository string
	fetched    bool
}

// OpenSource initializes an isolated repository for repository under workDir.
// Nothing is fetched until a resolution or read requires it.
func OpenSource(ctx context.Context, runner *Runner, workDir string, repository string) (*Source, error) {
	if runner == nil {
		return nil, fmt.Errorf("a git runner is required")
	}
	dir := filepath.Join(workDir, "source")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create git working directory %s: %w", dir, err)
	}
	if _, err := runner.run(ctx, dir, "init", "--bare", "--quiet"); err != nil {
		return nil, err
	}
	return &Source{runner: runner, dir: dir, repository: repository}, nil
}

// Repository returns the source repository reference.
func (s *Source) Repository() string { return s.repository }

// DefaultBranch resolves the repository's actual default branch name.
func (s *Source) DefaultBranch(ctx context.Context) (string, error) {
	output, err := s.runner.run(ctx, s.dir, "ls-remote", "--symref", "--", s.repository, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			return strings.TrimPrefix(fields[1], "refs/heads/"), nil
		}
	}
	return "", fmt.Errorf("could not determine the default branch of %s; specify an explicit ref", s.repository)
}

// Resolve determines what a configured ref names and which commit it points at.
//
// An empty ref resolves to the repository's default branch. A full object id
// resolves to a commit. Every other value must exist as exactly one of a branch
// or a tag; an ambiguous name is an actionable error rather than a silent
// preference.
func (s *Source) Resolve(ctx context.Context, ref string) (Resolution, error) {
	configured := strings.TrimSpace(ref)
	if configured == "" {
		branch, err := s.DefaultBranch(ctx)
		if err != nil {
			return Resolution{}, err
		}
		configured = branch
	}

	if IsCommitID(configured) {
		if err := s.ensureCommit(ctx, configured); err != nil {
			return Resolution{}, err
		}
		return Resolution{Ref: configured, Kind: skilllock.RefKindCommit, Commit: configured}, nil
	}

	output, err := s.runner.run(ctx, s.dir, "ls-remote", "--", s.repository,
		"refs/heads/"+configured, "refs/tags/"+configured, "refs/tags/"+configured+"^{}")
	if err != nil {
		return Resolution{}, err
	}

	var branchCommit, tagCommit, tagPeeled string
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case "refs/heads/" + configured:
			branchCommit = fields[0]
		case "refs/tags/" + configured:
			tagCommit = fields[0]
		case "refs/tags/" + configured + "^{}":
			tagPeeled = fields[0]
		}
	}

	switch {
	case branchCommit != "" && tagCommit != "":
		return Resolution{}, fmt.Errorf("ref %q is both a branch and a tag in %s; use the full object id to select one", configured, s.repository)
	case branchCommit != "":
		return Resolution{Ref: configured, Kind: skilllock.RefKindBranch, Commit: branchCommit}, nil
	case tagCommit != "":
		commit := tagPeeled
		if commit == "" {
			commit = tagCommit
		}
		return Resolution{Ref: configured, Kind: skilllock.RefKindTag, Commit: commit}, nil
	default:
		return Resolution{}, fmt.Errorf("ref %q does not exist in %s", configured, s.repository)
	}
}

// ensureCommit makes a commit object available locally, fetching it if needed.
func (s *Source) ensureCommit(ctx context.Context, commit string) error {
	if s.hasCommit(ctx, commit) {
		return nil
	}
	if err := s.fetchAll(ctx); err != nil {
		return err
	}
	if s.hasCommit(ctx, commit) {
		return nil
	}
	// A commit that is not reachable from any ref can still be fetched directly
	// when the server allows it; try that before failing.
	if _, err := s.runner.run(ctx, s.dir, "fetch", "--quiet", "--no-tags", "--", s.repository, commit); err != nil {
		return fmt.Errorf("commit %s could not be fetched from %s: %w", commit, s.repository, err)
	}
	if !s.hasCommit(ctx, commit) {
		return fmt.Errorf("commit %s could not be fetched from %s", commit, s.repository)
	}
	return nil
}

func (s *Source) hasCommit(ctx context.Context, commit string) bool {
	_, code, err := s.runner.runAllowExit(ctx, s.dir, []int{1, 128}, "cat-file", "-e", commit+"^{commit}")
	return err == nil && code == 0
}

// fetchAll mirrors every branch and tag once per source lifetime.
func (s *Source) fetchAll(ctx context.Context) error {
	if s.fetched {
		return nil
	}
	if _, err := s.runner.run(ctx, s.dir, "fetch", "--quiet", "--prune", "--", s.repository,
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
		return err
	}
	s.fetched = true
	return nil
}

// Fetch makes a resolved commit available locally.
func (s *Source) Fetch(ctx context.Context, commit string) error {
	return s.ensureCommit(ctx, commit)
}

// ReadTree returns the exact content of a repository path at a commit.
//
// The returned tree carries the same canonical shape as a local skill tree:
// slash-normalized relative paths, exact bytes, and the executable bit. A
// gitlink (submodule) or symlink entry is rejected without being followed.
//
// A path that does not exist at the commit yields an empty tree rather than an
// error: that is the correct merge input when a destination branch does not
// carry a skill yet. Callers that require the path to exist compare the result
// against recorded state, which an empty tree can never match.
func (s *Source) ReadTree(ctx context.Context, commit string, repoPath string) (skilltree.Tree, error) {
	exists, _, err := s.PathExists(ctx, commit, repoPath)
	if err != nil {
		return skilltree.Tree{}, err
	}
	if !exists {
		return skilltree.Tree{}, nil
	}
	spec := commit + ":" + repoPath
	output, err := s.runner.run(ctx, s.dir, "ls-tree", "-r", "-z", "--full-tree", spec)
	if err != nil {
		return skilltree.Tree{}, err
	}

	var files []skilltree.File
	for _, record := range strings.Split(string(output), "\x00") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		mode, objectType, object, name, parseErr := parseTreeRecord(record)
		if parseErr != nil {
			return skilltree.Tree{}, parseErr
		}
		if isIgnoredTreePath(name) {
			continue
		}
		switch {
		case objectType == gitObjectCommit:
			return skilltree.Tree{}, fmt.Errorf("%s/%s is a gitlink (submodule); imported skills may contain only directories and regular files", repoPath, name)
		case mode == "120000":
			return skilltree.Tree{}, fmt.Errorf("%s/%s is a symbolic link; imported skills may contain only directories and regular files", repoPath, name)
		case objectType != gitObjectBlob:
			return skilltree.Tree{}, fmt.Errorf("%s/%s is an unsupported git object type %q", repoPath, name, objectType)
		}
		data, catErr := s.runner.run(ctx, s.dir, "cat-file", gitObjectBlob, object)
		if catErr != nil {
			return skilltree.Tree{}, catErr
		}
		files = append(files, skilltree.File{Path: name, Data: data, Executable: mode == "100755"})
	}
	return skilltree.NewTree(files)
}

// parseTreeRecord splits one NUL-delimited `git ls-tree` record.
func parseTreeRecord(record string) (mode string, objectType string, object string, name string, err error) {
	tab := strings.IndexByte(record, '\t')
	if tab < 0 {
		return "", "", "", "", fmt.Errorf("unexpected git ls-tree record %q", record)
	}
	fields := strings.Fields(record[:tab])
	if len(fields) != 3 {
		return "", "", "", "", fmt.Errorf("unexpected git ls-tree record %q", record)
	}
	return fields[0], fields[1], fields[2], record[tab+1:], nil
}

// isIgnoredTreePath reports whether a repository-relative path names one of the
// three artifacts excluded from every canonical skill content operation.
func isIgnoredTreePath(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		switch segment {
		case ".git", ".DS_Store", "Thumbs.db":
			return true
		}
	}
	return false
}

// ListDirectories returns every directory path at a commit, sorted, so wildcard
// selectors can be expanded without checking out a working tree.
func (s *Source) ListDirectories(ctx context.Context, commit string) ([]string, error) {
	if err := s.ensureCommit(ctx, commit); err != nil {
		return nil, err
	}
	output, err := s.runner.run(ctx, s.dir, "ls-tree", "-r", "-d", "-z", "--name-only", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, name := range strings.Split(string(output), "\x00") {
		name = strings.TrimSpace(name)
		if name == "" || isIgnoredTreePath(name) {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// PathExists reports whether a repository path exists at a commit and whether
// it is a directory.
func (s *Source) PathExists(ctx context.Context, commit string, repoPath string) (exists bool, isDir bool, err error) {
	if err := s.ensureCommit(ctx, commit); err != nil {
		return false, false, err
	}
	output, _, err := s.runner.runAllowExit(ctx, s.dir, []int{128}, "cat-file", "-t", commit+":"+repoPath)
	if err != nil {
		return false, false, err
	}
	switch strings.TrimSpace(string(output)) {
	case gitObjectTree:
		return true, true, nil
	case gitObjectBlob:
		return true, false, nil
	default:
		return false, false, nil
	}
}

// MergeText performs a deterministic three-way text merge using git's own
// merge-file implementation, so Agent Layer does not maintain a second, subtly
// different diff3.
func (r *Runner) MergeText(ctx context.Context, base, local, remote []byte) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "al-skill-merge-")
	if err != nil {
		return nil, false, fmt.Errorf("failed to create merge working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	paths := map[string][]byte{"local": local, "base": base, "remote": remote}
	for name, data := range paths {
		if writeErr := os.WriteFile(filepath.Join(dir, name), data, 0o600); writeErr != nil {
			return nil, false, fmt.Errorf("failed to stage merge input %s: %w", name, writeErr)
		}
	}

	// merge-file exits with the number of conflicts (1..127) on a conflicted but
	// successful merge, so those codes are outcomes rather than failures.
	allowed := make([]int, 0, 127)
	for code := 1; code <= 127; code++ {
		allowed = append(allowed, code)
	}
	output, code, err := r.runAllowExit(ctx, dir, allowed,
		"merge-file", "-p", "--diff3",
		"-L", "local", "-L", "locked upstream base", "-L", "upstream",
		"local", "base", "remote")
	if err != nil {
		return nil, false, err
	}
	if code > 0 {
		return nil, true, nil
	}
	return output, false, nil
}

// TextMerger adapts MergeText to the skilltree merge contract.
func (r *Runner) TextMerger(ctx context.Context) skilltree.TextMerger {
	return func(base, local, remote []byte) ([]byte, bool, error) {
		return r.MergeText(ctx, base, local, remote)
	}
}
