package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// missingIdentityMarkers identify a commit that failed only because the user
// has no Git identity configured. Agent Layer never invents an author.
var missingIdentityMarkers = []string{
	"Please tell me who you are",
	"unable to auto-detect email address",
	"empty ident name",
}

// Destination is an isolated working repository used to publish one grouped
// upstream contribution. It can fetch from several repositories so a branch
// created for a fork can start from the locked source commit.
type Destination struct {
	runner     *Runner
	dir        string
	repository string
}

// OpenDestination initializes an isolated working repository under workDir for
// the destination repository.
func OpenDestination(ctx context.Context, runner *Runner, workDir string, repository string) (*Destination, error) {
	if runner == nil {
		return nil, fmt.Errorf("a git runner is required")
	}
	dir := filepath.Join(workDir, "destination")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create git working directory %s: %w", dir, err)
	}
	if _, err := runner.run(ctx, dir, "init", "--quiet"); err != nil {
		return nil, err
	}
	return &Destination{runner: runner, dir: dir, repository: repository}, nil
}

// Repository returns the destination repository reference.
func (d *Destination) Repository() string { return d.repository }

// DefaultBranch resolves the destination repository's default branch name.
func (d *Destination) DefaultBranch(ctx context.Context) (string, error) {
	output, err := d.runner.run(ctx, d.dir, "ls-remote", "--symref", "--", d.repository, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			return strings.TrimPrefix(fields[1], "refs/heads/"), nil
		}
	}
	return "", fmt.Errorf("could not determine the default branch of %s", d.repository)
}

// Head returns the destination branch's current commit. It reports exists =
// false when the branch does not exist yet, which `branch` write policy handles
// by creating it from the locked source commit.
func (d *Destination) Head(ctx context.Context, branch string) (commit string, exists bool, err error) {
	output, err := d.runner.run(ctx, d.dir, "ls-remote", "--", d.repository, "refs/heads/"+branch)
	if err != nil {
		return "", false, err
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "refs/heads/"+branch {
			return fields[0], true, nil
		}
	}
	return "", false, nil
}

// FetchCommit makes a commit from repository available in the destination
// working repository.
func (d *Destination) FetchCommit(ctx context.Context, repository string, commit string) error {
	if d.hasCommit(ctx, commit) {
		return nil
	}
	if _, err := d.runner.run(ctx, d.dir, "fetch", "--quiet", "--no-tags", "--", repository, commit); err != nil {
		// Some servers refuse to serve an arbitrary object id; mirroring the
		// refs makes reachable commits available instead.
		if _, mirrorErr := d.runner.run(ctx, d.dir, "fetch", "--quiet", "--", repository,
			"+refs/heads/*:refs/remotes/fetched/*", "+refs/tags/*:refs/tags/*"); mirrorErr != nil {
			return err
		}
	}
	if !d.hasCommit(ctx, commit) {
		return fmt.Errorf("commit %s could not be fetched from %s", commit, repository)
	}
	return nil
}

func (d *Destination) hasCommit(ctx context.Context, commit string) bool {
	_, code, err := d.runner.runAllowExit(ctx, d.dir, []int{1, 128}, "cat-file", "-e", commit+"^{commit}")
	return err == nil && code == 0
}

// ReadTree returns the content of a repository path at a commit that is already
// available in the destination working repository.
func (d *Destination) ReadTree(ctx context.Context, commit string, repoPath string) (skilltree.Tree, error) {
	source := &Source{runner: d.runner, dir: d.dir, repository: d.repository, fetched: true}
	return source.ReadTree(ctx, commit, repoPath)
}

// Update is one skill's desired destination content.
type Update struct {
	// Path is the destination repository-relative skill root.
	Path string
	// Tree is the exact desired content at Path.
	Tree skilltree.Tree
}

// Publish checks out base, replaces each update's path with its desired tree,
// commits the result, and pushes it to branch without force.
//
// It returns the pushed commit. Publish never rewrites history and never falls
// back to another branch or repository.
func (d *Destination) Publish(ctx context.Context, base string, branch string, updates []Update, message string) (string, error) {
	if len(updates) == 0 {
		return "", fmt.Errorf("publishing requires at least one skill update")
	}
	if _, err := d.runner.run(ctx, d.dir, "checkout", "--quiet", "--force", "--detach", base); err != nil {
		return "", err
	}
	for _, update := range updates {
		target := filepath.Join(d.dir, filepath.FromSlash(update.Path))
		if err := os.RemoveAll(target); err != nil {
			return "", fmt.Errorf("failed to clear destination path %s: %w", update.Path, err)
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			return "", fmt.Errorf("failed to create destination path %s: %w", update.Path, err)
		}
		if err := skilltree.Materialize(update.Tree, target); err != nil {
			return "", err
		}
	}
	if _, err := d.runner.run(ctx, d.dir, "add", "--all", "--"); err != nil {
		return "", err
	}
	if _, err := d.runner.run(ctx, d.dir, "commit", "--quiet", "--message", message); err != nil {
		return "", annotateIdentityFailure(err)
	}
	commit, err := d.runner.run(ctx, d.dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := d.runner.run(ctx, d.dir, "push", "--", d.repository, "HEAD:refs/heads/"+branch); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(commit)), nil
}

// annotateIdentityFailure turns git's missing-identity error into actionable
// guidance instead of letting Agent Layer invent an author.
func annotateIdentityFailure(err error) error {
	message := err.Error()
	for _, marker := range missingIdentityMarkers {
		if strings.Contains(message, marker) {
			return fmt.Errorf("%w; configure your Git identity (git config user.name and user.email) before pushing skill changes", err)
		}
	}
	return err
}
