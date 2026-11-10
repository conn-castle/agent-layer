package agentdispatch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	museBaselineInitReply = `{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}`
	museBaselineReadReply = `{"jsonrpc":"2.0","id":2,"result":{"session":{"sessionId":"` + runtimeSessionID + `","activeTurnId":null},"viewCursor":"cursor-1","lastTurn":{"turnId":"turn-1","terminal":"completed"}}}`
)

// writeMuseBaselineHost writes a fake `muse` whose `serve` host behaves per
// attempt: attempt N runs behaviors[N-1] (the last one repeats). It counts
// attempts in a sibling file so tests can assert how many hosts started.
func writeMuseBaselineHost(t *testing.T, behaviors ...string) (string, func() int) {
	t.Helper()
	root := t.TempDir()
	counter := filepath.Join(root, "attempts")
	var script strings.Builder
	script.WriteString("#!/bin/sh\n[ \"$1\" = serve ] || exit 64\n")
	script.WriteString("n=$(( $(cat '" + counter + "' 2>/dev/null || echo 0) + 1 ))\necho $n > '" + counter + "'\n")
	for i, behavior := range behaviors {
		condition := "[ $n -eq " + strconv.Itoa(i+1) + " ]"
		if i == len(behaviors)-1 {
			condition = "true"
		}
		script.WriteString("if " + condition + "; then\n" + behavior + "\nfi\n")
	}
	path := filepath.Join(root, "fake-muse")
	if err := os.WriteFile(path, []byte(script.String()), 0o700); err != nil { // #nosec G306 -- executable test fixture.
		t.Fatal(err)
	}
	return path, func() int {
		data, err := os.ReadFile(counter) // #nosec G304 -- test-owned temporary path.
		if err != nil {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return n
	}
}

const (
	museHostHangs          = "echo 'host warming up' >&2\nexec sleep 30"
	museHostHangsAfterInit = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nexec sleep 30"
	museHostExitsEarly     = "printf 'loading config\\nhost crashed\\n\\n' >&2\nexit 3"
	museHostAnswers        = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nIFS= read -r request\nIFS= read -r request\nprintf '%s\\n' '" + museBaselineReadReply + "'\nwhile IFS= read -r request; do :; done"
	museHostRejectsSession = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nIFS= read -r request\nIFS= read -r request\nprintf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":2,\"error\":{\"code\":-32602,\"message\":\"unknown session\"}}'\nwhile IFS= read -r request; do :; done"
)

// Timeouts used by these tests. A host that must answer gets at least
// museAnswerTimeout, so a loaded runner cannot turn an answer into a timeout;
// a host that must time out without answering anything uses museHangTimeout.
const (
	museAnswerTimeout = 2 * time.Second
	museHangTimeout   = 500 * time.Millisecond
	museNeverTimeout  = 30 * time.Second
)

func testMuseCommand(path string) providerCommand {
	return providerCommand{Path: path, Env: os.Environ(), Provider: AgentMuse, SessionID: runtimeSessionID}
}

// testMuseStderr is where readTestMuseBaseline sends host stderr.
func testMuseStderr(path string) string {
	return filepath.Join(filepath.Dir(path), museBaselineStderrFile)
}

// readTestMuseBaseline sends host stderr to testMuseStderr(path) unless the
// policy names another file.
func readTestMuseBaseline(path string, read museBaselineRead) (*museRecoveryBaseline, error) {
	if read.stderrPath == "" {
		read.stderrPath = testMuseStderr(path)
	}
	return readMuseRecoveryBaseline(testMuseCommand(path), filepath.Dir(path), read)
}

// stderrSuffix is the error detail for line written to testMuseStderr(path).
func stderrSuffix(path, line string) string {
	return "; muse serve stderr: " + line + " (full stderr: " + testMuseStderr(path) + ")"
}

// #526: a host that hangs once must not fail the continue with a closed-pipe error.
func TestMuseRecoveryBaselineRetriesHungHost(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
	var logged []string
	baseline, err := readTestMuseBaseline(path, museBaselineRead{
		attempts: museBaselineAttempts, timeout: museAnswerTimeout,
		stop:     func() error { return nil },
		logRetry: func(line string) { logged = append(logged, line) },
	})
	if err != nil {
		t.Fatalf("baseline error = %v", err)
	}
	if baseline.ViewCursor != "cursor-1" || baseline.TerminalRootID != "turn-1" || attempts() != 2 {
		t.Fatalf("baseline = %+v, attempts = %d", baseline, attempts())
	}
	want := "agent-layer: Muse recovery baseline attempt 1 failed, retrying with a fresh host: muse serve did not answer initialize within 2s" + stderrSuffix(path, "host warming up")
	if len(logged) != 1 || logged[0] != want {
		t.Fatalf("retry log = %q", logged)
	}
}

func TestMuseRecoveryBaselineReportsTimeoutStep(t *testing.T) {
	for _, test := range []struct {
		name, behavior string
		read           museBaselineRead
		want, stderr   string
	}{
		{"initialize exhausted", museHostHangs, museBaselineRead{attempts: 2, timeout: museHangTimeout}, "muse serve did not answer initialize within 500ms (2 attempts)", "host warming up"},
		{"session read single", museHostHangsAfterInit, museBaselineRead{attempts: 1, timeout: museAnswerTimeout}, "muse serve did not answer session/read within 2s", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, attempts := writeMuseBaselineHost(t, test.behavior)
			_, err := readTestMuseBaseline(path, test.read)
			want := test.want
			if test.stderr != "" {
				want += stderrSuffix(path, test.stderr)
			}
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			if attempts() != test.read.attempts {
				t.Fatalf("attempts = %d", attempts())
			}
		})
	}
}

// A host descendant that inherits stderr and outlives the host must not hold
// the read: stderr is a file, so nothing waits for its writers to close it.
func TestMuseRecoveryBaselineIgnoresStderrHeldByDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	path, _ := writeMuseBaselineHost(t, "sleep 30 </dev/null >/dev/null &\necho $! > '"+pidFile+"'\n"+museHostAnswers)
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile) // #nosec G304 -- test-owned temporary path.
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && convErr == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	started := time.Now()
	if _, err := readTestMuseBaseline(path, museBaselineRead{attempts: 1, timeout: museNeverTimeout}); err != nil {
		t.Fatalf("baseline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("baseline read took %s; it waited for the descendant holding stderr", elapsed)
	}
}

// Every attempt appends to the run's stderr file; an error reports only the
// failing attempt's last line, and no file means stderr is discarded.
func TestMuseRecoveryBaselineStderrFile(t *testing.T) {
	path, _ := writeMuseBaselineHost(t, museHostHangs, museHostExitsEarly)
	if _, err := readTestMuseBaseline(path, museBaselineRead{attempts: museBaselineAttempts, timeout: museAnswerTimeout}); err == nil {
		t.Fatal("failing hosts returned a baseline")
	}
	data, err := os.ReadFile(testMuseStderr(path)) // #nosec G304 -- test-owned temporary path.
	if err != nil || string(data) != "host warming up\nloading config\nhost crashed\n\n" {
		t.Fatalf("stderr file = %q, %v", data, err)
	}
	info, err := os.Stat(testMuseStderr(path))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stderr file mode = %v, %v", info, err)
	}

	discarded, _ := writeMuseBaselineHost(t, museHostExitsEarly)
	_, err = readMuseRecoveryBaseline(testMuseCommand(discarded), filepath.Dir(discarded), museBaselineRead{attempts: 1, timeout: museNeverTimeout})
	if err == nil || strings.Contains(err.Error(), "stderr") {
		t.Fatalf("error without a stderr file = %v", err)
	}
	if _, statErr := os.Stat(testMuseStderr(discarded)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stderr file created without a path: %v", statErr)
	}
}

// A host that exits fails the same way on a fresh host, so it is not retried.
func TestMuseRecoveryBaselineDoesNotRetryExitedHost(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, museHostExitsEarly)
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: museBaselineAttempts, timeout: museNeverTimeout, logRetry: func(line string) { t.Errorf("retried: %s", line) }})
	if err == nil || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
	message := err.Error()
	if !strings.HasPrefix(message, "muse serve stopped before answering initialize after ") || !strings.Contains(message, "(exit status 3): ") || !strings.HasSuffix(message, stderrSuffix(path, "host crashed")) || strings.Contains(message, "\n") {
		t.Fatalf("error = %q", message)
	}
}

// A clean exit is still the host's own status, not this read's cleanup.
func TestMuseRecoveryBaselineReportsExitStatusZero(t *testing.T) {
	path, _ := writeMuseBaselineHost(t, "exit 0")
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: 1, timeout: museNeverTimeout})
	if err == nil || !strings.Contains(err.Error(), "(exit status 0): ") {
		t.Fatalf("error = %v", err)
	}
}

// A host that closes stdout before it finishes exiting keeps its exit status.
func TestMuseRecoveryBaselineWaitsForExitingHost(t *testing.T) {
	path, _ := writeMuseBaselineHost(t, "exec >&-\nsleep 0.2\necho 'host crashed' >&2\nexit 1")
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: 1, timeout: museNeverTimeout})
	if err == nil || !strings.Contains(err.Error(), "(exit status 1): ") || !strings.HasSuffix(err.Error(), stderrSuffix(path, "host crashed")) {
		t.Fatalf("error = %v", err)
	}
}

// A host that closes its pipes but keeps running is killed after the grace
// and reports no exit status.
func TestMuseRecoveryBaselineOmitsStatusForCleanupKill(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, "exec >&- <&-\nexec sleep 30")
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: museBaselineAttempts, timeout: museNeverTimeout})
	if err == nil || !strings.HasPrefix(err.Error(), "muse serve stopped before answering initialize after ") || strings.Contains(err.Error(), "exit status") || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

// A retry that fails differently still reports that the first attempt timed out.
func TestMuseRecoveryBaselineRetryFailureKeepsEarlierTimeout(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostExitsEarly)
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: museBaselineAttempts, timeout: museAnswerTimeout})
	if err == nil || !errors.Is(err, io.EOF) || attempts() != 2 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
	if message := err.Error(); !strings.HasPrefix(message, "muse serve stopped before answering initialize after ") || !strings.HasSuffix(message, stderrSuffix(path, "host crashed")+" (after attempt 1 timed out waiting for initialize)") {
		t.Fatalf("error = %q", message)
	}
}

// A protocol answer is authoritative: retrying would not change it.
func TestMuseRecoveryBaselineDoesNotRetryProtocolAnswer(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, museHostRejectsSession)
	_, err := readTestMuseBaseline(path, museBaselineRead{attempts: museBaselineAttempts, timeout: museNeverTimeout})
	if err == nil || !strings.HasPrefix(err.Error(), "session/read: ") || !strings.Contains(err.Error(), "unknown session") || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

// stop is checked before every attempt, so a refused run starts no host and
// a refusal after a hang starts no retry.
func TestMuseRecoveryBaselineStops(t *testing.T) {
	for _, test := range []struct {
		name      string
		allowed   int
		wantHosts int
	}{
		{"before first attempt", 0, 0},
		{"before retry", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
			refused := errors.New("dispatch run was cancelled before provider launch")
			checks := 0
			_, err := readTestMuseBaseline(path, museBaselineRead{
				attempts: museBaselineAttempts, timeout: museHangTimeout,
				stop: func() error {
					checks++
					if checks > test.allowed {
						return refused
					}
					return nil
				},
				logRetry: func(line string) { t.Errorf("retried: %s", line) },
			})
			var stopped *museBaselineStopped
			if !errors.As(err, &stopped) || stopped.err != refused || attempts() != test.wantHosts {
				t.Fatalf("error = %v, hosts = %d", err, attempts())
			}
		})
	}
}

// executeProvider returns the fence's own refusal, not a wrapped baseline
// failure, and never starts a baseline host for a refused run.
func TestExecuteProviderMuseBaselineRefusalKeepsExitCode(t *testing.T) {
	for _, test := range []struct {
		name   string
		refuse func(*RunRecord)
		code   int
		want   string
	}{
		{"launch fenced", func(record *RunRecord) { record.LaunchFenced = true }, ExitUnavailable, "launch is fenced"},
		{"cancelled", func(record *RunRecord) {
			now := time.Now().UTC()
			record.State, record.CompletedAt = dispatchStateCancelled, &now
		}, ExitTargetFailure, "was cancelled before provider launch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path, attempts := writeMuseBaselineHost(t, museHostAnswers)
			run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeResume)
			if err != nil {
				t.Fatal(err)
			}
			test.refuse(&run.Record)
			if err := writeRunRecord(run.Dir, &run.Record); err != nil {
				t.Fatal(err)
			}
			command := testMuseCommand(path)
			command.Args, command.RunMode = []string{execSubcommand}, dispatchModeResume
			_, err = executeProvider(command, nil, run, root, nil, func(string) error { return nil })
			requireDispatchExitCode(t, err, test.code)
			if !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "read Muse recovery baseline") || attempts() != 0 {
				t.Fatalf("error = %v, hosts = %d", err, attempts())
			}
		})
	}
}

// The resumed-root check runs inside the provider's stdout handler, so a hung
// host costs one bounded attempt and never a retry.
func TestMuseResumedRootCheckMakesOneAttempt(t *testing.T) {
	previous := museResumedRootTimeout
	museResumedRootTimeout = museHangTimeout
	t.Cleanup(func() { museResumedRootTimeout = previous })
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
	_, err := verifyMuseResumedRoot(testMuseCommand(path), filepath.Dir(path), testMuseStderr(path), &museRecoveryBaseline{SessionID: runtimeSessionID, ViewCursor: "cursor-0"}, "turn-1")
	if err == nil || err.Error() != "muse serve did not answer initialize within 500ms"+stderrSuffix(path, "host warming up") || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

func TestSanitizeMuseStderrLine(t *testing.T) {
	long := strings.Repeat("a", museBaselineStderrLineLimit-1) + "é" + "tail"
	for _, test := range []struct{ name, tail, want string }{
		{"empty", "", ""},
		{"last non-empty line", "first\nsecond\r\n\n  \n", "second"},
		{"control characters", "bad\x1b[31mcolor\tend\n", "bad [31mcolor end"},
		{"split leading rune", "\xa9partial\n", "�partial"},
		{"long line on rune boundary", long + "\n", strings.Repeat("a", museBaselineStderrLineLimit-1) + "..."},
		{"unicode separators and format runes", "line\u2028sep\u2029para \u202Eevil\u200Bzero\n", "line sep para  evil zero"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitizeMuseStderrLine([]byte(test.tail)); got != test.want {
				t.Fatalf("line = %q, want %q", got, test.want)
			}
		})
	}
}

// The pre-launch read stops with the same refusal startFencedProvider gives.
func TestDispatchLaunchRefused(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatchLaunchRefused(root, run.Record.ID); err != nil {
		t.Fatalf("pending run refused: %v", err)
	}
	run.Record.LaunchFenced = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := dispatchLaunchRefused(root, run.Record.ID); err == nil || err.Error() != startFencedProviderError(run.Record, nil).Error() {
		t.Fatalf("fenced run error = %v", err)
	}
}

// The in-stream check reports why verification failed, on one line.
func TestMuseReducerReportsResumedRootVerifyError(t *testing.T) {
	const root = "01a132d6-69c2-7b51-bf05-e8b24f9711f3"
	reducer := &museReducer{expectedSession: runtimeSessionID, candidateRunID: root, recovery: &museRecoveryBaseline{
		SessionID: runtimeSessionID, ViewCursor: "cursor-0",
		verifyRoot: func(string) (*museRecoveryBaseline, error) {
			return nil, errors.New("muse serve did not answer initialize within 10s\nsecond line")
		},
	}}
	events := reducer.reduce(map[string]any{
		"schema_version": 1, "stream": map[string]any{"kind": "session", "id": runtimeSessionID},
		"payload_type": "run.terminal.completed",
		"payload":      map[string]any{"command_id": root, "run_stream": map[string]any{"kind": "run", "id": root}},
	})
	want := "Muse resumed terminal has no authoritative root/cursor evidence: muse serve did not answer initialize within 10s second line"
	if len(events) != 1 || events[0].Kind != eventFailure || events[0].Reason != want {
		t.Fatalf("events = %+v", events)
	}
}

func TestAppendWorkerLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, workerLogFile), []byte("worker started\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendWorkerLog(dir, "retrying")
	data, err := os.ReadFile(filepath.Join(dir, workerLogFile)) // #nosec G304 -- test-owned temporary path.
	if err != nil || string(data) != "worker started\nretrying\n" {
		t.Fatalf("worker.log = %q, %v", data, err)
	}
}
