//go:build unix

package fsutil

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	privateDirOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	privateDirMode      = uint32(privateDirPerm)
)

var privateDirFchmod = unix.Fchmod

func ensurePrivateDir(path, parent, name string) error {
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf(messages.FsutilPrivateDirStatFmt, path, err)
	}
	defer func() { _ = unix.Close(parentFD) }()

	if err := unix.Mkdirat(parentFD, name, privateDirMode); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf(messages.FsutilPrivateDirCreateFmt, path, err)
	}

	var st unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf(messages.FsutilPrivateDirStatFmt, path, err)
	}
	if !isPrivateDirMode(st) {
		return fmt.Errorf(messages.FsutilPrivateDirNotDirectoryFmt, path)
	}
	if st.Mode&0o077 == 0 {
		return nil
	}

	fd, err := unix.Openat(parentFD, name, privateDirOpenFlags, 0)
	if err != nil {
		if isNoFollowTypeError(err) {
			return fmt.Errorf(messages.FsutilPrivateDirNotDirectoryFmt, path)
		}
		return fmt.Errorf(messages.FsutilPrivateDirStatFmt, path, err)
	}
	defer func() { _ = unix.Close(fd) }()

	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf(messages.FsutilPrivateDirStatFmt, path, err)
	}
	if !isPrivateDirMode(st) {
		return fmt.Errorf(messages.FsutilPrivateDirNotDirectoryFmt, path)
	}
	if st.Mode&0o077 == 0 {
		return nil
	}
	if err := privateDirFchmod(fd, privateDirMode); err != nil {
		return fmt.Errorf(messages.FsutilPrivateDirChmodFmt, path, err)
	}
	return nil
}

func isPrivateDirMode(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR
}

func isNoFollowTypeError(err error) bool {
	return errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR)
}
