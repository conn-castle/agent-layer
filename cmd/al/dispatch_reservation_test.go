package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/projectpaths"

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
	return filepath.Join(root, projectpaths.Runs, id)
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
		reserved.InvocationID,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"Not A Handle",
	} {
		_, stderr, code := runDispatchCLI(t, reservedStartArgs(selector)...)
		if code != agentdispatch.ExitReservationNotFound {
			t.Fatalf("start --reservation %q exit %d, want %d: %s", selector, code, agentdispatch.ExitReservationNotFound, stderr)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, projectpaths.Runs))
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
	if _, err := os.Stat(filepath.Join(root, projectpaths.Runs)); !errors.Is(err, os.ErrNotExist) {
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
	if _, stderr, code := runDispatchCLI(t, reservedStartArgs(reserved.Handle)...); code != agentdispatch.ExitReservationCancelled {
		t.Fatalf("start of cancelled reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationCancelled, stderr)
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
	if _, stderr, code := runDispatchCLI(t, reservedStartArgs(reserved.Handle)...); code != agentdispatch.ExitReservationExpired {
		t.Fatalf("start of retained expired reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationExpired, stderr)
	}
	for _, selector := range []string{reserved.Handle, reserved.InvocationID} {
		inspected, stderr, code := runDispatchCLI(t, "inspect", selector)
		if code != 0 || inspected.InvocationID != reserved.InvocationID || inspected.State != "cancelled" || inspected.Error != "reservation expired before it started" {
			t.Fatalf("inspect retained expired reservation %s = %#v exit %d: %s", selector, inspected, code, stderr)
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

func seedCompletedConversationForCLI(t *testing.T, root string) (string, string) {
	t.Helper()
	reserved := reserveForTest(t)
	runDir := reservationRunDir(root, reserved.InvocationID)
	answerPath := filepath.Join(runDir, "result.md")
	if err := os.WriteFile(answerPath, []byte("completed answer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, filepath.Join(runDir, "dispatch.json"), func(record map[string]any) {
		record["agent"] = "codex"
		record["provider_version"] = "0.145.0"
		record["state"] = "completed"
		record["recovery_state"] = "resume_required"
		record["completed_at"] = now
		record["provider_session_id"] = "cli-provider-session"
		record["launch_digest"] = "claimed"
		record["launch_fenced"] = true
		record["termination_confirmed"] = true
		record["termination_confirmed_at"] = now
		record["termination_proof"] = "prelaunch_no_intent"
	})
	rewriteDispatchJSON(t, reservationSessionPath(root, reserved.Handle), func(session map[string]any) {
		session["agent"] = "codex"
		session["provider_session_id"] = "cli-provider-session"
		session["state"] = "durable"
		session["run_id"] = reserved.InvocationID
		session["active_run_id"] = ""
		session["active_claim_known"] = true
	})
	return reserved.Handle, reserved.InvocationID
}

func TestDispatchOptionsAdvertisesContinuationReservations(t *testing.T) {
	newReservationTestRepo(t, "")
	cmd := newDispatchCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"options"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("al dispatch options failed: %v (stderr: %s)", err, stderr.String())
	}
	var resp struct {
		Reservations *struct {
			Start    bool `json:"start"`
			Continue bool `json:"continue"`
		} `json:"reservations"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("decode options JSON %q: %v", stdout.String(), err)
	}
	if resp.Reservations == nil || !resp.Reservations.Start || !resp.Reservations.Continue {
		t.Fatalf("options reservations = %#v, want start=true continue=true", resp.Reservations)
	}
}

func TestDispatchContinueReserveCLIContract(t *testing.T) {
	root := newReservationTestRepo(t, "reservation_expiry_days = 2")
	handle, firstID := seedCompletedConversationForCLI(t, root)

	// Empty --continue fails without allocating a fresh start reservation.
	if _, stderr, code := runDispatchCLI(t, "reserve", "--continue", ""); code != agentdispatch.ExitUsage {
		t.Fatalf("reserve --continue '' exit %d, want %d: %s", code, agentdispatch.ExitUsage, stderr)
	}

	// Reserve continuation is idempotent and does not disturb handle queries.
	contRes, stderr, code := runDispatchCLI(t, "reserve", "--continue", handle)
	if code != 0 || contRes.Handle != handle || contRes.InvocationID == "" || contRes.InvocationID == firstID || contRes.State != "reserved" || contRes.ReservationExpiresAt == nil {
		t.Fatalf("reserve --continue = %#v exit %d: %s", contRes, code, stderr)
	}
	contRes2, stderr, code := runDispatchCLI(t, "reserve", "--continue", handle)
	if code != 0 || contRes2.InvocationID != contRes.InvocationID {
		t.Fatalf("second reserve --continue = %#v exit %d: %s (want same ID %s)", contRes2, code, stderr, contRes.InvocationID)
	}

	// Handle inspect still reports the latest launched invocation; ID inspect reports reserved.
	inspectedHandle, stderr, code := runDispatchCLI(t, "inspect", handle)
	if code != 0 || inspectedHandle.InvocationID != firstID || inspectedHandle.State != "completed" {
		t.Fatalf("inspect handle while pending = %#v exit %d: %s", inspectedHandle, code, stderr)
	}
	inspectedID, stderr, code := runDispatchCLI(t, "inspect", contRes.InvocationID)
	if code != 0 || inspectedID.InvocationID != contRes.InvocationID || inspectedID.State != "reserved" {
		t.Fatalf("inspect reserved ID = %#v exit %d: %s", inspectedID, code, stderr)
	}

	// Competing ordinary continue is refused.
	if _, stderr, code := runDispatchCLI(t, "continue", handle, "--prompt", "competing"); code != agentdispatch.ExitUnavailable {
		t.Fatalf("competing continue exit %d, want %d: %s", code, agentdispatch.ExitUnavailable, stderr)
	}

	// Empty/unknown --reservation on continue exits 80 without launching.
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{
		{"continue", "--reservation", "", "--prompt", "x"},
		{"continue", handle, "--reservation", "", "--prompt", "x"},
		{"continue", "--reservation", handle, "--prompt", "x"},
		{"continue", "--reservation", firstID, "--prompt", "x"},
		{"continue", "--reservation", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "--prompt", "x"},
	} {
		if _, stderr, code := runDispatchCLI(t, args...); code != agentdispatch.ExitReservationNotFound {
			t.Fatalf("al dispatch %v exit %d, want %d: %s", args, code, agentdispatch.ExitReservationNotFound, stderr)
		}
	}

	// Missing both handle and --reservation exits 64 (usage).
	if _, stderr, code := runDispatchCLI(t, "continue", "--prompt", "x"); code != agentdispatch.ExitUsage {
		t.Fatalf("continue without handle or reservation exit %d, want %d: %s", code, agentdispatch.ExitUsage, stderr)
	}

	// Cancelling unstarted continuation reservation returns ExitReservationCancelled on launch
	// and allows reserving a new continuation.
	cancelled, stderr, code := runDispatchCLI(t, "cancel", contRes.InvocationID)
	if code != 0 || cancelled.State != "cancelled" || !cancelled.TerminationConfirmed {
		t.Fatalf("cancel continuation reservation = %#v exit %d: %s", cancelled, code, stderr)
	}
	if _, stderr, code := runDispatchCLI(t, "continue", "--reservation", contRes.InvocationID, "--prompt", "x"); code != agentdispatch.ExitReservationCancelled {
		t.Fatalf("continue cancelled reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationCancelled, stderr)
	}

	// Expiry returns ExitReservationExpired (81) and makes conversation continuable again.
	contRes3, stderr, code := runDispatchCLI(t, "reserve", "--continue", handle)
	if code != 0 || contRes3.InvocationID == contRes.InvocationID {
		t.Fatalf("reserve after cancel = %#v exit %d: %s", contRes3, code, stderr)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rewriteDispatchJSON(t, filepath.Join(reservationRunDir(root, contRes3.InvocationID), "dispatch.json"), func(record map[string]any) {
		record["reservation_expires_at"] = past
	})
	if _, stderr, code := runDispatchCLI(t, "continue", "--reservation", contRes3.InvocationID, "--prompt", "x"); code != agentdispatch.ExitReservationExpired {
		t.Fatalf("continue expired reservation exit %d, want %d: %s", code, agentdispatch.ExitReservationExpired, stderr)
	}
	contRes4, stderr, code := runDispatchCLI(t, "reserve", "--continue", handle)
	if code != 0 || contRes4.InvocationID == contRes3.InvocationID {
		t.Fatalf("reserve after expiry = %#v exit %d: %s", contRes4, code, stderr)
	}
}
