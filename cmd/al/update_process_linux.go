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
		if pid != leader {
			group, err := syscall.Getpgid(pid)
			if errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				return false, fmt.Errorf("observe update process group for PID %d: %w", pid, err)
			}
			if group != leader {
				continue
			}
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat")) // #nosec G304 -- kernel process table, numeric PID only.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue // A process may exit while reading the snapshot.
		}
		if err != nil {
			return false, fmt.Errorf("observe update processes: %w", err)
		}
		// comm may itself contain spaces and parentheses; fields after the final
		// ')' start with state. Kernel membership was checked before this read.
		end := strings.LastIndexByte(string(data), ')')
		fields := strings.Fields(string(data)[end+1:])
		if end < 0 || len(fields) < 1 {
			return false, fmt.Errorf("invalid process stat for PID %d", pid)
		}
		if fields[0] != "Z" && fields[0] != "X" {
			return false, nil
		}
	}
	return true, nil
}
