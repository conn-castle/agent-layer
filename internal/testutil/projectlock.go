package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/projectlock"
)

// AssertCanceledLockWaitPreservesProject holds root's project lock as another
// process would, cancels run while it waits, and requires that run returns
// context.Canceled and leaves no writes queued after the lock is released.
func AssertCanceledLockWaitPreservesProject(t *testing.T, root string, run func(context.Context) error) {
	t.Helper()
	// A separate open file description holds the flock as another process would.
	holder, err := os.OpenFile(projectlock.Path(root), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- test-owned temporary project.
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })
	require.NoError(t, unix.Flock(int(holder.Fd()), unix.LOCK_EX|unix.LOCK_NB)) //nolint:gosec // Unix file descriptors are small non-negative ints.
	before := SnapshotEvidence(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("operation returned before cancellation while the lock was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Error("canceled operation kept waiting for the project lock")
		// Drain queued work on failure so it cannot outlive the test fixture.
		require.NoError(t, holder.Close())
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("operation did not finish after releasing the lock")
		}
	}
	_ = holder.Close()
	require.Equal(t, before, SnapshotEvidence(t, root), "cancellation must leave no writes queued after release")
}
