package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// updateCommandStopped observes the kernel process table without reaping or ps.
// Track the leader separately in case it moved out of its original group.
func updateCommandStopped(leader int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return false, fmt.Errorf("observe update processes: %w", err)
	}
	const zombie = 5 // SZOMB in Darwin's sys/proc.h.
	for _, process := range processes {
		if (int(process.Proc.P_pid) == leader || int(process.Eproc.Pgid) == leader) && process.Proc.P_stat != zombie {
			return false, nil
		}
	}
	return true, nil
}
