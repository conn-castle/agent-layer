//go:build darwin

package ownedprocess

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// darwinZombie is SZOMB from <sys/proc.h>; zombies retain ownership but no
// longer run or hold output.
const darwinZombie = 5

// leaderState reads only the unreaped leader. sysctl observes without forking,
// so observing cannot itself raise the SIGCHLD that wakes observation.
func leaderState(pgid int) (bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pgid)
	if err != nil {
		return false, fmt.Errorf("observe owned leader: %w", err)
	}
	return anyLive(procs), nil
}

// groupState observes the leader by PID, even if it moved groups, and the
// members of its original group, without reaping.
func groupState(pgid int) (leaderLive, groupLive bool, err error) {
	leaderLive, err = leaderState(pgid)
	if err != nil {
		return false, false, err
	}
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return false, false, fmt.Errorf("observe owned process group: %w", err)
	}
	return leaderLive, anyLive(procs), nil
}

// anyLive reports whether any listed process is not a zombie.
func anyLive(procs []unix.KinfoProc) bool {
	for _, proc := range procs {
		if proc.Proc.P_stat != darwinZombie {
			return true
		}
	}
	return false
}
