package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// updateOwnProcesses retains orphaned installer members until this CLI exits.
// This process-wide setting assumes a single script update, with no orphan reaper.
func updateOwnProcesses() (func() error, error) {
	prior, err := updateSubreaperState()
	if err != nil {
		return nil, err
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return nil, fmt.Errorf("retain update child processes: %w", err)
	}
	return func() error {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(prior), 0, 0, 0); err != nil {
			return fmt.Errorf("restore update child subreaper state: %w", err)
		}
		return nil
	}, nil
}

func updateSubreaperState() (uint32, error) {
	var state uint32
	_, _, err := unix.Syscall6(unix.SYS_PRCTL, unix.PR_GET_CHILD_SUBREAPER, uintptr(unsafe.Pointer(&state)), 0, 0, 0, 0) // #nosec G103 -- prctl synchronously writes the local 32-bit flag.
	if err != 0 {
		return 0, fmt.Errorf("read update child subreaper state: %w", err)
	}
	return state, nil
}

// updateCommandStopped observes without reaping or requiring an external ps.
// The unreaped leader reserves the original group ID, even if it moves groups.
func updateCommandStopped(leader int) (bool, error) {
	subreaper, err := updateSubreaperState()
	if err != nil {
		return false, err
	}
	// A member can fork after ReadDir and become a zombie before its stat read.
	// Confirm membership after every member in the first scan is known dead;
	// only those same dead processes may appear in the confirmation scan.
	dead := make(map[string]bool)
	for pass := 0; pass < 2; pass++ {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return false, fmt.Errorf("observe update processes: %w", err)
		}
		leaderSeen := false
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			data, err := updateReadProcessStat(filepath.Join("/proc", entry.Name(), "stat"))
			if pid != leader && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)) {
				if pass == 1 && subreaper == 0 {
					// An unclassified vanished PID may have forked after this listing.
					return false, nil
				}
				// Unreaped adopted members cannot vanish; their live/new ancestor
				// remains observable even if a member reaps one of its own children.
				continue
			}
			if pid != leader && errors.Is(err, os.ErrPermission) {
				// Skip unreadable records only with proof they are not owned members.
				// Readable stat records need no getpgid permission on unrelated PIDs.
				group, groupErr := syscall.Getpgid(pid)
				if errors.Is(groupErr, syscall.ESRCH) {
					if pass == 1 && subreaper == 0 {
						return false, nil
					}
					continue
				}
				if groupErr == nil && group != leader {
					continue
				}
			}
			if err != nil {
				return false, fmt.Errorf("observe update processes: %w", err)
			}
			// comm may itself contain spaces and parentheses; fields after the final
			// ')' start with state, ppid, and pgrp from the same kernel snapshot.
			end := strings.LastIndexByte(string(data), ')')
			fields := strings.Fields(string(data)[end+1:])
			if end < 0 || len(fields) < 3 {
				return false, fmt.Errorf("invalid process stat for PID %d", pid)
			}
			group, err := strconv.Atoi(fields[2])
			if err != nil {
				return false, fmt.Errorf("invalid process group for PID %d: %w", pid, err)
			}
			if pid != leader && group != leader {
				continue
			}
			if fields[0] != "Z" && fields[0] != "X" {
				return false, nil
			}
			if len(fields) < 20 {
				return false, fmt.Errorf("invalid process stat for PID %d", pid)
			}
			leaderSeen = leaderSeen || pid == leader
			identity := entry.Name() + ":" + fields[19] // PID and kernel start time.
			if pass == 0 {
				dead[identity] = true
			} else if !dead[identity] {
				return false, nil // A missed member could itself have forked before dying.
			}
		}
		if !leaderSeen {
			return false, fmt.Errorf("update process leader %d missing from process table", leader)
		}
	}
	return true, nil
}
