//go:build !darwin && !linux

package herdr

import (
	"fmt"
	"net"
)

func unixPeerPID(net.Conn) (int, error) {
	return 0, fmt.Errorf("managed Unix peer identity is unsupported on this platform")
}
