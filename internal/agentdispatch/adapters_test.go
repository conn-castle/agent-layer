package agentdispatch

// Testdata fixture provenance (captured 2026-05-22):
//   - testdata/claude/stream-json.txt was produced by:
//       claude --print --output-format stream-json --include-partial-messages "Hi"
//   - testdata/codex/jsonl.txt was produced by:
//       codex exec --json "Hi"
// Both fixtures use the real CLI stream shapes, trimmed to the minimum bytes
// needed to exercise the structured decoders. See
// .agent-layer/tmp/agent-dispatch-design.md "Verification Notes" and "Output
// and Artifacts" sections for the full provenance and protocol details.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDecodeClaudeStreamFixture(t *testing.T) {
	data, err := os.Open(filepath.Join("testdata", "claude", "stream-json.txt"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = data.Close() }()

	var stdout bytes.Buffer
	if err := decodeClaudeStream(data, &stdout, nil); err != nil {
		t.Fatalf("decodeClaudeStream error: %v", err)
	}
	if stdout.String() != "Hello from Claude." {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDecodeCodexStreamFixture(t *testing.T) {
	data, err := os.Open(filepath.Join("testdata", "codex", "jsonl.txt"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = data.Close() }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := decodeCodexStream(data, &stdout, &stderr); err != nil {
		t.Fatalf("decodeCodexStream error: %v", err)
	}
	if stdout.String() != "Hello from Codex." {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "codex: thread.started") {
		t.Fatalf("expected real progress event in stderr, got %q", stderr.String())
	}
}

func TestDecodeCodexStreamNoSynthesizedLiveness(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	// A silent stream now fails loudly (spec § Output and Artifacts:
	// dispatch must fail when the documented stream shape disappears).
	// The test still pins the no-heartbeat invariant: dispatch must not
	// pollute stdout/stderr with synthesized liveness lines while failing.
	err := decodeCodexStream(strings.NewReader(""), &stdout, &stderr)
	exitErr := requireDispatchExitCode(t, err, ExitTargetFailure)
	if !strings.Contains(exitErr.Error(), "no recognized answer-text event") {
		t.Fatalf("expected fail-loudly message, got %q", exitErr.Error())
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("expected no synthesized output for silent stream, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestDecodeInvalidStructuredOutput(t *testing.T) {
	err := decodeCodexStream(strings.NewReader("{not-json}\n"), &bytes.Buffer{}, &bytes.Buffer{})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitTargetFailure {
		t.Fatalf("expected target failure, got %T: %v", err, err)
	}
}

// TestRunStructuredCommandPreservesAllStderrAndDecodesStdout exercises
// F3 (drain pipes before Wait) and F4 (sync stderr writes). A stub child
// writes a recognized agent_message on stdout AND a non-trivial chunk of
// bytes on stderr; the test runs under -race and verifies (a) no race or
// truncation on stderr, (b) stdout is decoded correctly, (c) the full
// stderr payload from the child is preserved in the captured buffer.
func TestRunStructuredCommandPreservesAllStderrAndDecodesStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	binDir := t.TempDir()
	stderrPayload := strings.Repeat("e", 4096) // 4 KB
	// The stub writes a sizable stderr chunk and a final agent_message on
	// stdout. Both happen in the same process; the structured runner reads
	// both pipes concurrently.
	stubScript := fmt.Sprintf(`#!/bin/sh
printf '%%s' '%s' >&2
printf '{"type":"agent_message","message":"ok"}\n'
`, stderrPayload)
	stubPath := filepath.Join(binDir, "codex-stub")
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path is test-owned
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream); err != nil {
		t.Fatalf("runStructuredCommand error: %v\nstderr: %q", err, stderr.String())
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "ok")
	}
	if !strings.Contains(stderr.String(), stderrPayload) {
		t.Fatalf("expected full stderr payload (%d bytes) preserved; got %d bytes: %q",
			len(stderrPayload), len(stderr.String()), stderr.String())
	}
}

// TestRunStructuredCommandConcurrentStderrWritesUnderRace exercises F4:
// the stub interleaves many stderr writes with stdout JSON events so the
// decoder's progress writes and the stderr copier race. Under -race a
// missing mutex would surface as a data race on the shared bytes.Buffer.
func TestRunStructuredCommandConcurrentStderrWritesUnderRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	binDir := t.TempDir()
	// Stub emits N progress events on stdout interleaved with N stderr
	// chunks. Each event triggers the decoder to write a `codex: <event>`
	// line via the shared stderr; the copier concurrently appends the
	// child's stderr bytes. Both writers race on the same syncWriter.
	stubScript := `#!/bin/sh
i=0
while [ "$i" -lt 50 ]; do
  printf '{"type":"thread.started"}\n'
  printf 'progress-chunk-%d\n' "$i" >&2
  i=$((i + 1))
done
printf '{"type":"agent_message","message":"ok"}\n'
`
	stubPath := filepath.Join(binDir, "codex-stub")
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path is test-owned
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream); err != nil {
		t.Fatalf("runStructuredCommand error: %v", err)
	}
	if stdout.String() != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	// Sanity: both sources contributed.
	if !strings.Contains(stderr.String(), "progress-chunk-49") {
		t.Fatalf("expected last child stderr chunk in buffer, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "codex: thread.started") {
		t.Fatalf("expected decoder progress line in stderr, got %q", stderr.String())
	}
}

// TestSyncWriterSerializesConcurrentWrites is a focused race-free check
// that the syncWriter mutex prevents interleaved partial writes from two
// goroutines into a shared bytes.Buffer (which is not concurrency-safe).
func TestSyncWriterSerializesConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	w := newSyncWriter(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := w.Write([]byte("xyzxyzxyzxyzxyzx")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	// 4 goroutines * 200 writes * 16 bytes = 12800 bytes.
	if got := buf.Len(); got != 12800 {
		t.Fatalf("buffer length = %d, want 12800", got)
	}
}

// TestWaitWithSignalForwardsSIGINT exercises F10: a long-running stub
// child traps SIGINT, dispatch sends SIGINT to its own process group, and
// waitWithSignal must return ExitSigint (130). Skipped on Windows because
// the wrapper only registers POSIX signals.
func TestWaitWithSignalForwardsSIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal forwarding test is POSIX-only")
	}
	binDir := t.TempDir()
	// The child traps INT, prints a marker, and exits 130 itself. Whether
	// the child exits cleanly or is killed by the signal, mapWaitError or
	// the signal branch must return ExitSigint.
	stubScript := `#!/bin/sh
trap 'exit 130' INT
# Loop in 0.05s ticks for up to ~5s so the test self-times-out if the
# signal forwarding regresses.
i=0
while [ "$i" -lt 100 ]; do
  sleep 0.05
  i=$((i + 1))
done
exit 0
`
	stubPath := filepath.Join(binDir, "sleeper")
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path is test-owned
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stub: %v", err)
	}
	// Send SIGINT to our own process; waitWithSignal will see it via the
	// signal.Notify channel it sets up, forward to the child, and return
	// ExitSigint. Send after a short delay so the registration is in place.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	err := waitWithSignal(cmd, AgentCodex)
	exitErr := requireDispatchExitCode(t, err, ExitSigint)
	if !strings.Contains(exitErr.Error(), "SIGINT") {
		t.Fatalf("expected SIGINT in error message, got %q", exitErr.Error())
	}
}

// TestRunStructuredCommandForwardsSIGINTDuringStream covers the Round 2
// regression: the signal forwarder must be installed BEFORE pipe drain
// (not only inside cmd.Wait) so that SIGINT arriving while the decoder
// goroutine is still reading from the child is forwarded to the child.
// Without forwarding the child would sleep its full 5s; with forwarding
// the child is killed quickly and dispatch returns ExitSigint well under
// that budget.
func TestRunStructuredCommandForwardsSIGINTDuringStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal forwarding test is POSIX-only")
	}
	binDir := t.TempDir()
	stubScript := `#!/bin/sh
i=0
while [ "$i" -lt 100 ]; do
  sleep 0.05
  i=$((i + 1))
done
exit 0
`
	stubPath := filepath.Join(binDir, "stream-sleeper")
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatalf("write stub: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), stubPath) // #nosec G204 -- test stub path is test-owned
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	var stdout, stderr bytes.Buffer
	start := time.Now()
	err := runStructuredCommand(cmd, AgentCodex, &stdout, &stderr, decodeCodexStream)
	elapsed := time.Since(start)
	exitErr := requireDispatchExitCode(t, err, ExitSigint)
	if !strings.Contains(exitErr.Error(), "SIGINT") {
		t.Fatalf("expected SIGINT in error message, got %q", exitErr.Error())
	}
	// Stub sleeps ~5s without forwarding; with forwarding the child is
	// killed almost immediately. Generous bound (2s) keeps the test stable
	// on slow CI while still proving the forwarder fired during the stream.
	if elapsed > 2*time.Second {
		t.Fatalf("dispatch took %v; expected fast SIGINT forwarding (well under 5s child sleep budget)", elapsed)
	}
}
