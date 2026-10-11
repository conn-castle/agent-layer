package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// updateCommandStopped observes without reaping or requiring an external ps.
// The unreaped leader reserves the original group ID, even if it moves groups.
func updateCommandStopped(leader int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("observe update processes: %w", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := updateReadProcessStat(filepath.Join("/proc", entry.Name(), "stat"))
		if pid != leader && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)) {
			continue // A process may exit while reading the snapshot.
		}
		if pid != leader && errors.Is(err, os.ErrPermission) {
			// Skip unreadable records only with proof they are not owned members.
			// Readable stat records need no getpgid permission on unrelated PIDs.
			group, groupErr := syscall.Getpgid(pid)
			if errors.Is(groupErr, syscall.ESRCH) || (groupErr == nil && group != leader) {
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
	}
	return true, nil
}
