//go:build darwin

package herdr

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func unixPeerPID(connection net.Conn) (int, error) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("managed connection is %T, not Unix", connection)
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var controlErr error
	err = raw.Control(func(fd uintptr) {
		pid, controlErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, fmt.Errorf("read LOCAL_PEERPID: %w", controlErr)
	}
	if pid <= 1 {
		return 0, fmt.Errorf("read LOCAL_PEERPID: invalid PID %d", pid)
	}
	return pid, nil
}
