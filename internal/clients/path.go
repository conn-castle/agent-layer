package clients

import (
	"os"
	"path/filepath"
	"strings"
)

// SamePath compares existing filesystem objects by identity. For missing leaves,
// it also recognizes identical leaf names under physically equivalent parents.
// Case-only aliases require an existing object; names are never case-folded.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	if ae == nil && be == nil {
		return os.SameFile(ai, bi)
	}
	a, b = ResolvePath(a), ResolvePath(b)
	if a == b {
		return true
	}
	// EvalSymlinks need not normalize the spelling of an existing case alias.
	// Compare parent objects when the leaf spelling is identical and missing.
	ap, an := filepath.Split(strings.TrimRight(a, string(os.PathSeparator)))
	bp, bn := filepath.Split(strings.TrimRight(b, string(os.PathSeparator)))
	if an != bn || !os.IsNotExist(ae) || !os.IsNotExist(be) {
		return false
	}
	ai, ae = os.Stat(ap)
	bi, be = os.Stat(bp)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

// ResolvePath makes a path absolute without cleaning away symlink/.. pairs.
// Resolve existing components physically, including parents of missing leaves.
// If resolution fails, retain the unresolved suffix for the kernel to interpret.
func ResolvePath(path string) string {
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return path
		}
		path = cwd + string(os.PathSeparator) + path
	}
	return resolvePhysical(path)
}

func resolvePhysical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	trimmed := strings.TrimRight(path, string(os.PathSeparator))
	parent, leaf := filepath.Split(trimmed)
	if leaf == "" || parent == "" {
		return path
	}
	resolvedParent := resolvePhysical(parent)
	// Concatenation, rather than Join, preserves unresolved components before .. .
	return strings.TrimRight(resolvedParent, string(os.PathSeparator)) + string(os.PathSeparator) + leaf
}

// ResolveHome resolves relative provider homes from the caller's working
// directory. Absolute user overrides retain their spelling.
func ResolveHome(current string) string {
	if current == "" || filepath.IsAbs(current) {
		return current
	}
	return ResolvePath(current)
}

// ResolveRelativeHome preserves the project-home shorthand from subfolders:
// the home name with only dot or empty path components selects the project
// home when the caller-relative entry is absent. Other relative values retain
// physical symlink-before-.. semantics; they are never lexically cleaned.
func ResolveRelativeHome(root, current, name string) string {
	selected := ResolveHome(current)
	if current == "" || filepath.IsAbs(current) {
		return selected
	}
	if homeShorthand(current, name) {
		// Inspect the bare caller-relative entry without following it. Trailing
		// dot components would traverse a dangling symlink and report it absent.
		if _, err := os.Lstat(name); os.IsNotExist(err) {
			return filepath.Join(root, name)
		}
	}
	return selected
}

// homeShorthand recognizes spelling variations without cleaning .. or other
// components, whose meaning may depend on the filesystem.
func homeShorthand(current, name string) bool {
	found := false
	for _, part := range strings.FieldsFunc(current, func(r rune) bool { return r == '/' || r == rune(os.PathSeparator) }) {
		switch part {
		case ".":
		case name:
			if found {
				return false
			}
			found = true
		default:
			return false
		}
	}
	return found
}

// ClearStaleHome clears this project's managed home and normalizes any other
// relative override before a child changes its working directory.
func ClearStaleHome(root string, env []string, key, name string) []string {
	current, ok := GetEnv(env, key)
	if !ok {
		return env
	}
	selected := ResolveRelativeHome(root, current, name)
	if current != "" && SamePath(selected, filepath.Join(root, name)) {
		return UnsetEnv(env, key)
	}
	return SetUniqueEnv(env, key, selected)
}
