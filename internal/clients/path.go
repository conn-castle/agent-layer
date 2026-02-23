package clients

import "path/filepath"

// SamePath returns true if two paths resolve to the same filesystem location,
// accounting for symlinks and relative paths.
func SamePath(a, b string) bool {
	return ResolvePath(a) == ResolvePath(b)
}

// ResolvePath returns the absolute, symlink-resolved form of a path.
// If resolution fails at any step, it returns the best result available.
func ResolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return eval
	}
	return abs
}
