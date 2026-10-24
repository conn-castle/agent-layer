//go:build linux

package ownedprocess

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// leaderState reads only the unreaped leader; a missing stat means /proc is
// unavailable because the leader still reserves its PID until cleanup.
func leaderState(pgid int) (bool, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pgid), "stat")) // #nosec G304 -- /proc path built from a numeric PID.
	if err != nil {
		return false, fmt.Errorf("observe owned leader (requires /proc): %w", err)
	}
	pid, state, _, ok := parseProcStat(string(content))
	if !ok || pid != pgid {
		return false, fmt.Errorf("observe owned leader %d: malformed stat", pgid)
	}
	return procProcessLive("/proc", pid, state)
}

// groupState observes the leader and its original group without reaping.
func groupState(pgid int) (leaderLive, groupLive bool, err error) {
	return scanProc("/proc", pgid)
}

// scanProc skips unobservable unrelated processes, but fails closed for members
// of the owned group or an unreaped leader that has moved to another group.
func scanProc(root string, pgid int) (leaderLive, groupLive bool, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, false, fmt.Errorf("observe owned process group: %w", err)
	}
	for _, entry := range entries {
		pid, ok := procPID(entry.Name())
		if !ok {
			continue
		}
		// Group lookup avoids opening stat for every unrelated process on a
		// busy host. The unreaped leader is always observed, even if it moved.
		actualGroup, groupErr := syscall.Getpgid(pid)
		if errors.Is(groupErr, syscall.ESRCH) || (groupErr == nil && actualGroup != pgid && pid != pgid) {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat")) // #nosec G304 -- /proc path built from a numeric PID.
		if processVanished(readErr) {
			continue
		}
		statPID, state, pgrp, parsed := parseProcStat(string(content))
		if readErr != nil || !parsed || statPID != pid {
			if readErr == nil {
				readErr = errors.New("malformed stat")
			}
			return false, false, fmt.Errorf("observe owned process %d: %w", pid, errors.Join(readErr, groupErr))
		}
		if pid != pgid && pgrp != pgid {
			continue
		}
		live, err := procProcessLive(root, pid, state)
		if err != nil {
			return false, false, err
		}
		if live {
			leaderLive = leaderLive || pid == pgid
			groupLive = groupLive || pgrp == pgid
		}
	}
	return leaderLive, groupLive, nil
}

// procProcessLive checks threads only for zombie leaders: a live thread may
// retain output even after the process's main thread has exited.
func procProcessLive(root string, pid int, state byte) (bool, error) {
	if state != 'Z' {
		return processStateLive(state), nil
	}
	taskRoot := filepath.Join(root, strconv.Itoa(pid), "task")
	entries, err := os.ReadDir(taskRoot)
	if processVanished(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("observe owned process threads: %w", err)
	}
	for _, entry := range entries {
		tid, ok := procPID(entry.Name())
		if !ok {
			continue
		}
		content, err := os.ReadFile(filepath.Join(taskRoot, strconv.Itoa(tid), "stat")) // #nosec G304 G703 -- /proc path built from numeric process and thread IDs.
		if processVanished(err) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("observe owned thread %d: %w", tid, err)
		}
		statPID, threadState, _, ok := parseProcStat(string(content))
		if !ok || statPID != tid {
			return false, fmt.Errorf("observe owned thread %d: malformed stat", tid)
		}
		if processStateLive(threadState) {
			return true, nil
		}
	}
	return false, nil
}

// processStateLive excludes zombie and dead processes, which retain ownership.
func processStateLive(state byte) bool {
	return state != 'Z' && state != 'X' && state != 'x'
}

// processVanished recognizes races with a process exiting during observation.
func processVanished(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// procPID accepts only positive decimal process IDs, excluding other /proc files.
func procPID(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	pid, err := strconv.Atoi(value)
	return pid, err == nil && pid > 0
}

// parseProcStat splits after the last ')' because comm can contain spaces and
// closing parentheses. Only PID, state, parent PID and process group are needed.
func parseProcStat(content string) (pid int, state byte, pgrp int, ok bool) {
	open, close := strings.IndexByte(content, '('), strings.LastIndexByte(content, ')')
	if open < 0 || close <= open {
		return 0, 0, 0, false
	}
	pid, ok = procPID(strings.TrimSpace(content[:open]))
	fields := strings.Fields(content[close+1:])
	if !ok || len(fields) < 3 || len(fields[0]) != 1 {
		return 0, 0, 0, false
	}
	ppid, parentErr := strconv.Atoi(fields[1])
	pgrp, groupErr := strconv.Atoi(fields[2])
	if parentErr != nil || ppid < 0 || groupErr != nil || pgrp < 0 {
		return 0, 0, 0, false
	}
	return pid, fields[0][0], pgrp, true
}
