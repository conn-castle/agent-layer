package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// matrixTaskIdentities derives one task-tree checksum and one certified
// environment identity per selected task. generation stands in for a re-pinned
// DeepSWE checkout or a re-certified task image, which is what makes previously
// recorded evidence incomparable.
func matrixTaskIdentities(tasks []benchmarkPlanTask, generation string) (map[string]string, map[string]string) {
	checksums := make(map[string]string, len(tasks))
	environments := make(map[string]string, len(tasks))
	for _, task := range tasks {
		checksums[task.ID] = benchmarkIdentityFixture("checksum/" + generation + "/" + task.ID)
		environments[task.ID] = benchmarkIdentityFixture("environment/" + generation + "/" + task.ID)
	}
	return checksums, environments
}

func benchmarkIdentityFixture(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// installMatrixTestBoundaries replaces the Docker, provider-client, and pinned
// checkout prerequisites so a matrix runs offline. It reports how many times the
// prerequisite probes ran, which lets a test prove that argument validation
// rejects an unusable request before any external work starts.
func installMatrixTestBoundaries(
	t *testing.T,
	taskSet func([]benchmarkPlanTask) (map[string]string, map[string]string, error),
) *int {
	t.Helper()
	originalPreflight := preflightBenchmark
	originalPier := verifyBenchmarkPier
	originalAuth := validateBenchmarkAuthentication
	originalTaskSet := prepareBenchmarkTaskSet
	probes := 0
	preflightBenchmark = func([]parsedSelection) error { probes++; return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(string, []parsedSelection) error { return nil }
	prepareBenchmarkTaskSet = func(_ context.Context, _ string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		return taskSet(tasks)
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalPier
		validateBenchmarkAuthentication = originalAuth
		prepareBenchmarkTaskSet = originalTaskSet
	})
	return &probes
}

func writeMatrixSelectionFile(t *testing.T, path string, selection matrixSelection) {
	t.Helper()
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// matrixFixture prepares a repository with a valid two-task selection and the
// offline boundaries a matrix needs. The returned pointer selects which task
// identity generation the preflight reports.
func matrixFixture(t *testing.T) (MatrixOptions, *string) {
	t.Helper()
	repository := t.TempDir()
	selectionPath := filepath.Join(repository, "selection.json")
	writeMatrixSelectionFile(t, selectionPath, matrixSelectionFixture())
	generation := "first"
	installMatrixTestBoundaries(t, func(tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		checksums, environments := matrixTaskIdentities(tasks, generation)
		return checksums, environments, nil
	})
	return MatrixOptions{
		RepoRoot: repository, SelectionPath: selectionPath,
		BaselineExecutions: []string{"luna:low"},
	}, &generation
}

func TestCheckMatrixReportsPendingWorkWithoutWritingCampaignState(t *testing.T) {
	options, _ := matrixFixture(t)

	outcome, err := CheckMatrix(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Required != 2 || outcome.Completed != 0 || outcome.Missing != 2 {
		t.Fatalf("checked matrix = %#v", outcome)
	}
	if len(outcome.Arms) != 1 || outcome.Arms[0].Label != "Bare luna low" ||
		outcome.Arms[0].Mode != ArmBaseline || outcome.Arms[0].Missing != 2 {
		t.Fatalf("checked arms = %#v", outcome.Arms)
	}
	// --check runs before the user has approved any spending, so it must leave
	// the campaign directory untouched. A manifest written here would pin an
	// immutable arm identity nobody agreed to pay for.
	if _, err := os.Stat(filepath.Join(outcome.StateDir, "arms")); !os.IsNotExist(err) {
		t.Fatalf("read-only matrix check wrote arm state: %v", err)
	}
}

func TestRunMatrixRefusesToSpendWithoutConfirmation(t *testing.T) {
	options, _ := matrixFixture(t)
	executor := &baselineFakeExecutor{}

	outcome, err := RunMatrix(context.Background(), options, executor)
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed matrix error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("unconfirmed matrix made %d paid calls", len(executor.calls))
	}
	// The caller still needs the pending counts to present a cost prompt.
	if outcome.Missing != 2 || outcome.Required != 2 {
		t.Fatalf("unconfirmed matrix outcome = %#v", outcome)
	}
	if _, err := os.Stat(filepath.Join(outcome.StateDir, "arms")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed matrix wrote arm state: %v", err)
	}
}

func TestRunMatrixExecutesEveryCellOnceAndReportsFromStoredEvidence(t *testing.T) {
	options, _ := matrixFixture(t)
	options.BaselineExecutions = []string{"luna:low", "luna:medium"}
	options.Confirmed = true
	options.TaskConcurrency = 2
	executor := &baselineFakeExecutor{}

	outcome, err := RunMatrix(context.Background(), options, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 4 || outcome.Missing != 0 || outcome.Completed != 4 {
		t.Fatalf("matrix outcome = %#v, calls %d", outcome, len(executor.calls))
	}
	if outcome.Report == nil || len(outcome.Report.Arms) != 2 {
		t.Fatalf("matrix report = %#v", outcome.Report)
	}
	for _, path := range []string{outcome.JSONPath, outcome.HTMLPath} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() == 0 {
			t.Fatalf("report artifact %s = %v, %v", path, info, statErr)
		}
	}
	// Every cell must carry the certified environment identity, because that is
	// what lets a later re-certification invalidate one task instead of the arm.
	for _, call := range executor.calls {
		if len(call.EnvironmentIdentity) != 64 || len(call.TaskChecksum) != 64 {
			t.Fatalf("paid call lost task identity: %#v", call)
		}
	}
	// Each arm scores the published calibration applied to the observed F2P
	// fraction, weighted by the selection: .25*(.1+.8*.25) + .75*(.2+.5*.25).
	for _, arm := range outcome.Report.Arms {
		if math.Abs(arm.Score-.31875) > 1e-12 || math.Abs(arm.Cost.Midpoint-.1) > 1e-12 {
			t.Fatalf("arm %q score/cost = %v/%v", arm.Label, arm.Score, arm.Cost.Midpoint)
		}
		if len(arm.Tasks) != 2 || arm.InvocationCount != 2 {
			t.Fatalf("arm %q tasks = %#v", arm.Label, arm.Tasks)
		}
	}
	if outcome.Report.Arms[0].Reasoning == outcome.Report.Arms[1].Reasoning {
		t.Fatalf("both arms reported the same reasoning effort: %#v", outcome.Report.Arms)
	}

	// Completed evidence is immutable and paid for once. Re-running rebuilds the
	// identical report without touching the provider.
	repeat, err := RunMatrix(context.Background(), options, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("completed matrix re-executed paid cells: %d calls", len(executor.calls))
	}
	if repeat.Report.Arms[0].Score != outcome.Report.Arms[0].Score {
		t.Fatalf("regenerated score = %v, want %v", repeat.Report.Arms[0].Score, outcome.Report.Arms[0].Score)
	}
}

func TestRunMatrixExecutesSequentiallyWhenConcurrencyIsUnset(t *testing.T) {
	options, _ := matrixFixture(t)
	options.Confirmed = true
	options.TaskConcurrency = 0
	executor := &baselineFakeExecutor{}

	// Task concurrency is optional. An unset value must execute the matrix one
	// task at a time; dispatching to zero workers would leave the command
	// waiting forever with no output instead of failing.
	finished := make(chan error, 1)
	go func() {
		_, err := RunMatrix(context.Background(), options, executor)
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("matrix with unset task concurrency never finished")
	}
	if len(executor.calls) != 2 {
		t.Fatalf("default concurrency calls = %d, want 2", len(executor.calls))
	}
}

func TestMatrixExecutionRefusesToDispatchToNoWorkers(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}
	arms := []matrixArm{matrixArmFixture(root, "bare", ArmBaseline, model, effort, tasks)}

	err = executeMatrix(
		context.Background(), root,
		map[string]string{"first-task": "first-checksum"},
		map[string]string{"first-task": "first-environment"},
		arms, nil, 0, &baselineFakeExecutor{},
	)
	if err == nil || !strings.Contains(err.Error(), "at least one task worker") {
		t.Fatalf("zero-worker execution error = %v", err)
	}
}

func TestMatrixArmRefusesEvidenceFromADifferentPinnedTaskTree(t *testing.T) {
	options, generation := matrixFixture(t)
	options.Confirmed = true
	executor := &baselineFakeExecutor{}
	if _, err := RunMatrix(context.Background(), options, executor); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("first matrix calls = %d", len(executor.calls))
	}

	// The pinned DeepSWE checkout moved, so the recorded results describe task
	// trees that no longer exist. Continuing would blend two different
	// benchmarks under one arm label, so the arm must refuse rather than
	// quietly re-run and mix the evidence.
	*generation = "second"
	_, err := RunMatrix(context.Background(), options, executor)
	if err == nil || !strings.Contains(err.Error(), "conflicts with its immutable manifest") {
		t.Fatalf("changed task tree error = %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("conflicting matrix made %d extra paid calls", len(executor.calls)-2)
	}
}

func TestRunMatrixWithTaskFilterExecutesOnlyThatTaskAndDefersTheReport(t *testing.T) {
	options, _ := matrixFixture(t)
	options.Confirmed = true
	options.Tasks = []string{"second-task"}
	executor := &baselineFakeExecutor{}

	outcome, err := RunMatrix(context.Background(), options, executor)
	// A task filter is a deliberately bounded paid probe, so an incomplete
	// matrix is the expected outcome rather than a failure.
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 1 || executor.calls[0].Task != "second-task" {
		t.Fatalf("filtered matrix calls = %#v", executor.calls)
	}
	if outcome.Missing != 1 || outcome.Completed != 1 {
		t.Fatalf("filtered matrix outcome = %#v", outcome)
	}
	if outcome.Report != nil || outcome.JSONPath != "" {
		t.Fatalf("filtered matrix published an incomplete report: %#v", outcome)
	}
}

func TestMatrixTaskFilterMustNameDistinctSelectedTasks(t *testing.T) {
	for _, test := range []struct {
		name   string
		tasks  []string
		wanted string
	}{
		{"unselected", []string{"third-task"}, "is not in the selection"},
		{"duplicate", []string{"first-task", "first-task"}, "duplicate benchmark matrix task filter"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _ := matrixFixture(t)
			options.Tasks = test.tasks
			if _, err := CheckMatrix(context.Background(), options); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s task filter error = %v", test.name, err)
			}
		})
	}
}

func TestMatrixRejectsUnusableRequestsBeforeProbingPrerequisites(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*MatrixOptions)
		wanted string
	}{
		{"no repository", func(o *MatrixOptions) { o.RepoRoot = "" }, "requires a repository root"},
		{"no selection", func(o *MatrixOptions) { o.SelectionPath = "" }, "requires a repository root"},
		{"no baselines", func(o *MatrixOptions) { o.BaselineExecutions = nil }, "requires a repository root"},
		{"excessive concurrency", func(o *MatrixOptions) { o.TaskConcurrency = 9 }, "task concurrency must be from 1 to 8"},
		{
			"duplicate baseline",
			func(o *MatrixOptions) { o.BaselineExecutions = []string{"luna:low", "luna:low"} },
			"duplicate baseline execution",
		},
		{
			"unknown treatment mode",
			func(o *MatrixOptions) { o.TreatmentExecution, o.TreatmentMode = "luna:low", "instructions-and-vibes" },
			"unsupported benchmark treatment mode",
		},
		{
			"dispatch config without skills",
			func(o *MatrixOptions) {
				o.TreatmentExecution, o.TreatmentMode = "luna:low", TreatmentInstructionsOnly
				o.DispatchConfigPath = "dispatch.toml"
			},
			"instructions-only matrix treatment does not accept a dispatch config",
		},
		{
			"unsupported model",
			func(o *MatrixOptions) { o.BaselineExecutions = []string{"gemini:low"} },
			"unsupported model",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _ := matrixFixture(t)
			probes := installMatrixTestBoundaries(t, func(tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
				checksums, environments := matrixTaskIdentities(tasks, "first")
				return checksums, environments, nil
			})
			test.mutate(&options)
			if _, err := CheckMatrix(context.Background(), options); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
			// Rejecting the request offline keeps a typo from waiting on a Docker
			// daemon probe and a provider-client lookup first.
			if *probes != 0 {
				t.Fatalf("%s probed prerequisites %d times", test.name, *probes)
			}
		})
	}
}

func TestMatrixSelectionMustBeExactlyOneValidJSONDocument(t *testing.T) {
	valid, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		content []byte
		wanted  string
	}{
		{"empty", nil, "must be a non-empty JSON file"},
		{"trailing document", append(append([]byte(nil), valid...), valid...), "trailing JSON"},
		{"unknown field", append(valid[:len(valid)-1], []byte(`,"surprise":1}`)...), "decode benchmark selection"},
		{"not json", []byte("selection"), "decode benchmark selection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _ := matrixFixture(t)
			if err := os.WriteFile(options.SelectionPath, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			// The selection document fixes the exact paid allocation the user
			// reviewed on the website; anything ambiguous must be refused.
			if _, err := CheckMatrix(context.Background(), options); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s selection error = %v", test.name, err)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		options, _ := matrixFixture(t)
		if err := os.Remove(options.SelectionPath); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckMatrix(context.Background(), options); err == nil ||
			!strings.Contains(err.Error(), "inspect benchmark selection") {
			t.Fatalf("missing selection error = %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		options, _ := matrixFixture(t)
		if err := os.Remove(options.SelectionPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(options.SelectionPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckMatrix(context.Background(), options); err == nil ||
			!strings.Contains(err.Error(), "must be a non-empty JSON file") {
			t.Fatalf("directory selection error = %v", err)
		}
	})
}
