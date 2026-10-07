//go:build linux

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
	var credentials *unix.Ucred
	var controlErr error
	err = raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, fmt.Errorf("read SO_PEERCRED: %w", controlErr)
	}
	if credentials == nil || credentials.Pid <= 1 {
		return 0, fmt.Errorf("read SO_PEERCRED: invalid credentials")
	}
	return int(credentials.Pid), nil
}
