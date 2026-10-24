//go:build unix

package agentdispatch

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func readPromptFile(ctx context.Context, path string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, promptPreparationCancelled(ctx)
	}
	var fd int
	var err error
	for {
		fd, err = unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
		if !errors.Is(err, unix.EINTR) {
			break
		}
		if ctx.Err() != nil {
			return nil, promptPreparationCancelled(ctx)
		}
	}
	if err != nil {
		return nil, promptFileError(path, "open", err)
	}
	restoreBlocking := false
	defer func() {
		if restoreBlocking {
			_ = unix.SetNonblock(fd, false)
		}
		_ = unix.Close(fd)
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, promptFileError(path, "stat", err)
	}
	regular := stat.Mode&unix.S_IFMT == unix.S_IFREG
	var size int64
	if regular {
		if err := checkPromptFileSize(stat.Size); err != nil {
			return nil, err
		}
		size = stat.Size
	} else {
		// Darwin /dev/fd opens share the original description and ignore the
		// open flags. Restore the caller's blocking state before closing.
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			return nil, promptFileError(path, "fcntl", err)
		}
		if flags&unix.O_NONBLOCK == 0 {
			if err := unix.SetNonblock(fd, true); err != nil {
				return nil, promptFileError(path, "fcntl", err)
			}
			restoreBlocking = true
		}
	}
	return readPromptBytes(ctx, path, size, func(buf []byte) (int, error) {
		for {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if !regular {
				// Wait before reading: a writerless FIFO initially reads as EOF.
				// A shared FIFO whose previous writer closed may already report EOF.
				if err := waitPromptReadable(fd, 100*time.Millisecond); err != nil {
					if errors.Is(err, errPromptNotReadable) {
						continue
					}
					return 0, err
				}
			}
			n, err := unix.Read(fd, buf)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if errors.Is(err, unix.EAGAIN) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return n, err
		}
	})
}
