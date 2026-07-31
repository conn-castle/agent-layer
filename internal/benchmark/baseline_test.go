package benchmark

import (
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
	originalCertification := certifyBenchmarkTaskEnvironments
	originalStartupPreflight := preflightTaskStartups
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(string, []parsedSelection) error { return nil }
	ensurePinnedBenchmarkCheckout = func(context.Context, string) (string, error) { return checkout, nil }
	environmentIdentity := strings.Repeat("e", 64)
	certifyBenchmarkTaskEnvironments = func(_ context.Context, _ string, _ string, tasks []benchmarkPlanTask, _ map[string]string) (map[string]string, error) {
		identities := make(map[string]string, len(tasks))
		for _, task := range tasks {
			identities[task.ID] = environmentIdentity
		}
		return identities, nil
	}
	preflightTaskStartups = func(string, []benchmarkPlanTask) error { return nil }
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalPier
		validateBenchmarkAuthentication = originalAuth
		ensurePinnedBenchmarkCheckout = originalCheckout
		certifyBenchmarkTaskEnvironments = originalCertification
		preflightTaskStartups = originalStartupPreflight
	})

	executor := &baselineFakeExecutor{}
	options := BaselineOptions{RepoRoot: repository, PlanPath: planPath, Execution: "luna:low", TaskConcurrency: 2}
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
	for _, request := range executor.calls {
		if request.Effort != effortLow ||
			request.Model.PublishedIdentifier != publishedLuna {
			t.Fatalf("calibration plan changed execution request: %#v", request)
		}
	}
	if outcome.Summary.FreshBaselineMean != .375 || outcome.Summary.PublishedMean != .5 || math.Abs(outcome.ActualUSD-.20) > 1e-12 {
		t.Fatalf("baseline summary = %#v", outcome.Summary)
	}
	if outcome.Summary.CalibrationReference != "gpt-5-6-luna::medium" ||
		outcome.Summary.CalibrationContrast != "gpt-5-6-luna::high" ||
		outcome.Execution != "luna:low" {
		t.Fatalf("benchmark configurations = %#v, outcome %#v", outcome.Summary, outcome)
	}
	if outcome.Summary.PublishedComparable || outcome.Summary.LocalHarness != "codex" ||
		len(outcome.Summary.Limitations) != 2 ||
		!strings.Contains(outcome.Summary.Limitations[0], "executed gpt-5-6-luna::low") {
		t.Fatalf("baseline provenance = %#v", outcome.Summary)
	}
	if _, err := RunBaseline(context.Background(), options, executor); err != nil {
		t.Fatalf("cached RunBaseline: %v", err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("cached baseline made %d total calls; want 4", len(executor.calls))
	}
	oldManifestPath := filepath.Join(outcome.StateDir, "manifest.json")
	oldManifest, err := os.ReadFile(oldManifestPath) // #nosec G304 -- outcome returned this test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	environmentIdentity = strings.Repeat("f", 64)
	changed, err := CheckBaseline(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CampaignID == outcome.CampaignID || changed.StateDir == outcome.StateDir || changed.Completed != 0 {
		t.Fatalf("changed readiness reused baseline state: old %#v, new %#v", outcome, changed)
	}
	fresh, err := RunBaseline(context.Background(), options, executor)
	if err != nil || fresh.Completed != fresh.Required || len(executor.calls) != 8 {
		t.Fatalf("environment-qualified baseline = %#v, %v, calls %d", fresh, err, len(executor.calls))
	}
	preserved, err := os.ReadFile(oldManifestPath) // #nosec G304 -- old immutable evidence must remain readable.
	if err != nil || string(preserved) != string(oldManifest) {
		t.Fatalf("old baseline manifest was not preserved: %v", err)
	}
}

func TestExecutionConfigurationPartitionsCampaignIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	writeBenchmarkPlanFixture(t, path)
	loaded, err := loadBenchmarkPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	low, err := bindBenchmarkExecution(loaded, "luna:low")
	if err != nil {
		t.Fatal(err)
	}
	medium, err := bindBenchmarkExecution(loaded, "luna:medium")
	if err != nil {
		t.Fatal(err)
	}
	if low.ID != medium.ID || low.CampaignID == medium.CampaignID ||
		low.Effort != effortLow || medium.Effort != effortMedium {
		t.Fatalf("execution identities: low %#v, medium %#v", low, medium)
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
		RepoRoot:  repository,
		PlanPath:  planPath,
		Execution: "luna:low",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid cost-axis contract") {
		t.Fatalf("invalid cost-axis error = %v", err)
	}
	if preflightCalled {
		t.Fatal("invalid cost axis reached execution preflight")
	}
}

func TestLoadBenchmarkPlanKeepsVersionOnePlansReportOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-plan.json")
	writeLegacyBenchmarkPlanFixture(t, path, benchmarkPlanSchema)
	loaded, err := loadBenchmarkPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Legacy || loaded.CampaignID != loaded.ID ||
		loaded.Plan.CalibrationReference.ID != "gpt-5-6-luna::medium" ||
		loaded.Plan.CalibrationContrast.ID != "gpt-5-6-luna::high" {
		t.Fatalf("legacy plan = %#v", loaded)
	}
	if _, err := bindBenchmarkExecution(loaded, "luna:medium"); err == nil ||
		!strings.Contains(err.Error(), "report-only") {
		t.Fatalf("legacy execution error = %v", err)
	}
	writeLegacyBenchmarkPlanFixture(t, path, legacyBenchmarkPlanSchema)
	if loaded, err = loadBenchmarkPlan(path); err != nil || !loaded.Legacy {
		t.Fatalf("legacy diagnostic plan = %#v, %v", loaded, err)
	}
}

func writeBenchmarkPlanFixture(t *testing.T, path string) {
	t.Helper()
	plan := map[string]any{
		"schema": benchmarkPlanSchema, "schemaVersion": benchmarkPlanSchemaVersion,
		"snapshot": map[string]any{"url": DeepSWETrialsSourceURL, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"calibrationReference": map[string]any{
			"id": "gpt-5-6-luna::medium", "model": "gpt-5-6-luna",
			"reasoning": "medium", "harnesses": []string{"mini-swe-agent"},
		},
		"calibrationContrast": map[string]any{
			"id": "gpt-5-6-luna::high", "model": "gpt-5-6-luna",
			"reasoning": "high", "harnesses": []string{"mini-swe-agent"},
		},
		"parameters": map[string]any{
			"calibrationReferenceBudgetUsd": 1.0, "twoSidedSignificanceLevel": .05,
		},
		"result": map[string]any{
			"valid": true, "estimatedCalibrationReferenceSpendUsd": .20, "decisionThreshold": .04,
		},
		"tasks": []map[string]any{
			{"id": "first-benchmark-task", "repetitionsPerArm": 2, "calibrationReference": map[string]any{"mean": .25}, "calibrationContrast": map[string]any{"mean": .50}, "calibrationReferenceEstimatedBaselineCostUsd": .10},
			{"id": "second-benchmark-task", "repetitionsPerArm": 2, "calibrationReference": map[string]any{"mean": .75}, "calibrationContrast": map[string]any{"mean": .90}, "calibrationReferenceEstimatedBaselineCostUsd": .10},
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

func writeLegacyBenchmarkPlanFixture(t *testing.T, path, schema string) {
	t.Helper()
	writeBenchmarkPlanFixture(t, path)
	addCostAxisToPlanFixture(t, path)
	data, err := os.ReadFile(path) // #nosec G304 -- path belongs to this test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["schema"] = schema
	plan["schemaVersion"] = float64(legacyPlanSchemaVersion)
	plan["target"] = plan["calibrationReference"]
	plan["comparison"] = plan["calibrationContrast"]
	delete(plan, "calibrationReference")
	delete(plan, "calibrationContrast")
	parameters := plan["parameters"].(map[string]any)
	parameters["baselineBudgetUsd"] = parameters["calibrationReferenceBudgetUsd"]
	delete(parameters, "calibrationReferenceBudgetUsd")
	result := plan["result"].(map[string]any)
	result["estimatedBaselineSpendUsd"] = result["estimatedCalibrationReferenceSpendUsd"]
	delete(result, "estimatedCalibrationReferenceSpendUsd")
	for _, item := range plan["tasks"].([]any) {
		task := item.(map[string]any)
		task["target"] = task["calibrationReference"]
		task["comparison"] = task["calibrationContrast"]
		task["targetEstimatedBaselineCostUsd"] = task["calibrationReferenceEstimatedBaselineCostUsd"]
		delete(task, "calibrationReference")
		delete(task, "calibrationContrast")
		delete(task, "calibrationReferenceEstimatedBaselineCostUsd")
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil { // #nosec G703 -- path belongs to this test's temporary directory.
		t.Fatal(err)
	}
}
