package clients

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	filepathAbs          = filepath.Abs
	filepathEvalSymlinks = filepath.EvalSymlinks
)

// SamePath returns true if two paths resolve to the same filesystem location,
// accounting for symlinks and relative paths.
func SamePath(a, b string) bool {
	return ResolvePath(a) == ResolvePath(b)
}

// ResolvePath returns the absolute, symlink-resolved form of a path.
// If resolution fails at any step, it returns the best result available.
func ResolvePath(path string) string {
	abs, err := filepathAbs(path)
	if err != nil {
		abs = path
	}
	eval, err := filepathEvalSymlinks(abs)
	if err == nil {
		return eval
	}
	return abs
}

// ResolveRelativeHome returns the absolute form of a relative provider home
// directory, resolved from the caller's directory. Because al runs from any
// project subfolder, a value that cleans to name also selects <root>/name when
// nothing exists at the caller-relative path.
func ResolveRelativeHome(root, current, name string) string {
	expected := filepath.Join(root, name)
	home, err := filepathAbs(current)
	if err != nil {
		return current
	}
	// Compare parents: symlink resolution cannot match aliases of a missing leaf.
	if filepath.Base(home) == name && SamePath(filepath.Dir(home), root) {
		return expected
	}
	if filepath.Clean(current) == name {
		if _, err := os.Lstat(home); errors.Is(err, fs.ErrNotExist) {
			return expected
		}
	}
	return home
}
