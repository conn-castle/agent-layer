// Package ownedprocess runs commands in owned process groups and stops and joins
// the whole group before its wait returns.
package ownedprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	cleanupAllowance       = time.Second
	firstObservationDelay  = 5 * time.Millisecond
	steadyObservationDelay = 250 * time.Millisecond
)

// nextObservationDelay doubles short initial waits up to the steady interval.
func nextObservationDelay(previous time.Duration) time.Duration {
	if previous == 0 {
		return firstObservationDelay
	}
	return min(previous*2, steadyObservationDelay)
}

// CleanupError reports failed observation of the owned leader or failed proof
// that the leader and its group stopped.
type CleanupError struct{ Err error }

func (e *CleanupError) Error() string { return "owned process cleanup failed: " + e.Err.Error() }

func (e *CleanupError) Unwrap() error { return e.Err }

// Run owns a new process group until all signaling is finished, then reaps its
// leader once. Callers supply a plain command, output writers and context. A
// caller-requested Setsid is preserved; otherwise Run creates a process group.
// An ordinary exit returns the plain wait error; other failures are joined.
func Run(ctx context.Context, cmd *exec.Cmd) error {
	wait, err := Start(ctx, cmd)
	if err != nil {
		return err
	}
	return wait()
}

// Start starts a command in an owned process group, preserving a requested Setsid.
// Callers join cleanup by calling wait; later calls return the stored result.
// Non-file Stdout and Stderr destinations must not be read until wait returns.
func Start(ctx context.Context, cmd *exec.Cmd) (wait func() error, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	cmd.WaitDelay = cleanupAllowance
	output := &outputGuard{}
	// Caller-owned files pass straight to the child; no Go-side writes outlive wait.
	if _, file := cmd.Stdout.(*os.File); cmd.Stdout != nil && !file {
		cmd.Stdout = &guardedWriter{output, cmd.Stdout}
	}
	if _, file := cmd.Stderr.(*os.File); cmd.Stderr != nil && !file {
		cmd.Stderr = &guardedWriter{output, cmd.Stderr}
	}
	// Child-exit notifications wake observation promptly without reaping the
	// leader. Observation never forks, so it cannot wake itself. The timer
	// remains the fallback when notifications are coalesced.
	exited := make(chan os.Signal, 1)
	signal.Notify(exited, syscall.SIGCHLD)
	if err := cmd.Start(); err != nil {
		signal.Stop(exited)
		return nil, err
	}
	done := make(chan struct{})
	var result error
	go func() {
		// No Wait has run: even an exited leader reserves this freshly owned ID.
		pgid := cmd.Process.Pid
		delay := time.Duration(0)
		var observeErr error
		var ctxErr error
		observationFailures := 0
	observe:
		for ctxErr = ctx.Err(); ctxErr == nil; ctxErr = ctx.Err() {
			leaderLive, err := leaderState(pgid)
			switch {
			case err != nil:
				observationFailures++
				if errors.Is(err, os.ErrNotExist) || observationFailures >= 3 {
					observeErr = err
					break observe
				}
			case !leaderLive:
				break observe
			default:
				observationFailures = 0
			}
			delay = nextObservationDelay(delay)
			exitNotification := (<-chan os.Signal)(exited)
			if err != nil {
				delay = steadyObservationDelay
				exitNotification = nil
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
			case <-exitNotification:
			case <-timer.C:
			}
			timer.Stop()
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
		waitErr := reap(cmd, output, cleanupErr != nil)
		result = runResult(ctxErr, observeErr, cleanupErr, waitErr)
		signal.Stop(exited)
		close(done)
	}()
	return func() error {
		<-done
		return result
	}, nil
}

func runResult(ctxErr, observeErr, cleanupErr, waitErr error) error {
	if ctxErr == nil && observeErr == nil && cleanupErr == nil {
		return waitErr
	}
	var ownedErr error
	if observeErr != nil || cleanupErr != nil {
		ownedErr = &CleanupError{errors.Join(observeErr, cleanupErr)}
	}
	return errors.Join(ctxErr, ownedErr, waitErr)
}

// A failed stop proof cannot guarantee that Wait will return. Keep one eventual
// reaper, but detach caller output before returning so it can be read safely.
func reap(cmd *exec.Cmd, output *outputGuard, bounded bool) error {
	defer func() {
		output.Lock()
		output.stopped = true
		output.Unlock()
	}()
	if !bounded {
		return cmd.Wait()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(cleanupAllowance)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return errors.New("owned process reap did not finish within cleanup allowance")
	}
}

type outputGuard struct {
	sync.Mutex
	stopped bool
}

type guardedWriter struct {
	*outputGuard
	writer io.Writer
}

func (w *guardedWriter) Write(data []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	if w.stopped {
		return len(data), nil
	}
	return w.writer.Write(data)
}

func awaitGroupStopped(pgid int) error {
	deadline := time.Now().Add(cleanupAllowance)
	for {
		leaderLive, groupLive, err := groupState(pgid)
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !leaderLive && !groupLive {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("owned leader or process group %d stop unproven after SIGKILL: %w", pgid, err)
			}
			return fmt.Errorf("owned leader or process group %d remained running after SIGKILL", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
