package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
