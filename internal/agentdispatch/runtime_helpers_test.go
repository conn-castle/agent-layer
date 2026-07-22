package agentdispatch

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
