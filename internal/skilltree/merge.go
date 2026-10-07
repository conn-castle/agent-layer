package skilltree

import (
	"bytes"
	"fmt"
	"path"
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
	// ConflictBinary means both sides changed the content of a non-text file
	// differently.
	ConflictBinary ConflictKind = "binary"
	// ConflictMode means both sides added the same path with different
	// executable bits. With a base, a one-sided change to the bit applies and a
	// two-sided change agrees, so the bit alone never conflicts.
	ConflictMode ConflictKind = "mode"
	// ConflictFileDirectory means one side has a file where the other side
	// has files beneath a same-named directory.
	ConflictFileDirectory ConflictKind = "file/directory"
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
// preference. As in Git, a file's content and executable bit merge
// independently, so a content change on one side combines with a mode change
// on the other. Renames are handled as the deletion plus addition they are
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
		default:
			mergedFile, kind, err := mergeChangedFile(filePath, baseFile, hasBase, localFile, remoteFile, mergeText)
			if err != nil {
				return Tree{}, nil, err
			}
			if kind != "" {
				conflicts = append(conflicts, Conflict{Path: filePath, Kind: kind})
				continue
			}
			merged = append(merged, mergedFile)
		}
	}
	conflicts = append(conflicts, fileDirectoryConflicts(merged)...)

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

// mergeChangedFile reconciles a path both sides kept but changed differently.
// It returns the merged file, or the kind of conflict that prevents one.
func mergeChangedFile(filePath string, baseFile File, hasBase bool, localFile, remoteFile File, mergeText TextMerger) (File, ConflictKind, error) {
	var baseData []byte
	if hasBase {
		baseData = baseFile.Data
	}

	data := localFile.Data
	bothChangedData := false
	switch {
	case hasBase && bytes.Equal(localFile.Data, baseFile.Data):
		data = remoteFile.Data
	case hasBase && bytes.Equal(remoteFile.Data, baseFile.Data), bytes.Equal(localFile.Data, remoteFile.Data):
		// Only local changed the content, or both changed it the same way.
	default:
		bothChangedData = true
	}
	if bothChangedData && (!isText(localFile.Data) || !isText(remoteFile.Data) || (hasBase && !isText(baseFile.Data))) {
		return File{}, ConflictBinary, nil
	}

	executable := localFile.Executable
	switch {
	case !hasBase && localFile.Executable != remoteFile.Executable:
		return File{}, ConflictMode, nil
	case hasBase && localFile.Executable == baseFile.Executable:
		executable = remoteFile.Executable
	}

	if bothChangedData {
		mergedData, conflicted, err := mergeText(baseData, localFile.Data, remoteFile.Data)
		if err != nil {
			return File{}, "", fmt.Errorf("failed to merge %s: %w", filePath, err)
		}
		if conflicted {
			return File{}, ConflictContent, nil
		}
		data = mergedData
	}
	return File{Path: filePath, Data: data, Executable: executable}, "", nil
}

// fileDirectoryConflicts reports each merged file that is also a parent
// directory of another merged file. Paths are reconciled independently, so a
// file added on one side and a same-named directory added on the other both
// survive the per-path merge but cannot coexist on disk or in a Git tree.
func fileDirectoryConflicts(files []File) []Conflict {
	filePaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		filePaths[file.Path] = struct{}{}
	}
	var conflicts []Conflict
	for _, file := range files {
		for dir := path.Dir(file.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
			if _, isFile := filePaths[dir]; isFile {
				// Report each colliding file once.
				delete(filePaths, dir)
				conflicts = append(conflicts, Conflict{Path: dir, Kind: ConflictFileDirectory})
			}
		}
	}
	return conflicts
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
