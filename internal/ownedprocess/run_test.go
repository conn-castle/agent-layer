package ownedprocess

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunOwnsDescendants(t *testing.T) {
	sentinel := exec.Command("/bin/sleep", "30")
	sentinel.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sentinel.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() }()
	for _, mode := range []string{"deadline", "exited", "closed", "nonzero"} {
		t.Run(mode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "child")
			script := `trap '' TERM; /bin/sleep 30 & echo $! > "$1"; echo retained; `
			switch mode {
			case "exited":
				script += "exit 0"
			case "closed":
				script = `exec >/dev/null 2>&1; ` + script + "wait"
			case "nonzero":
				script += "exit 7"
			default:
				script += "wait"
			}
			// Only the timeout modes depend on the deadline; exit modes get slack
			// so a slow start under -race cannot turn an exit into a timeout.
			timeout := 5 * time.Second
			if mode == "deadline" || mode == "closed" {
				timeout = 300 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			t.Cleanup(func() {
				if data, err := os.ReadFile(pidFile); t.Failed() && err == nil { // #nosec G304 -- owned fixture PID path.
					if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			var output bytes.Buffer
			cmd := exec.Command("/bin/sh", "-c", script, "fixture", pidFile)
			cmd.Stdout, cmd.Stderr = &output, &output
			start := time.Now()
			err := Run(ctx, cmd)
			if time.Since(start) > 2*time.Second {
				t.Fatalf("unbounded return: %v", err)
			}
			if mode == "deadline" || mode == "closed" {
				if !errors.Is(err, ctx.Err()) || ctx.Err() == nil || strings.Contains(err.Error(), "process group") {
					t.Fatalf("context outcome: %v", err)
				}
			} else if mode == "nonzero" {
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 7 || strings.Contains(err.Error(), "process group") {
					t.Fatalf("exit outcome: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(pidFile) // #nosec G304 -- owned fixture PID path.
			if readErr != nil {
				t.Fatal(readErr)
			}
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			state, psErr := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
			var exit *exec.ExitError
			if parseErr != nil || pid <= 0 || (psErr != nil && (!errors.As(psErr, &exit) || exit.ExitCode() != 1)) || (psErr == nil && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z")) {
				t.Fatalf("child stop unproven: pid=%q parse=%v state=%q ps=%v", data, parseErr, state, psErr)
			}
			if mode != "closed" && !strings.Contains(output.String(), "retained") {
				t.Fatal("output lost")
			}
			if pid, err := syscall.Wait4(sentinel.Process.Pid, nil, syscall.WNOHANG, nil); err != nil || pid != 0 {
				t.Fatalf("unrelated sentinel: %v", err)
			}
		})
	}
}

func TestReapBoundsLiveLeaderAndDetachesOutput(t *testing.T) {
	var output bytes.Buffer
	guard := &outputGuard{}
	cmd := exec.Command("/bin/sh", "-c", "while :; do echo retained; done")
	cmd.Stdout = &guardedWriter{guard, &output}
	cmd.Stderr = &guardedWriter{guard, &output}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	start := time.Now()
	// Exercise failed stop proof with a live, writing leader; SIGKILL cannot
	// reliably simulate a process stuck in uninterruptible sleep in a test.
	err := reap(cmd, guard, true)
	if err == nil || !strings.Contains(err.Error(), "reap") || time.Since(start) > 2*cleanupAllowance {
		t.Fatalf("bounded reap: %v (%v)", err, time.Since(start))
	}
	if !strings.Contains(output.String(), "retained") {
		t.Fatal("output lost before detachment")
	}
}

// TestRunPreservesSession proves a requested session has no inherited session
// leader and its retained-output child is stopped before the successful return.
func TestRunPreservesSession(t *testing.T) {
	dir := t.TempDir()
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var output bytes.Buffer
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM
/bin/sleep 30 &
printf '%s %s\n' "$$" "$!" > "$1.tmp"
mv "$1.tmp" "$1"
while [ ! -f "$2" ]; do /bin/sleep 0.01; done
printf 'retained\n'
exit 0`, "fixture", ready, release)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = &output, &output
	done := make(chan error, 1)
	received := false
	t.Cleanup(func() {
		cancel()
		// Only a failed test may still own live fixtures; a passing one proved
		// they stopped, so signaling could hit a reused PID.
		pids, _ := sessionFixturePIDs(ready)
		for _, pid := range pids {
			if t.Failed() && !sessionFixtureStopped(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		if !received {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("session fixture did not join after cleanup")
			}
		}
	})
	go func() { done <- Run(ctx, cmd) }()
	deadline := time.Now().Add(5 * time.Second)
	var pids []int
	for {
		var err error
		pids, err = sessionFixturePIDs(ready)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session fixture readiness: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	session, sessionErr := unix.Getsid(pids[0])
	if err := os.WriteFile(release+".tmp", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(release+".tmp", release); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		received = true
		if err != nil {
			t.Fatalf("session outcome: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session command blocked on retained output")
	}
	if sessionErr != nil || session != pids[0] {
		t.Fatalf("leader %d session=%d err=%v", pids[0], session, sessionErr)
	}
	if output.String() != "retained\n" {
		t.Fatalf("output lost: %q", output.String())
	}
	for _, pid := range pids {
		if !sessionFixtureStopped(pid) {
			t.Errorf("session process %d still running after Run returned", pid)
		}
	}
}

// sessionFixturePIDs accepts a complete atomic readiness record for both PIDs.
func sessionFixturePIDs(path string) ([]int, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- owned fixture readiness path.
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(content))
	if len(fields) != 2 || !strings.HasSuffix(string(content), "\n") {
		return nil, errors.New("incomplete session readiness record")
	}
	pids := make([]int, 2)
	for i, field := range fields {
		pids[i], err = strconv.Atoi(field)
		if err != nil || pids[i] <= 0 {
			return nil, errors.New("invalid session fixture PID")
		}
	}
	return pids, nil
}

// TestNextObservationDelay keeps initial checks quick and long-running checks
// capped at the existing steady interval.
func TestNextObservationDelay(t *testing.T) {
	var delay time.Duration
	for i, want := range []time.Duration{5, 10, 20, 40, 80, 160, 250, 250, 250} {
		delay = nextObservationDelay(delay)
		if delay != want*time.Millisecond {
			t.Fatalf("delay %d: got %s, want %s", i, delay, want*time.Millisecond)
		}
	}
}

// sessionFixtureStopped accepts vanished, zombie and dead fixture processes.
func sessionFixtureStopped(pid int) bool {
	state, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		return errors.As(err, &exit) && exit.ExitCode() == 1
	}
	stat := strings.TrimSpace(string(state))
	return stat == "" || strings.ContainsAny(stat[:1], "ZXx")
}
