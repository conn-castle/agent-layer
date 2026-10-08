package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if err := writeNeutralAttributes(dir); err != nil {
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

// writeNeutralAttributes makes Git ignore attributes from the skill and the
// user's attributes file in the synthetic repository at dir. info/attributes
// outranks both. Unspecified merge/diff/eol keep Git's defaults so a skill
// cannot select a globally configured custom driver or hide a change as
// binary; -text/-ident/-filter still disable conversion and ident expansion.
func writeNeutralAttributes(dir string) error {
	infoDir := filepath.Join(dir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o750); err != nil {
		return fmt.Errorf("failed to create git attributes directory: %w", err)
	}
	attributes := []byte("* -text -ident -filter !eol !merge !diff\n")
	if err := os.WriteFile(filepath.Join(infoDir, "attributes"), attributes, 0o600); err != nil {
		return fmt.Errorf("failed to write git attributes: %w", err)
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
		if err := r.stageBlob(ctx, dir, file.Path, file, ""); err != nil {
			return "", err
		}
	}
	written, err := r.run(ctx, dir, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(written)), nil
}

// stageBlob writes file's bytes as a Git blob and stages it at indexPath with
// file's executable mode. where qualifies the staging failure message.
func (r *Runner) stageBlob(ctx context.Context, dir string, indexPath string, file skilltree.File, where string) error {
	object, err := r.runInput(ctx, dir, file.Data, "hash-object", "-w", "--no-filters", "--stdin")
	if err != nil {
		return fmt.Errorf("failed to write blob for %s: %w", indexPath, err)
	}
	mode := "100644"
	if file.Executable {
		mode = "100755"
	}
	if _, err := r.run(ctx, dir, "update-index", "--add", "--cacheinfo", mode, strings.TrimSpace(string(object)), indexPath); err != nil {
		return fmt.Errorf("failed to stage %s%s: %w", indexPath, where, err)
	}
	return nil
}

// readSkillTreeObject reads a Git tree-ish as a canonical skill tree. display
// prefixes entry names in ordinary rejection messages. A non-nil reject
// replaces those messages and also rejects ignored artifacts instead of
// dropping them; it receives the entry name and the unsupported kind.
func (r *Runner) readSkillTreeObject(ctx context.Context, dir string, treeish string, display string, reject func(name string, kind string) error) (skilltree.Tree, error) {
	output, err := r.run(ctx, dir, "ls-tree", "-r", "-z", "--full-tree", treeish)
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
			if reject != nil {
				return skilltree.Tree{}, reject(name, "artifact")
			}
			continue
		}
		switch {
		case objectType == gitObjectCommit:
			if reject != nil {
				return skilltree.Tree{}, reject(name, "gitlink (submodule)")
			}
			return skilltree.Tree{}, fmt.Errorf("%s%s is a gitlink (submodule); imported skills may contain only directories and regular files", display, name)
		case mode == "120000":
			if reject != nil {
				return skilltree.Tree{}, reject(name, "symbolic link")
			}
			return skilltree.Tree{}, fmt.Errorf("%s%s is a symbolic link; imported skills may contain only directories and regular files", display, name)
		case objectType != gitObjectBlob:
			if reject != nil {
				return skilltree.Tree{}, reject(name, fmt.Sprintf("Git object type %q", objectType))
			}
			return skilltree.Tree{}, fmt.Errorf("%s%s is an unsupported git object type %q", display, name, objectType)
		}
		data, catErr := r.run(ctx, dir, "cat-file", gitObjectBlob, object)
		if catErr != nil {
			return skilltree.Tree{}, catErr
		}
		files = append(files, skilltree.File{Path: name, Data: data, Executable: mode == "100755"})
	}
	return skilltree.NewTree(files)
}
