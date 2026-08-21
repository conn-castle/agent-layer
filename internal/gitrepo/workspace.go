package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Named branches in a skill conflict workspace.
const (
	ConflictBranchBase        = "base"
	ConflictBranchLocal       = "local"
	ConflictBranchUpstream    = "upstream"
	ConflictBranchDestination = "destination"
)

// syntheticIdentity is used only for conflict-workspace plumbing commits so a
// user's Git identity never authors those temporary objects.
var syntheticIdentity = []string{
	"GIT_AUTHOR_NAME=Agent Layer",
	"GIT_AUTHOR_EMAIL=agent-layer@invalid",
	"GIT_COMMITTER_NAME=Agent Layer",
	"GIT_COMMITTER_EMAIL=agent-layer@invalid",
}

const gitFalse = "false"

// ConflictWorkspaceSpec is the three skill trees materialized as a synthetic
// Git repository so an ordinary merge can be finished with git.
type ConflictWorkspaceSpec struct {
	Base         skilltree.Tree
	Local        skilltree.Tree
	Theirs       skilltree.Tree
	TheirsBranch string
}

// CreateConflictWorkspace replaces dir with a synthetic Git repository of one
// skill, checks out local, and runs a no-commit no-rename diff3 merge.
func (r *Runner) CreateConflictWorkspace(ctx context.Context, dir string, spec ConflictWorkspaceSpec) error {
	switch spec.TheirsBranch {
	case ConflictBranchUpstream, ConflictBranchDestination:
	default:
		return fmt.Errorf("conflict workspace theirs branch must be %q or %q", ConflictBranchUpstream, ConflictBranchDestination)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if _, err := r.run(ctx, dir, "init", "--quiet"); err != nil {
		return err
	}
	for _, kv := range [][2]string{
		{"core.autocrlf", gitFalse},
		{"core.eol", "lf"},
		{"core.safecrlf", gitFalse},
		{"core.symlinks", gitFalse},
		{"merge.conflictStyle", "diff3"},
		{"merge.renames", gitFalse},
		{"diff.renames", gitFalse},
		{"user.name", "Agent Layer"},
		{"user.email", "agent-layer@invalid"},
	} {
		if _, err := r.run(ctx, dir, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	infoDir := filepath.Join(dir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o750); err != nil {
		return fmt.Errorf("failed to create conflict workspace attributes directory: %w", err)
	}
	// info/attributes outranks in-tree .gitattributes. Unspecified merge/diff/eol
	// keep Git's defaults so a skill cannot select a globally configured custom
	// driver; -text/-ident/-filter still disable conversion and ident expansion.
	attributes := []byte("* -text -ident -filter !eol !merge !diff\n")
	if err := os.WriteFile(filepath.Join(infoDir, "attributes"), attributes, 0o600); err != nil {
		return fmt.Errorf("failed to write conflict workspace attributes: %w", err)
	}

	baseTree, err := r.writeSkillTree(ctx, dir, spec.Base)
	if err != nil {
		return err
	}
	baseCommit, err := r.commitTree(ctx, dir, baseTree, ConflictBranchBase)
	if err != nil {
		return err
	}
	localTree, err := r.writeSkillTree(ctx, dir, spec.Local)
	if err != nil {
		return err
	}
	localCommit, err := r.commitTree(ctx, dir, localTree, ConflictBranchLocal, baseCommit)
	if err != nil {
		return err
	}
	theirsTree, err := r.writeSkillTree(ctx, dir, spec.Theirs)
	if err != nil {
		return err
	}
	theirsCommit, err := r.commitTree(ctx, dir, theirsTree, spec.TheirsBranch, baseCommit)
	if err != nil {
		return err
	}

	for _, ref := range [][2]string{
		{"refs/heads/" + ConflictBranchBase, baseCommit},
		{"refs/heads/" + ConflictBranchLocal, localCommit},
		{"refs/heads/" + spec.TheirsBranch, theirsCommit},
	} {
		if _, err := r.run(ctx, dir, "update-ref", ref[0], ref[1]); err != nil {
			return err
		}
	}
	if _, err := r.run(ctx, dir, "symbolic-ref", "HEAD", "refs/heads/"+ConflictBranchLocal); err != nil {
		return err
	}
	if _, err := r.run(ctx, dir, "read-tree", "--reset", "-u", "HEAD"); err != nil {
		return err
	}
	// merge.renames=false and -X no-renames disable rename detection. Some Git
	// builds (including Apple Git) reject git merge --no-renames.
	_, _, err = r.runAllowExit(ctx, dir, []int{1},
		"merge", "--no-commit", "--no-ff", "-X", "no-renames", spec.TheirsBranch)
	if err != nil {
		return err
	}
	return nil
}

// ConflictIndexReady reports whether the workspace index is a fully staged
// resolution. Unmerged paths and unstaged tracked changes are refused;
// untracked files are ignored. An aborted or committed merge is refused so
// resolve cannot treat the pre-merge local tree as an accepted result.
func (r *Runner) ConflictIndexReady(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("conflict workspace merge is no longer in progress; finish the merge with git add, or recreate the workspace with the original command")
		}
		return err
	}
	unmerged, err := r.run(ctx, dir, "ls-files", "-u", "-z")
	if err != nil {
		return err
	}
	if len(bytes.Trim(unmerged, "\x00 \t\n\r")) > 0 {
		return fmt.Errorf("conflict workspace still has unmerged git entries; finish the merge and git add the result")
	}
	if _, _, err := r.runAllowExit(ctx, dir, []int{1}, "update-index", "--refresh"); err != nil {
		return err
	}
	_, code, err := r.runAllowExit(ctx, dir, []int{1}, "diff-files", "--quiet")
	if err != nil {
		return err
	}
	if code == 1 {
		return fmt.Errorf("conflict workspace has unstaged tracked changes; git add the resolved files")
	}
	return nil
}

// ReadConflictIndex returns the staged skill tree. The working tree is not
// read, so untracked mergetool files cannot enter the result.
func (r *Runner) ReadConflictIndex(ctx context.Context, dir string) (skilltree.Tree, error) {
	if err := r.ConflictIndexReady(ctx, dir); err != nil {
		return skilltree.Tree{}, err
	}
	treeID, err := r.run(ctx, dir, "write-tree")
	if err != nil {
		return skilltree.Tree{}, err
	}
	return r.readSkillTreeObject(ctx, dir, strings.TrimSpace(string(treeID)))
}

func (r *Runner) commitTree(ctx context.Context, dir string, tree string, message string, parents ...string) (string, error) {
	args := make([]string, 0, 6+2*len(parents))
	args = append(args, "-c", "commit.gpgSign=false", "commit-tree", tree)
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	output, err := r.runEnv(ctx, dir, syntheticIdentity, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
