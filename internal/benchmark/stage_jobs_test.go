package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func unreadableStageJobDir(t *testing.T, stage, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("directory-read permission fixture requires Unix permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read directories with mode 000")
	}
	dir := filepath.Join(stage, "jobs", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { // #nosec G302 -- restores the traversal bit on this test-owned directory so cleanup can remove it.
			t.Errorf("restore test-owned directory permissions: %v", err)
		}
	})
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(dir); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unreadable directory fixture returned %v, want permission error", err)
	}
	return dir
}

func writeStageJobFile(t *testing.T, stage string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{stage, "jobs"}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStageJobFilesMatchesFilesInWalkOrder(t *testing.T) {
	stage := t.TempDir()
	second := writeStageJobFile(t, stage, "trial-b", benchmarkArtifactsDir, benchmarkModelPatchFile)
	first := writeStageJobFile(t, stage, "trial-a", benchmarkArtifactsDir, benchmarkModelPatchFile)
	writeStageJobFile(t, stage, "trial-a", benchmarkAgentDir, benchmarkModelPatchFile)
	if err := os.MkdirAll(filepath.Join(stage, "jobs", "trial-c", benchmarkArtifactsDir, benchmarkModelPatchFile), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := stageJobFiles(stage, false, isSubmittedModelPatch)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{first, second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stageJobFiles() = %v, want %v", got, want)
	}
}

func TestStageJobFilesMissingJobsRoot(t *testing.T) {
	stage := t.TempDir()
	matchAll := func(string, fs.DirEntry) bool { return true }

	if _, err := stageJobFiles(stage, false, matchAll); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("strict walk error = %v, want os.ErrNotExist", err)
	}
	got, err := stageJobFiles(stage, true, matchAll)
	if err != nil || len(got) != 0 {
		t.Fatalf("tolerant walk = %v, %v; want no paths and no error", got, err)
	}
}

func TestRetainedProviderEvidenceRequiresOneModelPatch(t *testing.T) {
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := retainedProviderEvidence(stage, ExecutionRequest{}); err == nil ||
		!strings.Contains(err.Error(), "has no submitted model.patch") {
		t.Fatalf("no patch error = %v", err)
	}
	writeStageJobFile(t, stage, "trial-a", benchmarkArtifactsDir, benchmarkModelPatchFile)
	writeStageJobFile(t, stage, "trial-b", benchmarkArtifactsDir, benchmarkModelPatchFile)
	if _, _, _, err := retainedProviderEvidence(stage, ExecutionRequest{}); err == nil ||
		!strings.Contains(err.Error(), "has multiple model.patch files") {
		t.Fatalf("multiple patch error = %v", err)
	}
}

func TestLocateProviderCheckpointToleratesMissingJobsAndRejectsDuplicates(t *testing.T) {
	stage := t.TempDir()
	if _, found, err := locateProviderCheckpoint(stage); err != nil || found {
		t.Fatalf("missing jobs root = found %v, error %v; want not found and no error", found, err)
	}
	writeStageJobFile(t, stage, "trial-a", benchmarkAgentDir, providerCheckpointFile)
	writeStageJobFile(t, stage, "trial-b", benchmarkAgentDir, providerCheckpointFile)
	if _, _, err := locateProviderCheckpoint(stage); err == nil ||
		!strings.Contains(err.Error(), "has 2 provider completion checkpoints") {
		t.Fatalf("duplicate checkpoint error = %v", err)
	}
}

func TestPreserveReplayPatchIgnoresSymlinkedModelPatch(t *testing.T) {
	stage := t.TempDir()
	outside := filepath.Join(t.TempDir(), benchmarkModelPatchFile)
	if err := os.WriteFile(outside, []byte("diff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(stage, "jobs", "trial-a", benchmarkArtifactsDir, benchmarkModelPatchFile)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if err := preserveReplayPatch(stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(stage, replayInputDir, benchmarkModelPatchFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlinked model.patch was preserved: %v", err)
	}
}

func TestRetainedProviderEvidenceStopsAtFirstError(t *testing.T) {
	for _, traversalFirst := range []bool{false, true} {
		name := "duplicate before traversal error"
		if traversalFirst {
			name = "traversal error before duplicate"
		}
		t.Run(name, func(t *testing.T) {
			stage := t.TempDir()
			writeStageJobFile(t, stage, "a", benchmarkArtifactsDir, benchmarkModelPatchFile)
			second, unreadable := "b", "z"
			if traversalFirst {
				second, unreadable = "c", "b"
			}
			writeStageJobFile(t, stage, second, benchmarkArtifactsDir, benchmarkModelPatchFile)
			dir := unreadableStageJobDir(t, stage, unreadable)

			patch, agentDir, original, err := retainedProviderEvidence(stage, ExecutionRequest{})
			if patch != "" || agentDir != "" || original != nil {
				t.Fatalf("failure returned patch %q, agentDir %q, original %#v", patch, agentDir, original)
			}
			if traversalFirst {
				var pathErr *fs.PathError
				if !errors.As(err, &pathErr) || pathErr.Path != dir || !errors.Is(err, os.ErrPermission) {
					t.Fatalf("error = %v, want directory-read error for %s", err, dir)
				}
			} else if err == nil || err.Error() != "retained execution has multiple model.patch files" {
				t.Fatalf("error = %v, want exact duplicate patch error", err)
			}
		})
	}
}

func TestDispatchProviderSessionsPreservesPartialMapAndFirstError(t *testing.T) {
	for _, failure := range []string{"parse", "read", "traversal"} {
		t.Run(failure, func(t *testing.T) {
			stage := t.TempDir()
			dir := unreadableStageJobDir(t, stage, "z")
			valid := writeStageJobFile(t, stage, "a", dispatchEvidenceDir, "record.json")
			if err := os.WriteFile(valid, []byte(`{"provider_session_id":"session-a","state":"completed","terminal_reason":"success"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			var failedRecord string
			switch failure {
			case "parse":
				failedRecord = writeStageJobFile(t, stage, "b", dispatchEvidenceDir, "record.json")
				if err := os.WriteFile(failedRecord, []byte(`{"provider_session_id":`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "read":
				failedRecord = filepath.Join(stage, "jobs", "b", dispatchEvidenceDir, "record.json")
				if err := os.MkdirAll(filepath.Dir(failedRecord), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(stage, "nonexistent-target"), failedRecord); err != nil {
					t.Fatal(err)
				}
			}
			sessions, err := dispatchProviderSessions(stage)
			want := map[string][]dispatchProviderSession{
				"session-a": {{state: "completed", terminalReason: "success"}},
			}
			if sessions == nil || !reflect.DeepEqual(sessions, want) {
				t.Fatalf("sessions = %#v, want %#v alongside error %v", sessions, want, err)
			}
			if failure == "parse" {
				var syntaxErr *json.SyntaxError
				if !errors.As(err, &syntaxErr) {
					t.Fatalf("error = %v, want JSON syntax error before later traversal error", err)
				}
				return
			}
			var pathErr *fs.PathError
			wantPath, wantCause := dir, os.ErrPermission
			if failure == "read" {
				wantPath, wantCause = failedRecord, os.ErrNotExist
			}
			if !errors.As(err, &pathErr) || pathErr.Path != wantPath || !errors.Is(err, wantCause) {
				t.Fatalf("error = %v, want path error for %s matching %v", err, wantPath, wantCause)
			}
		})
	}
}

func TestDispatchProviderSessionsMissingJobsRoot(t *testing.T) {
	sessions, err := dispatchProviderSessions(t.TempDir())
	if sessions == nil || len(sessions) != 0 || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing jobs root returned %#v, %v; want non-nil empty map and os.ErrNotExist", sessions, err)
	}
}

func TestWalkStageJobFilesVanishedEntry(t *testing.T) {
	for _, ignoreMissing := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignoreMissing=%t", ignoreMissing), func(t *testing.T) {
			stage := t.TempDir()
			first := writeStageJobFile(t, stage, "a", "file")
			vanishing := filepath.Join(stage, "jobs", "b")
			if err := os.Mkdir(vanishing, 0o700); err != nil {
				t.Fatal(err)
			}
			visited := false
			err := walkStageJobFiles(stage, ignoreMissing, func(path string, _ fs.DirEntry) error {
				if path != first {
					t.Fatalf("unexpected visit: %s", path)
				}
				visited = true
				return os.Remove(vanishing)
			})
			if !visited {
				t.Fatal("visitor did not remove the already-listed directory")
			}
			if ignoreMissing {
				if err != nil {
					t.Fatalf("tolerant walk returned %v", err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("strict walk returned %v, want os.ErrNotExist", err)
			}
		})
	}
}

func TestWalkStageJobFilesReturnsVisitErrorUnchanged(t *testing.T) {
	stage := t.TempDir()
	first := writeStageJobFile(t, stage, "a", "file")
	writeStageJobFile(t, stage, "b", "file")
	visitErr := fmt.Errorf("visit failed: %w", os.ErrNotExist)
	var visited []string
	err := walkStageJobFiles(stage, true, func(path string, _ fs.DirEntry) error {
		visited = append(visited, path)
		return visitErr
	})
	if err != visitErr || !reflect.DeepEqual(visited, []string{first}) {
		t.Fatalf("walk returned %v after visiting %v, want unchanged visit error and only %s", err, visited, first)
	}
}
