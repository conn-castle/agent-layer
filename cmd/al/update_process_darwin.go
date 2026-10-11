package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func updateOwnProcesses() (func() error, error) {
	return func() error { return nil }, nil
}

// updateCommandStopped observes the kernel process table without reaping or ps.
// Track the leader separately in case it moved out of its original group.
func updateCommandStopped(leader int) (bool, error) {
	const zombie = 5 // SZOMB in Darwin's sys/proc.h.
	dead := make(map[string]bool)
	for pass := 0; pass < 2; pass++ {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
		if err != nil {
			return false, fmt.Errorf("observe update processes: %w", err)
		}
		leaderSeen := false
		for _, process := range processes {
			if int(process.Proc.P_pid) != leader && int(process.Eproc.Pgid) != leader {
				continue
			}
			if process.Proc.P_stat != zombie {
				return false, nil
			}
			leaderSeen = leaderSeen || int(process.Proc.P_pid) == leader
			identity := fmt.Sprintf("%d:%d:%d", process.Proc.P_pid, process.Proc.P_starttime.Sec, process.Proc.P_starttime.Usec)
			if pass == 0 {
				dead[identity] = true
			} else if !dead[identity] {
				return false, nil
			}
		}
		if !leaderSeen {
			return false, fmt.Errorf("update process leader %d missing from process table", leader)
		}
	}
	return true, nil
}
