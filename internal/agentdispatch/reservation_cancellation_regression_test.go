package agentdispatch

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestCancelledReservationCannotReopenSession exercises cancellation between
// the run claim and session claim, both before and after active claim cleanup.
func TestCancelledReservationCannotReopenSession(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		name := "before_cleanup"
		if cleanup {
			name = "after_cleanup"
		}
		t.Run(name, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			reservation := reserveInTest(t, root)
			if _, claimed, err := claimReservation(root, reservation.InvocationID, "sha256:test", func(record *RunRecord) {
				record.Agent = AgentCodex
			}); err != nil || !claimed {
				t.Fatalf("claim reservation = %t, %v", claimed, err)
			}
			if cleanup {
				if err := Cancel(CancelRequest{Root: root, InvocationID: reservation.InvocationID}); err != nil {
					t.Fatal(err)
				}
			} else if _, _, _, err := beginCancellation(root, reservation.InvocationID); err != nil {
				t.Fatal(err)
			}
			if _, err := claimReservedSession(root, reservation.Handle, reservation.InvocationID, AgentCodex, "", ""); err == nil {
				t.Fatal("cancelled reservation reopened its session")
			}
			session, err := loadSession(root, reservation.Handle)
			if err != nil {
				t.Fatal(err)
			}
			if session.State != sessionStateReserved || (cleanup && session.ActiveRunID != "") {
				t.Fatalf("cancelled reservation session = %#v", session)
			}
			err = Continue(ContinueOptions{Root: root, Handle: reservation.Handle, Prompt: "More", Env: []string{}, LookPath: alwaysFound})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitUnavailable || !strings.Contains(err.Error(), "reserved start") {
				t.Fatalf("continue cancelled reservation = %v", err)
			}
		})
	}
}

// TestCancelSettlesReservationExpiry verifies cancellation is the first
// observer of expiry, and repeated cancellation preserves the expired result.
func TestCancelSettlesReservationExpiry(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "unexpired"
		wantReason, wantCode := terminalReasonCancelledByCaller, ExitReservationCancelled
		if expired {
			name = "expired"
			wantReason, wantCode = terminalReasonReservationExpired, ExitReservationExpired
		}
		t.Run(name, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			reservation := reserveInTest(t, root)
			if expired {
				if _, err := updateRunEvidence(filepathForRun(root, reservation.InvocationID), func(record *RunRecord) error {
					past := time.Now().UTC().Add(-time.Minute)
					record.ReservationExpiresAt = &past
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := Cancel(CancelRequest{Root: root, InvocationID: reservation.InvocationID}); err != nil {
					t.Fatal(err)
				}
			}
			record, err := loadRunRecord(root, reservation.InvocationID)
			if err != nil {
				t.Fatal(err)
			}
			if record.TerminalReason != wantReason || !record.TerminationConfirmed || !record.LaunchFenced {
				t.Fatalf("cancelled reservation = %#v", record)
			}
			session, err := loadSession(root, reservation.Handle)
			if err != nil {
				t.Fatal(err)
			}
			if session.ActiveRunID != "" {
				t.Fatalf("cancel retained active claim %q", session.ActiveRunID)
			}
			err = Start(StartOptions{Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Work", Reservation: &reservation.InvocationID, Env: []string{}, launchWorker: func(string, string, string) (launchedWorker, error) {
				t.Fatal("cancelled reservation launched a worker")
				return launchedWorker{}, nil
			}})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != wantCode {
				t.Fatalf("start cancelled reservation = %v, want exit %d", err, wantCode)
			}
		})
	}
}
