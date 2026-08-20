package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const privateDirPerm os.FileMode = 0o700

// EnsurePrivateDir creates path as a real owner-only directory (0700).
// An existing directory that grants group or other access is tightened to
// 0700. Symlinks and non-directories fail rather than being followed or
// replaced.
//
// Parent directories may still be created with MkdirAll. The final component
// is created, inspected, and chmod'd through a parent-directory handle using
// no-follow operations, so a replacement symlink cannot redirect those
// mutations. Intermediate path components may be symlinks (required on
// platforms where /tmp or /var is a symlink).
func EnsurePrivateDir(path string) error {
	parent, name, err := splitPrivateDirPath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(parent, privateDirPerm); err != nil {
		return fmt.Errorf(messages.FsutilPrivateDirCreateFmt, path, err)
	}
	return ensurePrivateDir(path, parent, name)
}

func splitPrivateDirPath(path string) (parent, name string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf(messages.FsutilPrivateDirInvalidPathFmt, path)
	}
	cleaned := filepath.Clean(path)
	name = filepath.Base(cleaned)
	parent = filepath.Dir(cleaned)
	if !validPrivateDirName(name) {
		return "", "", fmt.Errorf(messages.FsutilPrivateDirInvalidPathFmt, path)
	}
	return parent, name, nil
}

func validPrivateDirName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return false
	}
	return true
}
