package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRunnerCancellationStopsGitTransportDescendants exercises real Git's SSH
// transport with helpers that ignore graceful signals and retain output pipes.
func TestRunnerCancellationStopsGitTransportDescendants(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	helper := filepath.Join(dir, "ssh")
	writeProcessFixture(t, helper, `trap '' INT TERM
/bin/sh -c "trap '' INT TERM; exec /bin/sleep 30" &
printf '%s %s\n' "$$" "$!" > "$READY.tmp"
mv "$READY.tmp" "$READY"
wait
`)
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", helper)
	t.Setenv("GIT_SSH_VARIANT", "ssh")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("READY", ready)
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git is required: %v", err)
	}
	call := startGitProcessCall(t, ready, func(ctx context.Context) gitProcessResult {
		out, err := runner.run(ctx, dir, "ls-remote", "--", "ssh://fixture.invalid/owned.git")
		return gitProcessResult{stdout: out, err: err}
	})
	pids := waitGitFixturePIDs(t, ready, 2)
	helperSession, sessionErr := unix.Getsid(pids[0])
	testSession, testSessionErr := unix.Getsid(0)
	call.cancel()
	result := call.await(t)
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancellation outcome: %v", result.err)
	}
	assertGitFixtureStopped(t, pids)
	if sessionErr != nil || testSessionErr != nil || helperSession == testSession {
		t.Fatalf("transport session=%d (%v), test session=%d (%v)", helperSession, sessionErr, testSession, testSessionErr)
	}
}

// TestRunnerJoinsDescendantsAfterGitExits preserves successful and allowed-exit
// output while stopping children whose inherited pipes would otherwise block EOF.
func TestRunnerJoinsDescendantsAfterGitExits(t *testing.T) {
	for _, code := range []int{0, 1} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			script := filepath.Join(dir, "git")
			writeProcessFixture(t, script, `trap '' TERM
/bin/sleep 30 &
printf '%s %s\n' "$$" "$!" > "$READY.tmp"
mv "$READY.tmp" "$READY"
printf 'payload\n'
exit "$EXIT_CODE"
`)
			runner := &Runner{path: script, env: append(nonInteractiveEnv(os.Environ()), "READY="+ready, "EXIT_CODE="+strconv.Itoa(code)), secrets: NewSecrets(nil)}
			call := startGitProcessCall(t, ready, func(ctx context.Context) gitProcessResult {
				if code == 0 {
					out, err := runner.run(ctx, dir, "fixture")
					return gitProcessResult{stdout: out, err: err}
				}
				out, exitCode, err := runner.runAllowExit(ctx, dir, []int{1}, "fixture")
				return gitProcessResult{stdout: out, code: exitCode, err: err}
			})
			pids := waitGitFixturePIDs(t, ready, 2)
			result := call.await(t)
			if result.err != nil || result.code != code || string(result.stdout) != "payload\n" {
				t.Fatalf("exit outcome: stdout=%q code=%d err=%v", result.stdout, result.code, result.err)
			}
			assertGitFixtureStopped(t, pids)
		})
	}
}

// TestRunnerAllowedExitCannotHideCancellation rejects an allowed signal exit
// when it accompanies cancellation, including through CommandError.Unwrap.
func TestRunnerAllowedExitCannotHideCancellation(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	script := filepath.Join(dir, "git")
	writeProcessFixture(t, script, `trap '' TERM
/bin/sleep 30 &
printf '%s %s\n' "$$" "$!" > "$READY.tmp"
mv "$READY.tmp" "$READY"
wait
`)
	runner := &Runner{path: script, env: append(nonInteractiveEnv(os.Environ()), "READY="+ready), secrets: NewSecrets(nil)}
	call := startGitProcessCall(t, ready, func(ctx context.Context) gitProcessResult {
		out, code, err := runner.runAllowExit(ctx, dir, []int{1, -1, 137}, "fixture")
		return gitProcessResult{stdout: out, code: code, err: err}
	})
	pids := waitGitFixturePIDs(t, ready, 2)
	call.cancel()
	result := call.await(t)
	if !errors.Is(result.err, context.Canceled) || result.code != -1 {
		t.Fatalf("allowed exit hid cancellation: code=%d err=%v", result.code, result.err)
	}
	assertGitFixtureStopped(t, pids)
}

// TestRunnerHangupStopsGitBeforeExiting proves a terminal hangup, which Git's
// own session no longer receives, stops Git and its descendants and then still
// ends this process. A re-executed test binary takes the hangup.
func TestRunnerHangupStopsGitBeforeExiting(t *testing.T) {
	if dir := os.Getenv("GITREPO_HANGUP_FIXTURE"); dir != "" {
		runHangupFixture(dir)
		return
	}
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("SIGHUP is ignored by this test process")
	}
	dir := t.TempDir()
	writeProcessFixture(t, filepath.Join(dir, "git"), `trap '' TERM
/bin/sleep 30 &
printf '%s %s\n' "$$" "$!" > "$READY.tmp"
mv "$READY.tmp" "$READY"
wait
`)
	ready := filepath.Join(dir, "ready")
	t.Cleanup(func() {
		pids, _ := readGitFixturePIDs(ready)
		for _, pid := range pids {
			if t.Failed() && !gitFixtureStopped(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunnerHangupStopsGitBeforeExiting$") // #nosec G204 G702 -- re-executes this test binary.
	cmd.Env = append(os.Environ(), "GITREPO_HANGUP_FIXTURE="+dir)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("fixture survived hangup: %v\n%s", err, output)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGHUP {
		t.Fatalf("fixture outcome %v, want SIGHUP termination\n%s", err, output)
	}
	pids, err := readGitFixturePIDs(ready)
	if err != nil || len(pids) != 2 {
		t.Fatalf("fixture readiness: pids=%v err=%v\n%s", pids, err, output)
	}
	assertGitFixtureStopped(t, pids)
}

// runHangupFixture blocks in a Git call, hangs itself up and must be ended by
// the re-raised hangup; any return reports the defect through its exit status.
func runHangupFixture(dir string) {
	ready := filepath.Join(dir, "ready")
	runner := &Runner{path: filepath.Join(dir, "git"), env: append(nonInteractiveEnv(os.Environ()), "READY="+ready), secrets: NewSecrets(nil)}
	done := make(chan error, 1)
	go func() {
		_, err := runner.run(context.Background(), dir, "fixture")
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pids, err := readGitFixturePIDs(ready); err == nil && len(pids) == 2 {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(3)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		os.Exit(4)
	}
	select {
	case <-done:
		// The re-raised hangup is asynchronous; give it time to end the process.
		time.Sleep(5 * time.Second)
		os.Exit(5)
	case <-time.After(10 * time.Second):
		os.Exit(6)
	}
}

type gitProcessResult struct {
	stdout []byte
	code   int
	err    error
}

type gitProcessCall struct {
	done     chan gitProcessResult
	cancel   context.CancelFunc
	received bool
}

// startGitProcessCall registers cleanup before launch so even a failing return
// bound kills all recorded fixture processes and joins the command goroutine.
func startGitProcessCall(t *testing.T, ready string, run func(context.Context) gitProcessResult) *gitProcessCall {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	call := &gitProcessCall{done: make(chan gitProcessResult, 1), cancel: cancel}
	t.Cleanup(func() {
		cancel()
		// A passing test already proved every fixture stopped; signaling then
		// could hit a reused PID.
		pids, _ := readGitFixturePIDs(ready)
		for _, pid := range pids {
			if t.Failed() && !gitFixtureStopped(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		if !call.received {
			select {
			case <-call.done:
			case <-time.After(10 * time.Second):
				t.Error("Git call did not join after fixture cleanup")
			}
		}
	})
	go func() { call.done <- run(ctx) }()
	return call
}

// await bounds each call well below the fixture's 30-second sleep.
func (call *gitProcessCall) await(t *testing.T) gitProcessResult {
	t.Helper()
	select {
	case result := <-call.done:
		call.received = true
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("Git call blocked on retained output for 5s")
		return gitProcessResult{}
	}
}

// writeProcessFixture writes a shell fixture executable only by its owner.
func writeProcessFixture(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil { // #nosec G306 -- owner-executable shell fixture.
		t.Fatal(err)
	}
}

// readGitFixturePIDs accepts only complete, atomically published PID records.
func readGitFixturePIDs(path string) ([]int, error) {
	content, err := os.ReadFile(path) // #nosec G304 G703 -- owned fixture readiness path.
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(string(content), "\n") {
		return nil, errors.New("incomplete fixture readiness record")
	}
	var pids []int
	for _, field := range strings.Fields(string(content)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid fixture PID %q", field)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// waitGitFixturePIDs waits for synchronization without assuming process speed.
func waitGitFixturePIDs(t *testing.T, path string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pids, err := readGitFixturePIDs(path)
		if err == nil && len(pids) == count {
			return pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture readiness: pids=%v err=%v", pids, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// gitFixtureStopped treats zombies and dead processes as stopped, like Run.
func gitFixtureStopped(pid int) bool {
	state, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		return errors.As(err, &exit) && exit.ExitCode() == 1
	}
	stat := strings.TrimSpace(string(state))
	return stat == "" || strings.HasPrefix(stat, "Z") || strings.HasPrefix(stat, "X") || strings.HasPrefix(stat, "x")
}

// assertGitFixtureStopped checks immediately after return, before test cleanup.
func assertGitFixtureStopped(t *testing.T, pids []int) {
	t.Helper()
	for _, pid := range pids {
		if !gitFixtureStopped(pid) {
			t.Errorf("fixture process %d still running after Git returned", pid)
		}
	}
}
