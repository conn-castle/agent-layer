//go:build darwin

package herdr

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processLineage(pid int) (int, string, error) {
	if pid <= 0 || uint64(pid) > uint64(^uint32(0)>>1) {
		return 0, "", fmt.Errorf("invalid process ID %d", pid)
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, "", err
	}
	if process.Proc.P_pid != int32(pid) {
		return 0, "", fmt.Errorf("process %d was not found", pid)
	}
	start := process.Proc.P_starttime
	return int(process.Eproc.Ppid), fmt.Sprintf("darwin:%d:%d", start.Sec, start.Usec), nil
}
