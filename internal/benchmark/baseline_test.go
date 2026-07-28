package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type baselineFakeExecutor struct {
	mutex sync.Mutex
	calls []ExecutionRequest
}

func (executor *baselineFakeExecutor) Execute(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
	executor.mutex.Lock()
	executor.calls = append(executor.calls, request)
	executor.mutex.Unlock()
	cost := .05
	duration := 1.0
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: request.EventID, Attempt: request.Attempt,
		Task: request.Task, Status: statusSuccess, F2PPassed: request.Attempt, F2PTotal: 4,
		F2PScore: float64(request.Attempt) / 4, CostUSD: &cost, CostKind: costKindProviderReported,
		DurationSeconds: &duration, TaskChecksum: request.TaskChecksum, StartedAt: time.Now().UTC(),
		FinishedAt: time.Now().UTC(), Provider: "openai", PublishedModel: request.Model.PublishedIdentifier,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		ProviderClientVersion: request.Model.ProviderClientVersion, DispatchConformant: true, InvocationCount: 1,
	}, nil
}

func TestRunBaselineUsesPerTaskRepetitionsAndReusesEvidence(t *testing.T) {
	repository := t.TempDir()
	checkout := filepath.Join(repository, "checkout")
	for _, task := range []string{"first-benchmark-task", "second-benchmark-task"} {
		for _, path := range []string{taskTOMLFile, taskInstructionFile, taskPreArtifactsFile, filepath.Join("tests", "test.sh")} {
			fullPath := filepath.Join(checkout, "tasks", task, path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fullPath, []byte(path+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	planPath := filepath.Join(repository, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	addCostAxisToPlanFixture(t, planPath)

	originalPreflight := preflightBenchmark
	originalPier := verifyBenchmarkPier
	originalAuth := validateBenchmarkAuthentication
	originalCheckout := ensurePinnedBenchmarkCheckout
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(string, []parsedSelection) error { return nil }
	ensurePinnedBenchmarkCheckout = func(context.Context, string) (string, error) { return checkout, nil }
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalPier
		validateBenchmarkAuthentication = originalAuth
		ensurePinnedBenchmarkCheckout = originalCheckout
	})

	executor := &baselineFakeExecutor{}
	options := BaselineOptions{RepoRoot: repository, PlanPath: planPath, TaskConcurrency: 2}
	checked, err := CheckBaseline(context.Background(), options)
	if err != nil || checked.Required != 4 || checked.Completed != 0 {
		t.Fatalf("CheckBaseline = %#v, %v", checked, err)
	}
	if _, err := os.Stat(filepath.Join(checked.StateDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("read-only baseline check wrote state: %v", err)
	}
	pending, err := RunBaseline(context.Background(), options, executor)
	if err != ErrConfirmationRequired || pending.Required != 4 || pending.Completed != 0 || len(executor.calls) != 0 {
		t.Fatalf("unconfirmed baseline = %#v, %v, calls %d", pending, err, len(executor.calls))
	}
	options.Confirmed = true
	outcome, err := RunBaseline(context.Background(), options, executor)
	if err != nil {
		t.Fatalf("RunBaseline: %v", err)
	}
	if outcome.Completed != 4 || len(executor.calls) != 4 || outcome.Summary == nil {
		t.Fatalf("completed baseline = %#v, calls %d", outcome, len(executor.calls))
	}
	if outcome.Summary.FreshBaselineMean != .375 || outcome.Summary.PublishedMean != .5 || math.Abs(outcome.ActualUSD-.20) > 1e-12 {
		t.Fatalf("baseline summary = %#v", outcome.Summary)
	}
	if outcome.Summary.PublishedComparable || outcome.Summary.LocalHarness != "codex" || len(outcome.Summary.Limitations) != 1 {
		t.Fatalf("baseline provenance = %#v", outcome.Summary)
	}
	if _, err := RunBaseline(context.Background(), options, executor); err != nil {
		t.Fatalf("cached RunBaseline: %v", err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("cached baseline made %d total calls; want 4", len(executor.calls))
	}
}

func TestBenchmarkPlanRejectsAllocationsThatCannotEstimateObservedVariance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	writeBenchmarkPlanFixture(t, path)
	data, err := os.ReadFile(path) // #nosec G304 -- path is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := plan["tasks"].([]any)
	tasks[0].(map[string]any)["repetitionsPerArm"] = 1
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadBenchmarkPlanJSON(data); err == nil {
		t.Fatal("plan with one repetition was accepted despite requiring observed variance")
	}
}

func TestBaselineRejectsInvalidCostAxisBeforePreflight(t *testing.T) {
	repository := t.TempDir()
	planPath := filepath.Join(repository, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	addCostAxisToPlanFixture(t, planPath)
	data, err := os.ReadFile(planPath) // #nosec G304 -- planPath is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["costAxis"].(map[string]any)["valid"] = false
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	originalPreflight := preflightBenchmark
	preflightCalled := false
	preflightBenchmark = func([]parsedSelection) error {
		preflightCalled = true
		return nil
	}
	t.Cleanup(func() { preflightBenchmark = originalPreflight })

	_, err = CheckBaseline(context.Background(), BaselineOptions{
		RepoRoot: repository,
		PlanPath: planPath,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid cost-axis contract") {
		t.Fatalf("invalid cost-axis error = %v", err)
	}
	if preflightCalled {
		t.Fatal("invalid cost axis reached execution preflight")
	}
}

func TestLoadBenchmarkPlanAcceptsLegacySchemaForExistingCampaignReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-plan.json")
	writeBenchmarkPlanFixture(t, path)
	data, err := os.ReadFile(path) // #nosec G304 -- path belongs to this test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(
		data,
		[]byte(`"schema":"`+benchmarkPlanSchema+`"`),
		[]byte(`"schema":"`+legacyBenchmarkPlanSchema+`"`),
		1,
	)
	if err := os.WriteFile(path, data, 0o600); err != nil { // #nosec G703 -- path belongs to this test's temporary directory.
		t.Fatal(err)
	}
	loaded, err := loadBenchmarkPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Plan.Schema != legacyBenchmarkPlanSchema {
		t.Fatalf("legacy schema = %q", loaded.Plan.Schema)
	}
}

func writeBenchmarkPlanFixture(t *testing.T, path string) {
	t.Helper()
	plan := map[string]any{
		"schema": benchmarkPlanSchema, "schemaVersion": benchmarkPlanSchemaVersion,
		"snapshot": map[string]any{"url": DeepSWETrialsSourceURL, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"target":   map[string]any{"model": "gpt-5-6-luna", "reasoning": "low", "harnesses": []string{"mini-swe-agent"}},
		"parameters": map[string]any{
			"baselineBudgetUsd": 1.0, "twoSidedSignificanceLevel": .05,
		},
		"result": map[string]any{
			"valid": true, "estimatedBaselineSpendUsd": .20, "decisionThreshold": .04,
		},
		"tasks": []map[string]any{
			{"id": "first-benchmark-task", "repetitionsPerArm": 2, "target": map[string]any{"mean": .25}, "targetEstimatedBaselineCostUsd": .10},
			{"id": "second-benchmark-task", "repetitionsPerArm": 2, "target": map[string]any{"mean": .75}, "targetEstimatedBaselineCostUsd": .10},
		},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
