package skillimports

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Scratch filenames for the three sides handed to `git merge-file`. They are
// only ever seen inside a disposable directory, but naming them keeps the write
// loop and the argument order impossible to transpose.
const (
	sideNameLocal = "local"
	sideNameBase  = "base"
	sideNameOther = "other"
)

// MergeConflict is one path that could not be reconciled automatically.
type MergeConflict struct {
	// Path is the skill-relative path that conflicts.
	Path string
	// Reason explains, in the user's terms, why the change could not be applied.
	Reason string
}

// MergeLabels name the three sides in conflict output and messages.
type MergeLabels struct {
	Base  string
	Local string
	Other string
}

// TextMerger merges one text file's three sides. It returns the merged bytes and
// whether the merge conflicted.
type TextMerger interface {
	MergeText(ctx context.Context, base []byte, local []byte, other []byte, labels MergeLabels) (merged []byte, conflicted bool, err error)
}

// GitTextMerger merges text with `git merge-file --diff3`, the same engine Git
// itself uses, so a merge Agent Layer accepts is one Git would accept.
type GitTextMerger struct {
	Runner GitRunner
	// TempDir is where the three sides are written for the merge. It must be a
	// directory the caller owns.
	TempDir string
}

// MergeText runs `git merge-file -p --diff3` over the three sides. Conflicts are
// reported as conflicts, never as merged content carrying markers.
func (m GitTextMerger) MergeText(ctx context.Context, base []byte, local []byte, other []byte, labels MergeLabels) ([]byte, bool, error) {
	dir, err := os.MkdirTemp(m.TempDir, "merge-")
	if err != nil {
		return nil, false, fmt.Errorf("create merge scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	sides := []struct {
		name string
		data []byte
	}{{sideNameLocal, local}, {sideNameBase, base}, {sideNameOther, other}}
	paths := make([]string, 0, len(sides))
	for _, side := range sides {
		sidePath := filepath.Join(dir, side.name)
		if err := os.WriteFile(sidePath, side.data, RegularFileMode); err != nil {
			return nil, false, fmt.Errorf("write merge input %s: %w", sidePath, err)
		}
		paths = append(paths, sidePath)
	}

	args := []string{
		"merge-file", "-p", "--diff3",
		"-L", labels.Local, "-L", labels.Base, "-L", labels.Other,
		paths[0], paths[1], paths[2],
	}
	out, err := m.Runner.Run(ctx, dir, args...)
	if err == nil {
		return out, false, nil
	}
	// git merge-file exits with the number of conflicts (1-127). Anything at or
	// above 128 is a real failure, not a merge outcome.
	var gitErr *GitError
	if errors.As(err, &gitErr) && gitErr.ExitCode > 0 && gitErr.ExitCode < 128 {
		return nil, true, nil
	}
	return nil, false, err
}

// MergeTrees reconciles three canonical trees path by path.
//
// A path changed on only one side applies cleanly. Identical changes on both
// sides apply cleanly. Incompatible changes to the same path, delete/modify
// cases, and non-text files changed on both sides are conflicts. A rename is
// seen as a deletion plus an addition, which is exactly how the path-wise rules
// treat it.
func MergeTrees(
	ctx context.Context,
	base *Tree,
	local *Tree,
	other *Tree,
	labels MergeLabels,
	merger TextMerger,
) (*Tree, []MergeConflict, error) {
	paths := unionPaths(base, local, other)
	merged := make([]File, 0, len(paths))
	var conflicts []MergeConflict

	for _, p := range paths {
		baseFile, hasBase := base.Lookup(p)
		localFile, hasLocal := local.Lookup(p)
		otherFile, hasOther := other.Lookup(p)

		if filesEqual(localFile, hasLocal, otherFile, hasOther) {
			if hasLocal {
				merged = append(merged, localFile)
			}
			continue
		}
		if !hasBase {
			switch {
			case hasLocal && !hasOther:
				merged = append(merged, localFile)
			case hasOther && !hasLocal:
				merged = append(merged, otherFile)
			default:
				conflicts = append(conflicts, MergeConflict{
					Path:   p,
					Reason: fmt.Sprintf("added with different content in %s and %s", labels.Local, labels.Other),
				})
			}
			continue
		}
		if filesEqual(localFile, hasLocal, baseFile, true) {
			// Untouched locally: the other side's change applies, including a delete.
			if hasOther {
				merged = append(merged, otherFile)
			}
			continue
		}
		if filesEqual(otherFile, hasOther, baseFile, true) {
			// Untouched on the other side: the local change applies, including a delete.
			if hasLocal {
				merged = append(merged, localFile)
			}
			continue
		}
		if !hasLocal || !hasOther {
			deleted, modified := labels.Local, labels.Other
			if hasLocal {
				deleted, modified = labels.Other, labels.Local
			}
			conflicts = append(conflicts, MergeConflict{
				Path:   p,
				Reason: fmt.Sprintf("deleted in %s and modified in %s", deleted, modified),
			})
			continue
		}
		if localFile.Executable != otherFile.Executable {
			conflicts = append(conflicts, MergeConflict{
				Path:   p,
				Reason: fmt.Sprintf("executable bit changed differently in %s and %s", labels.Local, labels.Other),
			})
			continue
		}
		if IsBinary(baseFile.Data) || IsBinary(localFile.Data) || IsBinary(otherFile.Data) {
			conflicts = append(conflicts, MergeConflict{
				Path:   p,
				Reason: fmt.Sprintf("binary file changed in both %s and %s", labels.Local, labels.Other),
			})
			continue
		}
		mergedData, conflicted, err := merger.MergeText(ctx, baseFile.Data, localFile.Data, otherFile.Data, labels)
		if err != nil {
			return nil, nil, fmt.Errorf("merge %s: %w", p, err)
		}
		if conflicted {
			conflicts = append(conflicts, MergeConflict{
				Path:   p,
				Reason: fmt.Sprintf("changed incompatibly in %s and %s", labels.Local, labels.Other),
			})
			continue
		}
		merged = append(merged, File{Path: p, Data: mergedData, Executable: localFile.Executable})
	}

	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
		return nil, conflicts, nil
	}
	tree, err := NewTree(merged)
	if err != nil {
		return nil, nil, err
	}
	return tree, nil, nil
}

// filesEqual compares two optional files by presence, bytes, and executable bit.
func filesEqual(left File, hasLeft bool, right File, hasRight bool) bool {
	if hasLeft != hasRight {
		return false
	}
	if !hasLeft {
		return true
	}
	return left.Executable == right.Executable && bytes.Equal(left.Data, right.Data)
}

// unionPaths returns every path present in any of the three trees, sorted.
func unionPaths(trees ...*Tree) []string {
	set := map[string]struct{}{}
	for _, tree := range trees {
		for _, p := range tree.Paths() {
			set[p] = struct{}{}
		}
	}
	return sortedKeys(set)
}

// FormatConflicts renders conflicts as an actionable multi-line message.
func FormatConflicts(conflicts []MergeConflict) string {
	lines := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		lines = append(lines, fmt.Sprintf("  %s: %s", conflict.Path, conflict.Reason))
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	var builder bytes.Buffer
	for i, line := range lines {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(line)
	}
	return builder.String()
}
