//go:build unix

package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestUpgradeSnapshotsRefuseFIFO(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "direct"
		if linked {
			name = "linked"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, upgradeSnapshotDirRelPath)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			versionPath := filepath.Join(root, ".agent-layer", "al.version")
			if err := os.WriteFile(versionPath, []byte("keep-version\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			healthy := upgradeSnapshot{SchemaVersion: upgradeSnapshotSchemaVersion, SnapshotID: "healthy", CreatedAtUTC: time.Now().UTC().Format(time.RFC3339), Status: upgradeSnapshotStatusApplied}
			if err := writeUpgradeSnapshotFile(filepath.Join(dir, "healthy.json"), healthy, RealSystem{}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "special.json")
			fifo := testutil.MakeFIFO(t, path, linked)
			err := testutil.BoundedFIFO(t, fifo, func() error {
				snapshots, err := ListUpgradeSnapshots(root, RealSystem{})
				if err == nil && (len(snapshots) != 1 || snapshots[0].ID != "healthy") {
					return errors.New("listing lost healthy snapshot or included FIFO")
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, read := range []func() error{
				func() error { _, err := readUpgradeSnapshot(path, RealSystem{}); return err },
				func() error {
					return RollbackUpgradeSnapshot(root, "special", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
				},
			} {
				err := testutil.BoundedFIFO(t, fifo, read)
				if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
					t.Fatalf("error = %v, want non-regular path %s", err, path)
				}
			}
			inst := &installer{root: root, sys: RealSystem{}}
			if err := testutil.BoundedFIFO(t, fifo, func() error { return inst.pruneUpgradeSnapshots(0) }); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "healthy.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("healthy snapshot not pruned: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("FIFO changed: %v, %v", info, err)
			}
			version, err := os.ReadFile(versionPath) // #nosec G304 -- path is a test-controlled managed file.
			if err != nil || string(version) != "keep-version\n" {
				t.Fatalf("rollback changed managed version: %q, %v", version, err)
			}
		})
	}
}

func TestUpgradeSnapshotFollowsRegularSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "snapshot.json")
	snapshot := upgradeSnapshot{SchemaVersion: upgradeSnapshotSchemaVersion, SnapshotID: "linked", CreatedAtUTC: time.Now().UTC().Format(time.RFC3339), Status: upgradeSnapshotStatusApplied}
	if err := writeUpgradeSnapshotFile(target, snapshot, RealSystem{}); err != nil {
		t.Fatal(err)
	}
	path := target + ".link"
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	got, err := readUpgradeSnapshot(path, RealSystem{})
	if err != nil || got.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("read linked snapshot = %v, %v", got, err)
	}
}
