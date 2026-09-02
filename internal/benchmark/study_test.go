package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStudyPublishedBareEstimateRequiresMatchingSelectorTarget(t *testing.T) {
	selection := matrixSelectionFixture()
	luna, low, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	sol, medium, err := ParseModelSelection("sol:medium")
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedStudy{selection: selection, experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "other bare"}, model: sol, effort: medium}}}
	outcome := studyProgress(prepared, StudyOptions{})
	if !outcome.HasBareExperiment || outcome.BarePublishedEstimateUSD != nil {
		t.Fatalf("unmatched bare estimate = %#v", outcome)
	}
	prepared.experiments = []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "selector bare"}, model: luna, effort: low}}
	outcome = studyProgress(prepared, StudyOptions{})
	if outcome.BarePublishedEstimateUSD == nil || *outcome.BarePublishedEstimateUSD != selection.EstimatedPublishedSpendUSD {
		t.Fatalf("matching bare estimate = %#v", outcome.BarePublishedEstimateUSD)
	}
}

func TestRunStudyDryRunAndPaidWorkflow(t *testing.T) {
	root := t.TempDir()
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selectionData))
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n")

	originalPreflight := preflightBenchmark
	originalVerifyPier := verifyBenchmarkPier
	originalValidateAuthentication := validateBenchmarkAuthentication
	originalPrepareTasks := prepareBenchmarkTaskSet
	preflightBenchmark = func(selections []parsedSelection) error {
		if len(selections) != 1 || selections[0].model.PublishedIdentifier != publishedLuna || selections[0].effort != effortLow {
			return fmt.Errorf("unexpected study selections: %#v", selections)
		}
		return nil
	}
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	prepareBenchmarkTaskSet = func(_ context.Context, gotRoot string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		if gotRoot != root || len(tasks) != 2 || tasks[0].ID != "first-task" || tasks[1].ID != "second-task" {
			return nil, nil, fmt.Errorf("unexpected task preparation: root=%q tasks=%#v", gotRoot, tasks)
		}
		return map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}, map[string]string{"first-task": "env-1", "second-task": "env-2"}, nil
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalVerifyPier
		validateBenchmarkAuthentication = originalValidateAuthentication
		prepareBenchmarkTaskSet = originalPrepareTasks
	})

	dryExecutor := &studyWorkflowExecutor{}
	preparedCalls := 0
	dryOutcome, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true,
		OnPrepared: func(outcome StudyOutcome) error {
			preparedCalls++
			if outcome.Completed != 0 || outcome.Missing != 2 {
				return fmt.Errorf("unexpected dry-run progress: %#v", outcome)
			}
			return nil
		},
	}, dryExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if preparedCalls != 1 || dryOutcome.Required != 2 || dryOutcome.Missing != 2 || len(dryExecutor.requests()) != 0 {
		t.Fatalf("dry-run outcome=%#v prepared calls=%d executor calls=%#v", dryOutcome, preparedCalls, dryExecutor.requests())
	}
	preparedErr := errors.New("progress output unavailable")
	blockedExecutor := &studyWorkflowExecutor{}
	if _, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"),
		OnPrepared: func(StudyOutcome) error { return preparedErr },
	}, blockedExecutor); !errors.Is(err, preparedErr) {
		t.Fatalf("prepared callback error=%v", err)
	}
	if calls := blockedExecutor.requests(); len(calls) != 0 {
		t.Fatalf("failed prepared callback reached provider: %#v", calls)
	}
	successfulPreflight := preflightBenchmark
	preflightErr := errors.New("provider prerequisites unavailable")
	preflightBenchmark = func([]parsedSelection) error { return preflightErr }
	preflightBlockedExecutor := &studyWorkflowExecutor{}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, preflightBlockedExecutor); !errors.Is(err, preflightErr) {
		t.Fatalf("preflight error=%v", err)
	}
	if calls := preflightBlockedExecutor.requests(); len(calls) != 0 {
		t.Fatalf("failed preflight reached provider: %#v", calls)
	}
	preflightBenchmark = successfulPreflight
	successfulPierVerification := verifyBenchmarkPier
	pierErr := errors.New("pinned Pier unavailable")
	verifyBenchmarkPier = func(context.Context) error { return pierErr }
	pierBlockedExecutor := &studyWorkflowExecutor{}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, pierBlockedExecutor); !errors.Is(err, pierErr) {
		t.Fatalf("Pier verification error=%v", err)
	}
	if calls := pierBlockedExecutor.requests(); len(calls) != 0 {
		t.Fatalf("failed Pier verification reached provider: %#v", calls)
	}
	verifyBenchmarkPier = successfulPierVerification

	executor := &studyWorkflowExecutor{}
	var observed []ObservedCostRange
	outcome, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), TaskConcurrency: 1,
		OnCellComplete: func(cost ObservedCostRange) { observed = append(observed, cost) },
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Completed != 2 || outcome.Missing != 0 || outcome.ObservedInvocationCost.Midpoint != .2 || len(observed) != 2 {
		t.Fatalf("paid outcome=%#v observed=%#v", outcome, observed)
	}
	calls := executor.requests()
	if len(calls) != 2 || calls[0].Task != "first-task" || calls[1].Task != "second-task" {
		t.Fatalf("paid executor calls=%#v", calls)
	}
	for _, path := range []string{outcome.JSONPath, outcome.HTMLPath} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("study report %q: info=%v err=%v", path, info, err)
		}
	}
	var report StudyReport
	if err := readStudyJSON(outcome.JSONPath, &report); err != nil {
		t.Fatal(err)
	}
	if report.StudyID != outcome.StudyID || len(report.Experiments) != 1 || report.Experiments[0].CompletedCells != 2 {
		t.Fatalf("study report=%#v", report)
	}

	resumeExecutor := &studyWorkflowExecutor{}
	resumed, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true}, resumeExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Completed != 2 || resumed.Missing != 0 || len(resumeExecutor.requests()) != 0 {
		t.Fatalf("resumed dry run=%#v executor calls=%#v", resumed, resumeExecutor.requests())
	}
}

func TestRecoveryOnlyFailsWithoutPreparingTasksWhenNoRecoverableStudyExists(t *testing.T) {
	root := t.TempDir()
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selectionData))
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n")

	originalPreflight := preflightBenchmark
	originalVerifyPier := verifyBenchmarkPier
	originalPrepareTasks := prepareBenchmarkTaskSet
	preflightBenchmark = func([]parsedSelection) error {
		t.Fatal("recovery-only ran provider preflight without a recoverable study")
		return nil
	}
	verifyBenchmarkPier = func(context.Context) error {
		t.Fatal("recovery-only verified Pier without a recoverable study")
		return nil
	}
	prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		t.Fatal("recovery-only prepared tasks without a recoverable study")
		return nil, nil, nil
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalVerifyPier
		prepareBenchmarkTaskSet = originalPrepareTasks
	})

	_, err = RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), RecoveryOnly: true, ResourcePreflight: true,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "no retained terminal verifier timeout or complete study to recover") {
		t.Fatalf("recovery-only without retained study = %v", err)
	}
}

func TestRunStudyRejectsMissingManifestBeforeProviderWork(t *testing.T) {
	executor := &studyWorkflowExecutor{}
	if _, err := RunStudy(context.Background(), StudyOptions{}, executor); err == nil || !strings.Contains(err.Error(), "requires one study.toml path") {
		t.Fatalf("missing study error=%v", err)
	}
	if calls := executor.requests(); len(calls) != 0 {
		t.Fatalf("invalid study reached provider: %#v", calls)
	}
}

func TestPrepareBenchmarkTasksValidatesBeforeCertification(t *testing.T) {
	repoRoot, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installAuditCheckout(t, checkout)
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"), func(context.Context, ...string) ([]byte, error) {
		return nil, nil
	})

	originalCertify := certifyBenchmarkTaskEnvironments
	certifications := 0
	certifyBenchmarkTaskEnvironments = func(_ context.Context, gotRoot, gotCheckout string, tasks []benchmarkPlanTask, checksums map[string]string) (map[string]string, error) {
		certifications++
		if gotRoot != repoRoot || gotCheckout != checkout || len(tasks) != 2 || len(checksums["first-task"]) != 64 || len(checksums["second-task"]) != 64 {
			return nil, fmt.Errorf("unexpected certification boundary: root=%q checkout=%q tasks=%#v checksums=%#v", gotRoot, gotCheckout, tasks, checksums)
		}
		return map[string]string{"first-task": "env-1", "second-task": "env-2"}, nil
	}
	t.Cleanup(func() { certifyBenchmarkTaskEnvironments = originalCertify })

	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	checksums, environments, err := prepareBenchmarkTasks(context.Background(), repoRoot, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if certifications != 1 || len(checksums) != 2 || environments["first-task"] != "env-1" || environments["second-task"] != "env-2" {
		t.Fatalf("checksums=%#v environments=%#v certifications=%d", checksums, environments, certifications)
	}

	if err := os.Remove(filepath.Join(checkout, "tasks", "second-task", taskInstructionFile)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareBenchmarkTasks(context.Background(), repoRoot, tasks); err == nil || !strings.Contains(err.Error(), "selected benchmark tasks failed static validation") || !strings.Contains(err.Error(), taskInstructionFile) {
		t.Fatalf("invalid task-tree error=%v", err)
	}
	if certifications != 1 {
		t.Fatalf("invalid task tree reached certification %d times", certifications)
	}
}

type studyWorkflowExecutor struct {
	mu    sync.Mutex
	calls []ExecutionRequest
}

func (executor *studyWorkflowExecutor) Execute(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
	executor.mu.Lock()
	executor.calls = append(executor.calls, request)
	executor.mu.Unlock()
	if err := writePierExecutionReceipt(request, nil, nil); err != nil {
		return AttemptResult{}, err
	}
	cost, duration := .1, 1.0
	now := time.Now().UTC()
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, Status: statusSuccess,
		F2PPassed: 8, F2PTotal: 10, F2PScore: .8,
		CostUSD: &cost, CostKind: costKindProviderReported, DurationSeconds: &duration,
		TaskChecksum: request.TaskChecksum, EnvironmentIdentity: request.EnvironmentIdentity,
		StartedAt: now, FinishedAt: now, Provider: request.Model.Adapter,
		PublishedModel: request.Model.PublishedIdentifier, RuntimeModel: request.Model.RuntimeIdentifier,
		ReasoningEffort: request.Effort, ProviderClientVersion: request.Model.ProviderClientVersion,
		DispatchConformant: true, InvocationCount: 1,
	}, nil
}

func (executor *studyWorkflowExecutor) requests() []ExecutionRequest {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return append([]ExecutionRequest(nil), executor.calls...)
}

func TestStudyReportUsesWithinCellWelchVarianceAndHolmFamily(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	for index := range selection.Tasks {
		selection.Tasks[index].Repetitions = 2
	}
	selectionID, err := hashCanonical(selection)
	if err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 2}}
	checksums := map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}
	arms := []matrixArm{
		matrixArmFixture(root, "A", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "B", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "C", ArmBaseline, model, effort, tasks),
	}
	for index := range arms {
		arms[index].ID = strings.Repeat(string(rune('a'+index)), 64)
	}
	for attempt, score := range []float64{.2, .4} {
		rewriteStudyAttempt(t, arms[0], "first-task", attempt+1, checksums["first-task"], score, 1)
	}
	for attempt, score := range []float64{.6, .8} {
		rewriteStudyAttempt(t, arms[0], "second-task", attempt+1, checksums["second-task"], score, 1)
	}
	for attempt, score := range []float64{.4, .8} {
		rewriteStudyAttempt(t, arms[1], "first-task", attempt+1, checksums["first-task"], score, 1)
	}
	for attempt, score := range []float64{.1, .3} {
		rewriteStudyAttempt(t, arms[1], "second-task", attempt+1, checksums["second-task"], score, 1)
	}
	for attempt, score := range []float64{.3, .5} {
		rewriteStudyAttempt(t, arms[2], "first-task", attempt+1, checksums["first-task"], score, 1)
	}
	for attempt, score := range []float64{.2, .6} {
		rewriteStudyAttempt(t, arms[2], "second-task", attempt+1, checksums["second-task"], score, 1)
	}
	study := preparedStudy{selection: selection, selectionID: selectionID, studyID: strings.Repeat("s", 64), experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "A"}, model: model, effort: effort, identity: "a"}, {studyExperiment: studyExperiment{Name: "B"}, model: model, effort: effort, identity: "b"}, {studyExperiment: studyExperiment{Name: "C"}, model: model, effort: effort, identity: "c"}}}
	report, _, _, err := buildStudyReport(study, matrixPreparation{selection: selection, selectionID: selectionID, stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums, environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: arms, taskConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments) != 3 || len(report.Comparisons) != 3 || report.HolmFamily.Size != 3 {
		t.Fatalf("report membership = %#v", report)
	}
	comparison := report.Comparisons[0]
	if !comparison.Available || comparison.InferenceSource != inferenceSourceObserved || comparison.Variance == nil || math.Abs(*comparison.Variance-.0048125) > 1e-12 {
		t.Fatalf("comparison = %#v, want observed variance .0048125", comparison)
	}
	if comparison.DegreesOfFreedom == nil || math.Abs(*comparison.DegreesOfFreedom-3.469645720438665) > 1e-12 || comparison.RawTwoSidedPValue == nil || comparison.HolmAdjustedPValue == nil {
		t.Fatalf("Welch/Holm df=%v raw=%v holm=%v", dereference(comparison.DegreesOfFreedom), dereference(comparison.RawTwoSidedPValue), dereference(comparison.HolmAdjustedPValue))
	}
}

func TestStudyReportDisclosesTerminalVerifierTestTimeouts(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}
	arm := matrixArmFixture(root, "Bare", ArmBaseline, model, effort, tasks)
	rewriteStudyAttempt(t, arm, "first-task", 1, checksums["first-task"], 0, .25)
	rewriteStudyAttempt(t, arm, "second-task", 1, checksums["second-task"], .5, .25)
	path := armResultPath(arm.StateDir, "first-task", 1)
	var timedOut AttemptResult
	if err := readStudyJSON(path, &timedOut); err != nil {
		t.Fatal(err)
	}
	timedOut.VerifierOutcome = verifierOutcomeTestTimeout
	if err := writeJSON(path, timedOut); err != nil {
		t.Fatal(err)
	}
	study := preparedStudy{selection: selection, studyID: strings.Repeat("s", 64), experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "Bare"}, model: model, effort: effort, identity: "bare"}}}
	report, _, _, err := buildStudyReport(study, matrixPreparation{
		selection: selection, stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums,
		environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: []matrixArm{arm}, taskConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Experiments[0].VerifierTestTimeoutRuns != 1 || report.Experiments[0].Tasks[0].VerifierTestTimeouts != 1 {
		t.Fatalf("timeout disclosure counts = %#v", report.Experiments[0])
	}
	if !containsString(report.Limitations, "1 completed run(s) exhausted the candidate test-execution timeout and were recorded explicitly as zero-score verifier outcomes.") {
		t.Fatalf("timeout limitation missing: %#v", report.Limitations)
	}
}

func TestStudyReportGatesNoncompliantComparisonsAndKeepsEligibleHolmFamily(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	for index := range selection.Tasks {
		selection.Tasks[index].Repetitions = 2
	}
	selectionID, err := hashCanonical(selection)
	if err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 2}}
	checksums := map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}
	arms := []matrixArm{
		matrixArmFixture(root, "Noncompliant", ArmTreatment, model, effort, tasks),
		matrixArmFixture(root, "Conformant", ArmTreatment, model, effort, tasks),
		matrixArmFixture(root, "Unconstrained", ArmBaseline, model, effort, tasks),
	}
	for index := range arms {
		arms[index].ID = strings.Repeat(string(rune('a'+index)), 64)
	}
	arms[0].Bundle = &TreatmentBundle{Manifest: TreatmentManifest{
		Mode: TreatmentInstructionsAndSkills, RequiredRoles: []string{requiredRoleImplementer},
	}}
	arms[1].Bundle = &TreatmentBundle{Manifest: TreatmentManifest{Mode: TreatmentInstructionsAndSkills}}
	for attempt, score := range []float64{.2, .4} {
		rewriteStudyAttemptConformance(t, arms[0], "first-task", attempt+1, checksums["first-task"], score, 1, false)
		rewriteStudyAttempt(t, arms[1], "first-task", attempt+1, checksums["first-task"], score, 1)
		rewriteStudyAttempt(t, arms[2], "first-task", attempt+1, checksums["first-task"], score, 1)
	}
	for attempt, score := range []float64{.6, .8} {
		rewriteStudyAttemptConformance(t, arms[0], "second-task", attempt+1, checksums["second-task"], score, 1, false)
		rewriteStudyAttempt(t, arms[1], "second-task", attempt+1, checksums["second-task"], score, 1)
		rewriteStudyAttempt(t, arms[2], "second-task", attempt+1, checksums["second-task"], score, 1)
	}
	study := preparedStudy{selection: selection, selectionID: selectionID, studyID: strings.Repeat("s", 64), experiments: []preparedStudyExperiment{
		{studyExperiment: studyExperiment{Name: "Noncompliant"}, model: model, effort: effort, identity: "a"},
		{studyExperiment: studyExperiment{Name: "Conformant"}, model: model, effort: effort, identity: "b"},
		{studyExperiment: studyExperiment{Name: "Unconstrained"}, model: model, effort: effort, identity: "c"},
	}}
	report, _, _, err := buildStudyReport(study, matrixPreparation{selection: selection, selectionID: selectionID, stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums, environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: arms, taskConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments) != 3 || report.Experiments[0].Score == nil || report.Experiments[1].Score == nil || report.Experiments[2].Score == nil {
		t.Fatalf("expected retained experiment scores: %#v", report.Experiments)
	}
	if report.Experiments[0].WorkflowNoncomplianceRuns != 4 || report.Experiments[0].DispatchConformantRuns != 0 {
		t.Fatalf("noncompliant counts = %#v", report.Experiments[0])
	}
	if !containsString(report.Experiments[0].ComparabilityWarnings, "Workflow-noncompliant completed runs are retained as scored evidence; statistical comparisons involving this experiment are unavailable.") {
		t.Fatalf("missing noncompliance warning: %#v", report.Experiments[0].ComparabilityWarnings)
	}
	if report.Experiments[1].WorkflowNoncomplianceRuns != 0 || report.Experiments[2].WorkflowNoncomplianceRuns != 0 {
		t.Fatalf("eligible experiments were marked noncompliant: %#v", report.Experiments)
	}
	if len(report.Comparisons) != 3 || report.HolmFamily.Size != 1 || len(report.HolmFamily.Members) != 1 || report.HolmFamily.Members[0] != "Conformant vs Unconstrained" {
		t.Fatalf("holm family = %#v comparisons = %#v", report.HolmFamily, report.Comparisons)
	}
	for _, comparison := range report.Comparisons {
		switch {
		case comparison.Left == "Conformant" && comparison.Right == "Unconstrained":
			if !comparison.Available || comparison.RawTwoSidedPValue == nil || comparison.HolmAdjustedPValue == nil || *comparison.HolmAdjustedPValue != *comparison.RawTwoSidedPValue {
				t.Fatalf("eligible comparison = %#v", comparison)
			}
		case comparison.Left == "Noncompliant" || comparison.Right == "Noncompliant":
			if comparison.Available || comparison.Difference != nil || comparison.HolmAdjustedPValue != nil || !strings.Contains(comparison.UnavailableReason, "workflow-noncompliant") {
				t.Fatalf("noncompliant comparison = %#v", comparison)
			}
		default:
			t.Fatalf("unexpected comparison = %#v", comparison)
		}
	}
}

func dereference(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

func TestStudyReportPreservesPartialAndOneExperimentWithoutInference(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	selectionID, _ := hashCanonical(selection)
	model, effort, _ := ParseModelSelection("luna:low")
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}
	arm := matrixArmFixture(root, "Only", ArmBaseline, model, effort, tasks)
	rewriteStudyAttempt(t, arm, "first-task", 1, checksums["first-task"], .2, 1)
	study := preparedStudy{selection: selection, selectionID: selectionID, studyID: strings.Repeat("p", 64), experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "Only"}, model: model, effort: effort, identity: "only"}}}
	report, jsonPath, htmlPath, err := buildStudyReport(study, matrixPreparation{selection: selection, selectionID: selectionID, stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums, environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: []matrixArm{arm}, taskConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Experiments[0].Score != nil || report.Experiments[0].Tasks[1].RepetitionsCompleted != 0 || len(report.Comparisons) != 0 || !containsString(report.Limitations, "This study declares one experiment, so there are no pairwise comparisons.") {
		t.Fatalf("partial report = %#v", report)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatal(err)
	}
}

func TestStudyProviderWarningsRequireEvidenceOnBothExperiments(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	selectionID, err := hashCanonical(selection)
	if err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	first := matrixArmFixture(root, "Observed", ArmBaseline, model, effort, tasks)
	second := matrixArmFixture(root, "Missing", ArmBaseline, model, effort, tasks)
	rewriteStudyAttempt(t, first, "first-task", 1, "first", .2, 1)
	rewriteStudyAttempt(t, first, "second-task", 1, "second", .2, 1)
	study := preparedStudy{selection: selection, selectionID: selectionID, studyID: strings.Repeat("w", 64), experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "Observed"}, model: model, effort: effort}, {studyExperiment: studyExperiment{Name: "Missing"}, model: model, effort: effort}}}
	report, _, _, err := buildStudyReport(study, matrixPreparation{selection: selection, selectionID: selectionID, stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: map[string]string{"first-task": "first", "second-task": "second"}, environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: []matrixArm{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments[0].ProviderClients) == 0 || len(report.Experiments[1].ProviderClients) != 0 || len(report.Experiments[0].ComparabilityWarnings) != 0 || len(report.Experiments[1].ComparabilityWarnings) != 0 {
		t.Fatalf("warnings without two-sided evidence = %#v", report.Experiments)
	}
}

func TestHolmAdjustmentUsesRemainingFamilyMultiplierAndCumulativeMaximum(t *testing.T) {
	p := func(value float64) *float64 { return &value }
	comparisons := []StudyComparisonReport{
		{Available: true, RawTwoSidedPValue: p(.01)},
		{Available: true, RawTwoSidedPValue: p(.04)},
		{Available: true, RawTwoSidedPValue: p(.03)},
		{Available: false},
	}
	applyHolm(comparisons)
	for index, want := range []float64{.03, .06, .06} {
		if comparisons[index].HolmAdjustedPValue == nil || math.Abs(*comparisons[index].HolmAdjustedPValue-want) > 1e-12 {
			t.Fatalf("comparison %d Holm = %v, want %v", index, dereference(comparisons[index].HolmAdjustedPValue), want)
		}
	}
}

func TestStudyEvidenceRejectsWrongEnvironmentAndCorruption(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	arm := matrixArmFixture(root, "evidence", ArmBaseline, model, effort, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}})
	rewriteStudyAttempt(t, arm, "first-task", 1, "checksum", .2, 1)
	path := armResultPath(arm.StateDir, "first-task", 1)
	if _, err := readStudyResult(path, "first-task", 1, "checksum", "wrong-environment", arm); err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("wrong environment error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readStudyResult(path, "first-task", 1, "checksum", "env-1", arm); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("corrupt evidence error = %v", err)
	}
}

func TestStudyProgressTreatsExistingBrokenReceiptAsActionableNotMissing(t *testing.T) {
	root := t.TempDir()
	model, effort, _ := ParseModelSelection("luna:low")
	arm := matrixArmFixture(root, "strict", ArmBaseline, model, effort, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}})
	arm.IgnoreProviderClientInManifest = true
	rewriteStudyAttempt(t, arm, "first-task", 1, "checksum", .2, 1)
	if err := os.Remove(filepath.Join(filepath.Dir(armResultPath(arm.StateDir, "first-task", 1)), "artifacts", strings.Repeat("e", 32), "execution-receipt.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectStudyCell(arm, "first-task", 1, "checksum", "env-1"); err == nil {
		t.Fatal("inspector accepted a result without its receipt")
	}
	_, err := studyProgressChecked(matrixPreparation{selectionID: "selection", tasks: []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}, checksums: map[string]string{"first-task": "checksum"}, environments: map[string]string{"first-task": "env-1"}, arms: []matrixArm{arm}})
	if err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("broken receipt was treated as missing: %v", err)
	}
}

func TestStudyReuseSurvivesCompositionAndDisplayNameChanges(t *testing.T) {
	repository := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selectionID := strings.Repeat("s", 64)
	armID := strings.Repeat("a", 64)
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": "checksum"}
	environments := map[string]string{"first-task": "env-1"}
	loaded := loadedBenchmarkPlan{Model: model, Effort: effort}
	loaded.Plan.Tasks = tasks
	source := matrixArm{ID: armID, Label: "Old display", Mode: ArmBaseline, StateDir: filepath.Join(repository, ".agent-layer", "state", "benchmarks", "deepswe", "studies", "old-study", "arms", armID), Loaded: loaded, AgentTimeoutMultiplier: skillsAgentTimeoutFactor, IgnoreProviderClientInManifest: true}
	if err := ensureStudyArmManifest(selectionID, tasks, checksums, &source); err != nil {
		t.Fatal(err)
	}
	var manifest studyArmManifest
	if err := readStudyJSON(filepath.Join(source.StateDir, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != studyArmManifestSchema || manifest.SchemaVersion == matrixManifestSchema {
		t.Fatalf("study arm manifest schema = %q", manifest.SchemaVersion)
	}
	rewriteStudyAttempt(t, source, "first-task", 1, checksums["first-task"], .5, 1)
	destination := source
	destination.Label = "Renamed display"
	destination.StateDir = filepath.Join(repository, ".agent-layer", "state", "benchmarks", "deepswe", "studies", "new-composition", "arms", armID)
	if err := ensureStudyArmManifest(selectionID, tasks, checksums, &destination); err != nil {
		t.Fatal(err)
	}
	if err := reuseMatchingStudyEvidence(repository, selectionID, tasks, checksums, environments, &destination); err != nil {
		t.Fatal(err)
	}
	state, _, err := inspectStudyCell(destination, "first-task", 1, checksums["first-task"], environments["first-task"])
	if err != nil || state != studyCellValid {
		t.Fatalf("reused cell = %v, %v", state, err)
	}
}

func TestHistoricalTreatmentMultiplierUsesPinnedExecutedContract(t *testing.T) {
	root := t.TempDir()
	selectionID := strings.Repeat("s", 64)
	sourceDir := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID, "arms", "old")
	pinDir := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID, "treatment-pins", "pin")
	if err := os.MkdirAll(pinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const treatmentHash = "treatment-hash"
	if err := writeJSON(filepath.Join(pinDir, "pin.json"), historicalMatrixTreatmentPin{ManifestHash: treatmentHash, Manifest: TreatmentManifest{AgentTimeoutMultiplier: skillsAgentTimeoutFactor}}); err != nil {
		t.Fatal(err)
	}
	if got := historicalTreatmentMultiplier(sourceDir, treatmentHash); got != skillsAgentTimeoutFactor {
		t.Fatalf("historical multiplier = %v", got)
	}
}

func TestLegacyMatrixArmManifestIsReadOnlyRecoveryInput(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selectionID := strings.Repeat("s", 64)
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": "checksum"}
	arm := matrixArmFixture(root, "Legacy", ArmBaseline, model, effort, tasks)
	arm.AgentTimeoutMultiplier = skillsAgentTimeoutFactor
	legacyRoot := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID, "arms", "legacy")
	legacy := matrixArmManifest{SchemaVersion: matrixManifestSchema, SelectionID: selectionID, Label: arm.Label, Mode: arm.Mode, Model: model.PublishedIdentifier, Reasoning: effort, TaskChecksums: checksums, Repetitions: repetitionsForTasks(tasks), AgentTimeoutMultiplier: skillsAgentTimeoutFactor}
	if err := writeJSON(filepath.Join(legacyRoot, "manifest.json"), legacy); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := reusableLegacyMatrixArmManifest(legacyRoot)
	if err != nil || !found || !compatibleLegacyMatrixArmManifest(loaded, selectionID, tasks, checksums, &arm, legacyRoot) {
		t.Fatalf("legacy manifest recovery = %#v, %t, %v", loaded, found, err)
	}
	studyManifest, found, err := reusableStudyArmManifest(legacyRoot)
	if err != nil || !found {
		t.Fatalf("study manifest reader = found=%t err=%v", found, err)
	}
	if compatibleStudyArmManifest(studyManifest, selectionID, tasks, checksums, &arm) {
		t.Fatal("legacy matrix schema was accepted as a study manifest")
	}
}

func TestBareCustomAdapterIdentityAndHistoricalManifestBoundary(t *testing.T) {
	grok, effort, err := ParseModelSelection(modelGrok45 + ":minimal")
	if err != nil {
		t.Fatal(err)
	}
	wantAdapter, err := embeddedPierAdapterSHA256()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := executionAdapterSHA256(grok, nil); err != nil || got != wantAdapter {
		t.Fatalf("bare Grok adapter hash = %q, %v", got, err)
	}
	luna, _, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := executionAdapterSHA256(luna, nil); err != nil || got != "" {
		t.Fatalf("native bare adapter hash = %q, %v", got, err)
	}

	prepared := preparedStudy{experiments: []preparedStudyExperiment{{model: grok, effort: effort}}}
	matching := immutableStudyManifest{Arms: []studyArmContract{{Adapter: wantAdapter}}}
	if !studyManifestBundleMatches(matching, &prepared, []*TreatmentBundle{nil}) {
		t.Fatal("matching bare custom adapter was rejected")
	}
	matching.Arms[0].Adapter = ""
	if studyManifestBundleMatches(matching, &prepared, []*TreatmentBundle{nil}) {
		t.Fatal("adapter-less historical manifest matched a bare custom arm")
	}
	if bundle := historicalTreatmentBundle(studyArmContract{Adapter: wantAdapter}); bundle != nil {
		t.Fatalf("bare custom adapter was reconstructed as treatment: %#v", bundle)
	}
}

func rewriteStudyAttempt(t *testing.T, arm matrixArm, task string, attempt int, checksum string, score, cost float64) {
	t.Helper()
	rewriteStudyAttemptConformance(t, arm, task, attempt, checksum, score, cost, true)
}

func rewriteStudyAttemptConformance(t *testing.T, arm matrixArm, task string, attempt int, checksum string, score, cost float64, dispatchConformant bool) {
	t.Helper()
	duration := 1.0
	now := time.Now().UTC()
	environment := "env-1"
	if task == "second-task" {
		environment = "env-2"
	}
	result := AttemptResult{SchemaVersion: StorageSchemaVersion, EventID: strings.Repeat("e", 32), Attempt: attempt, Task: task, Status: statusSuccess, F2PPassed: int(math.Round(score * 10)), F2PTotal: 10, F2PScore: score, CostUSD: &cost, CostKind: costKindProviderReported, DurationSeconds: &duration, TaskChecksum: checksum, EnvironmentIdentity: environment, StartedAt: now, FinishedAt: now, Provider: arm.Loaded.Model.Adapter, PublishedModel: arm.Loaded.Model.PublishedIdentifier, RuntimeModel: arm.Loaded.Model.RuntimeIdentifier, ReasoningEffort: arm.Loaded.Effort, ProviderClientVersion: arm.Loaded.Model.ProviderClientVersion, DispatchConformant: dispatchConformant, InvocationCount: 1}
	if err := writeJSON(armResultPath(arm.StateDir, task, attempt), result); err != nil {
		t.Fatal(err)
	}
	receipt := pierExecutionReceipt{SchemaVersion: pierExecutionReceiptSchema, EventID: result.EventID, Attempt: attempt, Task: task, TaskChecksum: checksum, EnvironmentIdentity: environment, Arm: arm.Mode, RuntimeModel: arm.Loaded.Model.RuntimeIdentifier, ReasoningEffort: arm.Loaded.Effort, CompletedAt: now, Succeeded: true, CleanupSucceeded: true, TreatmentHash: bundleManifestHash(arm.Bundle)}
	if err := writeJSON(filepath.Join(filepath.Dir(armResultPath(arm.StateDir, task, attempt)), "artifacts", result.EventID, "execution-receipt.json"), receipt); err != nil {
		t.Fatal(err)
	}
}

func TestStudyValidatesExplicitInputsAndIdentity(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	selectionPath := filepath.Join(root, "selection.json")
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectionPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[agents.codex]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"instructions", "skills"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, directory, "input.md"), []byte("input\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "entry.md"), []byte("Use skill for {{task}} exactly."), 0o600); err != nil {
		t.Fatal(err)
	}
	study := "selection = \"selection.json\"\n\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n\n[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\ninstructions = \"instructions\"\nskills = \"skills\"\nentry_prompt = \"entry.md\"\nrequired_dispatch_roles = []\n"
	studyPath := filepath.Join(root, "study.toml")
	if err := os.WriteFile(studyPath, []byte(study), 0o600); err != nil {
		t.Fatal(err)
	}
	firstPrepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: studyPath})
	if err != nil {
		t.Fatal(err)
	}
	secondPrepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: studyPath, TaskConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	if firstPrepared.studyID != "" || secondPrepared.studyID != "" || len(firstPrepared.experiments) != 2 {
		t.Fatalf("prepared study = %#v", firstPrepared)
	}
	membership := []struct{ Name, Arm string }{{Name: "Bare", Arm: strings.Repeat("a", 64)}, {Name: "Skills", Arm: strings.Repeat("b", 64)}}
	checksums := map[string]string{"first-task": "first", "second-task": "second"}
	environments := map[string]string{"first-task": "environment-one", "second-task": "environment-two"}
	first, err := identifyStudy(firstPrepared.selectionID, membership, checksums, environments)
	if err != nil {
		t.Fatal(err)
	}
	second, err := identifyStudy(secondPrepared.selectionID, membership, checksums, environments)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("production v3 study identity changed without an input change: %q != %q", first, second)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	bundles := []*TreatmentBundle{nil, {
		ManifestHash:      strings.Repeat("c", 64),
		AdapterSHA256:     strings.Repeat("d", 64),
		LinuxBinarySHA256: strings.Repeat("e", 64),
	}}
	firstBinding, err := bindStudyPreparation(root, &firstPrepared, tasks, checksums, environments, bundles, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	secondBinding, err := bindStudyPreparation(root, &secondPrepared, tasks, checksums, environments, bundles, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if firstPrepared.studyID != secondPrepared.studyID || firstBinding.stateDir != secondBinding.stateDir {
		t.Fatalf("task concurrency changed bound study identity: %q != %q", firstPrepared.studyID, secondPrepared.studyID)
	}
	for index := range firstBinding.arms {
		if firstBinding.arms[index].ID != secondBinding.arms[index].ID {
			t.Fatalf("task concurrency changed arm %d identity: %q != %q", index, firstBinding.arms[index].ID, secondBinding.arms[index].ID)
		}
	}
	environments["first-task"] = "environment-two"
	changed, err := identifyStudy(firstPrepared.selectionID, membership, checksums, environments)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("production v3 study identity ignored its certified environments")
	}
	unconstrained := firstPrepared.experiments[1]
	legacyIdentity, err := hashCanonical(struct {
		Schema    string            `json:"schema"`
		Model     string            `json:"model"`
		Reasoning string            `json:"reasoning"`
		Resources map[string]any    `json:"resources"`
		Inputs    map[string]string `json:"inputs"`
	}{
		Schema: "deepswe-benchmark-experiment-v1", Model: unconstrained.model.PublishedIdentifier, Reasoning: unconstrained.effort,
		Resources: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor},
		Inputs:    unconstrained.inputHashes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unconstrained.identity != legacyIdentity {
		t.Fatal("explicit empty required_dispatch_roles changed unconstrained experiment identity")
	}
}

func TestStudyRequiredDispatchRolesParticipateInIdentity(t *testing.T) {
	writeSkillsStudy := func(t *testing.T, root, roles string) string {
		t.Helper()
		selection, err := json.Marshal(matrixSelectionFixture())
		if err != nil {
			t.Fatal(err)
		}
		writeStudyInputFixture(t, root, "selection.json", string(selection))
		writeStudySkillsInputs(t, root)
		body := "selection = \"selection.json\"\n[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = " + roles + "\n"
		path := filepath.Join(root, "study.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	emptyRoot := t.TempDir()
	emptyPrepared, err := prepareStudy(StudyOptions{RepoRoot: emptyRoot, StudyPath: writeSkillsStudy(t, emptyRoot, "[]")})
	if err != nil {
		t.Fatal(err)
	}
	defer emptyPrepared.cleanupInputs()
	reorderedRoot := t.TempDir()
	reorderedPrepared, err := prepareStudy(StudyOptions{RepoRoot: reorderedRoot, StudyPath: writeSkillsStudy(t, reorderedRoot, "[\"implementer\", \"plan-reviewer\"]")})
	if err != nil {
		t.Fatal(err)
	}
	defer reorderedPrepared.cleanupInputs()
	canonicalRoot := t.TempDir()
	canonicalPrepared, err := prepareStudy(StudyOptions{RepoRoot: canonicalRoot, StudyPath: writeSkillsStudy(t, canonicalRoot, "[\"plan-reviewer\", \"implementer\"]")})
	if err != nil {
		t.Fatal(err)
	}
	defer canonicalPrepared.cleanupInputs()
	if reorderedPrepared.experiments[0].identity != canonicalPrepared.experiments[0].identity {
		t.Fatal("required_dispatch_roles order changed experiment identity")
	}
	if emptyPrepared.experiments[0].identity == canonicalPrepared.experiments[0].identity {
		t.Fatal("nonempty required_dispatch_roles did not change experiment identity")
	}
	if got := canonicalPrepared.experiments[0].RequiredDispatchRoles; len(got) != 2 || got[0] != requiredRoleImplementer || got[1] != requiredRolePlanReviewer {
		t.Fatalf("canonical roles = %#v", got)
	}
}

func TestStudySnapshotsDeclaredInputsBeforeStagingTreatment(t *testing.T) {
	root := t.TempDir()
	selection, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selection))
	configPath := writeStudyTreatmentConfig(t, root)
	originalConfig, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	studyPath := filepath.Join(root, "study.toml")
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Treatment\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\n")
	prepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: studyPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanupInputs()
	if err := os.WriteFile(configPath, []byte("[agents.codex]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment := prepared.experiments[0]
	if hashes, err := validateExperimentInputs(experiment.inputs); err != nil || hashes["config"] != experiment.inputHashes["config"] {
		t.Fatalf("immutable report inputs changed after source mutation: %#v, %v", hashes, err)
	}
	bundle, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bundle.Root) })
	stagedConfig, err := os.ReadFile(filepath.Join(bundle.Root, ".agent-layer", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stagedConfig) != string(originalConfig) {
		t.Fatalf("treatment staging used mutated declared input\nwant: %s\ngot:  %s", originalConfig, stagedConfig)
	}
}

func TestStudyRejectsInvalidSkillPromptBeforeCalls(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	selection.SchemaVersion = 1
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "selection.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "skill.md"), []byte("skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("no placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	study := "selection = \"selection.json\"\n[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = []\n"
	path := filepath.Join(root, "study.toml")
	if err := os.WriteFile(path, []byte(study), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = prepareStudy(StudyOptions{RepoRoot: root, StudyPath: path, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "exactly one literal") {
		t.Fatalf("error = %v", err)
	}
}

func TestStudyRejectsInvalidDeclaredSchemaAndInputs(t *testing.T) {
	validSelection := func(t *testing.T, root string) {
		t.Helper()
		data, err := json.Marshal(matrixSelectionFixture())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "selection.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write := func(t *testing.T, root, body string) string {
		t.Helper()
		path := filepath.Join(root, "study.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	bare := "selection = \"selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n"
	for _, test := range []struct {
		name  string
		body  string
		setup func(*testing.T, string)
		want  string
	}{
		{"missing selection", "[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n", nil, "requires selection"},
		{"zero experiments", "selection = \"selection.json\"\n", nil, "requires selection"},
		{"duplicate names", bare + "[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n", nil, "names must be unique"},
		{"duplicate identity", bare + "[[experiments]]\nname = \"Same inputs\"\nmodel = \"luna\"\nreasoning = \"low\"\n", nil, "duplicate content-addressed"},
		{"layer requires config", bare + "[[experiments]]\nname = \"Layer\"\nmodel = \"luna\"\nreasoning = \"low\"\ninstructions = \"instructions\"\n", func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "instructions"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "require config"},
		{"skills require prompt", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nrequired_dispatch_roles = []\n", func(t *testing.T, root string) {
			writeStudyInputFixture(t, root, "config.toml", "config")
			if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "skills require entry_prompt"},
		{"prompt requires skills", bare + "[[experiments]]\nname = \"Prompt\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nentry_prompt = \"prompt.md\"\n", func(t *testing.T, root string) {
			writeStudyInputFixture(t, root, "config.toml", "config")
			writeStudyInputFixture(t, root, "prompt.md", "{{task}}")
		}, "only valid with skills"},
		{"multiple placeholders", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = []\n", func(t *testing.T, root string) {
			writeStudyInputFixture(t, root, "config.toml", "config")
			if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeStudyInputFixture(t, filepath.Join(root, "skills"), "skill.md", "skill")
			writeStudyInputFixture(t, root, "prompt.md", "{{task}} {{task}}")
		}, "exactly one literal"},
		{"absolute path", "selection = \"/tmp/selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n", nil, "paths must be relative"},
		{"escaping path", "selection = \"../selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"luna\"\nreasoning = \"low\"\n", nil, "path escapes"},
		{"missing regular input", bare + "[[experiments]]\nname = \"Config\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"missing.toml\"\n", nil, "config:"},
		{"empty input", bare + "[[experiments]]\nname = \"Config\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\n", func(t *testing.T, root string) { writeStudyInputFixture(t, root, "config.toml", "") }, "non-empty regular"},
		{"directory input", bare + "[[experiments]]\nname = \"Config\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\n", func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "config.toml"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "non-empty regular"},
		{"noncanonical model", "selection = \"selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \"Luna\"\nreasoning = \"low\"\n", nil, "invalid explicit model"},
		{"skills require dispatch roles", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\n", writeStudySkillsInputs, "skills require required_dispatch_roles"},
		{"dispatch roles require skills", bare + "[[experiments]]\nname = \"Bare roles\"\nmodel = \"luna\"\nreasoning = \"low\"\nrequired_dispatch_roles = []\n", nil, "required_dispatch_roles is only valid with skills"},
		{"dispatch roles on config-only", bare + "[[experiments]]\nname = \"Config\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nrequired_dispatch_roles = [\"implementer\"]\n", func(t *testing.T, root string) {
			writeStudyInputFixture(t, root, "config.toml", "config")
		}, "required_dispatch_roles is only valid with skills"},
		{"unknown dispatch role", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = [\"reviewer\"]\n", writeStudySkillsInputs, "unsupported required_dispatch_roles"},
		{"blank dispatch role", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = [\" \"]\n", writeStudySkillsInputs, "blank values"},
		{"duplicate dispatch role", bare + "[[experiments]]\nname = \"Skills\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\nskills = \"skills\"\nentry_prompt = \"prompt.md\"\nrequired_dispatch_roles = [\"implementer\", \"implementer\"]\n", writeStudySkillsInputs, "duplicate values"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			validSelection(t, root)
			if test.setup != nil {
				test.setup(t, root)
			}
			_, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: write(t, root, test.body)})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStudyRejectsSymlinkDeclaredInputs(t *testing.T) {
	root := t.TempDir()
	selection, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selection))
	writeStudyInputFixture(t, root, "config-target.toml", "config")
	if err := os.Symlink("config-target.toml", filepath.Join(root, "config.toml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(root, "study.toml")
	body := "selection = \"selection.json\"\n[[experiments]]\nname = \"Config\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = prepareStudy(StudyOptions{RepoRoot: root, StudyPath: path})
	if err == nil || !strings.Contains(err.Error(), "non-empty regular") {
		t.Fatalf("symlink input error = %v", err)
	}
}

func writeStudyInputFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStudySkillsInputs(t *testing.T, root string) {
	t.Helper()
	writeStudyInputFixture(t, root, "config.toml", "config")
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, filepath.Join(root, "skills"), "skill.md", "skill")
	writeStudyInputFixture(t, root, "prompt.md", "{{task}}")
}
