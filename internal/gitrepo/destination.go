package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
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
// upstream contribution. It can fetch the destination base and any locked
// source commits needed for reconciliation without checking out either tree.
type Destination struct {
	runner *Runner
	dir    string
	// repository carries both the configured text used in every message and the
	// resolved value handed to git.
	repository Repository
}

// OpenDestination initializes an isolated working repository under workDir for
// the destination repository.
func OpenDestination(ctx context.Context, runner *Runner, workDir string, repository Repository) (*Destination, error) {
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

// Repository returns the configured destination repository reference, with any
// placeholder text intact.
func (d *Destination) Repository() string { return d.repository.String() }

// DefaultBranch resolves the destination repository's default branch name.
func (d *Destination) DefaultBranch(ctx context.Context) (string, error) {
	output, err := d.runner.run(ctx, d.dir, "ls-remote", "--symref", "--", d.repository.git, "HEAD")
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
// by creating it from the destination's default-branch commit.
func (d *Destination) Head(ctx context.Context, branch string) (commit string, exists bool, err error) {
	output, err := d.runner.run(ctx, d.dir, "ls-remote", "--", d.repository.git, "refs/heads/"+branch)
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
func (d *Destination) FetchCommit(ctx context.Context, repository Repository, commit string) error {
	exists, err := commitExists(ctx, d.runner, d.dir, commit)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := d.runner.run(ctx, d.dir, "fetch", "--quiet", "--no-tags", "--", repository.git, commit); err != nil {
		// Some servers refuse to serve an arbitrary object id; mirroring the
		// refs makes reachable commits available instead. Both diagnostics are
		// reported because the fallback failure is often the one that names the
		// real cause, such as an authentication problem.
		if _, mirrorErr := d.runner.run(ctx, d.dir, "fetch", "--quiet", "--", repository.git,
			"+refs/heads/*:refs/remotes/fetched/*", "+refs/tags/*:refs/tags/*"); mirrorErr != nil {
			return errors.Join(err, mirrorErr)
		}
	}
	exists, err = commitExists(ctx, d.runner, d.dir, commit)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("commit %s could not be fetched from %s", commit, repository)
	}
	return nil
}

// ReadTree returns the content of a repository path at a commit that is already
// available in the destination working repository.
func (d *Destination) ReadTree(ctx context.Context, commit string, repoPath string) (skilltree.Tree, error) {
	source := &Source{runner: d.runner, dir: d.dir, repository: d.repository, fetched: true}
	return source.readTree(ctx, commit, repoPath, true)
}

// Update is one skill's desired destination content.
type Update struct {
	// Path is the destination repository-relative skill root.
	Path string
	// Tree is the exact desired content at Path.
	Tree skilltree.Tree
}

// Publish loads base into Git's index, replaces each update's path with exact
// blobs, creates a commit through Git's object database, and pushes it to
// branch without force. It never checks out remote-controlled content.
//
// It returns the pushed commit. Publish never rewrites history and never falls
// back to another branch or repository.
func (d *Destination) Publish(ctx context.Context, base string, branch string, updates []Update, message string) (string, error) {
	if len(updates) == 0 {
		return "", fmt.Errorf("publishing requires at least one skill update")
	}
	for _, update := range updates {
		if err := skilltree.ValidateRelativePath(update.Path); err != nil {
			return "", fmt.Errorf("destination path %q is unsafe: %w", update.Path, err)
		}
		if _, err := d.ReadTree(ctx, base, update.Path); err != nil {
			return "", err
		}
	}
	if _, err := d.runner.run(ctx, d.dir, "read-tree", base); err != nil {
		return "", err
	}
	for _, update := range updates {
		if err := d.replaceIndexTree(ctx, update); err != nil {
			return "", err
		}
	}
	tree, err := d.runner.run(ctx, d.dir, "write-tree")
	if err != nil {
		return "", err
	}
	commit, err := d.runner.run(ctx, d.dir,
		"-c", "commit.gpgSign=false",
		"commit-tree", strings.TrimSpace(string(tree)), "-p", base, "-m", message)
	if err != nil {
		return "", annotateIdentityFailure(err)
	}
	commitID := strings.TrimSpace(string(commit))
	if _, err := d.runner.run(ctx, d.dir, "push", "--", d.repository.git, commitID+":refs/heads/"+branch); err != nil {
		return "", err
	}
	return commitID, nil
}

// replaceIndexTree removes every indexed entry below update.Path and inserts
// the desired tree as exact Git blobs. Paths and modes come only from the
// already validated canonical skill tree.
func (d *Destination) replaceIndexTree(ctx context.Context, update Update) error {
	tracked, err := d.runner.run(ctx, d.dir, "ls-files", "-z", "--", literalPathspec(update.Path))
	if err != nil {
		return fmt.Errorf("failed to list destination index path %s: %w", update.Path, err)
	}
	for _, existing := range strings.Split(string(tracked), "\x00") {
		if existing == "" {
			continue
		}
		if _, err := d.runner.run(ctx, d.dir, "update-index", "--force-remove", "--", existing); err != nil {
			return fmt.Errorf("failed to remove %s from the destination index: %w", existing, err)
		}
	}

	for _, file := range update.Tree.Files() {
		indexPath := path.Join(update.Path, file.Path)
		if err := skilltree.ValidateRelativePath(indexPath); err != nil {
			return fmt.Errorf("destination file path %q is unsafe: %w", indexPath, err)
		}
		object, err := d.runner.runInput(ctx, d.dir, file.Data, "hash-object", "-w", "--no-filters", "--stdin")
		if err != nil {
			return fmt.Errorf("failed to write blob for %s: %w", indexPath, err)
		}
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		if _, err := d.runner.run(ctx, d.dir, "update-index", "--add", "--cacheinfo", mode, strings.TrimSpace(string(object)), indexPath); err != nil {
			return fmt.Errorf("failed to stage %s in the destination index: %w", indexPath, err)
		}
	}
	return nil
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
