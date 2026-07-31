package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMatrixSelectionAcceptsOneRunAndUsesCanonicalIdentity(t *testing.T) {
	selection := matrixSelectionFixture()
	compact, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	indented, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	first, firstID, err := loadMatrixSelection("selection.json", compact)
	if err != nil {
		t.Fatal(err)
	}
	second, secondID, err := loadMatrixSelection("selection.json", indented)
	if err != nil {
		t.Fatal(err)
	}
	if firstID != secondID || len(first.Tasks) != 2 || len(second.Tasks) != 2 {
		t.Fatalf("selection identities differ: %q != %q", firstID, secondID)
	}
	const fixtureID = "62ab8876c8a70c14c3994401243eece6e6e807aa39629e42952b84adaa5bc8b0"
	if firstID != fixtureID {
		t.Fatalf("selection identity = %q, want %q", firstID, fixtureID)
	}
	if first.Tasks[0].Repetitions != 1 {
		t.Fatalf("repetitions = %d, want 1", first.Tasks[0].Repetitions)
	}

	selection.Tasks[0].Weight = .5
	invalid, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadMatrixSelection("selection.json", invalid); err == nil ||
		!strings.Contains(err.Error(), "weights or costs do not reconcile") {
		t.Fatalf("invalid weight error = %v", err)
	}
}

func TestExecuteMatrixReturnsParentCancellation(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}
	arms := []matrixArm{
		matrixArmFixture(root, "bare", ArmBaseline, model, effort, tasks),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = executeMatrix(
		ctx, root, map[string]string{"first-task": "first-checksum"},
		arms, nil, 1, &baselineFakeExecutor{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("matrix cancellation = %v, want context canceled", err)
	}
}

func TestExecuteMatrixRunsEachArmOnceAndReusesEvidence(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{
		{ID: "first-task", RepetitionsPerArm: 1},
		{ID: "second-task", RepetitionsPerArm: 1},
	}
	arms := []matrixArm{
		matrixArmFixture(root, "bare", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "agent-layer", ArmTreatment, model, effort, tasks),
	}
	checksums := map[string]string{
		"first-task": "first-checksum", "second-task": "second-checksum",
	}
	executor := &baselineFakeExecutor{}

	if err := executeMatrix(
		context.Background(), root, checksums, arms, nil, 2, executor,
	); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(executor.calls))
	}
	for index := range arms {
		execution := armExecution{
			stateDir: arms[index].StateDir, arm: arms[index].Mode,
			loaded: arms[index].Loaded, checksums: checksums,
		}
		if missing := missingPlanCells(execution); len(missing) != 0 {
			t.Fatalf("%s missing = %#v", arms[index].Label, missing)
		}
	}

	if err := executeMatrix(
		context.Background(), root, checksums, arms, nil, 2, executor,
	); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("cached execution made %d total calls, want 4", len(executor.calls))
	}
}

func TestExecuteMatrixFiltersTasks(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{
		{ID: "first-task", RepetitionsPerArm: 1},
		{ID: "second-task", RepetitionsPerArm: 1},
	}
	arms := []matrixArm{
		matrixArmFixture(root, "bare", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "agent-layer", ArmTreatment, model, effort, tasks),
	}
	checksums := map[string]string{
		"first-task": "first-checksum", "second-task": "second-checksum",
	}
	executor := &baselineFakeExecutor{}

	if err := executeMatrix(
		context.Background(), root, checksums, arms, []string{"second-task"}, 2, executor,
	); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(executor.calls))
	}
	for _, call := range executor.calls {
		if call.Task != "second-task" {
			t.Fatalf("executed task = %q, want second-task", call.Task)
		}
	}
}

func TestBuildMatrixReportUsesFixedCalibrationsAndWeights(t *testing.T) {
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
	tasks := []benchmarkPlanTask{
		{ID: "first-task", RepetitionsPerArm: 1},
		{ID: "second-task", RepetitionsPerArm: 1},
	}
	checksums := map[string]string{
		"first-task": "first-checksum", "second-task": "second-checksum",
	}
	arms := []matrixArm{
		matrixArmFixture(root, "Bare luna low", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "Agent Layer luna low", ArmTreatment, model, effort, tasks),
	}
	writeMatrixAttempt(t, &arms[0], "first-task", checksums["first-task"], .2, 1)
	writeMatrixAttempt(t, &arms[0], "second-task", checksums["second-task"], .6, 2)
	writeMatrixAttempt(t, &arms[1], "first-task", checksums["first-task"], .4, 3)
	writeMatrixAttempt(t, &arms[1], "second-task", checksums["second-task"], .8, 4)
	preparation := matrixPreparation{
		selection: selection, selectionID: selectionID,
		stateDir: filepath.Join(root, "matrix"), tasks: tasks,
		checksums: checksums, arms: arms, cleanup: func() {},
	}

	outcome, err := buildMatrixReport(preparation)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Report == nil || len(outcome.Report.Arms) != 2 {
		t.Fatalf("report = %#v", outcome.Report)
	}
	// .25*(.1+.8*.2) + .75*(.2+.5*.6)
	if math.Abs(outcome.Report.Arms[0].Score-.44) > 1e-12 {
		t.Fatalf("baseline score = %.12f, want .44", outcome.Report.Arms[0].Score)
	}
	// .25*(.1+.8*.4) + .75*(.2+.5*.8)
	if math.Abs(outcome.Report.Arms[1].Score-.555) > 1e-12 {
		t.Fatalf("treatment score = %.12f, want .555", outcome.Report.Arms[1].Score)
	}
	if outcome.Report.Arms[0].Cost.Midpoint != 3 ||
		outcome.Report.Arms[1].Cost.Midpoint != 7 {
		t.Fatalf("costs = %#v", outcome.Report.Arms)
	}
	html, err := os.ReadFile(outcome.HTMLPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "Bare luna low") ||
		!strings.Contains(string(html), "Agent Layer luna low") ||
		!strings.Contains(string(html), "no local run-to-run confidence interval") {
		t.Fatalf("HTML report is missing descriptive matrix evidence")
	}
}

func TestBuildMatrixReportSupportsBaselineOnly(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	selectionID, err := hashCanonical(selection)
	if err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:high")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{
		{ID: "first-task", RepetitionsPerArm: 1},
		{ID: "second-task", RepetitionsPerArm: 1},
	}
	checksums := map[string]string{
		"first-task": "first-checksum", "second-task": "second-checksum",
	}
	arm := matrixArmFixture(root, "Bare luna high", ArmBaseline, model, effort, tasks)
	writeMatrixAttempt(t, &arm, "first-task", checksums["first-task"], .2, 1)
	writeMatrixAttempt(t, &arm, "second-task", checksums["second-task"], .6, 2)
	preparation := matrixPreparation{
		selection: selection, selectionID: selectionID,
		stateDir: filepath.Join(root, "matrix"), tasks: tasks,
		checksums: checksums, arms: []matrixArm{arm}, cleanup: func() {},
	}

	outcome, err := buildMatrixReport(preparation)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Report == nil || len(outcome.Report.Arms) != 1 ||
		outcome.Report.Arms[0].Label != "Bare luna high" {
		t.Fatalf("report = %#v", outcome.Report)
	}
}

func matrixSelectionFixture() matrixSelection {
	var selection matrixSelection
	selection.Schema = matrixSelectionSchema
	selection.SchemaVersion = matrixSelectionSchemaVersion
	selection.Snapshot.URL = DeepSWETrialsSourceURL
	selection.Snapshot.SHA256 = strings.Repeat("a", 64)
	selection.Selector.Model = publishedLuna
	selection.Selector.Reasoning = effortLow
	selection.Selector.BudgetUSD = .2
	selection.Selector.IterationsPerTask = 1
	selection.EstimatedPublishedSpendUSD = .2
	selection.Tasks = []matrixSelectionTask{
		{
			ID: "first-task", Repetitions: 1, Weight: .25,
			Calibration: struct {
				Intercept float64 `json:"intercept"`
				Slope     float64 `json:"slope"`
			}{Intercept: .1, Slope: .8},
			PublishedMeanCostUSD: .1,
		},
		{
			ID: "second-task", Repetitions: 1, Weight: .75,
			Calibration: struct {
				Intercept float64 `json:"intercept"`
				Slope     float64 `json:"slope"`
			}{Intercept: .2, Slope: .5},
			PublishedMeanCostUSD: .1,
		},
	}
	return selection
}

func matrixArmFixture(
	root, label, mode string,
	model Model,
	effort string,
	tasks []benchmarkPlanTask,
) matrixArm {
	loaded := loadedBenchmarkPlan{
		ID: strings.Repeat("b", 64), CampaignID: strings.Repeat("b", 64),
		Model: model, Effort: effort, RunCount: len(tasks),
	}
	loaded.Plan.Tasks = append([]benchmarkPlanTask(nil), tasks...)
	return matrixArm{
		ID: strings.Repeat("c", 64), Label: label, Mode: mode,
		StateDir: filepath.Join(root, strings.ReplaceAll(label, " ", "-")),
		Loaded:   loaded,
	}
}

func writeMatrixAttempt(
	t *testing.T,
	arm *matrixArm,
	task, checksum string,
	score, cost float64,
) {
	t.Helper()
	duration := 1.0
	started := time.Now().UTC()
	result := AttemptResult{
		SchemaVersion: StorageSchemaVersion,
		EventID:       strings.Repeat("e", 32), Attempt: 1, Task: task,
		Status: statusSuccess, F2PPassed: int(math.Round(score * 10)),
		F2PTotal: 10, F2PScore: score,
		CostUSD: &cost, CostKind: costKindProviderReported,
		DurationSeconds: &duration, TaskChecksum: checksum,
		StartedAt: started, FinishedAt: started.Add(time.Second),
		Provider:              arm.Loaded.Model.Adapter,
		PublishedModel:        arm.Loaded.Model.PublishedIdentifier,
		RuntimeModel:          arm.Loaded.Model.RuntimeIdentifier,
		ReasoningEffort:       arm.Loaded.Effort,
		ProviderClientVersion: arm.Loaded.Model.ProviderClientVersion,
		DispatchConformant:    true, InvocationCount: 1,
	}
	if err := writeJSON(armResultPath(arm.StateDir, task, 1), result); err != nil {
		t.Fatal(err)
	}
}
