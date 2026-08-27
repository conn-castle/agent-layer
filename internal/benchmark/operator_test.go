package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadinessDiskPreflightRefusesImpossiblePlanBeforePulls(t *testing.T) {
	original := readinessDiskCapacity
	readinessDiskCapacity = func() (int64, error) { return 2 << 30, nil }
	t.Cleanup(func() { readinessDiskCapacity = original })
	tasks := make([]benchmarkPlanTask, 113)
	if err := preflightReadinessDisk(tasks, true); err == nil ||
		!strings.Contains(err.Error(), "113 benchmark task image") ||
		!strings.Contains(err.Error(), "452.0 GiB") || !strings.Contains(err.Error(), "2.0 GiB available") {
		t.Fatalf("capacity error = %v", err)
	}
	if err := preflightReadinessDisk(tasks, false); err == nil || !strings.Contains(err.Error(), "need about 4.0 GiB") {
		t.Fatalf("bounded capacity error = %v", err)
	}
	readinessDiskCapacity = func() (int64, error) { return 5 << 30, nil }
	if err := preflightReadinessDisk(tasks, false); err != nil {
		t.Fatalf("bounded plan rejected: %v", err)
	}
}

func TestAutomaticTaskConcurrencyIsAlwaysSafe(t *testing.T) {
	for _, providerCalls := range []bool{false, true} {
		workers := AutomaticTaskConcurrency(providerCalls)
		if workers < 1 || workers > 4 || (providerCalls && workers > 2) {
			t.Fatalf("automatic workers(provider=%t) = %d", providerCalls, workers)
		}
	}
}

func TestStudyTaskIDsReadsSelectionWithoutTreatmentSetup(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "selection.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "study.toml"), []byte("selection = \"selection.json\"\n[[experiments]]\nname = \"intentionally incomplete\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := StudyTaskIDs(filepath.Join(root, "study.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tasks, ","); got != "first-task,second-task" {
		t.Fatalf("study tasks = %q", got)
	}
}

func TestInitStudyCreatesSelfContainedSafeSnapshot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agent-layer", "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "implement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agent-layer", "instructions", "rules.md"), []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "implement", "SKILL.md"), []byte("skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	selectionPath := filepath.Join(repo, "website-selection.json")
	if err := os.WriteFile(selectionPath, selectionData, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(repo, "benchmarks", "study-one")
	studyPath, err := InitStudy(InitStudyOptions{RepoRoot: repo, SelectionPath: selectionPath, Directory: destination})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		studyPath, filepath.Join(destination, "selection.json"), filepath.Join(destination, "treatment", "config.toml"),
		filepath.Join(destination, "treatment", "instructions", "rules.md"), filepath.Join(destination, "treatment", "skills", "implement", "SKILL.md"),
		filepath.Join(destination, "treatment", "prompt.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing scaffold %s: %v", path, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(destination, "treatment", "config.toml")) // #nosec G304 -- test-owned destination.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "statusline") {
		t.Fatalf("scaffold copied host-only statusline config: %s", config)
	}
	if _, err := prepareStudy(StudyOptions{RepoRoot: repo, StudyPath: studyPath}); err != nil {
		t.Fatalf("generated study does not validate: %v", err)
	}
}
