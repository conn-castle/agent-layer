//go:build !unix

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// POSIX no-follow mkdir/open/fchmod are unavailable. The final component is
// still rejected when Lstat sees a symlink, but chmod remains pathname-based.
func ensurePrivateDir(path, parent, name string) error {
	target := filepath.Join(parent, name)
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.Mkdir(target, privateDirPerm); err != nil {
				return fmt.Errorf(messages.FsutilPrivateDirCreateFmt, path, err)
			}
			return nil
		}
		return fmt.Errorf(messages.FsutilPrivateDirStatFmt, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(messages.FsutilPrivateDirNotDirectoryFmt, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(target, privateDirPerm); err != nil {
			return fmt.Errorf(messages.FsutilPrivateDirChmodFmt, path, err)
		}
	}
	return nil
}
