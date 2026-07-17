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
	"syscall"
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
	if err := os.WriteFile(gitignorePath, []byte(originalGitignore), 0o600); err != nil {
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

	restored, readErr := os.ReadFile(gitignorePath) // #nosec G304 -- path is constructed from test-controlled inputs.
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

func TestRunWithOverwrite_RollbackRestoresStatuslineSourceOnFailure(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	// A pre-existing, user-customized statusline source must survive an automatic
	// rollback. writeStatuslineSources registers this path as a rollback target
	// (writeStatuslineSourcesTargetPaths), so unless the path is also captured by
	// upgradeSnapshotTargetPaths, rollback RemoveAll's it with no snapshot entry
	// to restore — silently destroying a file the user never asked to delete.
	// Statusline is disabled here, so the write step never touches the file: the
	// only thing that can delete it is the rollback bug this guards against.
	sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
	customSource := "#!/usr/bin/env bash\n# user-customized statusline\nprintf 'hello\\n'\n"
	if err := os.WriteFile(sourcePath, []byte(customSource), 0o600); err != nil {
		t.Fatalf("write statusline source: %v", err)
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

	restored, readErr := os.ReadFile(sourcePath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("statusline source was not restored after rollback: %v", readErr)
	}
	if string(restored) != customSource {
		t.Fatalf("statusline source not restored to user content.\nwant:\n%s\ngot:\n%s", customSource, string(restored))
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
	if err := os.WriteFile(unknownA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write unknownA: %v", err)
	}
	if err := os.WriteFile(unknownB, []byte("b"), 0o600); err != nil {
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
	seedWorkflowBundleForTest(t, root)

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
	if !strings.Contains(snapshot.FailureError, "write failure") || !strings.Contains(snapshot.FailureError, "rollback remove failure") {
		t.Fatalf("snapshot failure_error = %q, want upgrade and rollback failures", snapshot.FailureError)
	}
	failureStep := snapshot.FailureStep
	failureError := snapshot.FailureError
	unrelatedPath := launchers.VSCodePaths(root).Command
	unrelatedContent := []byte("post-failure user edit\n")
	if err := os.MkdirAll(filepath.Dir(unrelatedPath), 0o700); err != nil {
		t.Fatalf("mkdir unrelated path parent: %v", err)
	}
	if err := os.WriteFile(unrelatedPath, unrelatedContent, 0o600); err != nil {
		t.Fatalf("write unrelated post-failure change: %v", err)
	}
	for _, target := range snapshot.RollbackTargets {
		if target == ".agent-layer/open-vscode.command" {
			t.Fatalf("automatic rollback scope unexpectedly includes later launcher step target %q", target)
		}
	}
	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("retry automatic rollback failure: %v", err)
	}
	restored := latestSnapshot(t, root)
	if restored.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("retried snapshot status = %q, want %q", restored.Status, upgradeSnapshotStatusManuallyRolledBack)
	}
	if restored.FailureStep != failureStep || restored.FailureError != failureError {
		t.Fatalf("retry discarded automatic failure evidence: before=(%q, %q) after=(%q, %q)", failureStep, failureError, restored.FailureStep, restored.FailureError)
	}
	gotUnrelated, err := os.ReadFile(unrelatedPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read unrelated post-failure change: %v", err)
	}
	if !bytes.Equal(gotUnrelated, unrelatedContent) {
		t.Fatalf("retry overwrote unrelated post-failure change: got %q, want %q", gotUnrelated, unrelatedContent)
	}
}

func TestRunWithOverwrite_RollbackScopesToExecutedStepTargets(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.5.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	seedWorkflowBundleForTest(t, root)

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
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "tmp"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer/tmp: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs/agent-layer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.6.0\n"), 0o600); err != nil {
		t.Fatalf("write current pin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ROADMAP.md"), []byte("new roadmap\n"), 0o600); err != nil {
		t.Fatalf("write current roadmap: %v", err)
	}
	extraPath := filepath.Join(root, ".agent-layer", "tmp", "extra.txt")
	if err := os.WriteFile(extraPath, []byte("remove me"), 0o600); err != nil {
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

	versionBytes, err := os.ReadFile(filepath.Join(root, ".agent-layer", "al.version")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored pin: %v", err)
	}
	if string(versionBytes) != "0.5.0\n" {
		t.Fatalf("restored pin = %q, want %q", string(versionBytes), "0.5.0\n")
	}
	roadmapBytes, err := os.ReadFile(filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if restoredSnapshot.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("snapshot status mutated to %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusManuallyRolledBack)
	}
}

func TestRollbackUpgradeSnapshot_RestoresCreatedSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	// Simulate a partially-applied upgrade (Ctrl-C after snapshot creation
	// but before the transaction marked the snapshot as applied).
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.9.0\n"), 0o600); err != nil {
		t.Fatalf("write current pin: %v", err)
	}

	permFile := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "interrupted-upgrade-1",
		CreatedAtUTC:  time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &permFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.8.0\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if err := RollbackUpgradeSnapshot(root, "interrupted-upgrade-1", RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("rollback of created snapshot should succeed: %v", err)
	}

	versionBytes, err := os.ReadFile(filepath.Join(root, ".agent-layer", "al.version")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored pin: %v", err)
	}
	if string(versionBytes) != "0.8.0\n" {
		t.Fatalf("restored pin = %q, want %q", string(versionBytes), "0.8.0\n")
	}

	restoredSnapshot := latestSnapshot(t, root)
	if restoredSnapshot.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("snapshot status = %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusManuallyRolledBack)
	}
}

func TestRollbackUpgradeSnapshot_RejectsNonRollbackableStatuses(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	statuses := []upgradeSnapshotStatus{
		upgradeSnapshotStatusAutoRolledBack,
		upgradeSnapshotStatusManuallyRolledBack,
	}
	for idx, status := range statuses {
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("non-rollbackable-%d", idx),
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

func TestRollbackUpgradeSnapshot_RestoresNonCanonicalEntryPath(t *testing.T) {
	root := t.TempDir()
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(versionPath), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(versionPath, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("write current version: %v", err)
	}
	perm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "non-canonical-entry",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{{
			Path:          ".agent-layer/./al.version",
			Kind:          upgradeSnapshotEntryKindFile,
			Perm:          &perm,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("old\n")),
		}},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("rollback non-canonical entry: %v", err)
	}
	got, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored version: %v", err)
	}
	if string(got) != "old\n" {
		t.Fatalf("restored version = %q, want old snapshot content", got)
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

func TestRollbackUpgradeSnapshotState_AlwaysRestoresVersionEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs/agent-layer: %v", err)
	}
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	roadmapPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.WriteFile(versionPath, []byte("0.6.0\n"), 0o600); err != nil {
		t.Fatalf("write current version: %v", err)
	}
	if err := os.WriteFile(roadmapPath, []byte("new roadmap\n"), 0o600); err != nil {
		t.Fatalf("write current roadmap: %v", err)
	}

	filePerm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "always-restore-version",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &filePerm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			},
			{
				Path:          "docs/agent-layer/ROADMAP.md",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &filePerm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("old roadmap\n")),
			},
		},
	}

	// Simulate a target list that omits .agent-layer/al.version.
	if err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, []string{roadmapPath}); err != nil {
		t.Fatalf("rollbackUpgradeSnapshotState: %v", err)
	}

	versionBytes, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored version: %v", err)
	}
	if string(versionBytes) != "0.5.0\n" {
		t.Fatalf("restored version = %q, want %q", string(versionBytes), "0.5.0\n")
	}
	roadmapBytes, err := os.ReadFile(roadmapPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored roadmap: %v", err)
	}
	if string(roadmapBytes) != "old roadmap\n" {
		t.Fatalf("restored roadmap = %q, want %q", string(roadmapBytes), "old roadmap\n")
	}
}

func TestRollbackUpgradeSnapshotState_VersionEntryPathError(t *testing.T) {
	root := string([]byte{0})
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "invalid-version-entry",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          pinVersionRelPath,
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			},
		},
	}

	err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, nil)
	if err == nil {
		t.Fatal("expected path resolution error")
	}
}

func TestRollbackUpgradeSnapshotState_RemoveErrorReportsRelativePath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "remove-error-fallback",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
	}

	faults := newFaultSystem(RealSystem{})
	faults.removeErrs[normalizePath(target)] = errors.New("remove boom")

	err := rollbackUpgradeSnapshotState(root, faults, snapshot, []string{target})
	if err == nil {
		t.Fatal("expected remove error")
	}
	if !strings.Contains(err.Error(), "reset path target.txt") {
		t.Fatalf("expected relative target path in error, got %v", err)
	}
}

func TestRollbackUpgradeSnapshotState_RejectsOutsideTargetBeforeMutation(t *testing.T) {
	root := t.TempDir()
	outsideTarget := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideTarget, []byte("preserve\n"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "outside-target",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
	}

	err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, []string{outsideTarget})
	if err == nil || !strings.Contains(err.Error(), "outside repo root") {
		t.Fatalf("outside target error = %v, want containment rejection", err)
	}
	content, readErr := os.ReadFile(outsideTarget) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read preserved outside target: %v", readErr)
	}
	if string(content) != "preserve\n" {
		t.Fatalf("outside target changed to %q", content)
	}
}

func TestRollbackUpgradeSnapshotState_RejectsTargetThroughExternalSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "preserve.txt")
	if err := os.WriteFile(outsidePath, []byte("preserve\n"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatalf("create external ancestor symlink: %v", err)
	}
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "external-symlink-ancestor",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{{
			Path: "link/preserve.txt",
			Kind: upgradeSnapshotEntryKindAbsent,
		}},
	}

	err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, []string{filepath.Join(root, "link", "preserve.txt")})
	if err == nil || !strings.Contains(err.Error(), "ancestor that resolves outside repo root") {
		t.Fatalf("external symlink ancestor error = %v, want containment rejection", err)
	}
	content, readErr := os.ReadFile(outsidePath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read preserved outside target: %v", readErr)
	}
	if string(content) != "preserve\n" {
		t.Fatalf("outside target changed to %q", content)
	}
}

func TestRollbackUpgradeSnapshotState_AllowsTargetThroughInternalSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	actualDir := filepath.Join(root, "actual")
	if err := os.MkdirAll(actualDir, 0o700); err != nil {
		t.Fatalf("mkdir internal target: %v", err)
	}
	if err := os.Symlink("actual", filepath.Join(root, "link")); err != nil {
		t.Fatalf("create internal ancestor symlink: %v", err)
	}
	actualPath := filepath.Join(actualDir, "state.txt")
	if err := os.WriteFile(actualPath, []byte("current\n"), 0o600); err != nil {
		t.Fatalf("write current internal target: %v", err)
	}
	perm := uint32(0o600)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "internal-symlink-ancestor",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{{
			Path:          "link/state.txt",
			Kind:          upgradeSnapshotEntryKindFile,
			Perm:          &perm,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("captured\n")),
		}},
	}

	if err := rollbackUpgradeSnapshotState(root, RealSystem{}, snapshot, []string{filepath.Join(root, "link", "state.txt")}); err != nil {
		t.Fatalf("rollback through internal symlink ancestor: %v", err)
	}
	content, err := os.ReadFile(actualPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored internal target: %v", err)
	}
	if string(content) != "captured\n" {
		t.Fatalf("restored internal target = %q, want captured content", content)
	}
}

func TestRollbackUpgradeSnapshotState_PreparesUpgradeCreatedDirectoriesBeforeReset(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "captured")
	createdDir := filepath.Join(target, "created", "nested")
	if err := os.MkdirAll(createdDir, 0o700); err != nil {
		t.Fatalf("mkdir upgrade-created tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(createdDir, "extra.txt"), []byte("remove\n"), 0o400); err != nil {
		t.Fatalf("write upgrade-created file: %v", err)
	}
	if err := os.Chmod(filepath.Dir(createdDir), 0o500); err != nil { // #nosec G302 -- restrictive current mode exercises reset preparation.
		t.Fatalf("restrict created parent: %v", err)
	}
	if err := os.Chmod(createdDir, 0o500); err != nil { // #nosec G302 -- restrictive current mode exercises reset preparation.
		t.Fatalf("restrict created child: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Dir(createdDir), 0o700) // #nosec G302 -- test restores owner access if rollback fails.
		_ = os.Chmod(createdDir, 0o700)               // #nosec G302 -- test restores owner access if rollback fails.
	})
	perm := uint32(0o700)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "prepare-current-directories",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{{
			Path: "captured",
			Kind: upgradeSnapshotEntryKindDir,
			Perm: &perm,
		}},
	}
	sys := &requireWritableDirectoriesBeforeRemoveSystem{
		System: RealSystem{},
		dirs:   []string{filepath.Dir(createdDir), createdDir},
	}

	if err := rollbackUpgradeSnapshotState(root, sys, snapshot, []string{target}); err != nil {
		t.Fatalf("rollback with upgrade-created restrictive directories: %v", err)
	}
	if _, err := os.Stat(filepath.Join(createdDir, "extra.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upgrade-created file still exists after reset, stat error = %v", err)
	}
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_ErrorBranches(t *testing.T) {
	t.Run("snapshotEntryAbsPath error", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{Path: "../../outside", Kind: upgradeSnapshotEntryKindDir},
		})
		if err == nil || !strings.Contains(err.Error(), "outside repo root") {
			t.Fatalf("expected snapshotEntryAbsPath error, got %v", err)
		}
	})

	t.Run("restore directory mkdir error", func(t *testing.T) {
		root := t.TempDir()
		dirPath := filepath.Join(root, "docs", "agent-layer")
		faults := newFaultSystem(RealSystem{})
		faults.mkdirErrs[normalizePath(dirPath)] = errors.New("mkdir boom")
		err := restoreUpgradeSnapshotEntriesAtRoot(root, faults, []upgradeSnapshotEntry{
			{Path: "docs/agent-layer", Kind: upgradeSnapshotEntryKindDir},
		})
		if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
			t.Fatalf("expected mkdir error, got %v", err)
		}
	})

	t.Run("file decode error", func(t *testing.T) {
		root := t.TempDir()
		err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: "!!!",
			},
		})
		if err == nil || !strings.Contains(err.Error(), "decode content") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})

	t.Run("file parent mkdir error", func(t *testing.T) {
		root := t.TempDir()
		filePath := filepath.Join(root, ".agent-layer", "al.version")
		faults := newFaultSystem(RealSystem{})
		faults.mkdirErrs[normalizePath(filepath.Dir(filePath))] = errors.New("mkdir boom")
		err := restoreUpgradeSnapshotEntriesAtRoot(root, faults, []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.1.0\n")),
			},
		})
		if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
			t.Fatalf("expected parent mkdir error, got %v", err)
		}
	})

	t.Run("file write error", func(t *testing.T) {
		root := t.TempDir()
		filePath := filepath.Join(root, ".agent-layer", "al.version")
		faults := newFaultSystem(RealSystem{})
		faults.writeErrs[normalizePath(filePath)] = errors.New("write boom")
		err := restoreUpgradeSnapshotEntriesAtRoot(root, faults, []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.1.0\n")),
			},
		})
		if err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected write error, got %v", err)
		}
	})

	t.Run("symlink parent mkdir error", func(t *testing.T) {
		root := t.TempDir()
		linkPath := filepath.Join(root, ".agent-layer", "al.version")
		faults := newFaultSystem(RealSystem{})
		faults.mkdirErrs[normalizePath(filepath.Dir(linkPath))] = errors.New("mkdir boom")
		err := restoreUpgradeSnapshotEntriesAtRoot(root, faults, []upgradeSnapshotEntry{
			{
				Path:       ".agent-layer/al.version",
				Kind:       upgradeSnapshotEntryKindSymlink,
				LinkTarget: ".agent-layer/target.txt",
			},
		})
		if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
			t.Fatalf("expected parent mkdir error, got %v", err)
		}
	})

	t.Run("symlink create error", func(t *testing.T) {
		root := t.TempDir()
		linkPath := filepath.Join(root, ".agent-layer", "al.version")
		faults := newFaultSystem(RealSystem{})
		faults.symlinkErrs[normalizePath(linkPath)] = errors.New("symlink boom")
		err := restoreUpgradeSnapshotEntriesAtRoot(root, faults, []upgradeSnapshotEntry{
			{
				Path:       ".agent-layer/al.version",
				Kind:       upgradeSnapshotEntryKindSymlink,
				LinkTarget: ".agent-layer/target.txt",
			},
		})
		if err == nil || !strings.Contains(err.Error(), "symlink boom") {
			t.Fatalf("expected symlink error, got %v", err)
		}
	})
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_RestoresSymlinkEntries(t *testing.T) {
	root := t.TempDir()
	entries := []upgradeSnapshotEntry{
		{
			Path:       ".agent-layer/al.version",
			Kind:       upgradeSnapshotEntryKindSymlink,
			LinkTarget: ".agent-layer/target.txt",
		},
	}
	if err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, entries); err != nil {
		t.Fatalf("restoreUpgradeSnapshotEntriesAtRoot: %v", err)
	}

	linkPath := filepath.Join(root, ".agent-layer", "al.version")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat restored symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("restored path mode = %v, want symlink", info.Mode())
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink restored symlink: %v", err)
	}
	if target != ".agent-layer/target.txt" {
		t.Fatalf("restored symlink target = %q, want %q", target, ".agent-layer/target.txt")
	}
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_RestoresDescendantsBeforeDirectoryModes(t *testing.T) {
	root := t.TempDir()
	parentPath := filepath.Join(root, "captured")
	childPath := filepath.Join(parentPath, "nested")
	t.Cleanup(func() {
		_ = os.Chmod(parentPath, 0o700) // #nosec G302 -- test restores owner traversal/write access for TempDir cleanup.
		_ = os.Chmod(childPath, 0o700)  // #nosec G302 -- test restores owner traversal/write access for TempDir cleanup.
	})

	parentPerm := uint32(0o600)
	childPerm := uint32(0o500)
	filePerm := uint32(0o400)
	entries := []upgradeSnapshotEntry{
		// The non-canonical parent path must be sorted by its resolved depth;
		// counting separators in the raw value would apply its final mode first.
		{Path: "captured/./.", Kind: upgradeSnapshotEntryKindDir, Perm: &parentPerm},
		{Path: "captured/nested", Kind: upgradeSnapshotEntryKindDir, Perm: &childPerm},
		{
			Path:          "captured/nested/original.txt",
			Kind:          upgradeSnapshotEntryKindFile,
			Perm:          &filePerm,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("original\n")),
		},
	}

	if err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, entries); err != nil {
		t.Fatalf("restore entries with restrictive directory modes: %v", err)
	}
	parentInfo, err := os.Stat(parentPath)
	if err != nil {
		t.Fatalf("stat restored parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != os.FileMode(parentPerm) {
		t.Fatalf("parent mode = %o, want %o", got, parentPerm)
	}

	// The parent intentionally has no execute bit after restore. Reopen it only
	// after verifying its final mode so the descendant content and modes can be
	// inspected and the temporary restore mode cannot hide a missed final chmod.
	if err := os.Chmod(parentPath, 0o700); err != nil { // #nosec G302 -- test restores owner traversal for descendant assertions.
		t.Fatalf("make restored parent traversable for assertions: %v", err)
	}
	childInfo, err := os.Stat(childPath)
	if err != nil {
		t.Fatalf("stat restored child: %v", err)
	}
	if got := childInfo.Mode().Perm(); got != os.FileMode(childPerm) {
		t.Fatalf("child mode = %o, want %o", got, childPerm)
	}
	content, err := os.ReadFile(filepath.Join(childPath, "original.txt")) // #nosec G304 -- path is test-controlled.
	if err != nil {
		t.Fatalf("read restored descendant: %v", err)
	}
	if string(content) != "original\n" {
		t.Fatalf("restored descendant = %q, want original content", content)
	}
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_RejectsSymlinkBeforeTemporaryDirectoryMode(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod(outside, 0o700) // #nosec G302 -- test restores owner access for TempDir cleanup.
	})
	if err := os.Chmod(outside, 0o500); err != nil { // #nosec G302 -- restrictive mode proves restore never chmods through the symlink.
		t.Fatalf("set outside mode: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "captured")); err != nil {
		t.Fatalf("create captured directory symlink: %v", err)
	}

	perm := uint32(0o700)
	err := restoreUpgradeSnapshotEntriesAtRoot(root, RealSystem{}, []upgradeSnapshotEntry{{
		Path: "captured",
		Kind: upgradeSnapshotEntryKindDir,
		Perm: &perm,
	}})
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("restore symlink directory error = %v, want real-directory rejection", err)
	}
	outsideInfo, statErr := os.Stat(outside)
	if statErr != nil {
		t.Fatalf("stat outside directory: %v", statErr)
	}
	if got := outsideInfo.Mode().Perm(); got != 0o500 {
		t.Fatalf("outside directory mode = %o, want 500", got)
	}
}

func TestRestoreUpgradeSnapshotEntriesAtRoot_RevalidatesDirectoryBeforeFinalMode(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod(outside, 0o700) // #nosec G302 -- test restores owner access for TempDir cleanup.
	})
	if err := os.Chmod(outside, 0o500); err != nil { // #nosec G302 -- restrictive mode proves final restore never chmods through the symlink.
		t.Fatalf("set outside mode: %v", err)
	}
	capturedPath := filepath.Join(root, "captured")
	sys := &swapDirectoryForSymlinkAfterChmodSystem{
		System: RealSystem{},
		path:   capturedPath,
		target: outside,
	}
	perm := uint32(0o700)
	err := restoreUpgradeSnapshotEntriesAtRoot(root, sys, []upgradeSnapshotEntry{{
		Path: "captured",
		Kind: upgradeSnapshotEntryKindDir,
		Perm: &perm,
	}})
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("restore swapped directory error = %v, want real-directory rejection", err)
	}
	outsideInfo, statErr := os.Stat(outside)
	if statErr != nil {
		t.Fatalf("stat outside directory: %v", statErr)
	}
	if got := outsideInfo.Mode().Perm(); got != 0o500 {
		t.Fatalf("outside directory mode = %o, want 500", got)
	}
}

func TestRollbackUpgradeSnapshot_MalformedSnapshotFailsLoudly(t *testing.T) {
	root := t.TempDir()
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "bad.json"), []byte("{"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.6.0\n"), 0o600); err != nil {
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

func TestRollbackUpgradeSnapshot_RetriesPartialRestoreFailure(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs", "agent-layer")
	if err := os.MkdirAll(docsDir, 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	firstPath := filepath.Join(docsDir, "A.md")
	secondPath := filepath.Join(docsDir, "B.md")
	for path, content := range map[string]string{
		firstPath:  "new first\n",
		secondPath: "new second\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write current file %s: %v", path, err)
		}
	}

	perm := uint32(0o644)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "retry-partial-restore",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          "docs/agent-layer/A.md",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("old first\n")),
			},
			{
				Path:          "docs/agent-layer/B.md",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &perm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("old second\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	faults := &writeFailOnceSystem{
		base:     RealSystem{},
		failPath: secondPath,
		err:      errors.New("restore second file failed"),
	}
	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: faults}); err == nil {
		t.Fatal("expected first rollback attempt to fail")
	}
	firstRestored, err := os.ReadFile(firstPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read file restored before failure: %v", err)
	}
	if string(firstRestored) != "old first\n" {
		t.Fatalf("first file after partial restore = %q, want old snapshot content", firstRestored)
	}
	if _, err := os.Stat(secondPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second file should remain absent after restore failure, stat err = %v", err)
	}

	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), snapshot.SnapshotID+".json")
	failedSnapshot, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read failed snapshot: %v", err)
	}
	if failedSnapshot.Status != upgradeSnapshotStatusRollbackFailed {
		t.Fatalf("failed snapshot status = %q, want %q", failedSnapshot.Status, upgradeSnapshotStatusRollbackFailed)
	}
	if err := os.WriteFile(firstPath, []byte("corrupted after failure\n"), 0o600); err != nil {
		t.Fatalf("corrupt partially restored file: %v", err)
	}

	retryFaults := &writeFailOnceSystem{
		base:     RealSystem{},
		failPath: secondPath,
		err:      errors.New("retry restore failed"),
	}
	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: retryFaults}); err == nil {
		t.Fatal("expected second rollback attempt to fail")
	}
	retriedFailure, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read snapshot after failed retry: %v", err)
	}
	if retriedFailure.FailureStep != failedSnapshot.FailureStep || !strings.Contains(retriedFailure.FailureError, failedSnapshot.FailureError) || !strings.Contains(retriedFailure.FailureError, "retry restore failed") {
		t.Fatalf("failed retry discarded failure evidence: before=(%q, %q) after=(%q, %q)", failedSnapshot.FailureStep, failedSnapshot.FailureError, retriedFailure.FailureStep, retriedFailure.FailureError)
	}
	firstRestored, err = os.ReadFile(firstPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read first file after failed retry: %v", err)
	}
	if string(firstRestored) != "old first\n" {
		t.Fatalf("failed retry did not reset partially restored file: got %q", firstRestored)
	}

	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("retry rollback_failed snapshot: %v", err)
	}
	for path, want := range map[string]string{
		firstPath:  "old first\n",
		secondPath: "old second\n",
	} {
		got, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
		if err != nil {
			t.Fatalf("read restored file %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("restored file %s = %q, want %q", path, got, want)
		}
	}

	restoredSnapshot, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read retried snapshot: %v", err)
	}
	if restoredSnapshot.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("retried snapshot status = %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusManuallyRolledBack)
	}
	if restoredSnapshot.FailureStep != retriedFailure.FailureStep || restoredSnapshot.FailureError != retriedFailure.FailureError {
		t.Fatalf("successful retry discarded failure evidence: before=(%q, %q) after=(%q, %q)", retriedFailure.FailureStep, retriedFailure.FailureError, restoredSnapshot.FailureStep, restoredSnapshot.FailureError)
	}
}

func TestRollbackUpgradeSnapshot_RetriesDirectoryModeFailure(t *testing.T) {
	root := t.TempDir()
	dirPath := filepath.Join(root, "captured")
	childPath := filepath.Join(dirPath, "nested")
	filePath := filepath.Join(childPath, "original.txt")
	if err := os.MkdirAll(childPath, 0o700); err != nil {
		t.Fatalf("mkdir current directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("current\n"), 0o600); err != nil {
		t.Fatalf("write current file: %v", err)
	}

	dirPerm := uint32(0o500)
	childPerm := uint32(0o500)
	filePerm := uint32(0o400)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "retry-directory-mode",
		CreatedAtUTC:  time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{Path: "captured", Kind: upgradeSnapshotEntryKindDir, Perm: &dirPerm},
			{Path: "captured/nested", Kind: upgradeSnapshotEntryKindDir, Perm: &childPerm},
			{
				Path:          "captured/nested/original.txt",
				Kind:          upgradeSnapshotEntryKindFile,
				Perm:          &filePerm,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("original\n")),
			},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	modeFailure := errors.New("apply captured directory mode failed")
	faults := &chmodFailOnceSystem{
		System:   RealSystem{},
		failPath: dirPath,
		failMode: os.FileMode(dirPerm),
		err:      modeFailure,
	}
	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: faults}); err == nil || !strings.Contains(err.Error(), modeFailure.Error()) {
		t.Fatalf("first rollback error = %v, want directory mode failure", err)
	}

	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), snapshot.SnapshotID+".json")
	failedSnapshot, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read failed snapshot: %v", err)
	}
	if failedSnapshot.Status != upgradeSnapshotStatusRollbackFailed ||
		failedSnapshot.FailureStep != manualRollbackFailureStep ||
		!strings.Contains(failedSnapshot.FailureError, modeFailure.Error()) {
		t.Fatalf("failed snapshot evidence = (%q, %q, %q)", failedSnapshot.Status, failedSnapshot.FailureStep, failedSnapshot.FailureError)
	}

	if err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}); err != nil {
		t.Fatalf("retry rollback_failed snapshot: %v", err)
	}
	restoredSnapshot, err := readUpgradeSnapshot(snapshotPath, RealSystem{})
	if err != nil {
		t.Fatalf("read restored snapshot: %v", err)
	}
	if restoredSnapshot.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("retry status = %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusManuallyRolledBack)
	}
	if restoredSnapshot.FailureStep != failedSnapshot.FailureStep || restoredSnapshot.FailureError != failedSnapshot.FailureError {
		t.Fatalf("retry discarded original failure evidence: before=(%q, %q) after=(%q, %q)", failedSnapshot.FailureStep, failedSnapshot.FailureError, restoredSnapshot.FailureStep, restoredSnapshot.FailureError)
	}
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat restored directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != os.FileMode(dirPerm) {
		t.Fatalf("restored directory mode = %o, want %o", got, dirPerm)
	}
	childInfo, err := os.Stat(childPath)
	if err != nil {
		t.Fatalf("stat restored child directory: %v", err)
	}
	if got := childInfo.Mode().Perm(); got != os.FileMode(childPerm) {
		t.Fatalf("restored child directory mode = %o, want %o", got, childPerm)
	}
	content, err := os.ReadFile(filePath) // #nosec G304 -- path is test-controlled.
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(content) != "original\n" {
		t.Fatalf("restored file = %q, want original content", content)
	}
	if err := os.Chmod(dirPath, 0o700); err != nil { // #nosec G302 -- test restores owner write access for TempDir cleanup.
		t.Fatalf("make restored directory writable for cleanup: %v", err)
	}
	if err := os.Chmod(childPath, 0o700); err != nil { // #nosec G302 -- test restores owner write access for TempDir cleanup.
		t.Fatalf("make restored child directory writable for cleanup: %v", err)
	}
}

func TestRollbackUpgradeSnapshot_FailureAndStatusWriteFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.WriteFile(versionPath, []byte("0.6.0\n"), 0o600); err != nil {
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
	faults.writeErrs[normalizePath(snapshotPath)] = errors.New("snapshot write failed")

	err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: faults})
	if err == nil {
		t.Fatal("expected rollback error")
	}
	if !strings.Contains(err.Error(), "persist rollback targets before rollback") {
		t.Fatalf("unexpected error: %v", err)
	}
	content, readErr := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read version after scope write failure: %v", readErr)
	}
	if string(content) != "0.6.0\n" {
		t.Fatalf("version changed before rollback scope persisted: %q", content)
	}
}

func TestRollbackUpgradeSnapshot_RejectsRollbackFailedSnapshotWithoutPersistedTargets(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "state.txt")
	if err := os.WriteFile(targetPath, []byte("current\n"), 0o600); err != nil {
		t.Fatalf("write current target: %v", err)
	}
	perm := uint32(0o600)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "missing-retry-scope",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusRollbackFailed,
		FailureStep:   manualRollbackFailureStep,
		FailureError:  "prior rollback failed",
		Entries: []upgradeSnapshotEntry{{
			Path:          "state.txt",
			Kind:          upgradeSnapshotEntryKindFile,
			Perm:          &perm,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("captured\n")),
		}},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("write rollback_failed snapshot: %v", err)
	}

	err := RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), "missing persisted rollback targets") {
		t.Fatalf("missing retry scope error = %v", err)
	}
	content, readErr := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read target after rejected retry: %v", readErr)
	}
	if string(content) != "current\n" {
		t.Fatalf("target changed after rejected retry: %q", content)
	}
}

func TestRollbackUpgradeSnapshot_PersistsAutomaticTargetsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "state.txt")
	if err := os.WriteFile(targetPath, []byte("current\n"), 0o600); err != nil {
		t.Fatalf("write current target: %v", err)
	}
	perm := uint32(0o600)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "automatic-scope-write-fails",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusRollbackFailed,
		FailureStep:   "write-state",
		FailureError:  "upgrade failed",
		Entries: []upgradeSnapshotEntry{{
			Path:          "state.txt",
			Kind:          upgradeSnapshotEntryKindFile,
			Perm:          &perm,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("captured\n")),
		}},
	}
	snapshotPath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), snapshot.SnapshotID+".json")
	faults := newFaultSystem(RealSystem{})
	faults.writeErrs[normalizePath(snapshotPath)] = errors.New("persist scope failed")
	inst := &installer{root: root, sys: faults}

	err := inst.rollbackUpgradeSnapshot(&snapshot, []string{targetPath})
	if err == nil || !strings.Contains(err.Error(), "persist rollback targets before rollback") {
		t.Fatalf("automatic scope persistence error = %v", err)
	}
	if len(snapshot.RollbackTargets) != 1 || snapshot.RollbackTargets[0] != "state.txt" {
		t.Fatalf("automatic rollback targets = %v, want [state.txt]", snapshot.RollbackTargets)
	}
	content, readErr := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read target after scope persistence failure: %v", readErr)
	}
	if string(content) != "current\n" {
		t.Fatalf("target changed before automatic scope persisted: %q", content)
	}
}

func TestRollbackUpgradeSnapshot_RestoresSpecialPathEntries(t *testing.T) {
	root := t.TempDir()
	deepDir := filepath.Join(root, "docs", "agent-layer", "Deep Space")
	if err := os.MkdirAll(deepDir, 0o700); err != nil {
		t.Fatalf("mkdir deep dir: %v", err)
	}

	targetPath := filepath.Join(deepDir, "notes v1.md")
	if err := os.WriteFile(targetPath, []byte("new notes\n"), 0o600); err != nil {
		t.Fatalf("write current notes: %v", err)
	}
	extraPath := filepath.Join(deepDir, "extra.tmp")
	if err := os.WriteFile(extraPath, []byte("remove"), 0o600); err != nil {
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

	restored, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read restored notes: %v", err)
	}
	if string(restored) != "old notes\n" {
		t.Fatalf("restored notes = %q, want %q", string(restored), "old notes\n")
	}
	if _, err := os.Stat(extraPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected extra path removed, stat err = %v", err)
	}

	restoredSnapshot := latestSnapshot(t, root)
	if restoredSnapshot.Status != upgradeSnapshotStatusManuallyRolledBack {
		t.Fatalf("snapshot status mutated to %q, want %q", restoredSnapshot.Status, upgradeSnapshotStatusManuallyRolledBack)
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
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed snapshot: %v", err)
	}
	err := inst.writeUpgradeSnapshot(snapshot, true)
	if err != nil {
		t.Fatalf("expected prune to skip malformed snapshot, got error: %v", err)
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
	faults.lstatErrs[normalizePath(versionPath)] = errors.New("lstat failed")
	inst = &installer{root: root, sys: faults}
	if _, err := inst.createUpgradeSnapshot(); err == nil {
		t.Fatal("expected createUpgradeSnapshot error")
	}
}

func TestPruneUpgradeSnapshots_SkipsMalformedSnapshot(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	malformedPath := filepath.Join(snapshotDir, "bad.json")
	if err := os.WriteFile(malformedPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed snapshot: %v", err)
	}
	err := inst.pruneUpgradeSnapshots(1)
	if err != nil {
		t.Fatalf("expected prune to skip malformed snapshot, got: %v", err)
	}
}

func TestPruneUpgradeSnapshots_SkipsInvalidSnapshot(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
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
	if err := os.WriteFile(invalidPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write invalid snapshot: %v", err)
	}
	err := inst.pruneUpgradeSnapshots(1)
	if err != nil {
		t.Fatalf("expected prune to skip invalid snapshot, got: %v", err)
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

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("1.0.0\n"), 0o600); err != nil {
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

func TestCaptureUpgradeSnapshotTarget_CapturesSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	target := filepath.Join(root, ".agent-layer", "target.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	entries := make(map[string]upgradeSnapshotEntry)
	if err := inst.captureUpgradeSnapshotTarget(link, entries); err != nil {
		t.Fatalf("capture symlink target: %v", err)
	}

	entry, ok := entries[".agent-layer/al.version"]
	if !ok {
		t.Fatalf("expected symlink entry, got keys: %v", entries)
	}
	if entry.Kind != upgradeSnapshotEntryKindSymlink {
		t.Fatalf("entry kind = %q, want %q", entry.Kind, upgradeSnapshotEntryKindSymlink)
	}
	if entry.LinkTarget != target {
		t.Fatalf("link target = %q, want %q", entry.LinkTarget, target)
	}
	if entry.ContentBase64 != "" {
		t.Fatalf("expected empty content for symlink entry, got %q", entry.ContentBase64)
	}
}

func TestCaptureUpgradeSnapshotTarget_StatError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	faults := newFaultSystem(RealSystem{})
	faults.lstatErrs[normalizePath(path)] = errors.New("lstat failed")
	inst := &installer{root: root, sys: faults}

	err := inst.captureUpgradeSnapshotTarget(path, map[string]upgradeSnapshotEntry{})
	if err == nil || !strings.Contains(err.Error(), "failed to lstat") {
		t.Fatalf("expected lstat error, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotDirectory_CapturesSymlinkEntryWithoutFollowingTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "state", "links")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	target := filepath.Join(root, ".agent-layer", "target.bin")
	targetBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x01, 0x02}
	if err := os.WriteFile(target, targetBytes, 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	link := filepath.Join(dir, "icon.link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	entries := make(map[string]upgradeSnapshotEntry)
	if err := inst.captureUpgradeSnapshotDirectory(dir, entries); err != nil {
		t.Fatalf("captureUpgradeSnapshotDirectory: %v", err)
	}
	entry, ok := entries[".agent-layer/state/links/icon.link"]
	if !ok {
		t.Fatalf("expected symlink snapshot entry, got keys: %v", entries)
	}
	if entry.Kind != upgradeSnapshotEntryKindSymlink {
		t.Fatalf("entry kind = %q, want %q", entry.Kind, upgradeSnapshotEntryKindSymlink)
	}
	if entry.ContentBase64 != "" {
		t.Fatalf("expected no captured file content for symlink, got %q", entry.ContentBase64)
	}
	if entry.LinkTarget != target {
		t.Fatalf("link target = %q, want %q", entry.LinkTarget, target)
	}
}

func TestCaptureUpgradeSnapshotDirectory_CapturesExternalSymlinkWithoutReadingTarget(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "state", "links")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	target := filepath.Join(external, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write external target file: %v", err)
	}
	link := filepath.Join(dir, "outside.link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	entries := make(map[string]upgradeSnapshotEntry)
	if err := inst.captureUpgradeSnapshotDirectory(dir, entries); err != nil {
		t.Fatalf("captureUpgradeSnapshotDirectory: %v", err)
	}
	entry, ok := entries[".agent-layer/state/links/outside.link"]
	if !ok {
		t.Fatalf("expected external symlink snapshot entry, got keys: %v", entries)
	}
	if entry.Kind != upgradeSnapshotEntryKindSymlink {
		t.Fatalf("entry kind = %q, want %q", entry.Kind, upgradeSnapshotEntryKindSymlink)
	}
	if entry.ContentBase64 != "" {
		t.Fatalf("expected no content for symlink entry, got %q", entry.ContentBase64)
	}
	if entry.LinkTarget != target {
		t.Fatalf("link target = %q, want %q", entry.LinkTarget, target)
	}
}

func TestCaptureUpgradeSnapshotDirectory_CapturesSymlinkToDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "state", "links")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	targetDir := filepath.Join(root, ".agent-layer", "target-dir")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	link := filepath.Join(dir, "dir-link")
	if err := os.Symlink(targetDir, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	entries := map[string]upgradeSnapshotEntry{}
	if err := inst.captureUpgradeSnapshotDirectory(dir, entries); err != nil {
		t.Fatalf("captureUpgradeSnapshotDirectory: %v", err)
	}
	entry, ok := entries[".agent-layer/state/links/dir-link"]
	if !ok {
		t.Fatalf("expected symlink snapshot entry, got keys: %v", entries)
	}
	if entry.Kind != upgradeSnapshotEntryKindSymlink {
		t.Fatalf("entry kind = %q, want %q", entry.Kind, upgradeSnapshotEntryKindSymlink)
	}
	if entry.LinkTarget != targetDir {
		t.Fatalf("link target = %q, want %q", entry.LinkTarget, targetDir)
	}
}

func TestCaptureUpgradeSnapshotDirectory_SymlinkReadlinkError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "state", "links")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	target := filepath.Join(root, ".agent-layer", "target.bin")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	link := filepath.Join(dir, "broken.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	faults := newFaultSystem(RealSystem{})
	faults.readlinkErrs[normalizePath(link)] = errors.New("readlink failed")
	inst := &installer{root: root, sys: faults}
	err := inst.captureUpgradeSnapshotDirectory(dir, map[string]upgradeSnapshotEntry{})
	if err == nil || !strings.Contains(err.Error(), "failed to read symlink") {
		t.Fatalf("expected readlink failure, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotFile_ReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("1.0.0\n"), 0o600); err != nil {
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
			name: "missing created_at_utc",
			mutate: func(snapshot *upgradeSnapshot) {
				snapshot.CreatedAtUTC = " "
			},
			want: "created_at_utc is required",
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
			name: "absent has content",
			entry: upgradeSnapshotEntry{
				Path:          "docs/agent-layer/extra.md",
				Kind:          upgradeSnapshotEntryKindAbsent,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("x")),
			},
			want: "must not set content_base64",
		},
		{
			name: "file has link target",
			entry: upgradeSnapshotEntry{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("x")),
				LinkTarget:    ".agent-layer/target.txt",
			},
			want: "must not set link_target",
		},
		{
			name: "symlink missing target",
			entry: upgradeSnapshotEntry{
				Path: ".agent-layer/al.version",
				Kind: upgradeSnapshotEntryKindSymlink,
			},
			want: "requires link_target",
		},
		{
			name: "symlink has content",
			entry: upgradeSnapshotEntry{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindSymlink,
				LinkTarget:    ".agent-layer/target.txt",
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("x")),
			},
			want: "must not set content_base64",
		},
		{
			name: "symlink has perm",
			entry: upgradeSnapshotEntry{
				Path:       ".agent-layer/al.version",
				Kind:       upgradeSnapshotEntryKindSymlink,
				LinkTarget: ".agent-layer/target.txt",
				Perm:       &perm,
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

func TestCreateUpgradeSnapshot_WriteFailurePropagates(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	faults := newFaultSystem(RealSystem{})
	faults.mkdirErrs[normalizePath(snapshotDir)] = errors.New("mkdir failed")

	inst := &installer{root: root, sys: faults}
	if _, err := inst.createUpgradeSnapshot(); err == nil || !strings.Contains(err.Error(), "failed to create directory for") {
		t.Fatalf("expected snapshot write failure, got %v", err)
	}
}

func TestWriteUpgradeSnapshot_ValidationError(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	invalid := upgradeSnapshot{
		SchemaVersion: 0,
		SnapshotID:    "invalid",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
	}
	if err := inst.writeUpgradeSnapshot(invalid, false); err == nil || !strings.Contains(err.Error(), "validate upgrade snapshot") {
		t.Fatalf("expected validate error, got %v", err)
	}
}

func TestPruneUpgradeSnapshots_RemoveError(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	for idx := 0; idx < 2; idx++ {
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("remove-fail-%d", idx),
			CreatedAtUTC:  time.Date(2026, time.January, 1, 0, idx, 0, 0, time.UTC).Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot %d: %v", idx, err)
		}
	}

	removePath := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath), "remove-fail-0.json")
	faults := newFaultSystem(RealSystem{})
	faults.removeErrs[normalizePath(removePath)] = errors.New("remove failed")
	inst = &installer{root: root, sys: faults}
	if err := inst.pruneUpgradeSnapshots(1); err == nil || !strings.Contains(err.Error(), "delete old upgrade snapshot") {
		t.Fatalf("expected remove error, got %v", err)
	}
}

func TestCaptureUpgradeSnapshotTarget_AbsentRepoRelativeError(t *testing.T) {
	inst := &installer{root: "relative/root", sys: RealSystem{}}
	target := filepath.Join(t.TempDir(), "does-not-exist")
	err := inst.captureUpgradeSnapshotTarget(target, map[string]upgradeSnapshotEntry{})
	if err == nil {
		t.Fatal("expected repo-relative error for absolute target with relative root")
	}
}

func TestCaptureUpgradeSnapshotTarget_SpecialFileRepoRelativeError(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("mkfifo unsupported on windows")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "test.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	inst := &installer{root: "relative/root", sys: RealSystem{}}
	err := inst.captureUpgradeSnapshotTarget(fifoPath, map[string]upgradeSnapshotEntry{})
	if err == nil {
		t.Fatal("expected repo-relative error for special file")
	}
}

func TestCaptureUpgradeSnapshotDirectory_CallbackErrAndRepoRelativeErrors(t *testing.T) {
	t.Run("callback err", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: walkCallbackErrSystem{base: RealSystem{}}}
		err := inst.captureUpgradeSnapshotDirectory(inst.root, map[string]upgradeSnapshotEntry{})
		if err == nil || !strings.Contains(err.Error(), "walk callback boom") {
			t.Fatalf("expected callback error, got %v", err)
		}
	})

	t.Run("dir repo-relative error", func(t *testing.T) {
		rootPath := t.TempDir()
		inst := &installer{root: "relative/root", sys: RealSystem{}}
		err := inst.captureUpgradeSnapshotDirectory(rootPath, map[string]upgradeSnapshotEntry{})
		if err == nil {
			t.Fatal("expected repo-relative error for directory entry")
		}
	})

	t.Run("non-regular repo-relative error", func(t *testing.T) {
		if os.PathSeparator == '\\' {
			t.Skip("mkfifo unsupported on windows")
		}
		rootPath := t.TempDir()
		fifoPath := filepath.Join(rootPath, "item.fifo")
		if err := syscall.Mkfifo(fifoPath, 0o644); err != nil {
			t.Fatalf("mkfifo: %v", err)
		}
		inst := &installer{root: "relative/root", sys: RealSystem{}}
		err := inst.captureUpgradeSnapshotDirectory(rootPath, map[string]upgradeSnapshotEntry{})
		if err == nil {
			t.Fatal("expected repo-relative error for non-regular entry")
		}
	})
}

func TestCaptureUpgradeSnapshotFile_RepoRelativeError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	inst := &installer{root: "relative/root", sys: RealSystem{}}
	if err := inst.captureUpgradeSnapshotFile(path, 0o644, map[string]upgradeSnapshotEntry{}); err == nil {
		t.Fatal("expected repo-relative error")
	}
}

func TestUniqueNormalizedPaths_SkipsDotAndEmpty(t *testing.T) {
	got := uniqueNormalizedPaths([]string{"", ".", "a", "a/.", "b", "./b"})
	if len(got) != 2 {
		t.Fatalf("expected 2 unique paths, got %d (%v)", len(got), got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected normalized paths: %v", got)
	}
}

func TestRepoRelativePath_RelError(t *testing.T) {
	inst := &installer{root: string([]byte{0}), sys: RealSystem{}}
	if _, err := inst.repoRelativePath("/tmp/file"); err == nil {
		t.Fatal("expected filepath.Rel error")
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

type chmodFailOnceSystem struct {
	System
	failPath string
	failMode os.FileMode
	err      error
	fired    bool
}

type swapDirectoryForSymlinkAfterChmodSystem struct {
	System
	path    string
	target  string
	swapped bool
}

type requireWritableDirectoriesBeforeRemoveSystem struct {
	System
	dirs []string
}

func (s *chmodFailOnceSystem) Chmod(name string, mode os.FileMode) error {
	if !s.fired && normalizePath(name) == normalizePath(s.failPath) && mode.Perm() == s.failMode.Perm() {
		s.fired = true
		return s.err
	}
	return s.System.Chmod(name, mode)
}

func (s *swapDirectoryForSymlinkAfterChmodSystem) Chmod(name string, mode os.FileMode) error {
	if err := s.System.Chmod(name, mode); err != nil {
		return err
	}
	if s.swapped || normalizePath(name) != normalizePath(s.path) {
		return nil
	}
	if err := s.RemoveAll(name); err != nil {
		return err
	}
	if err := s.Symlink(s.target, name); err != nil {
		return err
	}
	s.swapped = true
	return nil
}

func (s *requireWritableDirectoriesBeforeRemoveSystem) RemoveAll(path string) error {
	for _, dir := range s.dirs {
		info, err := s.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o700 != 0o700 {
			return fmt.Errorf("directory %s was not prepared before reset", dir)
		}
	}
	return s.System.RemoveAll(path)
}

func (s *writeFailOnceSystem) Chmod(name string, mode os.FileMode) error {
	return s.base.Chmod(name, mode)
}

func (s *writeFailOnceSystem) EvalSymlinks(path string) (string, error) {
	return s.base.EvalSymlinks(path)
}

func (s *writeFailOnceSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *writeFailOnceSystem) Lstat(name string) (os.FileInfo, error) {
	return s.base.Lstat(name)
}

func (s *writeFailOnceSystem) ReadFile(name string) ([]byte, error) {
	return s.base.ReadFile(name)
}

func (s *writeFailOnceSystem) Readlink(name string) (string, error) {
	return s.base.Readlink(name)
}

func (s *writeFailOnceSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
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

func (s *writeFailOnceSystem) Symlink(oldname string, newname string) error {
	return s.base.Symlink(oldname, newname)
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

type walkCallbackErrSystem struct {
	base System
}

func (s walkCallbackErrSystem) Chmod(name string, mode os.FileMode) error {
	return s.base.Chmod(name, mode)
}

func (s walkCallbackErrSystem) EvalSymlinks(path string) (string, error) {
	return s.base.EvalSymlinks(path)
}

func (s walkCallbackErrSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s walkCallbackErrSystem) Lstat(name string) (os.FileInfo, error) {
	return s.base.Lstat(name)
}

func (s walkCallbackErrSystem) ReadFile(name string) ([]byte, error) {
	return s.base.ReadFile(name)
}

func (s walkCallbackErrSystem) Readlink(name string) (string, error) {
	return s.base.Readlink(name)
}

func (s walkCallbackErrSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
}

func (s walkCallbackErrSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s walkCallbackErrSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s walkCallbackErrSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
}

func (s walkCallbackErrSystem) Symlink(oldname string, newname string) error {
	return s.base.Symlink(oldname, newname)
}

func (s walkCallbackErrSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return fn(root, nil, errors.New("walk callback boom"))
}

func (s walkCallbackErrSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return s.base.WriteFileAtomic(filename, data, perm)
}

func TestValidateUpgradeSnapshotEntry_EmptyFile(t *testing.T) {
	perm := uint32(0o644)
	entry := upgradeSnapshotEntry{
		Path:          ".agent-layer/tmp/empty.txt",
		Kind:          upgradeSnapshotEntryKindFile,
		Perm:          &perm,
		ContentBase64: "",
	}
	err := validateUpgradeSnapshotEntry(entry)
	if err != nil {
		t.Fatalf("expected nil error for empty file snapshot entry, got: %v", err)
	}
}

func TestValidateUpgradeSnapshotEntry_Symlink(t *testing.T) {
	entry := upgradeSnapshotEntry{
		Path:       ".agent-layer/al.version",
		Kind:       upgradeSnapshotEntryKindSymlink,
		LinkTarget: ".agent-layer/target.txt",
	}
	if err := validateUpgradeSnapshotEntry(entry); err != nil {
		t.Fatalf("expected nil error for symlink snapshot entry, got: %v", err)
	}
}

func TestListUpgradeSnapshots(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	// No snapshots yet.
	snapshots, err := ListUpgradeSnapshots(root, RealSystem{})
	if err != nil {
		t.Fatalf("ListUpgradeSnapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snapshots))
	}

	// Create some snapshots.
	for idx := 0; idx < 3; idx++ {
		now := time.Date(2026, time.January, 1, 0, idx, 0, 0, time.UTC)
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    fmt.Sprintf("snapshot-%d", idx),
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
		}
		if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
			t.Fatalf("write snapshot %d: %v", idx, err)
		}
	}

	snapshots, err = ListUpgradeSnapshots(root, RealSystem{})
	if err != nil {
		t.Fatalf("ListUpgradeSnapshots: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(snapshots))
	}

	// Order should be newest first.
	if snapshots[0].ID != "snapshot-2" || snapshots[1].ID != "snapshot-1" || snapshots[2].ID != "snapshot-0" {
		t.Fatalf("unexpected order: %v, %v, %v", snapshots[0].ID, snapshots[1].ID, snapshots[2].ID)
	}

	// Unreadable snapshot should be skipped.
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.WriteFile(filepath.Join(snapshotDir, "unreadable.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write unreadable snapshot: %v", err)
	}
	snapshots, err = ListUpgradeSnapshots(root, RealSystem{})
	if err != nil {
		t.Fatalf("ListUpgradeSnapshots: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("expected 3 snapshots (unreadable skipped), got %d", len(snapshots))
	}
}

func TestWriteUpgradeSnapshot_SizeWarning(t *testing.T) {
	root := t.TempDir()
	var warn bytes.Buffer
	inst := &installer{
		root:       root,
		sys:        RealSystem{},
		warnWriter: &warn,
	}

	originalThreshold := upgradeSnapshotSizeWarningBytes
	upgradeSnapshotSizeWarningBytes = 1024
	t.Cleanup(func() {
		upgradeSnapshotSizeWarningBytes = originalThreshold
	})

	largeContent := make([]byte, 2048)
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "large-snapshot",
		CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          "large-file.bin",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString(largeContent),
			},
		},
	}

	if err := inst.writeUpgradeSnapshot(snapshot, false); err != nil {
		t.Fatalf("writeUpgradeSnapshot: %v", err)
	}

	if !strings.Contains(warn.String(), "is large") {
		t.Fatalf("expected size warning, got %q", warn.String())
	}
}

func TestUpgradeSnapshotTargetPaths_ExcludesAgentLayerTmp(t *testing.T) {
	// `.agent-layer/tmp/` accumulates ephemeral agent run artifacts that may
	// total hundreds of MB. Snapshot capture must skip them so snapshots stay
	// small and `al upgrade rollback` does not attempt to restore stale
	// in-progress agent work.
	root := t.TempDir()
	tmpDir := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	tmpFile := filepath.Join(tmpDir, "report.md")
	if err := os.WriteFile(tmpFile, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}
	otherUnknown := filepath.Join(root, ".agent-layer", "stray.txt")
	if err := os.WriteFile(otherUnknown, []byte("x"), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	inst := &installer{
		root:     root,
		sys:      RealSystem{},
		unknowns: []string{tmpDir, tmpFile, otherUnknown},
	}
	paths := inst.upgradeSnapshotTargetPaths()
	for _, p := range paths {
		if p == tmpDir || p == tmpFile {
			t.Fatalf("upgradeSnapshotTargetPaths must not include tmp paths, got %q in %v", p, paths)
		}
	}
	foundOther := false
	for _, p := range paths {
		if p == filepath.Clean(otherUnknown) {
			foundOther = true
			break
		}
	}
	if !foundOther {
		t.Fatalf("upgradeSnapshotTargetPaths must still include non-tmp unknowns, got %v", paths)
	}
}

func TestCaptureUpgradeSnapshotDirectory_DefensivelySkipsTmpDescendants(t *testing.T) {
	// Belt-and-braces: even if a caller passes a target whose subtree happens
	// to contain `.agent-layer/tmp/`, the recursive walk must skip the tmp
	// subtree rather than base64-encoding ephemeral run artifacts.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "tmp", "runs"), 0o700); err != nil {
		t.Fatalf("mkdir tmp/runs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "tmp", "runs", "huge.log"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}
	keepFile := filepath.Join(root, ".agent-layer", "templates", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(keepFile), 0o700); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(keepFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write keep: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	entries := make(map[string]upgradeSnapshotEntry)
	if err := inst.captureUpgradeSnapshotDirectory(filepath.Join(root, ".agent-layer"), entries); err != nil {
		t.Fatalf("captureUpgradeSnapshotDirectory: %v", err)
	}
	for path := range entries {
		if path == ".agent-layer/tmp" || strings.HasPrefix(path, ".agent-layer/tmp/") {
			t.Fatalf("snapshot must not contain tmp path %q (entries: %v)", path, entries)
		}
	}
	if _, ok := entries[".agent-layer/templates/keep.txt"]; !ok {
		t.Fatalf("expected non-tmp file to be captured, got entries %v", entries)
	}
}

func TestHandleUnknownsTargetPaths_ExcludesAgentLayerTmp(t *testing.T) {
	// Rollback safety invariant: tmp paths are excluded from snapshots, so
	// they must also be excluded from rollback targets. Otherwise an
	// automatic rollback (e.g., a failure during handleUnknowns) would
	// `RemoveAll` tmp paths during the reset phase with no snapshot entry to
	// restore, silently wiping tmp content the user never confirmed.
	root := t.TempDir()
	tmpFile := filepath.Join(root, ".agent-layer", "tmp", "report.md")
	otherUnknown := filepath.Join(root, ".agent-layer", "stray.txt")
	inst := &installer{
		root:     root,
		unknowns: []string{tmpFile, otherUnknown},
	}
	targets := inst.handleUnknownsTargetPaths()
	for _, p := range targets {
		if p == tmpFile {
			t.Fatalf("handleUnknownsTargetPaths must not include tmp paths, got %q in %v", p, targets)
		}
	}
	foundOther := false
	for _, p := range targets {
		if p == filepath.Clean(otherUnknown) {
			foundOther = true
			break
		}
	}
	if !foundOther {
		t.Fatalf("handleUnknownsTargetPaths must still include non-tmp unknowns, got %v", targets)
	}
}

func TestIsUnderAgentLayerTmp(t *testing.T) {
	root := filepath.Join(string(os.PathSeparator), "repo")
	inst := &installer{root: root}
	tmp := filepath.Join(root, ".agent-layer", "tmp")
	tests := []struct {
		path string
		want bool
	}{
		{tmp, true},
		{filepath.Join(tmp, "file.md"), true},
		{filepath.Join(tmp, "runs", "deep", "report.md"), true},
		{filepath.Join(root, ".agent-layer", "tmp-other"), false},
		{filepath.Join(root, ".agent-layer", "templates"), false},
		{filepath.Join(root, "tmp"), false},
		{root, false},
	}
	for _, tc := range tests {
		if got := inst.isUnderAgentLayerTmp(tc.path); got != tc.want {
			t.Errorf("isUnderAgentLayerTmp(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
