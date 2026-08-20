package fsutil

import (
	"fmt"
	"os"

	"github.com/conn-castle/agent-layer/internal/messages"
)

var (
	privateDirLstat    = os.Lstat
	privateDirMkdirAll = os.MkdirAll
	privateDirChmod    = os.Chmod
)

// EnsurePrivateDir creates path as a real owner-only directory (0700).
// An existing directory that grants group or other access is tightened to
// 0700. Symlinks and non-directories fail rather than being followed or
// replaced.
func EnsurePrivateDir(path string) error {
	info, err := privateDirLstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := privateDirMkdirAll(path, 0o700); err != nil {
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
		if err := privateDirChmod(path, 0o700); err != nil {
			return fmt.Errorf(messages.FsutilPrivateDirChmodFmt, path, err)
		}
	}
	return nil
}
