package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadinessAuditRemovesOnlyAfterTheSuccessorImageIsReady(t *testing.T) {
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task", "third-task")
	installAuditCheckout(t, checkout)
	var events []string
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task", "third-task"),
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
	if outcome.Certified != 3 || outcome.Failed != 0 || outcome.Blocked != 0 {
		t.Fatalf("audit outcome = %#v", outcome)
	}
	want := strings.Join([]string{
		"run:" + auditTaskImage("first-task") + "@" + testReadinessDigest,
		"run:" + auditTaskImage("second-task") + "@" + testReadinessDigest,
		"remove:" + auditTaskImage("first-task") + "@" + testReadinessDigest,
		"run:" + auditTaskImage("third-task") + "@" + testReadinessDigest,
		"remove:" + auditTaskImage("second-task") + "@" + testReadinessDigest,
		"remove:" + auditTaskImage("third-task") + "@" + testReadinessDigest,
	}, "|")
	if got := strings.Join(events, "|"); got != want {
		t.Fatalf("bounded-disk Docker events = %q, want %q", got, want)
	}
}

func TestReadinessAuditCombinesCleanupFailureWhenLaterLoadFails(t *testing.T) {
	checkout := t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installReadinessTestBoundaries(t, auditContractsFixture("first-task"),
		func(_ context.Context, arguments ...string) ([]byte, error) {
			if len(arguments) > 3 && arguments[0] == "image" && arguments[1] == "rm" {
				return []byte("permission denied"), errors.New("exit status 1")
			}
			return nil, nil
		})

	outcome, err := checkTaskReadinessWithBoundedDisk(
		context.Background(),
		ReadinessAuditOptions{RepoRoot: t.TempDir()},
		checkout,
		[]benchmarkPlanTask{{ID: "first-task"}, {ID: "second-task"}},
		[]string{strings.Repeat("1", 64), strings.Repeat("2", 64)},
		[]ReadinessAuditTask{
			{Task: "first-task", Status: readinessStatusValidated},
			{Task: "second-task", Status: readinessStatusValidated},
		},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "no mandatory environment readiness contract") ||
		!strings.Contains(err.Error(), "reclaim benchmark readiness Docker images") ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("combined load and cleanup error = %v", err)
	}
	if outcome.Certified != 1 || outcome.Validated != 1 || outcome.Failed != 0 {
		t.Fatalf("audit outcome = %#v", outcome)
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
		"readiness timed out after 10m0s",
	} {
		if !isReadinessInfrastructureFailure(assertionError(message)) {
			t.Fatalf("message %q was not classified as infrastructure failure", message)
		}
	}
	if isReadinessInfrastructureFailure(assertionError("missing required command: node")) {
		t.Fatal("task prerequisite failure was classified as infrastructure failure")
	}
}

func TestSelectReadinessTasksFiltersAndShardsDeterministically(t *testing.T) {
	catalog := []benchmarkPlanTask{{ID: "alpha"}, {ID: "bravo"}, {ID: "charlie"}, {ID: "delta"}, {ID: "echo"}}
	var shards [][]benchmarkPlanTask
	for index := 1; index <= 3; index++ {
		shard, err := selectReadinessTasks(catalog, nil, index, 3)
		if err != nil {
			t.Fatal(err)
		}
		shards = append(shards, shard)
	}
	if got := taskIDs(shards[0]); strings.Join(got, ",") != "alpha,delta" {
		t.Fatalf("first shard = %v", got)
	}
	if got := taskIDs(shards[1]); strings.Join(got, ",") != "bravo,echo" {
		t.Fatalf("second shard = %v", got)
	}
	if got := taskIDs(shards[2]); strings.Join(got, ",") != "charlie" {
		t.Fatalf("third shard = %v", got)
	}
	filtered, err := selectReadinessTasks(catalog, []string{"echo", "bravo"}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(filtered); strings.Join(got, ",") != "bravo,echo" {
		t.Fatalf("filtered tasks = %v", got)
	}
	for _, test := range []struct {
		tasks []string
		want  string
	}{
		{[]string{"bravo", "bravo"}, "duplicate"},
		{[]string{"missing"}, "not in the pinned catalog"},
	} {
		if _, err := selectReadinessTasks(catalog, test.tasks, 1, 1); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("selection error = %v, want %q", err, test.want)
		}
	}
}

func taskIDs(tasks []benchmarkPlanTask) []string {
	ids := make([]string, len(tasks))
	for index, task := range tasks {
		ids[index] = task.ID
	}
	return ids
}

func TestReadinessAuditReportsProgressAndBoundsTaskRuntime(t *testing.T) {
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installAuditCheckout(t, checkout)
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
		func(ctx context.Context, arguments ...string) ([]byte, error) {
			if len(arguments) > 1 && arguments[0] == "image" && arguments[1] == "rm" {
				return nil, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
	var progress []ReadinessAuditProgress
	outcome, err := CheckAllTaskReadiness(context.Background(), ReadinessAuditOptions{
		RepoRoot: repository, TaskConcurrency: 1, RemoveTaskImages: true,
		TaskTimeout: 10 * time.Millisecond,
		OnTaskProgress: func(event ReadinessAuditProgress) {
			progress = append(progress, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Blocked != 2 || outcome.Failed != 0 {
		t.Fatalf("timeout outcome = %#v", outcome)
	}
	if len(progress) != 2 || progress[0].Status != readinessStatusChecking || progress[1].Status != readinessStatusBlocked ||
		progress[1].Completed != 1 || progress[1].Required != 2 {
		t.Fatalf("progress = %#v", progress)
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
