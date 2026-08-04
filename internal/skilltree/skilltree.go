// Package skilltree owns the one canonical definition of an Agent Skill's
// content: which filesystem nodes belong to a skill, how they are hashed, how
// they are materialized, and how two divergent versions are merged.
//
// Every skill content operation — import, local change detection, pull merge,
// push delta, and ordinary projection — reads trees through this package so a
// permissive copier can never drift from the strict import policy.
package skilltree

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// SkillManifestName is the canonical skill manifest filename. Imports require
// it exactly; no lowercase fallback is accepted for imported sources.
const SkillManifestName = "SKILL.md"

// ignoredNames are the only filesystem artifacts excluded from a skill tree.
// Everything else — including hidden files — is part of the skill.
var ignoredNames = map[string]struct{}{
	".git":      {},
	".DS_Store": {},
	"Thumbs.db": {},
}

// NodePolicy selects how non-directory, non-regular filesystem nodes are
// treated while reading a tree.
type NodePolicy int

const (
	// PolicyStrict rejects symlinks, gitlinks, submodules, devices, sockets,
	// and every other node type without dereferencing them. Imported skill
	// trees use this policy so an import can never smuggle in a link.
	PolicyStrict NodePolicy = iota
	// PolicyLenient skips symlinks instead of failing. User-managed skill
	// sources use this policy so projects that already contain a symlinked
	// resource keep syncing exactly as before.
	PolicyLenient
)

// FS is the filesystem surface a tree read needs. It is deliberately small so
// the sync package's injectable System and a plain OS implementation both
// satisfy it.
type FS interface {
	Lstat(name string) (os.FileInfo, error)
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// OSFS reads through the real filesystem.
type OSFS struct{}

// Lstat returns file information without following symlinks.
func (OSFS) Lstat(name string) (os.FileInfo, error) { return os.Lstat(name) }

// ReadDir lists a directory's entries.
func (OSFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }

// ReadFile returns a file's exact bytes.
func (OSFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name) // #nosec G304 -- callers pass paths rooted at a resolved skill directory.
}

// File is one regular file in a skill tree.
type File struct {
	// Path is the slash-normalized path relative to the skill root.
	Path string
	// Data is the file's exact bytes.
	Data []byte
	// Executable records whether any execute bit is set.
	Executable bool
}

// Tree is a skill's complete regular-file content, sorted by path.
//
// Directories are intentionally absent: an empty directory carries no skill
// content and is not representable in Git, so including it would make local
// and upstream hashes disagree for identical skills.
type Tree struct {
	files []File
}

// NewTree builds a tree from files, sorting them and rejecting duplicates.
func NewTree(files []File) (Tree, error) {
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Path == sorted[i-1].Path {
			return Tree{}, fmt.Errorf("skill tree contains duplicate path %q", sorted[i].Path)
		}
	}
	return Tree{files: sorted}, nil
}

// Files returns the tree's files in sorted path order.
func (t Tree) Files() []File { return t.files }

// Len returns the number of files in the tree.
func (t Tree) Len() int { return len(t.files) }

// IsEmpty reports whether the tree holds no files.
func (t Tree) IsEmpty() bool { return len(t.files) == 0 }

// Paths returns every file path in sorted order.
func (t Tree) Paths() []string {
	paths := make([]string, 0, len(t.files))
	for _, file := range t.files {
		paths = append(paths, file.Path)
	}
	return paths
}

// File returns the file recorded at a path.
func (t Tree) File(filePath string) (File, bool) {
	index := sort.Search(len(t.files), func(i int) bool { return t.files[i].Path >= filePath })
	if index < len(t.files) && t.files[index].Path == filePath {
		return t.files[index], true
	}
	return File{}, false
}

// Hash returns the canonical content hash: sorted slash-normalized relative
// paths, exact file bytes, and the executable bit. The encoding is
// length-prefixed so no combination of paths and content can collide by
// concatenation.
func (t Tree) Hash() string {
	digest := sha256.New()
	writeChunk := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(value)
	}
	for _, file := range t.files {
		writeChunk([]byte(file.Path))
		mode := []byte{0}
		if file.Executable {
			mode = []byte{1}
		}
		writeChunk(mode)
		writeChunk(file.Data)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

// Equal reports whether two trees carry identical content.
func (t Tree) Equal(other Tree) bool { return t.Hash() == other.Hash() }

// Read enumerates the skill tree rooted at dir.
//
// It walks with Lstat so a link is classified without being followed, includes
// every regular file (hidden files included), ignores only `.git`, `.DS_Store`,
// and `Thumbs.db`, and applies policy to every other node type. A missing root
// directory yields an empty tree so callers can distinguish "no content" from a
// read failure by checking the directory themselves.
func Read(fsys FS, dir string, policy NodePolicy) (Tree, error) {
	var files []File
	if err := readInto(fsys, dir, "", policy, &files); err != nil {
		return Tree{}, err
	}
	return NewTree(files)
}

func readInto(fsys FS, dir string, relativeDir string, policy NodePolicy, files *[]File) error {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		// FS is injectable, so a implementation may wrap its error. errors.Is
		// keeps a wrapped missing directory reading as the documented empty
		// tree instead of a hard failure.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		name := entry.Name()
		if _, ignored := ignoredNames[name]; ignored {
			continue
		}
		nodePath := filepath.Join(dir, name)
		relativePath := name
		if relativeDir != "" {
			relativePath = relativeDir + "/" + name
		}

		info, err := fsys.Lstat(nodePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", nodePath, err)
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if err := readInto(fsys, nodePath, relativePath, policy, files); err != nil {
				return err
			}
		case mode.IsRegular():
			data, err := fsys.ReadFile(nodePath)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", nodePath, err)
			}
			*files = append(*files, File{
				Path:       relativePath,
				Data:       data,
				Executable: mode.Perm()&0o111 != 0,
			})
		case policy == PolicyLenient && mode&os.ModeSymlink != 0:
			// Existing user-managed skill sources may contain symlinks that
			// predate imports. Skipping them keeps ordinary sync working.
			continue
		default:
			return fmt.Errorf("%s is a %s; skill trees may contain only directories and regular files", nodePath, describeNode(mode))
		}
	}
	return nil
}

func describeNode(mode fs.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symbolic link"
	case mode&os.ModeDevice != 0:
		return "device node"
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeIrregular != 0:
		return "non-regular file"
	default:
		return "unsupported filesystem node"
	}
}

// FileMode returns the permission bits used when materializing a tree file.
func (f File) FileMode() os.FileMode {
	if f.Executable {
		return 0o755
	}
	return 0o644
}

// Materialize writes the tree into dir, which must already exist and is
// expected to be an empty staging directory owned by the caller. It never
// writes outside dir.
func Materialize(tree Tree, dir string) error {
	for _, file := range tree.Files() {
		if err := ValidateRelativePath(file.Path); err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("failed to create %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, file.Data, file.FileMode()); err != nil { // #nosec G306 -- skill resources preserve their upstream executable bit.
			return fmt.Errorf("failed to write %s: %w", target, err)
		}
	}
	return nil
}

// ValidateRelativePath rejects any tree path that could escape its skill root.
func ValidateRelativePath(value string) error {
	if value == "" {
		return fmt.Errorf("skill tree contains an empty path")
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("skill tree path %q must use '/' as the separator", value)
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf("skill tree path %q must be relative", value)
	}
	if value != path.Clean(value) {
		return fmt.Errorf("skill tree path %q is not normalized", value)
	}
	for _, segment := range strings.Split(value, "/") {
		switch segment {
		case "", ".", "..":
			return fmt.Errorf("skill tree path %q is not normalized", value)
		}
	}
	return nil
}
