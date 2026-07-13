package agentdispatch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSignalProviderProcessTerminatesProviderProcessGroup(t *testing.T) {
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("/bin/sh", "-c", `sleep 30 & child=$!; printf '%s\n' "$child" > "$1"; wait "$child"`, "sh", childPIDPath) // #nosec G204 -- fixed test-only shell command.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start provider process group: %v", err)
	}
	waitCalled := false
	terminated := false
	t.Cleanup(func() {
		if terminated {
			return
		}
		// Keep the group SIGKILL armed until both the child and the group are
		// confirmed dead. If an exit assertion below fails, the orphaned child
		// is still alive and keeps the process group ID reserved, so signalling
		// -pid reliably reaps the leaked sleeper.
		signalProviderProcess(cmd, syscall.SIGKILL)
		if !waitCalled {
			_ = cmd.Wait()
		}
	})

	childPID := waitForProviderChildPID(t, childPIDPath)
	signalProviderProcess(cmd, syscall.SIGTERM)
	waitCalled = true
	if err := cmd.Wait(); err == nil {
		t.Fatal("provider process unexpectedly exited successfully after SIGTERM")
	}
	waitForProviderProcessExit(t, childPID)
	waitForProviderProcessGroupExit(t, cmd.Process.Pid)
	terminated = true
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
	t.Fatalf("provider child %d remained alive after SIGTERM", pid)
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
	t.Fatalf("provider process group %d remained alive after SIGTERM", pid)
}
