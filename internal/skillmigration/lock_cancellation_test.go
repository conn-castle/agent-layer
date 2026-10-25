package skillmigration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestCheckVersionCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700))
	run := func(ctx context.Context) error { return CheckVersion(ctx, root, "0.23.1") }
	testutil.AssertCanceledLockWaitPreservesProject(t, root, run)
}
