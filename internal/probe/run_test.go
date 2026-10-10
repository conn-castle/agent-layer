package probe

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
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
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
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 7 || strings.Contains(err.Error(), "process group") {
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
