package agentdispatch

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// A delayed retry must never claim a different reservation after retention
// makes its human-readable handle available for reuse.
func TestReservedStartRejectsRetiredIdentityAfterHandleReuse(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	old := reserveInTest(t, root)
	_, err := updateRunEvidence(filepathForRun(root, old.InvocationID), func(record *RunRecord) error {
		past := time.Now().Add(-time.Hour)
		record.ReservationExpiresAt = &past
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retireExpiredReservation(root, old.InvocationID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, time.Now().Add(60*24*time.Hour), 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	// Exercise the allocator's reuse path directly to avoid depending on a
	// random collision in the finite name vocabulary.
	fresh, err := newReservedDispatchRun(root, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, allocated, err := createExclusiveSession(root, old.Handle, fresh); err != nil || !allocated {
		t.Fatalf("reuse retired handle: allocated=%t err=%v", allocated, err)
	}
	for _, selector := range []string{old.Handle, old.InvocationID} {
		t.Run(selector, func(t *testing.T) {
			var output bytes.Buffer
			err := Start(StartOptions{
				Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "Old workflow work", Reservation: &selector,
				Stdout: &output, Env: []string{}, LookPath: alwaysFound,
				VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentCodex], nil },
				launchWorker: func(string, string, string) (launchedWorker, error) {
					t.Fatal("stale reservation selector launched a worker")
					return launchedWorker{}, nil
				},
			})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitReservationNotFound {
				t.Fatalf("stale reservation error = %v, want exit %d", err, ExitReservationNotFound)
			}
			if output.Len() != 0 {
				t.Fatalf("rejected start returned success output: %s", output.String())
			}
		})
	}
	record, err := loadRunRecord(root, fresh.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateReserved || record.LaunchDigest != "" {
		t.Fatalf("stale retry changed replacement reservation: %#v", record)
	}
}
