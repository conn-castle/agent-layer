package install

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestRollbackUpgradeSnapshotCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700))
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	require.NoError(t, os.WriteFile(pinPath, []byte("1.1.0\n"), 0o600))
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "cancel-rollback",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{{
			Path:          ".agent-layer/al.version",
			Kind:          upgradeSnapshotEntryKindFile,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("1.0.0\n")),
		}},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	require.NoError(t, inst.writeUpgradeSnapshot(snapshot, false))
	run := func(ctx context.Context) error {
		return RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{Context: ctx, System: RealSystem{}})
	}
	testutil.AssertCanceledLockWaitPreservesProject(t, root, run)
}
