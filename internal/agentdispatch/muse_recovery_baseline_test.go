package agentdispatch

import (
	"bytes"
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
	museHostHangs           = "echo 'host warming up' >&2\nexec sleep 30"
	museHostHangsAfterInit  = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nexec sleep 30"
	museHostExitsEarly      = "echo 'host crashed' >&2\nexit 3"
	museHostAnswers         = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nIFS= read -r request\nIFS= read -r request\nprintf '%s\\n' '" + museBaselineReadReply + "'\nwhile IFS= read -r request; do :; done"
	museHostRejectsSession  = "IFS= read -r request\nprintf '%s\\n' '" + museBaselineInitReply + "'\nIFS= read -r request\nIFS= read -r request\nprintf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":2,\"error\":{\"code\":-32602,\"message\":\"unknown session\"}}'\nwhile IFS= read -r request; do :; done"
	museBaselineTestTimeout = 300 * time.Millisecond
)

func shrinkMuseBaselineTimeout(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousTimeout, previousDiagnostics := museBaselineAttemptTimeout, museBaselineDiagnostics
	diagnostics := &bytes.Buffer{}
	museBaselineAttemptTimeout, museBaselineDiagnostics = museBaselineTestTimeout, diagnostics
	t.Cleanup(func() { museBaselineAttemptTimeout, museBaselineDiagnostics = previousTimeout, previousDiagnostics })
	return diagnostics
}

func readTestMuseBaseline(path string) (*museRecoveryBaseline, error) {
	return readMuseRecoveryBaseline(providerCommand{Path: path, Env: os.Environ(), Provider: AgentMuse, SessionID: runtimeSessionID}, filepath.Dir(path))
}

// #526: a host that hangs once must not fail the continue with a closed-pipe error.
func TestMuseRecoveryBaselineRetriesHungHost(t *testing.T) {
	diagnostics := shrinkMuseBaselineTimeout(t)
	path, attempts := writeMuseBaselineHost(t, museHostHangs, museHostAnswers)
	baseline, err := readTestMuseBaseline(path)
	if err != nil {
		t.Fatalf("baseline error = %v", err)
	}
	if baseline.ViewCursor != "cursor-1" || baseline.TerminalRootID != "turn-1" || attempts() != 2 {
		t.Fatalf("baseline = %+v, attempts = %d", baseline, attempts())
	}
	logged := diagnostics.String()
	if !strings.Contains(logged, "attempt 1 failed, retrying") || !strings.Contains(logged, "did not answer initialize within 300ms") || !strings.Contains(logged, "host warming up") {
		t.Fatalf("diagnostics = %q", logged)
	}
}

func TestMuseRecoveryBaselineReportsTimeoutStep(t *testing.T) {
	for _, test := range []struct {
		name, behavior, step string
	}{
		{"initialize", museHostHangs, "initialize"},
		{"session read", museHostHangsAfterInit, "session/read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			shrinkMuseBaselineTimeout(t)
			path, attempts := writeMuseBaselineHost(t, test.behavior)
			started := time.Now()
			_, err := readTestMuseBaseline(path)
			if err == nil {
				t.Fatal("hung host returned a baseline")
			}
			message := err.Error()
			want := "did not answer " + test.step + " within 300ms"
			if !strings.Contains(message, "attempt 1: muse serve "+want) || !strings.Contains(message, "attempt 2: muse serve "+want) || strings.Contains(message, "file already closed") {
				t.Fatalf("error = %q", message)
			}
			if attempts() != museBaselineAttempts {
				t.Fatalf("attempts = %d", attempts())
			}
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Fatalf("hung hosts took %s", elapsed)
			}
		})
	}
}

func TestMuseRecoveryBaselineRetriesExitedHost(t *testing.T) {
	shrinkMuseBaselineTimeout(t)
	path, attempts := writeMuseBaselineHost(t, museHostExitsEarly)
	_, err := readTestMuseBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "stopped before answering initialize") || !strings.Contains(err.Error(), "muse serve stderr: host crashed") || attempts() != museBaselineAttempts {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

// A protocol answer is authoritative: retrying would not change it.
func TestMuseRecoveryBaselineDoesNotRetryProtocolAnswer(t *testing.T) {
	shrinkMuseBaselineTimeout(t)
	path, attempts := writeMuseBaselineHost(t, museHostRejectsSession)
	_, err := readTestMuseBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "session/read: ") || !strings.Contains(err.Error(), "unknown session") || attempts() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts())
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	buffer := &tailBuffer{limit: 5}
	_, _ = buffer.Write([]byte("abc"))
	_, _ = buffer.Write([]byte("defgh"))
	if got := buffer.String(); got != "defgh" {
		t.Fatalf("tail = %q", got)
	}
}
