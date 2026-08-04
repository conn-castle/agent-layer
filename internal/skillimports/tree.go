package skillimports

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// SkillManifestName is the only accepted skill manifest filename for an import.
const SkillManifestName = "SKILL.md"

// File modes used for materialized skill content. Git records exactly two
// regular-file modes, and both are preserved verbatim.
const (
	// RegularFileMode is the local mode for a Git 100644 blob.
	RegularFileMode os.FileMode = 0o644
	// ExecutableFileMode is the local mode for a Git 100755 blob.
	ExecutableFileMode os.FileMode = 0o755
	// DirectoryMode is the local mode for a materialized directory.
	DirectoryMode os.FileMode = 0o755
)

// IsIgnoredTreeName reports whether a path segment is excluded from the
// canonical skill file set. The list is owned by internal/config so import,
// comparison, merge, projection, and push all read one source.
func IsIgnoredTreeName(name string) bool {
	return config.IsIgnoredSkillResourceName(name)
}

// File is one regular file in a canonical skill tree.
type File struct {
	// Path is the slash-normalized path relative to the skill root.
	Path string
	// Data is the exact file bytes.
	Data []byte
	// Executable records the Git executable bit.
	Executable bool
}

// Mode returns the local file mode for the file's executable bit.
func (f File) Mode() os.FileMode {
	if f.Executable {
		return ExecutableFileMode
	}
	return RegularFileMode
}

// Tree is one skill's complete canonical content: every regular file under the
// skill root, sorted by path. Directories are not represented because Git cannot
// represent an empty one, so the file set is the whole tree.
type Tree struct {
	files []File
}

// NewTree builds a tree from files, sorting them and rejecting duplicates.
func NewTree(files []File) (*Tree, error) {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Path == sorted[i-1].Path {
			return nil, fmt.Errorf("duplicate path %q in skill tree", sorted[i].Path)
		}
	}
	return &Tree{files: sorted}, nil
}

// Files returns the tree's files in canonical order.
func (t *Tree) Files() []File {
	if t == nil {
		return nil
	}
	return t.files
}

// Len reports how many files the tree contains.
func (t *Tree) Len() int {
	if t == nil {
		return 0
	}
	return len(t.files)
}

// Lookup returns the file at a canonical path.
func (t *Tree) Lookup(p string) (File, bool) {
	if t == nil {
		return File{}, false
	}
	index := sort.Search(len(t.files), func(i int) bool { return t.files[i].Path >= p })
	if index < len(t.files) && t.files[index].Path == p {
		return t.files[index], true
	}
	return File{}, false
}

// Paths returns every canonical path in the tree.
func (t *Tree) Paths() []string {
	if t == nil {
		return nil
	}
	paths := make([]string, 0, len(t.files))
	for _, file := range t.files {
		paths = append(paths, file.Path)
	}
	return paths
}

// Hash returns the canonical tree hash: a digest over each file's
// slash-normalized relative path, executable bit, length, and exact bytes, in
// sorted path order. It is the single hash used for import, comparison, merge,
// projection, and push.
func (t *Tree) Hash() string {
	files := make([]skilltree.File, 0, len(t.Files()))
	for _, file := range t.Files() {
		files = append(files, skilltree.File{Path: file.Path, Data: file.Data, Executable: file.Executable})
	}
	return skilltree.Hash(files)
}

// Equal reports whether two trees have identical canonical content.
func (t *Tree) Equal(other *Tree) bool {
	return t.Hash() == other.Hash()
}

// normalizeTreePath validates one canonical relative path.
func normalizeTreePath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty path in skill tree")
	}
	if strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("path %q must use forward slashes", raw)
	}
	cleaned := path.Clean(raw)
	if cleaned != raw {
		return "", fmt.Errorf("path %q is not normalized", raw)
	}
	if strings.HasPrefix(cleaned, "/") || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q escapes the skill root", raw)
	}
	return cleaned, nil
}

// ReadGitTree materializes the canonical tree for one skill root at a commit.
// Only directories and regular files are accepted: symlinks, gitlinks,
// submodules, and any other node type fail loudly and are never dereferenced.
func ReadGitTree(ctx context.Context, space *workspace, commit string, sourcePath string) (*Tree, error) {
	out, err := space.run(ctx, "ls-tree", "-r", "-z", "--full-tree", commit+":"+sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read tree %s at %s: %w", sourcePath, commit, err)
	}
	var files []File
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		mode, objectType, object, name, parseErr := parseLsTreeRecord(record)
		if parseErr != nil {
			return nil, fmt.Errorf("read tree %s at %s: %w", sourcePath, commit, parseErr)
		}
		normalized, normErr := normalizeTreePath(name)
		if normErr != nil {
			return nil, fmt.Errorf("read tree %s at %s: %w", sourcePath, commit, normErr)
		}
		if hasIgnoredSegment(normalized) {
			continue
		}
		switch mode {
		case "100644", "100755":
			if objectType != "blob" {
				return nil, fmt.Errorf(
					"%s/%s is a %s with a regular-file mode; refusing to import an unexpected object type",
					sourcePath, normalized, objectType,
				)
			}
		case "120000":
			return nil, fmt.Errorf(
				"%s/%s is a symlink; Agent Layer imports only directories and regular files and never dereferences links",
				sourcePath, normalized,
			)
		case "160000":
			return nil, fmt.Errorf(
				"%s/%s is a gitlink (submodule); Agent Layer imports only directories and regular files",
				sourcePath, normalized,
			)
		default:
			return nil, fmt.Errorf("%s/%s has unsupported mode %s", sourcePath, normalized, mode)
		}
		data, readErr := space.run(ctx, "cat-file", "blob", object)
		if readErr != nil {
			return nil, fmt.Errorf("read %s/%s at %s: %w", sourcePath, normalized, commit, readErr)
		}
		files = append(files, File{Path: normalized, Data: data, Executable: mode == "100755"})
	}
	return NewTree(files)
}

// parseLsTreeRecord splits one NUL-terminated `git ls-tree -z` record.
func parseLsTreeRecord(record string) (mode string, objectType string, object string, name string, err error) {
	meta, name, ok := strings.Cut(record, "\t")
	if !ok {
		return "", "", "", "", fmt.Errorf("unexpected ls-tree record %q", record)
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return "", "", "", "", fmt.Errorf("unexpected ls-tree metadata %q", meta)
	}
	return fields[0], fields[1], fields[2], name, nil
}

// hasIgnoredSegment reports whether any segment of a canonical path is excluded
// from the skill file set.
func hasIgnoredSegment(p string) bool {
	for _, segment := range strings.Split(p, "/") {
		if IsIgnoredTreeName(segment) {
			return true
		}
	}
	return false
}

// ReadLocalTree materializes the canonical tree for a directory on disk using
// exactly the same file set, ordering, and mode rules as ReadGitTree. Symlinks
// and other irregular nodes fail loudly rather than being followed or skipped.
func ReadLocalTree(dir string) (*Tree, error) {
	var files []File
	err := filepath.WalkDir(dir, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(dir, current)
		if relErr != nil {
			return fmt.Errorf("resolve %s under %s: %w", current, dir, relErr)
		}
		if relative == "." {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		if IsIgnoredTreeName(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", current, infoErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf(
				"%s is not a regular file (mode %s); Agent Layer manages only directories and regular files inside a skill",
				current, info.Mode(),
			)
		}
		// #nosec G304,G122 -- current comes from walking a caller-owned managed
		// directory, and every irregular node (including symlinks) is rejected above
		// before it can be read, so there is no link to follow.
		data, readErr := os.ReadFile(current)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", current, readErr)
		}
		normalized, normErr := normalizeTreePath(slashed)
		if normErr != nil {
			return fmt.Errorf("read %s: %w", current, normErr)
		}
		files = append(files, File{
			Path:       normalized,
			Data:       data,
			Executable: info.Mode().Perm()&0o100 != 0,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewTree(files)
}

// WriteTree materializes a canonical tree into an empty directory.
func WriteTree(tree *Tree, dir string) error {
	if err := os.MkdirAll(dir, DirectoryMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, file := range tree.Files() {
		target := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), DirectoryMode); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, file.Data, file.Mode()); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		// WriteFile only applies the mode when it creates the file, so an existing
		// file keeps its old bit. Setting it explicitly keeps the executable bit
		// part of the canonical content rather than an accident of creation order.
		if err := os.Chmod(target, file.Mode()); err != nil {
			return fmt.Errorf("set mode on %s: %w", target, err)
		}
	}
	return nil
}

// HasManifest reports whether the tree contains the canonical SKILL.md at its root.
func (t *Tree) HasManifest() bool {
	_, ok := t.Lookup(SkillManifestName)
	return ok
}

// binaryInspectionWindow is how many leading bytes are inspected for a NUL byte
// when deciding whether a file is binary. It matches Git's own heuristic bound.
const binaryInspectionWindow = 8 * 1024

// IsBinary reports whether content is treated as binary for merge purposes: a
// NUL byte anywhere in the first binaryInspectionWindow bytes.
func IsBinary(data []byte) bool {
	window := data
	if len(window) > binaryInspectionWindow {
		window = window[:binaryInspectionWindow]
	}
	return bytes.IndexByte(window, 0) >= 0
}
