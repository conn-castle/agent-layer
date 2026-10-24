package agentdispatch

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

func waitPromptReadable(fd int, timeout time.Duration) error {
	if fd >= unix.FD_SETSIZE {
		return fmt.Errorf("dispatch prompt descriptor %d exceeds select limit %d", fd, unix.FD_SETSIZE)
	}
	var readfds unix.FdSet
	readfds.Set(fd)
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	n, err := unix.Select(fd+1, &readfds, nil, nil, &tv)
	if errors.Is(err, unix.EINTR) {
		return errPromptNotReadable
	}
	if err == nil && n == 0 {
		return errPromptNotReadable
	}
	return err
}
