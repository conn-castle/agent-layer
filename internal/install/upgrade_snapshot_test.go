package install

import (
	"bytes"
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
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestRunWithOverwrite_WritesAppliedUpgradeSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.6.0"}); err != nil {
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

	err := Run(root, Options{System: faultsOnce, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.6.0"})
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

	err := Run(root, Options{System: faults, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.6.0"})
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

	err := Run(root, Options{System: faultsOnce, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.6.0"})
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

func TestRollbackUpgradeSnapshot_RestoresAppliedSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "tmp"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer/tmp: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs/agent-layer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write current pin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ROADMAP.md"), []byte("new roadmap\n"), 0o644); err != nil {
		t.Fatalf("write current roadmap: %v", err)
	}
	extraPath := filepath.Join(root, ".agent-layer", "tmp", "extra.txt")
	if err := os.WriteFile(extraPath, []byte("remove me"), 0o644); err != nil {
		t.Fatalf("write extra file: %v", err)
	}

	permFile := uint32(0o644)
	permDir := uint32(0o755)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "manual-restore-1",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &permFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			},
			{
				Path: ".agent-layer/tmp/extra.txt",
				Kind: upgradeSnapshotEntryKindAbsent,
			},
			{
				Path: "docs/agent-layer",
				Kind: upgradeSnapshotEntryKindDir,
				Perm: &permDir,
			},
			{
				Path:          "docs/agent-layer/ROADMAP.md",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &permFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("old roadmap\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if err := RollbackUpgradeSnapshot(root, "manual-restore-1", RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("manual rollback: %v", err)
	}

	versionBytes, err := os.ReadFile(filepath.Join(root, ".agent-layer", "al.version"))
	if err != nil {
		t.Fatalf("read restored pin: %v", err)
	}
	if string(versionBytes) != "0.5.0\n" {
		t.Fatalf("restored pin = %q, want %q", string(versionBytes), "0.5.0\n")
	}
	roadmapBytes, err := os.ReadFile(filepath.Join(root, "docs", "agent-layer", "ROADMAP.md"))
	if err != nil {
		t.Fatalf("read restored roadmap: %v", err)
	}
	if string(roadmapBytes) != "old roadmap\n" {
		t.Fatalf("restored roadmap = %q, want %q", string(roadmapBytes), "old roadmap\n")
	}
	if _, err := os.Stat(extraPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected extra file removed, stat err = %v", err)
	}

	restoredSnapshot := latestSnapshot(t, root)
	if restoredSnapshot.Status != upgradeSnapshotStatusApplied {
		t.Fatalf("snapshot status mutated to %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusApplied)
	}
}

func TestRollbackUpgradeSnapshot_RejectsNonAppliedStatuses(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	statuses := []upgradeSnapshotStatus{
		upgradeSnapshotStatusCreated,
		upgradeSnapshotStatusAutoRolledBack,
		upgradeSnapshotStatusRollbackFailed,
	}
	for idx, status := range statuses {
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("non-applied-%d", idx),
			CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, idx, 0, time.UTC).Format(time.RFC3339),
			Status:        status,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot for status %q: %v", status, err)
		}
		err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}})
		if err == nil {
			t.Fatalf("expected rollback rejection for status %q", status)
		}
		if !strings.Contains(err.Error(), "not rollbackable") {
			t.Fatalf("expected rollbackability error for status %q, got %v", status, err)
		}
	}
}

func TestRollbackUpgradeSnapshot_RequiresSnapshotID(t *testing.T) {
	root := t.TempDir()
	err := RollbackUpgradeSnapshot(root, " ", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	if err == nil {
		t.Fatal("expected missing snapshot id error")
	}
	if !strings.Contains(err.Error(), messages.InstallUpgradeRollbackSnapshotIDRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_RejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"../evil", "../../etc/passwd", "sub/dir", "a/../b"} {
		err := RollbackUpgradeSnapshot(root, id, RollbackUpgradeSnapshotOptions{System: RealSystem{}})
		if err == nil {
			t.Fatalf("expected path traversal error for %q", id)
		}
		if !strings.Contains(err.Error(), "must not contain path separators") {
			t.Fatalf("expected path separator error for %q, got %v", id, err)
		}
	}
}

func TestRollbackUpgradeSnapshot_NotFound(t *testing.T) {
	root := t.TempDir()
	err := RollbackUpgradeSnapshot(root, "missing-id", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestRollbackUpgradeSnapshot_RequiresRoot(t *testing.T) {
	err := RollbackUpgradeSnapshot(" ", "snapshot-id", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	if err == nil {
		t.Fatal("expected missing root error")
	}
	if !strings.Contains(err.Error(), messages.InstallRootRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_RequiresSystem(t *testing.T) {
	root := t.TempDir()
	err := RollbackUpgradeSnapshot(root, "snapshot-id", RollbackUpgradeSnapshotOptions{})
	if err == nil {
		t.Fatal("expected missing system error")
	}
	if !strings.Contains(err.Error(), messages.InstallSystemRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_StatError(t *testing.T) {
	root := t.TempDir()
	snapshotID := "snapshot-id"
	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), snapshotID+".json")

	faults := newFaultSystem(RealSystem{})
	faults.statErrs[normalizePath(snapshotPath)] = errors.New("stat failed")

	err := RollbackUpgradeSnapshot(root, snapshotID, RollbackUpgradeSnapshotOptions{System: faults})
	if err == nil {
		t.Fatal("expected stat error")
	}
	if !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSnapshotEntryAbsPath_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	_, err := snapshotEntryAbsPath(root, "../../outside")
	if err == nil {
		t.Fatal("expected outside-root error")
	}
	if !strings.Contains(err.Error(), "outside repo root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSnapshotEntryAbsPath_RejectsDot(t *testing.T) {
	root := t.TempDir()
	_, err := snapshotEntryAbsPath(root, ".")
	if err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("expected invalid path error, got %v", err)
	}
}

func TestRollbackTargetsFromSnapshotEntries_DedupesPaths(t *testing.T) {
	root := t.TempDir()
	entries := []upgradeSnapshotEntry{
		{Path: ".agent-layer/al.version", Kind: upgradeSnapshotEntryKindFile},
		{Path: ".agent-layer/./al.version", Kind: upgradeSnapshotEntryKindFile},
		{Path: "docs/agent-layer/ROADMAP.md", Kind: upgradeSnapshotEntryKindFile},
	}

	targets, err := rollbackTargetsFromSnapshotEntries(root, entries)
	if err != nil {
		t.Fatalf("rollbackTargetsFromSnapshotEntries: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("target count = %d, want 2 (%v)", len(targets), targets)
	}
}

func TestRollbackTargetsFromSnapshotEntries_InvalidPath(t *testing.T) {
	root := t.TempDir()
	_, err := rollbackTargetsFromSnapshotEntries(root, []upgradeSnapshotEntry{
		{Path: " ", Kind: upgradeSnapshotEntryKindFile},
	})
	if err == nil {
		t.Fatal("expected invalid path error")
	}
}

func TestRollbackTargetRelativeDepth_UsesRepoRelativePath(t *testing.T) {
	root := filepath.Join(string(os.PathSeparator), "tmp", "repo")
	rel, depth := rollbackTargetRelativeDepth(root, filepath.Join(root, "a", "b", "c.txt"))
	if rel != "a/b/c.txt" {
		t.Fatalf("relative path = %q, want %q", rel, "a/b/c.txt")
	}
	if depth != 2 {
		t.Fatalf("depth = %d, want 2", depth)
	}
}

func TestRollbackTargetRelativeDepth_FallbackOnInvalidPath(t *testing.T) {
	rel, depth := rollbackTargetRelativeDepth("/tmp/repo", string([]byte{0}))
	if rel == "" {
		t.Fatalf("expected non-empty fallback relative path, got %q", rel)
	}
	if depth < 0 {
		t.Fatalf("depth = %d, want non-negative", depth)
	}
}

func TestRollbackUpgradeSnapshotState_NoTargets(t *testing.T) {
	root := t.TempDir()
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "no-targets",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
	}
	if err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, nil); err != nil {
		t.Fatalf("rollbackUpgradeSnapshotState: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_MalformedSnapshotFailsLoudly(t *testing.T) {
	root := t.TempDir()
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed snapshot: %v", err)
	}

	err := RollbackUpgradeSnapshot(root, "bad", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	if err == nil {
		t.Fatal("expected malformed snapshot error")
	}
	if !strings.Contains(err.Error(), "decode upgrade snapshot") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_FailureMarksSnapshotRollbackFailed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write current pin: %v", err)
	}

	perm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "manual-rollback-fails",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	failPath := filepath.Join(root, ".agent-layer", "al.version")
	faults := &writeFailOnceSystem{
		base:     RealSystem{},
		failPath: failPath,
		err:      errors.New("restore write failed"),
	}

	err := RollbackUpgradeSnapshot(root, "manual-rollback-fails", RollbackUpgradeSnapshotOptions{System: faults})
	if err == nil {
		t.Fatal("expected rollback failure")
	}
	if !strings.Contains(err.Error(), "rollback snapshot manual-rollback-fails failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), "manual-rollback-fails.json")
	updated, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read updated snapshot: %v", err)
	}
	if updated.Status != upgradeSnapshotStatusRollbackFailed {
		t.Fatalf("updated snapshot status = %q, want %q", updated.Status, upgradeSnapshotStatusRollbackFailed)
	}
	if updated.FailureStep != manualRollbackFailureStep {
		t.Fatalf("updated failure_step = %q, want %q", updated.FailureStep, manualRollbackFailureStep)
	}
	if !strings.Contains(updated.FailureError, "restore write failed") {
		t.Fatalf("updated failure_error = %q, want restore failure detail", updated.FailureError)
	}
}

func TestRollbackUpgradeSnapshot_FailureAndStatusWriteFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.WriteFile(versionPath, []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write current pin: %v", err)
	}

	perm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "manual-rollback-write-fail",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), snapshot.SnapshotID+".json")
	faults := newFaultSystem(RealSystem{})
	faults.removeErrs[normalizePath(versionPath)] = errors.New("remove failed")
	faults.writeErrs[normalizePath(snapshotPath)] = errors.New("snapshot write failed")

	err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: faults})
	if err == nil {
		t.Fatal("expected rollback error")
	}
	if !strings.Contains(err.Error(), "failed to persist rollback_failed state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_RestoresSpecialPathEntries(t *testing.T) {
	root := t.TempDir()
	deepDir := filepath.Join(root, "docs", "agent-layer", "Deep Space")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatalf("mkdir deep dir: %v", err)
	}

	targetPath := filepath.Join(deepDir, "notes v1.md")
	if err := os.WriteFile(targetPath, []byte("new notes\n"), 0o644); err != nil {
		t.Fatalf("write current notes: %v", err)
	}
	extraPath := filepath.Join(deepDir, "extra.tmp")
	if err := os.WriteFile(extraPath, []byte("remove"), 0o644); err != nil {
		t.Fatalf("write extra path: %v", err)
	}

	perm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "manual-restore-special",
		CreatedAtUTC:  time.Date(2026, time.January, 3, 1, 2, 3, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          "docs/agent-layer/Deep Space/notes v1.md",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("old notes\n")),
			},
			{
				Path: "docs/agent-layer/Deep Space/extra.tmp",
				Kind: upgradeSnapshotEntryKindAbsent,
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("rollback snapshot: %v", err)
	}

	restored, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read restored notes: %v", err)
	}
	if string(restored) != "old notes\n" {
		t.Fatalf("restored notes = %q, want %q", string(restored), "old notes\n")
	}
	if _, err := os.Stat(extraPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected extra path removed, stat err = %v", err)
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

func TestPruneUpgradeSnapshots_RetainZero(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	for idx := 0; idx < 3; idx++ {
		now := time.Date(2026, time.January, 1, 0, idx, 0, 0, time.UTC)
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("zero-%d", idx),
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot %d: %v", idx, err)
		}
	}

	if err := inst.pruneUpgradeSnapshots(0); err != nil {
		t.Fatalf("prune snapshots: %v", err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("snapshot count = %d, want 0", len(files))
	}
}

func TestPruneUpgradeSnapshots_RetainCountNoOp(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	for idx := 0; idx < 3; idx++ {
		now := time.Date(2026, time.January, 1, 1, idx, 0, 0, time.UTC)
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("retain-%d", idx),
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot %d: %v", idx, err)
		}
	}

	if err := inst.pruneUpgradeSnapshots(3); err != nil {
		t.Fatalf("prune snapshots: %v", err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("snapshot count = %d, want 3", len(files))
	}
	if files[0].id != "retain-0" || files[1].id != "retain-1" || files[2].id != "retain-2" {
		t.Fatalf("unexpected snapshot order after no-op prune: %+v", files)
	}
}

func TestPruneUpgradeSnapshots_InvalidRetain(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.pruneUpgradeSnapshots(-1)
	if err == nil || !strings.Contains(err.Error(), "retain must be non-negative") {
		t.Fatalf("expected invalid retain error, got %v", err)
	}
}

func TestListUpgradeSnapshotFiles_SortsByIDWhenCreatedAtEqual(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	ts := time.Date(2026, time.January, 1, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
	first := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "zzz",
		CreatedAtUTC:  ts,
		Status:        upgradeSnapshotStatusApplied,
	}
	second := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "aaa",
		CreatedAtUTC:  ts,
		Status:        upgradeSnapshotStatusApplied,
	}
	if err := inst.writeUpgradeSnapshot(first, false); err != nil {
		t.Fatalf("write first snapshot: %v", err)
	}
	if err := inst.writeUpgradeSnapshot(second, false); err != nil {
		t.Fatalf("write second snapshot: %v", err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("snapshot count = %d, want 2", len(files))
	}
	if files[0].id != "aaa" || files[1].id != "zzz" {
		t.Fatalf("unexpected ordering for equal timestamps: %+v", files)
	}
}

func TestPruneUpgradeSnapshots_StatError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	faults := newFaultSystem(RealSystem{})
	faults.statErrs[normalizePath(dir)] = errors.New("stat failed")
	inst := &installer{root: root, sys: faults}
	err := inst.pruneUpgradeSnapshots(1)
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestWriteUpgradeSnapshot_PruneAndMkdirErrors(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "new",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
	}

	// prune-before-create propagates malformed snapshot failures.
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed snapshot: %v", err)
	}
	err := inst.writeUpgradeSnapshot(snapshot, true)
	if err == nil || !strings.Contains(err.Error(), "list upgrade snapshots under") {
		t.Fatalf("expected prune failure, got %v", err)
	}

	// mkdir failures are surfaced during snapshot writes.
	faults := newFaultSystem(RealSystem{})
	faults.mkdirErrs[normalizePath(snapshotDir)] = errors.New("mkdir failed")
	inst = &installer{root: root, sys: faults}
	err = inst.writeUpgradeSnapshot(snapshot, false)
	if err == nil || !strings.Contains(err.Error(), "failed to create directory for") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestCreateUpgradeSnapshot_SuccessAndCaptureError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn}
	snapshot, err := inst.createUpgradeSnapshot()
	if err != nil {
		t.Fatalf("createUpgradeSnapshot: %v", err)
	}
	if snapshot.Status != upgradeSnapshotStatusCreated {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, upgradeSnapshotStatusCreated)
	}
	if !strings.Contains(warn.String(), "Created upgrade snapshot:") {
		t.Fatalf("expected snapshot creation warning output, got %q", warn.String())
	}

	faults := newFaultSystem(RealSystem{})
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	faults.statErrs[normalizePath(versionPath)] = errors.New("stat failed")
	inst = &installer{root: root, sys: faults}
	if _, err := inst.createUpgradeSnapshot(); err == nil {
		t.Fatal("expected createUpgradeSnapshot error")
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
	err := inst.pruneUpgradeSnapshots(1)
	if err == nil {
		t.Fatal("expected prune to fail on malformed snapshot")
	}
	if !strings.Contains(err.Error(), "list upgrade snapshots under") {
		t.Fatalf("expected prune error context, got %v", err)
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

func TestWriteUpgradeSnapshotFile_Errors(t *testing.T) {
	root := t.TempDir()
	invalid := upgradeSnapshot{
		SchemaVersion: 99,
		SnapshotID:    "invalid",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
	}
	err := writeUpgradeSnapshotFile(filepath.Join(root, "invalid.json"), invalid, RealSystem{})
	if err == nil || !strings.Contains(err.Error(), "validate upgrade snapshot") {
		t.Fatalf("expected validation error, got %v", err)
	}

	valid := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "valid",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
	}
	target := filepath.Join(root, "valid.json")
	faults := newFaultSystem(RealSystem{})
	faults.writeErrs[normalizePath(target)] = errors.New("write failed")
	err = writeUpgradeSnapshotFile(target, valid, faults)
	if err == nil || !strings.Contains(err.Error(), "failed to write") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestReadUpgradeSnapshot_ReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing.json")
	faults := newFaultSystem(RealSystem{})
	faults.readErrs[normalizePath(path)] = errors.New("read failed")
	_, err := readUpgradeSnapshot(path, faults)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotTarget_AbsentThenFile(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	entries := make(map[string]upgradeSnapshotEntry)
	path := filepath.Join(root, ".agent-layer", "al.version")

	if err := inst.captureUpgradeSnapshotTarget(path, entries); err != nil {
		t.Fatalf("capture absent target: %v", err)
	}
	entry, ok := entries[".agent-layer/al.version"]
	if !ok || entry.Kind != upgradeSnapshotEntryKindAbsent {
		t.Fatalf("expected absent entry, got %+v", entry)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := inst.captureUpgradeSnapshotTarget(path, entries); err != nil {
		t.Fatalf("capture file target: %v", err)
	}
	entry = entries[".agent-layer/al.version"]
	if entry.Kind != upgradeSnapshotEntryKindFile {
		t.Fatalf("expected file entry after upsert, got %+v", entry)
	}
}

func TestCaptureUpgradeSnapshotTarget_StatError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	faults := newFaultSystem(RealSystem{})
	faults.statErrs[normalizePath(path)] = errors.New("stat failed")
	inst := &installer{root: root, sys: faults}

	err := inst.captureUpgradeSnapshotTarget(path, map[string]upgradeSnapshotEntry{})
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotDirectory_UnsupportedSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	target := filepath.Join(root, ".agent-layer", "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.captureUpgradeSnapshotDirectory(dir, map[string]upgradeSnapshotEntry{})
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("expected unsupported file type error, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotFile_ReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	faults := newFaultSystem(RealSystem{})
	faults.readErrs[normalizePath(path)] = errors.New("read failed")
	inst := &installer{root: root, sys: faults}
	err := inst.captureUpgradeSnapshotFile(path, 0o644, map[string]upgradeSnapshotEntry{})
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestRepoRelativePath_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.repoRelativePath(filepath.Join(root, "..", "outside.txt"))
	if err == nil || !strings.Contains(err.Error(), "outside repo root") {
		t.Fatalf("expected outside root error, got %v", err)
	}
}

func TestPermFromSnapshot_FallbackAndMask(t *testing.T) {
	if got := permFromSnapshot(nil, 0o640); got != 0o640 {
		t.Fatalf("permFromSnapshot(nil) = %v, want %v", got, fs.FileMode(0o640))
	}
	perm := uint32(0o120777)
	if got := permFromSnapshot(&perm, 0o600); got != 0o777 {
		t.Fatalf("permFromSnapshot(masked) = %v, want %v", got, fs.FileMode(0o777))
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

// writeFailOnceSystem injects a one-shot write failure for a path.
// Unlike faultSystem.writeErrs, it fails only the first matching write so tests
// can validate rollback paths where a follow-up write must succeed.
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

func (s *writeFailOnceSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
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
