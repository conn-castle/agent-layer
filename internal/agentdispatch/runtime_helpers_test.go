package agentdispatch

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProviderObservePollIntervalIsSlowerOnDarwin(t *testing.T) {
	got := providerObservePollInterval()
	if runtime.GOOS == "darwin" {
		if got <= providerTerminationPollInterval {
			t.Fatalf("darwin observe interval %s is not slower than termination poll %s", got, providerTerminationPollInterval)
		}
		return
	}
	if got != providerTerminationPollInterval {
		t.Fatalf("observe interval = %s, want %s", got, providerTerminationPollInterval)
	}
}

func TestProviderTerminationAllowsGracefulProcessGroupExit(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; touch "$1"; while :; do sleep 1; done`, "sh", readyPath) // #nosec G204 -- test-owned path passed as an argument.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start provider process group: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
	})
	waitForTestPath(t, readyPath)
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: processStartIdentity(cmd.Process.Pid)}
	termination, err := newStartedProviderTermination(cmd, record, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	termination.request()
	waitErr := cmd.Wait()
	stopped = true
	if waitErr != nil {
		t.Fatalf("graceful provider exit: %v", waitErr)
	}
	if err := termination.providerStopped(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("graceful termination took %s, want less than escalation grace", elapsed)
	}
}

func TestProviderTerminationWaitsForZombieReaping(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: processStartIdentity(cmd.Process.Pid)}
	termination, err := newStartedProviderTermination(cmd, record, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(cmd.Process.Pid)).Output() // #nosec G204 -- test-owned PID.
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test process did not become a zombie")
		}
		time.Sleep(10 * time.Millisecond)
	}
	termination.request()
	select {
	case <-termination.done:
		t.Fatalf("termination returned before zombie reaping could prove group death: %v", termination.err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	reaped = true
	if err := termination.providerStopped(); err != nil {
		t.Fatalf("reaped zombie group failed termination: %v", err)
	}
	if !providerProcessGroupDead(cmd.Process.Pid) {
		t.Fatal("reaped test process retained a live group")
	}
}

func TestProviderTerminationEscalatesAndUnblocksDescendantPipesAndWait(t *testing.T) {
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; (trap '' TERM; while :; do sleep 1; done) & child=$!; printf '%s\n' "$child" > "$1"; wait "$child"`, "sh", childPIDPath) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	waited := false
	t.Cleanup(func() {
		if !stopped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if !waited {
				_ = cmd.Wait()
			}
		}
	})
	childPID := waitForProviderChildPID(t, childPIDPath)
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: processStartIdentity(cmd.Process.Pid)}
	termination, err := newStartedProviderTermination(cmd, record, 75*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	stdoutDrained := make(chan struct{})
	stderrDrained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, stdout); close(stdoutDrained) }()
	go func() { _, _ = io.Copy(io.Discard, stderr); close(stderrDrained) }()

	termination.request()
	select {
	case <-stdoutDrained:
	case <-time.After(2 * time.Second):
		t.Fatal("provider stdout pipe did not drain after forced escalation")
	}
	select {
	case <-stderrDrained:
	case <-time.After(2 * time.Second):
		t.Fatal("provider stderr pipe did not drain after forced escalation")
	}
	waited = true
	if err := cmd.Wait(); err != nil {
		t.Fatalf("graceful provider leader exit: %v", err)
	}
	if err := termination.providerStopped(); err != nil {
		t.Fatal(err)
	}
	waitForProviderProcessExit(t, childPID)
	waitForProviderProcessGroupExit(t, cmd.Process.Pid)
	stopped = true
}

func TestTerminateDoesNotSignalReusedProcessGroup(t *testing.T) {
	unrelated := exec.Command("/bin/sh", "-c", `trap '' TERM; while :; do sleep 1; done`) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(unrelated)
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	pid := unrelated.Process.Pid
	defer func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = unrelated.Wait()
	}()
	current := processStartIdentity(pid)
	if current == "" {
		t.Fatal("process start identity is required for reuse tests")
	}
	group := ownedProviderProcessGroup{pid: pid, pgid: pid, start: current + "-other"}
	if err := group.terminate(50 * time.Millisecond); !errors.Is(err, errProviderGroupIdentityMismatch) {
		t.Fatalf("reused group terminate = %v, want identity mismatch", err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("reused process group was signalled: %v", err)
	}
}

func TestProviderStoppedAfterReapDoesNotSignalWhenGroupDead(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: processStartIdentity(cmd.Process.Pid)}
	termination, err := newStartedProviderTermination(cmd, record, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := termination.providerStopped(); err != nil {
		t.Fatalf("dead group after reap: %v", err)
	}
	if termination.hasRequested() {
		t.Fatal("providerStopped signalled a reaped group with no descendants")
	}
}

func TestProviderStoppedAfterReapTerminatesDescendants(t *testing.T) {
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("/bin/sh", "-c", `(trap '' TERM; while :; do sleep 1; done) & child=$!; printf '%s\n' "$child" > "$1"; exit 0`, "sh", childPIDPath) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	childPID := waitForProviderChildPID(t, childPIDPath)
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: processStartIdentity(cmd.Process.Pid)}
	termination, err := newStartedProviderTermination(cmd, record, 75*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("leader exit: %v", err)
	}
	if err := termination.providerStopped(); err != nil {
		t.Fatal(err)
	}
	waitForProviderProcessExit(t, childPID)
	waitForProviderProcessGroupExit(t, cmd.Process.Pid)
	stopped = true
}

func TestReapOwnedProviderLeaderDoesNotWaitOnReusedPID(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; while :; do sleep 1; done`) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	current := processStartIdentity(pid)
	if current == "" {
		t.Fatal("process start identity is required for reuse tests")
	}
	reaped, err := reapOwnedProviderLeader(cmd, current+"-other")
	if reaped || !errors.Is(err, errProviderGroupIdentityMismatch) {
		t.Fatalf("reap reused pid = %t, %v", reaped, err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("reap collected a reused process: %v", err)
	}
}

func TestReapDuringTerminationReleasesUnreapedLeader(t *testing.T) {
	unrelated := exec.Command("/bin/sh", "-c", `trap '' TERM; while :; do sleep 1; done`) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(unrelated)
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	pid := unrelated.Process.Pid
	current := processStartIdentity(pid)
	if current == "" {
		t.Fatal("process start identity is required for reuse tests")
	}
	defer func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, 0, nil)
	}()
	group := ownedProviderProcessGroup{pid: pid, pgid: pid, start: current + "-other"}
	termination := &providerTermination{group: group, grace: 50 * time.Millisecond, done: make(chan struct{})}
	termination.request()
	waitErr, reaped := reapDuringTermination(unrelated, current+"-other", termination)
	if reaped {
		t.Fatal("reaped a reused process group leader")
	}
	if !errors.Is(termination.err, errProviderGroupIdentityMismatch) && !errors.Is(waitErr, errProviderGroupIdentityMismatch) {
		t.Fatalf("unproven reused termination = wait %v terminate %v", waitErr, termination.err)
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err == nil || !strings.Contains(err.Error(), "already released") {
		t.Fatalf("unreaped reused leader was not released: %v", err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("released reused leader was signalled: %v", err)
	}
}

func TestProviderTerminationRejectsMismatchedProcessIdentityWithoutSignalling(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; while :; do sleep 1; done`) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	missingIdentity := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid}
	if _, err := newStartedProviderTermination(cmd, missingIdentity, time.Second); err == nil {
		t.Fatal("missing process-start identity produced a termination controller")
	}
	record := RunRecord{PID: cmd.Process.Pid, ProcessGroupID: cmd.Process.Pid, ProcessStartIdentity: "different-start-identity"}
	if _, err := verifiedProviderProcessGroup(record); err == nil {
		t.Fatal("mismatched process identity produced a termination capability")
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("identity rejection signalled unrelated process: %v", err)
	}
}

func waitForProviderChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path) // #nosec G304 -- path is in a test-owned temporary directory.
		if err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed == "" {
				// The shell's `>` redirect can create child.pid before printf
				// writes the PID, so an empty/whitespace-only test-owned file is
				// not yet published; retry until the deadline instead of failing.
				time.Sleep(10 * time.Millisecond)
				continue
			}
			pid, parseErr := strconv.Atoi(trimmed)
			if parseErr != nil || pid <= 0 {
				t.Fatalf("parse child PID %q: %v", data, parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read child PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider child did not record its PID")
	return 0
}

func waitForProviderProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider child %d remained alive after termination", pid)
}

func waitForProviderProcessGroupExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider process group %d remained alive after termination", pid)
}
