package agentdispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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

func procStatFields(content string) []string {
	closeParen := strings.LastIndex(content, ")")
	if closeParen == -1 {
		return nil
	}
	return strings.Fields(content[closeParen+1:])
}

// procStatState extracts state (field 3) from /proc/<pid>/stat. The
// parenthesized comm field may itself contain spaces or parentheses, so
// fields are indexed after the last ")": the remainder starts at field 3.
func procStatState(content string) string {
	fields := procStatFields(content)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// procStatStartTime extracts starttime (field 22) from /proc/<pid>/stat. The
// parenthesized comm field may itself contain spaces or parentheses, so
// fields are indexed after the last ")": the remainder starts at field 3
// (state), putting starttime at remainder index 19.
func procStatStartTime(content string) string {
	fields := procStatFields(content)
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

func processIsZombie(pid int) bool {
	if pid <= 0 {
		return false
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		return procStatState(string(data)) == "Z"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output() // #nosec G204 -- pid is an Agent Layer-owned integer.
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

const (
	providerTerminationGrace        = time.Second
	providerTerminationPollInterval = 10 * time.Millisecond
	// Darwin has no /proc, so identity and zombie probes exec ps. Pre-terminal
	// observation is much slower there; termination still uses the 10ms poll.
	darwinProviderObserveInterval = 250 * time.Millisecond
)

func providerObservePollInterval() time.Duration {
	if runtime.GOOS == "darwin" {
		return darwinProviderObserveInterval
	}
	return providerTerminationPollInterval
}

var (
	errProviderGroupDead             = errors.New("provider process group is already dead")
	errProviderGroupIdentityMismatch = errors.New("provider process group ownership identity no longer matches")
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

// providerProcessGroupReused proves that a live group leader is a different
// process. A PID cannot be allocated again while its original process group
// still reserves that ID. Never signal the new owner's group.
func providerProcessGroupReused(record RunRecord) bool {
	if record.PID <= 0 || record.PID != record.ProcessGroupID || record.ProcessStartIdentity == "" {
		return false
	}
	current := processStartIdentity(record.PID)
	if current == "" || current == record.ProcessStartIdentity {
		return false
	}
	pgid, err := syscall.Getpgid(record.PID)
	return err == nil && pgid == record.PID
}

// prepareSignal proves it is still safe to signal this process group.
// A live leader must match the captured start identity. After that leader
// becomes a zombie or is reaped, descendants keep the process-group ID
// reserved until they exit, so a still-live group is safe to signal. A live
// replacement leader with a different start identity must not be signalled.
func (group ownedProviderProcessGroup) prepareSignal() error {
	record := RunRecord{PID: group.pid, ProcessGroupID: group.pgid, ProcessStartIdentity: group.start}
	if providerProcessGroupReused(record) {
		return errProviderGroupIdentityMismatch
	}
	if providerProcessGroupDead(group.pgid) {
		return errProviderGroupDead
	}
	current := processStartIdentity(group.pid)
	switch {
	case current == group.start && current != "":
		if processIsZombie(group.pid) {
			return nil
		}
		return group.verifyLiveIdentity()
	case current != "":
		return errProviderGroupIdentityMismatch
	default:
		if providerProcessGroupDead(group.pgid) {
			return errProviderGroupDead
		}
		if current := processStartIdentity(group.pid); current != "" && current != group.start {
			return errProviderGroupIdentityMismatch
		}
		return nil
	}
}

func (group ownedProviderProcessGroup) signal(sig syscall.Signal) error {
	if err := group.prepareSignal(); err != nil {
		if errors.Is(err, errProviderGroupDead) {
			return nil
		}
		return err
	}
	if err := syscall.Kill(-group.pgid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

// terminate sends SIGTERM to one verified Agent Layer-owned process group,
// escalates to SIGKILL after the bounded grace period, and returns only after
// group death is proven or a second bounded proof window expires.
func (group ownedProviderProcessGroup) terminate(grace time.Duration) error {
	var signalErr error
	if err := group.signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, errProviderGroupIdentityMismatch) {
			return err
		}
		// Darwin can report EPERM for a group containing only zombies.
		// Reaping runs concurrently, so retain the error but still allow the
		// bounded death-proof window. A signal error alone is not that proof.
		signalErr = fmt.Errorf("send SIGTERM to provider process group: %w", err)
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
			if err := group.signal(syscall.SIGKILL); err != nil {
				if errors.Is(err, errProviderGroupIdentityMismatch) {
					return errors.Join(signalErr, err)
				}
				signalErr = errors.Join(signalErr, fmt.Errorf("send SIGKILL to provider process group after %s grace: %w", grace, err))
			}
			proofTimer := time.NewTimer(grace)
			defer proofTimer.Stop()
			for {
				select {
				case <-ticker.C:
					if providerProcessGroupDead(group.pgid) {
						proofTimer.Stop()
						return nil
					}
				case <-proofTimer.C:
					return errors.Join(signalErr, fmt.Errorf("provider process group %d remained live %s after SIGKILL", group.pgid, grace))
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
	done      chan struct{}
	err       error
	mu        sync.Mutex
	requested bool
}

// newStartedProviderTermination latches process-group ownership after cmd.Start
// only when a durable process-start identity was captured. Once captured, a
// later ESRCH is safe for a short-lived provider that exited during setup.
func newStartedProviderTermination(cmd *exec.Cmd, record RunRecord, grace time.Duration) (*providerTermination, error) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid != record.PID || record.PID <= 0 || record.ProcessGroupID != record.PID {
		return nil, errors.New("started provider command does not match recorded process group")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		return nil, errors.New("started provider command has no isolated process group")
	}
	if record.ProcessStartIdentity == "" {
		return nil, errors.New("started provider process has no durable start identity")
	}
	if current := processStartIdentity(record.PID); current != "" && current != record.ProcessStartIdentity {
		return nil, errors.New("started provider process identity changed before ownership capture")
	}
	if pgid, err := syscall.Getpgid(record.PID); err == nil {
		if pgid != record.ProcessGroupID {
			return nil, fmt.Errorf("started provider process group mismatch: recorded group %d, live group %d", record.ProcessGroupID, pgid)
		}
	} else if !errors.Is(err, syscall.ESRCH) {
		return nil, fmt.Errorf("read started provider process group: %w", err)
	}
	group := ownedProviderProcessGroup{pid: record.PID, pgid: record.ProcessGroupID, start: record.ProcessStartIdentity}
	return &providerTermination{group: group, grace: grace, done: make(chan struct{})}, nil
}

func (termination *providerTermination) request() {
	termination.mu.Lock()
	if termination.requested {
		termination.mu.Unlock()
		return
	}
	termination.requested = true
	termination.mu.Unlock()
	go func() {
		termination.err = termination.group.terminate(termination.grace)
		close(termination.done)
	}()
}

func (termination *providerTermination) hasRequested() bool {
	termination.mu.Lock()
	defer termination.mu.Unlock()
	return termination.requested
}

// providerStopped joins any in-flight escalation. If termination was not
// started while the leader identity was still reserved, it signals only when
// ownership can still be proven; a reused or unprovable group is not signalled.
func (termination *providerTermination) providerStopped() error {
	if termination.hasRequested() {
		<-termination.done
		return termination.err
	}
	if err := termination.group.prepareSignal(); err != nil {
		if errors.Is(err, errProviderGroupDead) {
			return nil
		}
		return err
	}
	termination.request()
	<-termination.done
	return termination.err
}

type providerWaitStatusError struct {
	status syscall.WaitStatus
}

func (e *providerWaitStatusError) Error() string {
	if e.status.Exited() {
		return fmt.Sprintf("exit status %d", e.status.ExitStatus())
	}
	if e.status.Signaled() {
		return fmt.Sprintf("signal: %s", e.status.Signal())
	}
	return "process wait failed"
}

func (e *providerWaitStatusError) ExitCode() int {
	if e.status.Exited() {
		return e.status.ExitStatus()
	}
	return -1
}

func waitStatusToError(status syscall.WaitStatus) error {
	if status.Exited() && status.ExitStatus() == 0 {
		return nil
	}
	return &providerWaitStatusError{status: status}
}

// reapOwnedProviderLeader reaps the started leader without blocking. It never
// waits on a PID whose start identity no longer matches, so a reused process
// is not collected as if it were the provider.
func reapOwnedProviderLeader(cmd *exec.Cmd, start string) (bool, error) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return true, nil
	}
	pid := cmd.Process.Pid
	if providerProcessGroupReused(RunRecord{PID: pid, ProcessGroupID: pid, ProcessStartIdentity: start}) {
		return false, errProviderGroupIdentityMismatch
	}
	if current := processStartIdentity(pid); current != "" && start != "" && current != start {
		return false, errProviderGroupIdentityMismatch
	}
	var status syscall.WaitStatus
	var rusage syscall.Rusage
	wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, &rusage)
	if err != nil {
		if errors.Is(err, syscall.ECHILD) {
			_ = cmd.Process.Release()
			return true, nil
		}
		return false, err
	}
	if wpid == 0 {
		return false, nil
	}
	_ = cmd.Process.Release()
	return true, waitStatusToError(status)
}

func releaseUnreapedProvider(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	_ = cmd.Process.Release()
}

// reapDuringTermination reaps the owned leader concurrently with group
// termination so a zombie can release the process-group ID. It never starts a
// blocking cmd.Wait goroutine; if the leader remains live after termination
// fails, the process handle is released instead of leaking a waiter.
func reapDuringTermination(cmd *exec.Cmd, start string, termination *providerTermination) (error, bool) {
	ticker := time.NewTicker(providerTerminationPollInterval)
	defer ticker.Stop()
	var waitErr error
	reaped := false
	for {
		if !reaped {
			done, err := reapOwnedProviderLeader(cmd, start)
			if done {
				reaped = true
				waitErr = err
			} else if err != nil && waitErr == nil {
				waitErr = err
			}
		}
		select {
		case <-termination.done:
			if !reaped {
				done, err := reapOwnedProviderLeader(cmd, start)
				if done {
					reaped = true
					waitErr = err
				} else if err != nil && waitErr == nil {
					waitErr = err
				}
			}
			if !reaped {
				releaseUnreapedProvider(cmd)
			}
			return waitErr, reaped
		case <-ticker.C:
		}
	}
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

type waitExitCoder interface {
	ExitCode() int
}

func providerWaitError(target string, err error) error {
	if err == nil {
		return nil
	}
	var coder waitExitCoder
	if errors.As(err, &coder) {
		code := coder.ExitCode()
		if code <= 0 {
			code = 1
		}
		return exitError(ExitTargetFailure, fmt.Sprintf("%s exited with code %d; `al dispatch` exiting 70", target, code))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf("wait for %s: %v", target, err), err)
}
