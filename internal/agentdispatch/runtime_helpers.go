package agentdispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func defaultProviderCommandFactory(name string, args ...string) *exec.Cmd {
	// #nosec G204 -- the provider binary comes from the fixed Agent Dispatch registry.
	return exec.CommandContext(context.Background(), name, args...)
}

func prepareProviderProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func processStartIdentity(pid int) string {
	if pid <= 0 {
		return ""
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if start := procStatStartTime(string(data)); start != "" {
			return "proc:" + start
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output() // #nosec G204 -- pid is an Agent Layer-owned integer.
	if err != nil {
		return ""
	}
	return "ps:" + strings.TrimSpace(string(out))
}

// procStatStartTime extracts starttime (field 22) from /proc/<pid>/stat. The
// parenthesized comm field may itself contain spaces or parentheses, so
// fields are indexed after the last ")": the remainder starts at field 3
// (state), putting starttime at remainder index 19.
func procStatStartTime(content string) string {
	closeParen := strings.LastIndex(content, ")")
	if closeParen == -1 {
		return ""
	}
	fields := strings.Fields(content[closeParen+1:])
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

const (
	providerTerminationGrace        = time.Second
	providerTerminationPollInterval = 10 * time.Millisecond
)

type ownedProviderProcessGroup struct {
	pid   int
	pgid  int
	start string
}

// verifiedProviderProcessGroup returns a process-group capability only when
// the live leader still has the start identity and process group recorded by
// Agent Layer. Once verified, the capability remains safe to use if the leader
// exits during the grace period because its descendants keep that process
// group ID reserved until they also exit.
func verifiedProviderProcessGroup(record RunRecord) (ownedProviderProcessGroup, error) {
	if record.PID <= 0 || record.ProcessGroupID <= 0 || record.ProcessStartIdentity == "" {
		return ownedProviderProcessGroup{}, errors.New("provider process group has incomplete ownership identity")
	}
	group := ownedProviderProcessGroup{pid: record.PID, pgid: record.ProcessGroupID, start: record.ProcessStartIdentity}
	if err := group.verifyLiveIdentity(); err != nil {
		return ownedProviderProcessGroup{}, err
	}
	return group, nil
}

func (group ownedProviderProcessGroup) verifyLiveIdentity() error {
	if current := processStartIdentity(group.pid); current == "" || current != group.start {
		return errors.New("provider process group ownership identity no longer matches")
	}
	pgid, err := syscall.Getpgid(group.pid)
	if err != nil {
		return fmt.Errorf("read provider process group: %w", err)
	}
	if pgid != group.pgid || pgid != group.pid {
		return fmt.Errorf("provider process group mismatch: pid %d, recorded group %d, live group %d", group.pid, group.pgid, pgid)
	}
	return nil
}

func providerProcessGroupDead(pgid int) bool {
	if pgid <= 0 {
		return true
	}
	return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// terminate sends SIGTERM to one verified Agent Layer-owned process group,
// escalates to SIGKILL after the bounded grace period, and returns only after
// group death is proven or a second bounded proof window expires.
func (group ownedProviderProcessGroup) terminate(grace time.Duration) error {
	if err := syscall.Kill(-group.pgid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("send SIGTERM to provider process group: %w", err)
	}
	if grace <= 0 {
		grace = providerTerminationGrace
	}
	timer := time.NewTimer(grace)
	ticker := time.NewTicker(providerTerminationPollInterval)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if providerProcessGroupDead(group.pgid) {
				return nil
			}
		case <-timer.C:
			if err := syscall.Kill(-group.pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("send SIGKILL to provider process group after %s grace: %w", grace, err)
			}
			proofTimer := time.NewTimer(grace)
			defer proofTimer.Stop()
			for {
				select {
				case <-ticker.C:
					if providerProcessGroupDead(group.pgid) {
						return nil
					}
				case <-proofTimer.C:
					return fmt.Errorf("provider process group %d remained live %s after SIGKILL", group.pgid, grace)
				}
			}
		}
	}
}

func (group ownedProviderProcessGroup) terminateReverified(grace time.Duration) error {
	if err := group.verifyLiveIdentity(); err != nil {
		return err
	}
	return group.terminate(grace)
}

type providerTermination struct {
	group     ownedProviderProcessGroup
	grace     time.Duration
	done      chan error
	mu        sync.Mutex
	requested bool
	completed bool
}

// newStartedProviderTermination latches the process-group ownership proved by
// this execution's successful cmd.Start call. Unlike a later cancel command,
// the owning execution does not need a second live-process lookup when a very
// short provider has already exited before identity capture completes.
func newStartedProviderTermination(cmd *exec.Cmd, record RunRecord, grace time.Duration) (*providerTermination, error) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid != record.PID || record.PID <= 0 || record.ProcessGroupID != record.PID {
		return nil, errors.New("started provider command does not match recorded process group")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		return nil, errors.New("started provider command has no isolated process group")
	}
	if record.ProcessStartIdentity != "" {
		if current := processStartIdentity(record.PID); current != "" && current != record.ProcessStartIdentity {
			return nil, errors.New("started provider process identity changed before ownership capture")
		}
	}
	if pgid, err := syscall.Getpgid(record.PID); err == nil {
		if pgid != record.ProcessGroupID {
			return nil, fmt.Errorf("started provider process group mismatch: recorded group %d, live group %d", record.ProcessGroupID, pgid)
		}
	} else if !errors.Is(err, syscall.ESRCH) {
		return nil, fmt.Errorf("read started provider process group: %w", err)
	}
	group := ownedProviderProcessGroup{pid: record.PID, pgid: record.ProcessGroupID, start: record.ProcessStartIdentity}
	return &providerTermination{group: group, grace: grace, done: make(chan error, 1)}, nil
}

func (termination *providerTermination) request() {
	termination.mu.Lock()
	if termination.requested || termination.completed {
		termination.mu.Unlock()
		return
	}
	termination.requested = true
	termination.mu.Unlock()
	go func() {
		termination.done <- termination.group.terminate(termination.grace)
		close(termination.done)
	}()
}

// providerStopped completes the controller after cmd.Wait has reaped the
// provider leader. It also joins any in-flight escalation before claim release.
func (termination *providerTermination) providerStopped() error {
	termination.mu.Lock()
	if termination.completed {
		termination.mu.Unlock()
		return nil
	}
	termination.completed = true
	requested := termination.requested
	termination.mu.Unlock()
	if !requested {
		return nil
	}
	return <-termination.done
}

func installProviderSignalForwarder(requestTermination func()) (caught func() os.Signal, stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	exited := make(chan struct{})
	var (
		mu    sync.Mutex
		first os.Signal
	)
	go func() {
		defer close(exited)
		for {
			select {
			case sig := <-signals:
				mu.Lock()
				if first == nil {
					first = sig
				}
				mu.Unlock()
				requestTermination()
			case <-done:
				return
			}
		}
	}()
	return func() os.Signal {
			mu.Lock()
			defer mu.Unlock()
			return first
		}, func() {
			signal.Stop(signals)
			close(done)
			<-exited
		}
}

func providerStartError(target string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		meta, ok := lookupTarget(target)
		binary := target
		if ok {
			binary = meta.Binary
		}
		return exitError(ExitUnavailable, fmt.Sprintf("`al dispatch` target %s requires `%s` on PATH", target, binary))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf("start %s: %v", target, err), err)
}

func providerWaitError(target string, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code <= 0 {
			code = 1
		}
		return exitError(ExitTargetFailure, fmt.Sprintf("%s exited with code %d; `al dispatch` exiting 70", target, code))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf("wait for %s: %v", target, err), err)
}
