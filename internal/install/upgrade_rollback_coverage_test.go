package install

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRollbackUpgradeSnapshot_AdditionalBranches(t *testing.T) {
	t.Run("rollback targets conversion error propagates", func(t *testing.T) {
		root := t.TempDir()
		snapshotID := "invalid-targets"
		snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
		sys := RealSystem{}
		if err := sys.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		snapshotPath := filepath.Join(snapshotDir, snapshotID+".json")
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    snapshotID,
			CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{{
				Path: "../outside",
				Kind: upgradeSnapshotEntryKindAbsent,
			}},
		}
		if err := writeUpgradeSnapshotFile(snapshotPath, snapshot, RealSystem{}); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}

		err := RollbackUpgradeSnapshot(root, snapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}})
		if err == nil || !strings.Contains(err.Error(), "resolves outside repo root") {
			t.Fatalf("expected rollback target conversion error, got %v", err)
		}
	})

	t.Run("successful rollback cannot persist manually rolled back status", func(t *testing.T) {
		root := t.TempDir()
		snapshotID := "status-write-fails"
		snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
		sys := RealSystem{}
		if err := sys.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		snapshotPath := filepath.Join(snapshotDir, snapshotID+".json")
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    snapshotID,
			CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries:       []upgradeSnapshotEntry{},
		}
		if err := writeUpgradeSnapshotFile(snapshotPath, snapshot, RealSystem{}); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}

		fault := newFaultSystem(RealSystem{})
		fault.writeErrs[normalizePath(snapshotPath)] = errors.New("write status boom")
		err := RollbackUpgradeSnapshot(root, snapshotID, RollbackUpgradeSnapshotOptions{System: fault})
		if err == nil || !strings.Contains(err.Error(), "failed to persist manually_rolled_back state") {
			t.Fatalf("expected manual status write error, got %v", err)
		}
	})
}

func TestRollbackUpgradeSnapshotState_AdditionalBranches(t *testing.T) {
	t.Run("invalid snapshot validation error", func(t *testing.T) {
		err := rollbackUpgradeSnapshotState(t.TempDir(), RealSystem{}, upgradeSnapshot{SchemaVersion: 0}, nil)
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_AdditionalBranches(t *testing.T) {
	t.Run("sorts multiple symlink entries", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{Path: "z-link", Kind: upgradeSnapshotEntryKindSymlink, LinkTarget: "z-target"},
			{Path: "a-link", Kind: upgradeSnapshotEntryKindSymlink, LinkTarget: "a-target"},
		})
		if err != nil {
			t.Fatalf("restore symlink entries: %v", err)
		}
	})

	t.Run("file entry path resolution error", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{Path: "../outside", Kind: upgradeSnapshotEntryKindFile, ContentBase64: base64.StdEncoding.EncodeToString([]byte("x"))},
		})
		if err == nil || !strings.Contains(err.Error(), "resolves outside repo root") {
			t.Fatalf("expected file path resolution error, got %v", err)
		}
	})

	t.Run("symlink entry path resolution error", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{Path: "../outside", Kind: upgradeSnapshotEntryKindSymlink, LinkTarget: "target"},
		})
		if err == nil || !strings.Contains(err.Error(), "resolves outside repo root") {
			t.Fatalf("expected symlink path resolution error, got %v", err)
		}
	})

	t.Run("symlink requires link target", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{Path: "valid", Kind: upgradeSnapshotEntryKindSymlink, LinkTarget: ""},
		})
		if err == nil || !strings.Contains(err.Error(), "requires link_target") {
			t.Fatalf("expected missing link target error, got %v", err)
		}
	})
}
