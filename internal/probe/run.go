// Package probe provides execution bounded to a capability probe's owned group.
package probe

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const cleanupAllowance = time.Second

// Run owns a new process group until all signaling is finished, then reaps its
// leader once. Callers supply a plain command, output writers and probe context.
func Run(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = cleanupAllowance
	if err := cmd.Start(); err != nil {
		return err
	}
	// No Wait has run: even an exited leader reserves this freshly owned ID.
	pgid := cmd.Process.Pid
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var observeErr error
	var ctxErr error
	for ctxErr = ctx.Err(); ctxErr == nil; ctxErr = ctx.Err() {
		var leaderLive bool
		leaderLive, _, observeErr = groupState(pgid)
		if observeErr != nil || !leaderLive {
			break
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	// The unreaped leader may have moved groups. Signal only its owned PID and
	// original group, never its destination group. Darwin may reject zombie
	// signals; only the bounded proof of both stops decides cleanup success.
	signalErr := errors.Join(cmd.Process.Kill(), syscall.Kill(-pgid, syscall.SIGKILL))
	cleanupErr := awaitGroupStopped(pgid)
	if cleanupErr != nil {
		cleanupErr = errors.Join(signalErr, cleanupErr)
	}
	// All signal decisions precede Wait; there is no delayed escalation that
	// could hit a reused group. WaitDelay also bounds escaped pipe holders.
	waitErr := cmd.Wait()
	return errors.Join(ctxErr, observeErr, cleanupErr, waitErr)
}

func awaitGroupStopped(pgid int) error {
	deadline := time.Now().Add(cleanupAllowance)
	for {
		leaderLive, groupLive, err := groupState(pgid)
		if err != nil {
			return err
		}
		if !leaderLive && !groupLive {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("probe leader or process group %d remained running after SIGKILL", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// groupState observes without reaping. ps exposes the same columns on Linux
// and Darwin. Track the leader by PID even if it moves groups; zombies retain
// ownership but are no longer running processes.
func groupState(pgid int) (leaderLive, groupLive bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupAllowance)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,pgid=,stat=")
	cmd.WaitDelay = cleanupAllowance
	output, err := cmd.Output()
	if err != nil {
		return false, false, fmt.Errorf("observe probe process group: %w", err)
	}
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		if fields[1] == strconv.Itoa(pgid) {
			groupLive = true
		}
		if fields[0] == strconv.Itoa(pgid) {
			leaderLive = true
		}
	}
	return leaderLive, groupLive, nil
}
