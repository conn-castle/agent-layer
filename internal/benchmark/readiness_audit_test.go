package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadinessAuditRemovesOnlyAfterTheSuccessorImageIsReady(t *testing.T) {
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installAuditCheckout(t, checkout)
	var events []string
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
		func(_ context.Context, arguments ...string) ([]byte, error) {
			switch {
			case len(arguments) > 2 && arguments[0] == commandRun:
				events = append(events, "run:"+arguments[len(arguments)-2])
			case len(arguments) > 3 && arguments[0] == "image" && arguments[1] == "rm":
				events = append(events, "remove:"+arguments[3])
			default:
				t.Fatalf("unexpected Docker readiness command: %#v", arguments)
			}
			return nil, nil
		})

	outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
		RepoRoot: repository, TaskConcurrency: 1, RemoveTaskImages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Certified != 2 || outcome.Failed != 0 || outcome.Blocked != 0 {
		t.Fatalf("audit outcome = %#v", outcome)
	}
	want := strings.Join([]string{
		"run:" + auditTaskImage("first-task") + "@" + testReadinessDigest,
		"run:" + auditTaskImage("second-task") + "@" + testReadinessDigest,
		"remove:" + auditTaskImage("first-task") + "@" + testReadinessDigest,
		"remove:" + auditTaskImage("second-task") + "@" + testReadinessDigest,
	}, "|")
	if got := strings.Join(events, "|"); got != want {
		t.Fatalf("bounded-disk Docker events = %q, want %q", got, want)
	}
}

func TestReadinessAuditImageRemovalFailsLoudly(t *testing.T) {
	installReadinessTestBoundaries(t, auditContractsFixture(),
		func(context.Context, ...string) ([]byte, error) {
			return []byte("permission denied"), errors.New("exit status 1")
		})
	err := removeTaskReadinessImages(context.Background(), loadedTaskReadiness{pinnedImage: "registry.example/task@sha256:" + strings.Repeat("1", 64)})
	if err == nil || !strings.Contains(err.Error(), "remove exact task images") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("image removal error = %v", err)
	}
}

func TestListPinnedBenchmarkTasksAllowsCatalogMetadata(t *testing.T) {
	root := t.TempDir()
	tasksRoot := filepath.Join(root, "tasks")
	if err := os.MkdirAll(filepath.Join(tasksRoot, "first-task"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.md", "dataset.toml", "manifest.json", "manifest.schema.json"} {
		contents := []byte("metadata")
		if name == "manifest.json" {
			contents = []byte(`{"task_count":1,"tasks":[{"task_id":"first-task"}]}`)
		}
		if err := os.WriteFile(filepath.Join(tasksRoot, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := listPinnedBenchmarkTasks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "first-task" {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestListPinnedBenchmarkTasksRejectsUnexpectedCatalogEntries(t *testing.T) {
	root := t.TempDir()
	tasksRoot := filepath.Join(root, "tasks")
	if err := os.MkdirAll(tasksRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksRoot, "unexpected.txt"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := listPinnedBenchmarkTasks(root); err == nil || !strings.Contains(err.Error(), "unexpected.txt") {
		t.Fatalf("unexpected catalog error = %v", err)
	}
}

func TestIsReadinessInfrastructureFailure(t *testing.T) {
	for _, message := range []string{
		"docker: no space left on device",
		"registry response: toomanyrequests",
		"registry response: rate exceeded",
	} {
		if !isReadinessInfrastructureFailure(assertionError(message)) {
			t.Fatalf("message %q was not classified as infrastructure failure", message)
		}
	}
	if isReadinessInfrastructureFailure(assertionError("missing required command: node")) {
		t.Fatal("task prerequisite failure was classified as infrastructure failure")
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
