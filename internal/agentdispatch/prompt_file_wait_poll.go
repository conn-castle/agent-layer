//go:build unix && !darwin

package agentdispatch

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func waitPromptReadable(fd int, timeout time.Duration) error {
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, int(timeout/time.Millisecond)) // #nosec G115 -- fd is an OS descriptor.
	if errors.Is(err, unix.EINTR) {
		return errPromptNotReadable
	}
	if err == nil && n == 0 {
		return errPromptNotReadable
	}
	return err
}
