package skilltree

import (
	"bytes"
	"fmt"
	"sort"
	"unicode/utf8"
)

// ConflictKind names why a path could not be reconciled automatically.
type ConflictKind string

const (
	// ConflictContent means both sides changed the same text file
	// incompatibly.
	ConflictContent ConflictKind = "content"
	// ConflictDeleteModify means one side deleted a path the other changed.
	ConflictDeleteModify ConflictKind = "delete/modify"
	// ConflictBinary means both sides changed a non-text file differently.
	ConflictBinary ConflictKind = "binary"
	// ConflictMode means both sides changed the executable bit differently.
	ConflictMode ConflictKind = "mode"
)

// Conflict reports one unmergeable path.
type Conflict struct {
	Path string
	Kind ConflictKind
}

// Error renders a stable single-line conflict description.
func (c Conflict) Error() string {
	return fmt.Sprintf("%s (%s)", c.Path, c.Kind)
}

// TextMerger performs a three-way merge of text content. It returns the merged
// bytes and whether the merge produced conflicts. An error means the merge
// could not be attempted at all.
type TextMerger func(base, local, remote []byte) (merged []byte, conflicted bool, err error)

// Merge reconciles local and remote against their common base.
//
// A path changed on only one side applies cleanly, identical changes on both
// sides coalesce, compatible text changes are merged by mergeText, and every
// remaining divergence is reported as a conflict rather than resolved by
// preference. Renames are handled as the deletion plus addition they are
// recorded as, because a skill tree carries no rename metadata.
//
// Merge never partially applies: when any conflict is reported the returned
// tree must be discarded by the caller.
func Merge(base, local, remote Tree, mergeText TextMerger) (Tree, []Conflict, error) {
	if mergeText == nil {
		return Tree{}, nil, fmt.Errorf("a text merger is required to reconcile skill trees")
	}

	paths := unionPaths(base, local, remote)
	merged := make([]File, 0, len(paths))
	var conflicts []Conflict

	for _, filePath := range paths {
		baseFile, hasBase := base.File(filePath)
		localFile, hasLocal := local.File(filePath)
		remoteFile, hasRemote := remote.File(filePath)

		localChanged := !sameFile(baseFile, hasBase, localFile, hasLocal)
		remoteChanged := !sameFile(baseFile, hasBase, remoteFile, hasRemote)

		switch {
		case !localChanged && !remoteChanged:
			if hasBase {
				merged = append(merged, baseFile)
			}
		case localChanged && !remoteChanged:
			if hasLocal {
				merged = append(merged, localFile)
			}
		case !localChanged && remoteChanged:
			if hasRemote {
				merged = append(merged, remoteFile)
			}
		case sameFile(localFile, hasLocal, remoteFile, hasRemote):
			// Both sides made the same change; coalesce it.
			if hasLocal {
				merged = append(merged, localFile)
			}
		case !hasLocal || !hasRemote:
			conflicts = append(conflicts, Conflict{Path: filePath, Kind: ConflictDeleteModify})
		case !isText(localFile.Data) || !isText(remoteFile.Data) || (hasBase && !isText(baseFile.Data)):
			conflicts = append(conflicts, Conflict{Path: filePath, Kind: ConflictBinary})
		case localFile.Executable != remoteFile.Executable:
			conflicts = append(conflicts, Conflict{Path: filePath, Kind: ConflictMode})
		default:
			var baseData []byte
			if hasBase {
				baseData = baseFile.Data
			}
			mergedData, conflicted, err := mergeText(baseData, localFile.Data, remoteFile.Data)
			if err != nil {
				return Tree{}, nil, fmt.Errorf("failed to merge %s: %w", filePath, err)
			}
			if conflicted {
				conflicts = append(conflicts, Conflict{Path: filePath, Kind: ConflictContent})
				continue
			}
			merged = append(merged, File{Path: filePath, Data: mergedData, Executable: localFile.Executable})
		}
	}

	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
		return Tree{}, conflicts, nil
	}
	tree, err := NewTree(merged)
	if err != nil {
		return Tree{}, nil, err
	}
	return tree, nil, nil
}

// Diff reports the paths added, modified, and deleted going from base to next.
type Diff struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// IsEmpty reports whether the two trees carry identical content.
func (d Diff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Modified) == 0 && len(d.Deleted) == 0
}

// Changed returns every changed path in sorted order.
func (d Diff) Changed() []string {
	changed := make([]string, 0, len(d.Added)+len(d.Modified)+len(d.Deleted))
	changed = append(changed, d.Added...)
	changed = append(changed, d.Modified...)
	changed = append(changed, d.Deleted...)
	sort.Strings(changed)
	return changed
}

// Compare returns the file-level delta from base to next.
func Compare(base, next Tree) Diff {
	var diff Diff
	for _, filePath := range unionPaths(base, next, Tree{}) {
		baseFile, hasBase := base.File(filePath)
		nextFile, hasNext := next.File(filePath)
		switch {
		case !hasBase && hasNext:
			diff.Added = append(diff.Added, filePath)
		case hasBase && !hasNext:
			diff.Deleted = append(diff.Deleted, filePath)
		case !sameFile(baseFile, hasBase, nextFile, hasNext):
			diff.Modified = append(diff.Modified, filePath)
		}
	}
	return diff
}

func unionPaths(trees ...Tree) []string {
	seen := make(map[string]struct{})
	for _, tree := range trees {
		for _, file := range tree.Files() {
			seen[file.Path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for filePath := range seen {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	return paths
}

func sameFile(left File, hasLeft bool, right File, hasRight bool) bool {
	if hasLeft != hasRight {
		return false
	}
	if !hasLeft {
		return true
	}
	return left.Executable == right.Executable && bytes.Equal(left.Data, right.Data)
}

// isText reports whether content can be line-merged. Content with a NUL byte
// or invalid UTF-8 is treated as binary, matching the specification's rule that
// non-text files changed on both sides conflict.
func isText(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}
