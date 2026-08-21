package gitrepo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// DiffTrees returns an ordinary Git unified diff of two skill trees. Prefixes
// are `a/<from>/` and `b/<to>/`. Identical trees produce no output.
func (r *Runner) DiffTrees(ctx context.Context, fromName string, from skilltree.Tree, toName string, to skilltree.Tree) ([]byte, error) {
	if err := validateDiffSideName(fromName); err != nil {
		return nil, err
	}
	if err := validateDiffSideName(toName); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "al-skill-diff-")
	if err != nil {
		return nil, fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if _, err := r.run(ctx, dir, "init", "--quiet"); err != nil {
		return nil, err
	}
	fromTree, err := r.writeSkillTree(ctx, dir, from)
	if err != nil {
		return nil, err
	}
	toTree, err := r.writeSkillTree(ctx, dir, to)
	if err != nil {
		return nil, err
	}
	if fromTree == toTree {
		return nil, nil
	}
	output, err := r.run(ctx, dir,
		"diff-tree", "-r", "-p", "--no-color", "--no-ext-diff", "--no-renames",
		"--src-prefix=a/"+fromName+"/",
		"--dst-prefix=b/"+toName+"/",
		fromTree, toTree)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func validateDiffSideName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\ \t\r\n") {
		return fmt.Errorf("invalid diff side label %q", name)
	}
	return nil
}

// writeSkillTree stores a skill tree as a Git tree object and returns its id.
func (r *Runner) writeSkillTree(ctx context.Context, dir string, tree skilltree.Tree) (string, error) {
	if _, err := r.run(ctx, dir, "read-tree", "--empty"); err != nil {
		return "", err
	}
	for _, file := range tree.Files() {
		if err := skilltree.ValidateRelativePath(file.Path); err != nil {
			return "", fmt.Errorf("skill path %q is unsafe: %w", file.Path, err)
		}
		object, err := r.runInput(ctx, dir, file.Data, "hash-object", "-w", "--no-filters", "--stdin")
		if err != nil {
			return "", fmt.Errorf("failed to write blob for %s: %w", file.Path, err)
		}
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		if _, err := r.run(ctx, dir, "update-index", "--add", "--cacheinfo", mode, strings.TrimSpace(string(object)), file.Path); err != nil {
			return "", fmt.Errorf("failed to stage %s: %w", file.Path, err)
		}
	}
	written, err := r.run(ctx, dir, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(written)), nil
}

// readSkillTreeObject reads a Git tree object as a canonical skill tree.
func (r *Runner) readSkillTreeObject(ctx context.Context, dir string, treeID string) (skilltree.Tree, error) {
	output, err := r.run(ctx, dir, "ls-tree", "-r", "-z", "--full-tree", treeID)
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
			return skilltree.Tree{}, fmt.Errorf("%s is a gitlink (submodule); imported skills may contain only directories and regular files", name)
		case mode == "120000":
			return skilltree.Tree{}, fmt.Errorf("%s is a symbolic link; imported skills may contain only directories and regular files", name)
		case objectType != gitObjectBlob:
			return skilltree.Tree{}, fmt.Errorf("%s is an unsupported git object type %q", name, objectType)
		}
		data, catErr := r.run(ctx, dir, "cat-file", gitObjectBlob, object)
		if catErr != nil {
			return skilltree.Tree{}, catErr
		}
		files = append(files, skilltree.File{Path: name, Data: data, Executable: mode == "100755"})
	}
	return skilltree.NewTree(files)
}
