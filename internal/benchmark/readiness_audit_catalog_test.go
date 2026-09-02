package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// writeAuditCatalogFixture builds a pinned DeepSWE checkout containing the
// named tasks plus the catalog manifest the audit cross-checks directories
// against.
func writeAuditCatalogFixture(t *testing.T, checkout string, tasks ...string) {
	t.Helper()
	root := filepath.Join(checkout, "tasks")
	manifest := struct {
		TaskCount int `json:"task_count"`
		Tasks     []struct {
			ID string `json:"task_id"`
		} `json:"tasks"`
	}{TaskCount: len(tasks)}
	for _, task := range tasks {
		writeReadinessTaskFixture(t, checkout, task, auditTaskImage(task), "#!/bin/bash\nrun-tests\n")
		for _, name := range []string{taskInstructionFile, taskPreArtifactsFile} {
			if err := os.WriteFile(filepath.Join(root, task, name), []byte(name+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		manifest.Tasks = append(manifest.Tasks, struct {
			ID string `json:"task_id"`
		}{ID: task})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskCatalogManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func auditTaskImage(task string) string {
	return "registry.example/" + task + ":v1"
}

func auditContractsFixture(tasks ...string) fs.FS {
	contracts := fstest.MapFS{}
	for _, task := range tasks {
		root := "readiness/" + DeepSWECommit + "/" + task + "/"
		contract := fmt.Sprintf(
			`{"schema":%q,"task":%q,"image":%q,"image_digest":%q,"check":"check.sh"}`,
			readinessContractSchema, task, auditTaskImage(task), testReadinessDigest,
		)
		contracts[root+"contract.json"] = &fstest.MapFile{Data: []byte(contract)}
		contracts[root+"check.sh"] = &fstest.MapFile{Data: []byte("#!/bin/bash\ncheck-tools\n")}
	}
	return contracts
}

func TestVersionPinnedAptOverlaysUseImmutableSnapshots(t *testing.T) {
	root := "readiness/" + DeepSWECommit
	var pinnedOverlays int
	err := fs.WalkDir(readinessContracts, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "agent.Dockerfile" {
			return nil
		}
		data, err := fs.ReadFile(readinessContracts, path)
		if err != nil {
			return err
		}
		contents := string(data)
		for _, line := range strings.Split(contents, "\n") {
			if !strings.Contains(line, "apt-get install") || !strings.Contains(line, "=") {
				continue
			}
			pinnedOverlays++
			for _, required := range []string{
				"snapshot.debian.org/archive/debian/",
				"Acquire::Check-Valid-Until",
				"rm -f /etc/apt/sources.list.d/",
			} {
				if !strings.Contains(contents, required) {
					t.Errorf("version-pinned apt overlay %s does not contain %q", path, required)
				}
			}
			break
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pinnedOverlays == 0 {
		t.Fatal("pinned catalog contains no version-pinned apt overlay to audit")
	}
}

// installAuditCheckout points the audit at a fixture checkout instead of
// cloning the pinned DeepSWE repository.
func installAuditCheckout(t *testing.T, checkout string) {
	t.Helper()
	original := ensurePinnedBenchmarkCheckout
	ensurePinnedBenchmarkCheckout = func(context.Context, string) (string, error) { return checkout, nil }
	t.Cleanup(func() { ensurePinnedBenchmarkCheckout = original })
}

func TestReadinessAuditCertifiesEveryTaskInThePinnedCatalog(t *testing.T) {
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installAuditCheckout(t, checkout)
	var readinessRuns int
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
		func(context.Context, ...string) ([]byte, error) {
			readinessRuns++
			return nil, nil
		})

	outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
		RepoRoot: repository, TaskConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Required != 2 || outcome.Certified != 2 ||
		outcome.Failed != 0 || outcome.Blocked != 0 || outcome.Validated != 0 {
		t.Fatalf("audit outcome = %#v", outcome)
	}
	if outcome.DeepSWECommit != DeepSWECommit || outcome.Schema != readinessAuditSchema {
		t.Fatalf("audit provenance = %#v", outcome)
	}
	// Certification is what proves the environment starts with the network
	// disabled, so every task must actually have run its readiness program.
	if readinessRuns != 2 {
		t.Fatalf("readiness program ran %d times, want 2", readinessRuns)
	}
	// Each task is reported individually so an operator can act on the exact
	// task that is not ready.
	for _, task := range outcome.Tasks {
		if task.Status != readinessStatusCertified || task.Error != "" {
			t.Fatalf("task report = %#v", task)
		}
	}
}

func TestReadinessAuditReportsStaticFailuresBeforeAnyDockerWork(t *testing.T) {
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	if err := os.Remove(filepath.Join(checkout, "tasks", "second-task", taskInstructionFile)); err != nil {
		t.Fatal(err)
	}
	installAuditCheckout(t, checkout)
	var readinessRuns int
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
		func(context.Context, ...string) ([]byte, error) {
			readinessRuns++
			return nil, nil
		})

	outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
		RepoRoot: repository, TaskConcurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A catalog that cannot even be read statically is a defect in the pinned
	// checkout. Pulling and running images for the remaining tasks would spend
	// minutes of Docker work to reach the same conclusion.
	if readinessRuns != 0 {
		t.Fatalf("static failure started %d Docker readiness runs", readinessRuns)
	}
	if outcome.Failed != 1 || outcome.Validated != 1 || outcome.Certified != 0 {
		t.Fatalf("audit outcome = %#v", outcome)
	}
	for _, task := range outcome.Tasks {
		if task.Task == "second-task" &&
			(task.Status != readinessStatusFailed || !strings.Contains(task.Error, taskInstructionFile)) {
			t.Fatalf("failed task report = %#v", task)
		}
	}
}

func TestReadinessAuditSeparatesTaskDefectsFromSharedInfrastructureFailures(t *testing.T) {
	t.Run("task defect fails only that task", func(t *testing.T) {
		repository, checkout := t.TempDir(), t.TempDir()
		writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
		installAuditCheckout(t, checkout)
		installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
			func(_ context.Context, arguments ...string) ([]byte, error) {
				if strings.Contains(strings.Join(arguments, " "), auditTaskImage("first-task")) {
					return []byte("missing required command: node"), errors.New("exit status 127")
				}
				return nil, nil
			})

		outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
			RepoRoot: repository, TaskConcurrency: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		// One unusable task must not disqualify the rest of the catalog: the
		// operator needs the full list of usable tasks to plan a campaign.
		if outcome.Failed != 1 || outcome.Certified != 1 || outcome.Blocked != 0 {
			t.Fatalf("audit outcome = %#v", outcome)
		}
	})

	t.Run("infrastructure failure blocks the remaining tasks", func(t *testing.T) {
		repository, checkout := t.TempDir(), t.TempDir()
		writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
		installAuditCheckout(t, checkout)
		runs := 0
		installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
			func(ctx context.Context, _ ...string) ([]byte, error) {
				if err := ctx.Err(); err != nil {
					return nil, fmt.Errorf("docker run interrupted: %w", err)
				}
				runs++
				if runs == 1 {
					return []byte("write /var/lib/docker: no space left on device"), errors.New("exit status 1")
				}
				return nil, nil
			})

		outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
			RepoRoot: repository, TaskConcurrency: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		// A full disk says nothing about the tasks. Reporting the remainder as
		// failed would send the operator hunting for task defects instead of
		// freeing space and re-running the audit.
		if outcome.Blocked != 2 || outcome.Failed != 0 || outcome.Certified != 0 {
			t.Fatalf("audit outcome = %#v", outcome)
		}
		for _, task := range outcome.Tasks {
			if task.Status != readinessStatusBlocked || task.Error == "" {
				t.Fatalf("blocked task report = %#v", task)
			}
		}
	})
}

func TestReadinessAuditRejectsUnusableOptions(t *testing.T) {
	for _, test := range []struct {
		name    string
		options ReadinessAuditOptions
		wanted  string
	}{
		{"no repository", ReadinessAuditOptions{TaskConcurrency: 1}, "requires a repository root"},
		{"no workers", ReadinessAuditOptions{RepoRoot: "repo"}, "task concurrency must be from 1 to 8"},
		{"too many workers", ReadinessAuditOptions{RepoRoot: "repo", TaskConcurrency: 9}, "task concurrency must be from 1 to 8"},
		{"image removal with parallel workers", ReadinessAuditOptions{RepoRoot: "repo", TaskConcurrency: 2, RemoveTaskImages: true}, "image removal requires task concurrency 1"},
		{"negative timeout", ReadinessAuditOptions{RepoRoot: "repo", TaskConcurrency: 1, TaskTimeout: -1}, "timeout cannot be negative"},
		{"shard count without index", ReadinessAuditOptions{RepoRoot: "repo", TaskConcurrency: 1, TaskShardCount: 2}, "shard index"},
		{"shard index without count", ReadinessAuditOptions{RepoRoot: "repo", TaskConcurrency: 1, TaskShardIndex: 1}, "shard count"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CheckAllTaskReadiness(context.Background(), test.options); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}
}
