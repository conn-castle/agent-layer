package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func watchPREventsScript(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "skills", "ship-pr", "scripts", "watch-pr-events.sh")
}

func oversizedArgJSONBytes(t *testing.T) int {
	t.Helper()
	const maxFixture = 3 << 20
	out, err := exec.Command("getconf", "ARG_MAX").Output()
	if err != nil {
		return maxFixture
	}
	argMax, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || argMax < 1<<20 {
		return maxFixture
	}
	// Prefer a payload just over ARG_MAX. When ARG_MAX is larger than
	// maxFixture, still return maxFixture: a single --argjson value can fail
	// below ARG_MAX (Linux MAX_ARG_STRLEN). The caller must skip if jq still
	// accepts the fixture.
	size := argMax + 256<<10
	if size > maxFixture {
		return maxFixture
	}
	return size
}

func writeWatchPRJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func watchPREventsGHStubEnv(t *testing.T, prState1, prState2, comments1, comments2 string) []string {
	t.Helper()
	bin := t.TempDir()
	stub := filepath.Join(bin, "gh")
	script := `#!/usr/bin/env bash
set -euo pipefail
args="$*"
mode="${GH_STUB_MODE:-ok}"
if [[ "$mode" == "fail" ]]; then
  printf 'gh: boom\n' >&2
  exit 1
fi
count_file="${GH_STUB_COUNT_FILE:-}"
snapshot=0
if [[ -n "$count_file" && -f "$count_file" ]]; then
  snapshot="$(cat "$count_file")"
fi
if [[ "$args" == *"pr view"* ]]; then
  snapshot=$((snapshot + 1))
  if [[ -n "$count_file" ]]; then
    printf '%s\n' "$snapshot" >"$count_file"
  fi
  if [[ "$snapshot" -ge 2 && -n "${GH_STUB_PR_STATE_2:-}" ]]; then
    cat "${GH_STUB_PR_STATE_2}"
  else
    cat "${GH_STUB_PR_STATE_1}"
  fi
  exit 0
fi
if [[ "$args" == *"/pulls/"*"/comments"* ]]; then
  if [[ "$args" != *"--paginate"* ]]; then
    printf 'missing --paginate\n' >&2
    exit 1
  fi
  if [[ "$snapshot" -ge 2 && -n "${GH_STUB_COMMENTS_2:-}" ]]; then
    cat "${GH_STUB_COMMENTS_2}"
  else
    cat "${GH_STUB_COMMENTS_1}"
  fi
  exit 0
fi
printf 'unexpected gh invocation: %s\n' "$args" >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- executable test stub requires owner execute permission.
		t.Fatalf("write gh stub: %v", err)
	}
	countFile := filepath.Join(t.TempDir(), "gh-count")
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_STUB_MODE=ok",
		"GH_STUB_COUNT_FILE="+countFile,
		"GH_STUB_PR_STATE_1="+prState1,
		"GH_STUB_PR_STATE_2="+prState2,
		"GH_STUB_COMMENTS_1="+comments1,
		"GH_STUB_COMMENTS_2="+comments2,
	)
	return env
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type watchPREventsProc struct {
	stderr  *lockedBuffer
	done    <-chan struct{}
	waitErr *error
}

func startWatchPREvents(t *testing.T, env []string, args ...string) *watchPREventsProc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "bash", append([]string{watchPREventsScript(t)}, args...)...) // #nosec G204 -- test invokes the checked-in watcher with explicit args.
	cmd.Env = env
	var stdout bytes.Buffer
	stderr := &lockedBuffer{}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start watch-pr-events: %v", err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	return &watchPREventsProc{stderr: stderr, done: done, waitErr: &waitErr}
}

func waitForWatchLogLine(t *testing.T, logFile string, proc *watchPREventsProc) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-proc.done:
			t.Fatalf("watch-pr-events exited before logging a change: %v\nstderr=%s", *proc.waitErr, proc.stderr.String())
		default:
		}
		data, err := os.ReadFile(logFile) // #nosec G304 -- test-owned watcher log path.
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			t.Fatalf("read log: %v", err)
		}
		line, rest, found := strings.Cut(string(data), "\n")
		if !found || line == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			preview := line
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			t.Fatalf("log is not JSON %q (%d leftover bytes): %v", preview, len(rest), err)
		}
		return event
	}
	t.Fatalf("timed out waiting for watcher log line\nstderr=%s", proc.stderr.String())
	return nil
}

func TestWatchPREventsRequiresRepoPRAndLogFile(t *testing.T) {
	requireJQ(t)
	cmd := exec.Command("bash", watchPREventsScript(t)) // #nosec G204 -- test invokes the checked-in watcher.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "--repo must be in owner/name form") {
		t.Fatalf("missing args error = %v, stderr %q", err, stderr.String())
	}
	cmd = exec.Command("bash", watchPREventsScript(t), "--repo", "o/n", "--pr", "1") // #nosec G204 -- test invokes the checked-in watcher.
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "--log-file is required") {
		t.Fatalf("missing log file error = %v, stderr %q", err, stderr.String())
	}
}

func TestWatchPREventsLogsStateChanges(t *testing.T) {
	requireJQ(t)
	dir := t.TempDir()
	pr1 := filepath.Join(dir, "pr1.json")
	pr2 := filepath.Join(dir, "pr2.json")
	comments := filepath.Join(dir, "comments.json")
	writeWatchPRJSON(t, pr1, map[string]any{
		"headRefOid":        "abc",
		"mergeable":         "MERGEABLE",
		"reviewDecision":    "REVIEW_REQUIRED",
		"updatedAt":         "2026-01-01T00:00:00Z",
		"comments":          []any{},
		"reviews":           []any{},
		"statusCheckRollup": []any{},
	})
	writeWatchPRJSON(t, pr2, map[string]any{
		"headRefOid":        "def",
		"mergeable":         "MERGEABLE",
		"reviewDecision":    "APPROVED",
		"updatedAt":         "2026-01-01T00:01:00Z",
		"comments":          []any{},
		"reviews":           []any{},
		"statusCheckRollup": []any{},
	})
	writeWatchPRJSON(t, comments, []any{
		map[string]any{"id": 11, "body": "please fix"},
	})
	env := watchPREventsGHStubEnv(t, pr1, pr2, comments, comments)
	logFile := filepath.Join(dir, "events.jsonl")
	proc := startWatchPREvents(t, env,
		"--repo", "acme/widgets",
		"--pr", "7",
		"--log-file", logFile,
		"--interval-seconds", "1",
	)
	event := waitForWatchLogLine(t, logFile, proc)
	if event["repo"] != "acme/widgets" {
		t.Fatalf("repo = %v", event["repo"])
	}
	if event["pr"] != float64(7) {
		t.Fatalf("pr = %v", event["pr"])
	}
	state, ok := event["state"].(map[string]any)
	if !ok {
		t.Fatalf("state missing: %v", event)
	}
	pr, ok := state["pull_request"].(map[string]any)
	if !ok || pr["headRefOid"] != "def" {
		t.Fatalf("logged pull_request = %v", state["pull_request"])
	}
	commentsVal, ok := state["inline_comments"].([]any)
	if !ok || len(commentsVal) != 1 {
		t.Fatalf("logged inline_comments = %v", state["inline_comments"])
	}
}

func TestWatchPREventsSurvivesPayloadsTooLargeForArgJSON(t *testing.T) {
	requireJQ(t)
	pad := strings.Repeat("x", oversizedArgJSONBytes(t))
	dir := t.TempDir()
	pr1 := filepath.Join(dir, "pr1.json")
	pr2 := filepath.Join(dir, "pr2.json")
	comments1 := filepath.Join(dir, "comments1.json")
	comments2 := filepath.Join(dir, "comments2.json")
	writeWatchPRJSON(t, pr1, map[string]any{
		"headRefOid":        "abc",
		"mergeable":         "MERGEABLE",
		"reviewDecision":    "REVIEW_REQUIRED",
		"updatedAt":         "2026-01-01T00:00:00Z",
		"comments":          []any{map[string]any{"body": pad}},
		"reviews":           []any{},
		"statusCheckRollup": []any{},
	})
	writeWatchPRJSON(t, pr2, map[string]any{
		"headRefOid":        "def",
		"mergeable":         "MERGEABLE",
		"reviewDecision":    "APPROVED",
		"updatedAt":         "2026-01-01T00:01:00Z",
		"comments":          []any{map[string]any{"body": pad}},
		"reviews":           []any{},
		"statusCheckRollup": []any{},
	})
	writeWatchPRJSON(t, comments1, []any{map[string]any{"id": 11, "body": pad}})
	writeWatchPRJSON(t, comments2, []any{map[string]any{"id": 12, "body": pad}})

	// The same snapshot must be too large to pass through jq --argjson.
	prBytes, err := os.ReadFile(pr1) // #nosec G304 -- test-owned fixture path.
	if err != nil {
		t.Fatal(err)
	}
	jq := exec.Command("jq", "-n", "--argjson", "pr_state", string(prBytes), "$pr_state") // #nosec G204 -- documents the ARG_MAX failure the watcher must avoid.
	if err := jq.Run(); err == nil {
		t.Skipf("jq --argjson accepted a %d-byte payload; ARG_MAX is too large to exceed with a practical fixture", len(prBytes))
	}

	env := watchPREventsGHStubEnv(t, pr1, pr2, comments1, comments2)
	logFile := filepath.Join(dir, "events.jsonl")
	proc := startWatchPREvents(t, env,
		"--repo", "acme/widgets",
		"--pr", "7",
		"--log-file", logFile,
		"--interval-seconds", "1",
	)
	event := waitForWatchLogLine(t, logFile, proc)
	state, ok := event["state"].(map[string]any)
	if !ok {
		t.Fatalf("state missing: %v", event)
	}
	pr, ok := state["pull_request"].(map[string]any)
	if !ok || pr["headRefOid"] != "def" {
		t.Fatalf("logged pull_request = %v", state["pull_request"])
	}
	inline, ok := state["inline_comments"].([]any)
	if !ok || len(inline) != 1 {
		t.Fatalf("logged inline_comments = %v", state["inline_comments"])
	}
}

func TestWatchPREventsFailsOnAPIError(t *testing.T) {
	requireJQ(t)
	dir := t.TempDir()
	pr := filepath.Join(dir, "pr.json")
	comments := filepath.Join(dir, "comments.json")
	writeWatchPRJSON(t, pr, map[string]any{})
	writeWatchPRJSON(t, comments, []any{})
	env := watchPREventsGHStubEnv(t, pr, pr, comments, comments)
	for i, e := range env {
		if strings.HasPrefix(e, "GH_STUB_MODE=") {
			env[i] = "GH_STUB_MODE=fail"
		}
	}
	cmd := exec.Command("bash", watchPREventsScript(t), // #nosec G204 -- test invokes the checked-in watcher.
		"--repo", "acme/widgets",
		"--pr", "7",
		"--log-file", filepath.Join(dir, "events.jsonl"),
		"--interval-seconds", "1",
	)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("api failure = %v, stderr %q", err, stderr.String())
	}
}
