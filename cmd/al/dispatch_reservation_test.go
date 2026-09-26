package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/config"
)

// These tests drive `al dispatch` reservations through the CLI without
// launching anything. Launching behavior (repeat, mismatch, and concurrent
// starts) is exercised against the built binary in the agent-dispatch e2e
// scenario.

type dispatchCLIResult struct {
	Handle               string     `json:"handle"`
	InvocationID         string     `json:"invocation_id"`
	State                string     `json:"state"`
	Error                string     `json:"error"`
	TerminationConfirmed bool       `json:"termination_confirmed"`
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"`
	AlreadyStarted       bool       `json:"already_started"`
}

func newReservationTestRepo(t *testing.T, dispatchConfig string) string {
	t.Helper()
	root := t.TempDir()
	writeTestRepo(t, root)
	if dispatchConfig != "" {
		path := config.DefaultPaths(root).ConfigPath
		data, err := os.ReadFile(path) // #nosec G304 -- path is the test repository config.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n[dispatch]\n"+dispatchConfig+"\n")...), 0o600); err != nil { // #nosec G703 -- path is the test repository config.
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

func runDispatchCLI(t *testing.T, args ...string) (dispatchCLIResult, string, int) {
	t.Helper()
	cmd := newDispatchCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	code := 0
	if err := cmd.Execute(); err != nil {
		var silent *SilentExitError
		if !errors.As(err, &silent) {
			t.Fatalf("al dispatch %v: unexpected error %v", args, err)
		}
		code = silent.Code
	}
	var result dispatchCLIResult
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("al dispatch %v wrote invalid JSON %q: %v", args, stdout.String(), err)
		}
	}
	return result, stderr.String(), code
}

func reserveForTest(t *testing.T) dispatchCLIResult {
	t.Helper()
	reserved, stderr, code := runDispatchCLI(t, "reserve")
	if code != 0 {
		t.Fatalf("reserve exit %d: %s", code, stderr)
	}
	return reserved
}

func reservedStartArgs(selector string) []string {
	return []string{"start", "--reservation", selector, "--agent", "codex", "--prompt", "Reserved work"}
}

func rewriteDispatchJSON(t *testing.T, path string, update func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is test-owned dispatch state.
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	update(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func reservationRunDir(root string, id string) string {
	return filepath.Join(root, ".agent-layer", "tmp", "runs", id)
}

func reservationSessionPath(root string, handle string) string {
	return filepath.Join(root, ".agent-layer", "state", "dispatch", handle+".json")
}

func TestDispatchReserveCreatesInspectableReservation(t *testing.T) {
	root := newReservationTestRepo(t, "reservation_expiry_days = 2")
	before := time.Now()
	reserved := reserveForTest(t)
	if reserved.Handle == "" || reserved.InvocationID == "" || reserved.State != "reserved" || reserved.TerminationConfirmed {
		t.Fatalf("reserve result = %#v", reserved)
	}
	if reserved.ReservationExpiresAt == nil {
		t.Fatal("reserve omitted reservation_expires_at")
	}
	if got := reserved.ReservationExpiresAt.Sub(before); got < 48*time.Hour || got > 48*time.Hour+time.Minute {
		t.Fatalf("reservation expires %s after reserve, want the configured 2 days", got)
	}
	for _, selector := range []string{reserved.Handle, reserved.InvocationID} {
		inspected, stderr, code := runDispatchCLI(t, "inspect", selector)
		if code != 0 || inspected.State != "reserved" || inspected.InvocationID != reserved.InvocationID || inspected.Handle != reserved.Handle {
			t.Fatalf("inspect %s = %#v exit %d: %s", selector, inspected, code, stderr)
		}
	}
	if _, stderr, code := runDispatchCLI(t, "output", reserved.Handle, "--artifact", "events"); code != agentdispatch.ExitUnavailable {
		t.Fatalf("output on a reservation exit %d, want %d: %s", code, agentdispatch.ExitUnavailable, stderr)
	}
	if _, stderr, code := runDispatchCLI(t, "continue", reserved.Handle, "--prompt", "more"); code != agentdispatch.ExitUnavailable {
		t.Fatalf("continue on a reservation exit %d, want %d: %s", code, agentdispatch.ExitUnavailable, stderr)
	}
	if _, err := os.Stat(filepath.Join(reservationRunDir(root, reserved.InvocationID), "worker-request.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserve prepared a worker: %v", err)
	}
	second := reserveForTest(t)
	if second.Handle == reserved.Handle || second.InvocationID == reserved.InvocationID {
		t.Fatalf("second reservation reused identity: %#v", second)
	}
}

func TestDispatchStartRejectsUnknownReservationWithoutLaunching(t *testing.T) {
	root := newReservationTestRepo(t, "")
	reserved := reserveForTest(t)
	for _, selector := range []string{
		reserved.Handle + "x",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"Not A Handle",
	} {
		_, stderr, code := runDispatchCLI(t, reservedStartArgs(selector)...)
		if code != agentdispatch.ExitReservationNotFound {
			t.Fatalf("start --reservation %q exit %d, want %d: %s", selector, code, agentdispatch.ExitReservationNotFound, stderr)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".agent-layer", "tmp", "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("unknown reservations created invocations: %d run directories", len(entries))
	}
	inspected, _, code := runDispatchCLI(t, "inspect", reserved.Handle)
	if code != 0 || inspected.State != "reserved" {
		t.Fatalf("unrelated reservation changed: %#v", inspected)
	}
}

func TestDispatchStartRejectsEmptyReservationWithoutLaunching(t *testing.T) {
	root := newReservationTestRepo(t, "")
	// No provider is reachable, so a fallback to plain start fails differently
	// instead of launching an agent.
	t.Setenv("PATH", t.TempDir())
	for _, selector := range []string{"", "   "} {
		_, stderr, code := runDispatchCLI(t, reservedStartArgs(selector)...)
		if code != agentdispatch.ExitReservationNotFound {
			t.Fatalf("start --reservation %q exit %d, want %d: %s", selector, code, agentdispatch.ExitReservationNotFound, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "tmp", "runs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty reservations created invocations: %v", err)
	}
}

func TestDispatchCancelRetiresReservationWithoutLaunching(t *testing.T) {
	newReservationTestRepo(t, "")
	reserved := reserveForTest(t)
	cancelled, stderr, code := runDispatchCLI(t, "cancel", reserved.Handle)
	if code != 0 || cancelled.State != "cancelled" || !cancelled.TerminationConfirmed || cancelled.InvocationID != reserved.InvocationID {
		t.Fatalf("cancel reservation = %#v exit %d: %s", cancelled, code, stderr)
	}
	waited, stderr, code := runDispatchCLI(t, "wait", reserved.InvocationID)
	if code != 0 || waited.State != "cancelled" {
		t.Fatalf("wait on cancelled reservation = %#v exit %d: %s", waited, code, stderr)
	}
	for _, selector := range []string{reserved.Handle, reserved.InvocationID} {
		_, stderr, code := runDispatchCLI(t, reservedStartArgs(selector)...)
		if code != agentdispatch.ExitReservationCancelled {
			t.Fatalf("start of cancelled reservation %s exit %d, want %d: %s", selector, code, agentdispatch.ExitReservationCancelled, stderr)
		}
	}
	inspected, _, _ := runDispatchCLI(t, "inspect", reserved.Handle)
	if inspected.State != "cancelled" || !inspected.TerminationConfirmed {
		t.Fatalf("inspect after rejected start = %#v", inspected)
	}
}

func TestDispatchReservationExpiresWithoutLaunchingAndIsCleanedUp(t *testing.T) {
	root := newReservationTestRepo(t, "session_retention_days = 1")
	reserved := reserveForTest(t)
	runFile := filepath.Join(reservationRunDir(root, reserved.InvocationID), "dispatch.json")
	// Simulate the passage of time by moving the durable deadline into the past.
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, runFile, func(record map[string]any) { record["reservation_expires_at"] = past })

	for range 2 {
		_, stderr, code := runDispatchCLI(t, reservedStartArgs(reserved.Handle)...)
		if code != agentdispatch.ExitReservationExpired {
			t.Fatalf("start of expired reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationExpired, stderr)
		}
	}
	inspected, stderr, code := runDispatchCLI(t, "inspect", reserved.InvocationID)
	if code != 0 || inspected.State != "cancelled" || !inspected.TerminationConfirmed || inspected.Error == "" {
		t.Fatalf("inspect expired reservation = %#v exit %d: %s", inspected, code, stderr)
	}
	var record map[string]any
	data, err := os.ReadFile(runFile) // #nosec G304 -- path is test-owned dispatch state.
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if _, launched := record["launch_digest"]; launched || record["provider_launch_intent"] == true || record["supervisor_pid"] != nil {
		t.Fatalf("expired reservation shows launch evidence: %v", record)
	}

	// Age the retired reservation past session retention; the next dispatch
	// command that prunes evidence removes it.
	old := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, runFile, func(record map[string]any) { record["completed_at"] = old })
	rewriteDispatchJSON(t, reservationSessionPath(root, reserved.Handle), func(session map[string]any) {
		session["created_at"] = old
		session["last_used_at"] = old
	})
	reserveForTest(t)
	if _, err := os.Stat(runFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired reservation evidence was not cleaned up: %v", err)
	}
	if _, err := os.Stat(reservationSessionPath(root, reserved.Handle)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired reservation mapping was not cleaned up: %v", err)
	}
	if _, stderr, code := runDispatchCLI(t, reservedStartArgs(reserved.Handle)...); code != agentdispatch.ExitReservationNotFound {
		t.Fatalf("start of cleaned-up reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationNotFound, stderr)
	}
}

func TestDispatchExpiredReservationHandleIsKeptWithItsInvocation(t *testing.T) {
	root := newReservationTestRepo(t, "session_retention_days = 1")
	reserved := reserveForTest(t)
	// The reservation was made longer ago than the retention window and has
	// just expired; its handle must stay answerable while its evidence is kept.
	old := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339Nano)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, filepath.Join(reservationRunDir(root, reserved.InvocationID), "dispatch.json"), func(record map[string]any) {
		record["reservation_expires_at"] = past
	})
	rewriteDispatchJSON(t, reservationSessionPath(root, reserved.Handle), func(session map[string]any) {
		session["created_at"] = old
		session["last_used_at"] = old
	})
	if _, stderr, code := runDispatchCLI(t, reservedStartArgs(reserved.Handle)...); code != agentdispatch.ExitReservationExpired {
		t.Fatalf("start of expired reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationExpired, stderr)
	}
	reserveForTest(t) // prunes evidence
	for _, selector := range []string{reserved.Handle, reserved.InvocationID} {
		if _, stderr, code := runDispatchCLI(t, reservedStartArgs(selector)...); code != agentdispatch.ExitReservationExpired {
			t.Fatalf("start of retained expired reservation %s exit %d, want %d: %s", selector, code, agentdispatch.ExitReservationExpired, stderr)
		}
	}
}

func TestDispatchPruningRetiresOnlyExpiredReservations(t *testing.T) {
	root := newReservationTestRepo(t, "")
	expiring := reserveForTest(t)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, filepath.Join(reservationRunDir(root, expiring.InvocationID), "dispatch.json"), func(record map[string]any) {
		record["reservation_expires_at"] = past
	})
	fresh := reserveForTest(t)
	data, err := os.ReadFile(filepath.Join(reservationRunDir(root, expiring.InvocationID), "dispatch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.State != "cancelled" {
		t.Fatalf("expired reservation state after pruning = %q, want cancelled", record.State)
	}
	inspected, _, _ := runDispatchCLI(t, "inspect", fresh.Handle)
	if inspected.State != "reserved" {
		t.Fatalf("unexpired reservation state = %q, want reserved", inspected.State)
	}
}
