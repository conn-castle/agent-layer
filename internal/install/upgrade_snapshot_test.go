package install

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/launchers"
)

func TestRunWithOverwrite_WritesAppliedUpgradeSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Force: true, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("overwrite run: %v", err)
	}

	snapshot := latestSnapshot(t, root)
	if snapshot.Status != upgradeSnapshotStatusApplied {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusApplied)
	}
	if len(snapshot.Entries) == 0 {
		t.Fatal("snapshot entries should not be empty")
	}

	versionEntry, ok := findSnapshotEntry(snapshot, ".agent-layer/al.version")
	if !ok {
		t.Fatal("snapshot missing .agent-layer/al.version entry")
	}
	if versionEntry.Kind != upgradeSnapshotEntryKindFile {
		t.Fatalf("version entry kind = %q, want %q", versionEntry.Kind, upgradeSnapshotEntryKindFile)
	}
	versionBytes, decodeErr := base64.StdEncoding.DecodeString(versionEntry.ContentBase64)
	if decodeErr != nil {
		t.Fatalf("decode version entry content: %v", decodeErr)
	}
	if string(versionBytes) != "0.5.0\n" {
		t.Fatalf("version entry content = %q, want %q", string(versionBytes), "0.5.0\n")
	}
}

func TestRunWithOverwrite_RollbackRestoresGitignoreOnFailure(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	originalGitignore := "node_modules/\n# user line\n"
	gitignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(originalGitignore), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	faultsOnce := &writeFailOnceSystem{
		base:     RealSystem{},
		failPath: launchers.VSCodePaths(root).Command,
		err:      errors.New("launcher write failed"),
	}

	err := Run(root, Options{System: faultsOnce, Overwrite: true, Force: true, PinVersion: "0.6.0"})
	if err == nil {
		t.Fatal("expected upgrade failure")
	}
	if !strings.Contains(err.Error(), "writeVSCodeLaunchers") {
		t.Fatalf("expected failure in writeVSCodeLaunchers, got %v", err)
	}

	restored, readErr := os.ReadFile(gitignorePath)
	if readErr != nil {
		t.Fatalf("read gitignore: %v", readErr)
	}
	if string(restored) != originalGitignore {
		t.Fatalf("gitignore was not restored.\nwant:\n%s\ngot:\n%s", originalGitignore, string(restored))
	}

	snapshot := latestSnapshot(t, root)
	if snapshot.Status != upgradeSnapshotStatusAutoRolledBack {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusAutoRolledBack)
	}
	if snapshot.FailureStep != "writeVSCodeLaunchers" {
		t.Fatalf("snapshot failure_step = %q, want writeVSCodeLaunchers", snapshot.FailureStep)
	}
}

func TestRunWithOverwrite_RollbackRestoresDeletedUnknownPath(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	unknownA := filepath.Join(root, ".agent-layer", "a-unknown.txt")
	unknownB := filepath.Join(root, ".agent-layer", "b-unknown.txt")
	if err := os.WriteFile(unknownA, []byte("a"), 0o644); err != nil {
		t.Fatalf("write unknownA: %v", err)
	}
	if err := os.WriteFile(unknownB, []byte("b"), 0o644); err != nil {
		t.Fatalf("write unknownB: %v", err)
	}

	deletePromptCount := 0
	err := Run(root, Options{
		System:    RealSystem{},
		Overwrite: true,
		Prompter: PromptFuncs{
			OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
			OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
			OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:          func([]string) (bool, error) { return false, nil },
			DeleteUnknownFunc: func(path string) (bool, error) {
				deletePromptCount++
				if strings.HasSuffix(path, "a-unknown.txt") {
					return true, nil
				}
				return false, fmt.Errorf("prompt failed for %s", path)
			},
		},
		PinVersion: "0.6.0",
	})
	if err == nil {
		t.Fatal("expected upgrade failure")
	}
	if !strings.Contains(err.Error(), "handleUnknowns") {
		t.Fatalf("expected failure in handleUnknowns, got %v", err)
	}
	if deletePromptCount < 2 {
		t.Fatalf("expected per-path delete prompts for both unknown paths, got %d", deletePromptCount)
	}
	if _, statErr := os.Stat(unknownA); statErr != nil {
		t.Fatalf("unknownA not restored: %v", statErr)
	}
	if _, statErr := os.Stat(unknownB); statErr != nil {
		t.Fatalf("unknownB not restored: %v", statErr)
	}

	snapshot := latestSnapshot(t, root)
	if snapshot.Status != upgradeSnapshotStatusAutoRolledBack {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusAutoRolledBack)
	}
	if snapshot.FailureStep != "handleUnknowns" {
		t.Fatalf("snapshot failure_step = %q, want handleUnknowns", snapshot.FailureStep)
	}
}

func TestRunWithOverwrite_SnapshotMarksRollbackFailed(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	faults := newFaultSystem(RealSystem{})
	managedFile := filepath.Join(root, ".gitignore")
	faults.writeErrs[normalizePath(managedFile)] = errors.New("write failure")
	faults.removeErrs[normalizePath(managedFile)] = errors.New("rollback remove failure")

	err := Run(root, Options{System: faults, Overwrite: true, Force: true, PinVersion: "0.6.0"})
	if err == nil {
		t.Fatal("expected upgrade failure")
	}
	if !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("expected rollback failure, got %v", err)
	}

	snapshot := latestSnapshot(t, root)
	if snapshot.Status != upgradeSnapshotStatusRollbackFailed {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusRollbackFailed)
	}
}

func TestRunWithOverwrite_RollbackScopesToExecutedStepTargets(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	faults := newFaultSystem(RealSystem{})
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	unrelatedPath := filepath.Join(root, "docs", "agent-layer")
	faults.removeErrs[normalizePath(unrelatedPath)] = errors.New("unexpected remove of unrelated path")
	faultsOnce := &writeFailOnceSystem{
		base:     faults,
		failPath: versionPath,
		err:      errors.New("version write failure"),
	}

	err := Run(root, Options{System: faultsOnce, Overwrite: true, Force: true, PinVersion: "0.6.0"})
	if err == nil {
		t.Fatal("expected upgrade failure")
	}
	if !strings.Contains(err.Error(), "writeVersionFile") {
		t.Fatalf("expected failure in writeVersionFile, got %v", err)
	}
	if strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("rollback should not attempt unrelated paths, got %v", err)
	}

	snapshot := latestSnapshot(t, root)
	if snapshot.Status != upgradeSnapshotStatusAutoRolledBack {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusAutoRolledBack)
	}
	if snapshot.FailureStep != "writeVersionFile" {
		t.Fatalf("snapshot failure_step = %q, want writeVersionFile", snapshot.FailureStep)
	}

	manifestPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Fatalf("expected unrelated memory file to remain: %v", statErr)
	}
}

func TestPruneUpgradeSnapshots_KeepNewest(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	for idx := 0; idx < 5; idx++ {
		now := time.Date(2026, time.January, 1, 0, idx, 0, 0, time.UTC)
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("s-%d", idx),
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot %d: %v", idx, err)
		}
	}
	if err := inst.pruneUpgradeSnapshots(2); err != nil {
		t.Fatalf("prune snapshots: %v", err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("snapshot count = %d, want 2", len(files))
	}
	if files[0].id != "s-3" || files[1].id != "s-4" {
		t.Fatalf("unexpected snapshots after prune: %+v", files)
	}
}

func TestPruneUpgradeSnapshots_FailsLoudlyOnMalformedSnapshot(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	malformedPath := filepath.Join(snapshotDir, "bad.json")
	if err := os.WriteFile(malformedPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed snapshot: %v", err)
	}
	if err := inst.pruneUpgradeSnapshots(1); err == nil {
		t.Fatal("expected prune to fail on malformed snapshot")
	}
}

func TestPruneUpgradeSnapshots_FailsLoudlyOnInvalidSnapshot(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	invalidPath := filepath.Join(snapshotDir, "invalid.json")
	invalid := `{
  "schema_version": 1,
  "snapshot_id": "invalid",
  "created_at_utc": "2026-01-01T00:00:00Z",
  "status": "bad-status",
  "entries": []
}`
	if err := os.WriteFile(invalidPath, []byte(invalid), 0o644); err != nil {
		t.Fatalf("write invalid snapshot: %v", err)
	}
	err := inst.pruneUpgradeSnapshots(1)
	if err == nil {
		t.Fatal("expected prune to fail on invalid snapshot")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid status failure, got %v", err)
	}
}

func TestValidateUpgradeSnapshot_RejectsInvalidSnapshot(t *testing.T) {
	makeValidSnapshot := func() upgradeSnapshot {
		perm := uint32(0o644)
		return upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "snapshot-1",
			CreatedAtUTC:  "2026-01-01T00:00:00Z",
			Status:        upgradeSnapshotStatusCreated,
			Entries: []upgradeSnapshotEntry{
				{
					Path:          ".agent-layer/al.version",
					Kind:          upgradeSnapshotEntryKindFile,
					Perm:          &perm,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
				},
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(snapshot *upgradeSnapshot)
		want   string
	}{
		{
			name: "schema version",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.SchemaVersion = 99
			},
			want: "unsupported schema_version",
		},
		{
			name: "missing snapshot id",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.SnapshotID = " "
			},
			want: "snapshot_id is required",
		},
		{
			name: "invalid created_at_utc",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.CreatedAtUTC = "not-a-time"
			},
			want: "invalid created_at_utc",
		},
		{
			name: "invalid status",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.Status = "bogus"
			},
			want: "invalid status",
		},
		{
			name: "duplicate entries",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.Entries = append(snapshot.Entries, snapshot.Entries[0])
			},
			want: "duplicate snapshot entry path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := makeValidSnapshot()
			tc.mutate(&snapshot)
			err := validateUpgradeSnapshot(snapshot)
			if err == nil {
				t.Fatalf("expected validation error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateUpgradeSnapshotEntry_RejectsInvalidEntry(t *testing.T) {
	perm := uint32(0o644)
	tests := []struct {
		name  string
		entry upgradeSnapshotEntry
		want  string
	}{
		{
			name: "missing path",
			entry: upgradeSnapshotEntry{
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("x")),
			},
			want: "snapshot entry path is required",
		},
		{
			name: "file missing content",
			entry: upgradeSnapshotEntry{
				Path: ".agent-layer/al.version",
				Kind: upgradeSnapshotEntryKindFile,
				Perm: &perm,
			},
			want: "requires content_base64",
		},
		{
			name: "file invalid base64",
			entry: upgradeSnapshotEntry{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: "!!!",
			},
			want: "invalid content_base64",
		},
		{
			name: "dir has content",
			entry: upgradeSnapshotEntry{
				Path:          "docs/agent-layer",
				Kind:          upgradeSnapshotEntryKindDir,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("x")),
			},
			want: "must not set content_base64",
		},
		{
			name: "absent has perm",
			entry: upgradeSnapshotEntry{
				Path: "docs/agent-layer/extra.md",
				Kind: upgradeSnapshotEntryKindAbsent,
				Perm: &perm,
			},
			want: "must not set perm",
		},
		{
			name: "invalid kind",
			entry: upgradeSnapshotEntry{
				Path: ".agent-layer/al.version",
				Kind: "bogus",
			},
			want: "invalid snapshot entry kind",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpgradeSnapshotEntry(tc.entry)
			if err == nil {
				t.Fatalf("expected validation error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func latestSnapshot(t *testing.T, root string) upgradeSnapshot {
	t.Helper()
	inst := &installer{root: root, sys: RealSystem{}}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one snapshot")
	}
	snapshot, err := readUpgradeSnapshot(files[len(files)-1].path, inst.sys)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	return snapshot
}

func findSnapshotEntry(snapshot upgradeSnapshot, path string) (upgradeSnapshotEntry, bool) {
	for _, entry := range snapshot.Entries {
		if entry.Path == path {
			return entry, true
		}
	}
	return upgradeSnapshotEntry{}, false
}

type writeFailOnceSystem struct {
	base     System
	failPath string
	err      error
	fired    bool
}

func (s *writeFailOnceSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *writeFailOnceSystem) ReadFile(name string) ([]byte, error) {
	return s.base.ReadFile(name)
}

func (s *writeFailOnceSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s *writeFailOnceSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s *writeFailOnceSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return s.base.WalkDir(root, fn)
}

func (s *writeFailOnceSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	if !s.fired && normalizePath(filename) == normalizePath(s.failPath) {
		s.fired = true
		return s.err
	}
	return s.base.WriteFileAtomic(filename, data, perm)
}
