package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// ResolvePaddedPath returns path verbatim when an entry with that exact name
// exists, and otherwise returns it with surrounding whitespace trimmed. POSIX
// names may legally begin or end with whitespace, so an existing literal entry
// always wins over the trimmed spelling; padding is only treated as accidental
// when nothing exists at the literal path. Lstat is used so a literal symlink,
// even a dangling one, is still the selected entry rather than a sibling. Any
// failure other than nonexistence also keeps the literal path, so the caller
// reports that failure instead of silently using a different entry.
func ResolvePaddedPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == path {
		return path
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return path
	}
	return trimmed
}
