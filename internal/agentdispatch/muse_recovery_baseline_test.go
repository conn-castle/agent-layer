package agentdispatch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func readTestMuseBaseline(path string, read museBaselineRead) (*museRecoveryBaseline, error) {
	return readMuseRecoveryBaseline(testMuseCommand(path), filepath.Dir(path), read)
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
	want := "agent-layer: Muse recovery baseline attempt 1 failed, retrying with a fresh host: muse serve did not answer initialize within 2s; muse serve stderr: host warming up"
	if len(logged) != 1 || logged[0] != want {
		t.Fatalf("retry log = %q", logged)
	}
}

func TestMuseRecoveryBaselineReportsTimeoutStep(t *testing.T) {
	for _, test := range []struct {
		name, behavior string
		read           museBaselineRead
		want           string
	}{
		{"initialize exhausted", museHostHangs, museBaselineRead{attempts: 2, timeout: museHangTimeout}, "muse serve did not answer initialize within 500ms (2 attempts); muse serve stderr: host warming up"},
		{"session read single", museHostHangsAfterInit, museBaselineRead{attempts: 1, timeout: museAnswerTimeout}, "muse serve did not answer session/read within 2s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, attempts := writeMuseBaselineHost(t, test.behavior)
			_, err := readTestMuseBaseline(path, test.read)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if attempts() != test.read.attempts {
				t.Fatalf("attempts = %d", attempts())
			}
		})
	}
}

// A host descendant that inherits stderr and outlives the host must not hold
// the read until it exits: the drain gets a bounded grace after Wait.
func TestMuseRecoveryBaselineIgnoresStderrHeldByDescendant(t *testing.T) {
	previous := museStderrDrainGrace
	museStderrDrainGrace = 200 * time.Millisecond
	t.Cleanup(func() { museStderrDrainGrace = previous })
	path, _ := writeMuseBaselineHost(t, "sleep 30 </dev/null >/dev/null &\n"+museHostAnswers)
	started := time.Now()
	if _, err := readTestMuseBaseline(path, museBaselineRead{attempts: 1, timeout: museNeverTimeout}); err != nil {
		t.Fatalf("baseline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 10*time.Second {
		t.Fatalf("baseline read took %s; it waited for the descendant holding stderr", elapsed)
	}
}

func TestMuseStderrTailKeepsLastBytes(t *testing.T) {
	var tail museStderrTail
	data := strings.Repeat("x", 3*museBaselineStderrTail) + "last line\n"
	tail.drain(strings.NewReader(data))
	if got := string(tail.bytes()); got != data[len(data)-museBaselineStderrTail:] || cap(tail.data) > 4*museBaselineStderrTail {
		t.Fatalf("tail = %d bytes ending %q, cap %d", len(got), got[len(got)-10:], cap(tail.data))
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
	if !strings.HasPrefix(message, "muse serve stopped before answering initialize after ") || !strings.Contains(message, "(exit status 3): ") || !strings.HasSuffix(message, "; muse serve stderr: host crashed") || strings.Contains(message, "\n") {
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

// A host that closes its pipes and is then killed by this read reports no exit status.
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
	if message := err.Error(); !strings.HasPrefix(message, "muse serve stopped before answering initialize after ") || !strings.HasSuffix(message, "; muse serve stderr: host crashed (after attempt 1 timed out waiting for initialize)") {
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

func TestMuseRecoveryBaselineStopsBeforeRetry(t *testing.T) {
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
	cancelled := errors.New("dispatch run was cancelled before provider launch")
	_, err := readTestMuseBaseline(path, museBaselineRead{
		attempts: museBaselineAttempts, timeout: museHangTimeout,
		stop:     func() error { return cancelled },
		logRetry: func(line string) { t.Errorf("retried: %s", line) },
	})
	if !errors.Is(err, cancelled) || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

// The resumed-root check runs inside the provider's stdout handler, so a hung
// host costs one bounded attempt and never a retry.
func TestMuseResumedRootCheckMakesOneAttempt(t *testing.T) {
	previous := museResumedRootTimeout
	museResumedRootTimeout = museHangTimeout
	t.Cleanup(func() { museResumedRootTimeout = previous })
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
	_, err := verifyMuseResumedRoot(testMuseCommand(path), filepath.Dir(path), &museRecoveryBaseline{SessionID: runtimeSessionID, ViewCursor: "cursor-0"}, "turn-1")
	if err == nil || err.Error() != "muse serve did not answer initialize within 500ms; muse serve stderr: host warming up" || attempts() != 1 {
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
