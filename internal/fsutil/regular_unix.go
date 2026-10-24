//go:build unix

package fsutil

import (
	"os"

	"golang.org/x/sys/unix"
)

func openRegularFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		// Some special files (such as sockets) cannot be opened at all.
		if info, statErr := os.Stat(path); statErr == nil {
			if kindErr := RequireRegularFile(info, path); kindErr != nil {
				return nil, kindErr
			}
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
