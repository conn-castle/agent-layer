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

func TestCampaignRunsVersionedTreatmentAndBuildsReportFromImmutableEvidence(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	addCostAxisToPlanFixture(t, planPath)
	raw, err := os.ReadFile(planPath) // #nosec G304 -- planPath is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBenchmarkPlanJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = bindBenchmarkExecution(loaded, "luna:low")
	if err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{
		"first-benchmark-task":  strings.Repeat("b", 64),
		"second-benchmark-task": strings.Repeat("c", 64),
	}
	baselineDir := baselineStateDir(root, loaded.CampaignID)
	baseline := baselineManifest{
		SchemaVersion:  baselineStateSchema,
		PlanID:         loaded.ID,
		CampaignID:     loaded.CampaignID,
		PlanSnapshot:   loaded.Plan.Snapshot.SHA256,
		Model:          loaded.Model.PublishedIdentifier,
		Reasoning:      loaded.Effort,
		ProviderClient: loaded.Model.ProviderClientVersion,
		DeepSWECommit:  DeepSWECommit,
		PierVersion:    PierVersion,
		TaskChecksums:  checksums,
		Repetitions:    repetitionsForPlan(loaded.Plan),
	}
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baseline); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		task                string
		baseline, treatment []float64
	}{
		{task: "first-benchmark-task", baseline: []float64{.2, .4}, treatment: []float64{.5, .7}},
		{task: "second-benchmark-task", baseline: []float64{.6, .8}, treatment: []float64{.7, .9}},
	} {
		for index := range item.baseline {
			writeCampaignAttemptFixture(t, baselineDir, loaded, item.task, index+1, item.baseline[index], .1, 0, 0, true, false, checksums[item.task])
		}
	}
	originalPreflight := preflightBenchmark
	originalPier := verifyBenchmarkPier
	originalAuth := validateBenchmarkAuthentication
	originalBundle := buildCampaignTreatmentBundle
	var bundledModel Model
	var bundledEffort string
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(string, []parsedSelection) error { return nil }
	buildCampaignTreatmentBundle = func(_ string, _ string, mode string, model Model, effort string) (*TreatmentBundle, error) {
		bundledModel = model
		bundledEffort = effort
		return &TreatmentBundle{
			Root:          t.TempDir(),
			ManifestHash:  strings.Repeat("d", 64),
			AdapterSHA256: strings.Repeat("e", 64),
			Manifest: TreatmentManifest{
				SchemaVersion:          TreatmentSchemaVersion,
				Mode:                   mode,
				AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
				RequiredRoles:          []string{requiredRolePlanReviewer, requiredRoleImplementer, requiredRoleCodeReviewer},
			},
		}, nil
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalPier
		validateBenchmarkAuthentication = originalAuth
		buildCampaignTreatmentBundle = originalBundle
	})

	options := TreatmentOptions{
		RepoRoot: root, PlanPath: planPath, Execution: "luna:low",
		Label: "Iteration 1", TaskConcurrency: 2,
	}
	baseline.ProviderClient = "stale-codex"
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckTreatment(context.Background(), options); err == nil ||
		!strings.Contains(err.Error(), "baseline provider client version") {
		t.Fatalf("stale provider client error = %v", err)
	}
	baseline.ProviderClient = loaded.Model.ProviderClientVersion
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baseline); err != nil {
		t.Fatal(err)
	}
	checked, err := CheckTreatment(context.Background(), options)
	if err != nil || checked.Missing != loaded.RunCount {
		t.Fatalf("CheckTreatment = %#v, %v", checked, err)
	}
	if bundledModel.PublishedIdentifier != publishedLuna || bundledEffort != effortLow {
		t.Fatalf("treatment used calibration as execution: %s %s", bundledModel.PublishedIdentifier, bundledEffort)
	}
	if _, err := os.Stat(filepath.Join(checked.StateDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("read-only treatment check wrote state: %v", err)
	}
	pending, err := RunTreatment(context.Background(), options, &baselineFakeExecutor{})
	if !errors.Is(err, ErrConfirmationRequired) || pending.Missing != loaded.RunCount {
		t.Fatalf("unconfirmed treatment = %#v, %v", pending, err)
	}
	if _, err := os.Stat(filepath.Join(checked.StateDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed treatment wrote state: %v", err)
	}
	options.Confirmed = true
	executor := &baselineFakeExecutor{}
	completed, err := RunTreatment(context.Background(), options, executor)
	if err != nil || completed.Missing != 0 || completed.Completed != loaded.RunCount || !completed.ProviderCall {
		t.Fatalf("completed treatment = %#v, %v", completed, err)
	}
	cached, err := CheckTreatment(context.Background(), options)
	if err != nil || cached.Missing != 0 || cached.Completed != loaded.RunCount ||
		cached.Label != options.Label || cached.ProviderCall {
		t.Fatalf("cached treatment check = %#v, %v", cached, err)
	}
	baseline.SchemaVersion = providerlessBaselineSchema
	baseline.ProviderClient = ""
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckTreatment(context.Background(), options); err == nil ||
		!strings.Contains(err.Error(), "remains reportable but cannot seed") {
		t.Fatalf("providerless baseline treatment error = %v", err)
	}
	var incomplete campaignTreatmentManifest
	if err := readCampaignJSON(filepath.Join(checked.StateDir, "manifest.json"), &incomplete); err != nil {
		t.Fatal(err)
	}
	incomplete.Label = "Incomplete iteration"
	incomplete.CreatedAt = incomplete.CreatedAt.Add(time.Minute)
	incompleteDir := filepath.Join(campaignRoot(root, loaded.CampaignID), "treatments", strings.Repeat("f", 64))
	if err := writeJSON(filepath.Join(incompleteDir, "manifest.json"), incomplete); err != nil {
		t.Fatal(err)
	}

	outcome, err := BuildCampaignReport(CampaignReportOptions{
		RepoRoot: root, PlanPath: planPath, Execution: "luna:low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Versions != 1 || outcome.Report.ObservedCampaign == nil ||
		len(outcome.Report.ObservedCampaign.Versions) != 1 ||
		outcome.Report.ObservedCampaign.Versions[0].Label != "Iteration 1" ||
		len(outcome.SkippedTreatments) != 1 ||
		len(outcome.Report.ObservedCampaign.Warnings) != 1 {
		t.Fatalf("campaign report = %#v", outcome)
	}
	for _, path := range []string{outcome.JSONPath, outcome.HTMLPath, outcome.Analyses[0]} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("campaign artifact %s = %v, %v", path, info, err)
		}
	}
	html, err := os.ReadFile(outcome.HTMLPath) // #nosec G304 -- path is produced in this test's temporary repository.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `Skipped incomplete treatment &#34;Incomplete iteration&#34;`) {
		t.Fatalf("report omitted incomplete-treatment warning: %s", html)
	}
}

func TestBuildCampaignReportReusesSchemaVersionOneEvidence(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "legacy-plan.json")
	writeLegacyBenchmarkPlanFixture(t, planPath, benchmarkPlanSchema)
	loaded, err := loadBenchmarkPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{
		"first-benchmark-task":  strings.Repeat("b", 64),
		"second-benchmark-task": strings.Repeat("c", 64),
	}
	baselineDir := baselineStateDir(root, loaded.ID)
	baseline := baselineManifest{
		SchemaVersion: legacyBaselineStateSchema,
		PlanID:        loaded.ID,
		PlanSnapshot:  loaded.Plan.Snapshot.SHA256,
		Model:         loaded.Model.PublishedIdentifier,
		Reasoning:     loaded.Effort,
		DeepSWECommit: DeepSWECommit,
		PierVersion:   PierVersion,
		TaskChecksums: checksums,
		Repetitions:   repetitionsForPlan(loaded.Plan),
	}
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baseline); err != nil {
		t.Fatal(err)
	}
	treatmentDir := filepath.Join(campaignRoot(root, loaded.ID), "treatments", strings.Repeat("d", 64))
	treatment := campaignTreatmentManifest{
		SchemaVersion: legacyCampaignTreatmentSchema,
		PlanID:        loaded.ID,
		CreatedAt:     time.Date(2026, 7, 27, 15, 37, 0, 0, time.UTC),
		Label:         "Legacy iteration",
		Arm:           ArmTreatment,
		Model:         loaded.Model.PublishedIdentifier,
		Reasoning:     loaded.Effort,
		TaskChecksums: checksums,
		Repetitions:   repetitionsForPlan(loaded.Plan),
		Treatment: TreatmentManifest{
			SchemaVersion:          TreatmentSchemaVersion,
			Mode:                   TreatmentInstructionsAndSkills,
			AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
		},
	}
	if err := writeJSON(filepath.Join(treatmentDir, "manifest.json"), treatment); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		task                string
		baseline, treatment []float64
	}{
		{task: "first-benchmark-task", baseline: []float64{.2, .4}, treatment: []float64{.5, .7}},
		{task: "second-benchmark-task", baseline: []float64{.6, .8}, treatment: []float64{.7, .9}},
	} {
		for index := range item.baseline {
			writeCampaignAttemptFixture(t, baselineDir, loaded, item.task, index+1, item.baseline[index], .1, .09, .11, true, false, checksums[item.task])
			writeCampaignAttemptFixture(t, treatmentDir, loaded, item.task, index+1, item.treatment[index], .2, .19, .21, true, false, checksums[item.task])
		}
	}

	outcome, err := BuildCampaignReport(CampaignReportOptions{RepoRoot: root, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.PlanID != loaded.ID || outcome.CampaignID != loaded.ID ||
		outcome.Execution != "luna:medium" || outcome.Versions != 1 ||
		outcome.Report.ComparisonID != loaded.ID ||
		outcome.Report.ObservedCampaign == nil ||
		outcome.Report.ObservedCampaign.CalibrationReference != "gpt-5-6-luna::medium" ||
		outcome.Report.ObservedCampaign.CalibrationContrast != "gpt-5-6-luna::high" {
		t.Fatalf("legacy report = %#v", outcome)
	}
	roundTrip, err := BuildCampaignReport(CampaignReportOptions{
		RepoRoot: root, PlanPath: planPath,
		LegacyAnalysisPaths: outcome.Analyses,
	})
	if err != nil || roundTrip.Report.ComparisonID != loaded.ID {
		t.Fatalf("current legacy analysis round trip = %#v, %v", roundTrip, err)
	}
	analysisData, err := os.ReadFile(outcome.Analyses[0]) // #nosec G304 -- the report generated this path in the test repository.
	if err != nil {
		t.Fatal(err)
	}
	var oldAnalysis map[string]any
	if err := json.Unmarshal(analysisData, &oldAnalysis); err != nil {
		t.Fatal(err)
	}
	oldAnalysis["schemaVersion"] = float64(legacyObservedAnalysisSchemaVersion)
	delete(oldAnalysis, "campaignId")
	experiment := oldAnalysis["experiment"].(map[string]any)
	delete(experiment, "calibrationReference")
	delete(experiment, "calibrationContrast")

	analysisOnlyPlanPath := filepath.Join(root, "legacy-analysis-only-plan.json")
	writeLegacyBenchmarkPlanFixture(t, analysisOnlyPlanPath, legacyBenchmarkPlanSchema)
	analysisOnlyPlanData, err := os.ReadFile(analysisOnlyPlanPath) // #nosec G304 -- path belongs to the test repository.
	if err != nil {
		t.Fatal(err)
	}
	var analysisOnlyPlan map[string]any
	if err := json.Unmarshal(analysisOnlyPlanData, &analysisOnlyPlan); err != nil {
		t.Fatal(err)
	}
	delete(analysisOnlyPlan, "costAxis")
	analysisOnlyPlanData, err = json.Marshal(analysisOnlyPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(analysisOnlyPlanPath, analysisOnlyPlanData, 0o600); err != nil {
		t.Fatal(err)
	}
	analysisOnlyLoaded, err := loadBenchmarkPlan(analysisOnlyPlanPath)
	if err != nil {
		t.Fatal(err)
	}
	oldAnalysis["planId"] = analysisOnlyLoaded.ID
	oldAnalysisData, err := json.Marshal(oldAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	oldAnalysisPath := filepath.Join(root, "legacy-analysis.json")
	if err := os.WriteFile(oldAnalysisPath, oldAnalysisData, 0o600); err != nil {
		t.Fatal(err)
	}
	analysisOnlyOutcome, err := BuildCampaignReport(CampaignReportOptions{
		RepoRoot: root, PlanPath: analysisOnlyPlanPath,
		LegacyAnalysisPaths: []string{oldAnalysisPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysisOnlyOutcome.Report.ComparisonID != analysisOnlyLoaded.ID ||
		analysisOnlyOutcome.Report.ObservedCampaign == nil ||
		analysisOnlyOutcome.Report.ObservedCampaign.CampaignID != analysisOnlyLoaded.ID {
		t.Fatalf("upgraded legacy analysis report = %#v", analysisOnlyOutcome)
	}
	if _, err := RunTreatment(context.Background(), TreatmentOptions{
		RepoRoot: root, PlanPath: planPath, Execution: "luna:medium",
	}, nil); err == nil || !strings.Contains(err.Error(), "report-only") {
		t.Fatalf("legacy treatment execution error = %v", err)
	}
}

func TestAnalyzeCampaignVersionDerivesThresholdAndCostsFromArmEvidence(t *testing.T) {
	root := t.TempDir()
	baselineDir := filepath.Join(root, "baseline")
	treatmentDir := filepath.Join(root, "treatment")
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	plan := benchmarkPlan{}
	plan.CalibrationReference.ID = "gpt-5-6-luna::medium"
	plan.CalibrationContrast.ID = "gpt-5-6-luna::high"
	plan.Parameters.TwoSidedSignificanceLevel = .05
	plan.CostAxis = &benchmarkPlanCostAxis{
		Valid: true, Scale: "logarithmic", ReferenceConfiguration: observedCostAxisReference,
		ReferenceSnapshotSHA256: strings.Repeat("a", 64), ReferenceEstimatedArmCostUSD: 310.61,
		RoundingIncrementUSD: observedCostAxisRoundingUSD, MaximumUSD: 350,
	}
	task := benchmarkPlanTask{ID: "campaign-task", RepetitionsPerArm: 2}
	plan.Tasks = []benchmarkPlanTask{task}
	loaded := loadedBenchmarkPlan{
		ID: strings.Repeat("b", 64), CampaignID: strings.Repeat("d", 64),
		Plan: plan, Model: model, Effort: effort, RunCount: 2,
	}
	checksums := map[string]string{task.ID: strings.Repeat("c", 64)}
	baseline := baselineManifest{TaskChecksums: checksums, Repetitions: map[string]int{task.ID: 2}}
	treatment := campaignTreatmentManifest{
		CreatedAt: time.Date(2026, 7, 27, 15, 37, 0, 0, time.UTC),
		Label:     "Iteration 1",
		Treatment: TreatmentManifest{AgentTimeoutMultiplier: skillsAgentTimeoutFactor},
	}
	writeCampaignAttemptFixture(t, baselineDir, loaded, task.ID, 1, .4, 1, 1, 0, true, true, checksums[task.ID])
	writeCampaignAttemptFixture(t, baselineDir, loaded, task.ID, 2, .6, 1, 1, 0, true, false, checksums[task.ID])
	writeCampaignAttemptFixture(t, treatmentDir, loaded, task.ID, 1, .7, 2, 1.9, 2.1, true, false, checksums[task.ID])
	writeCampaignAttemptFixture(t, treatmentDir, loaded, task.ID, 2, .8, 3, 2.9, 3.1, false, true, checksums[task.ID])

	document, err := analyzeCampaignVersion(loaded, baselineDir, baseline, treatmentDir, treatment)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(document.Result.BaselineMean-.5) > 1e-12 ||
		math.Abs(document.Result.TreatmentMean-.75) > 1e-12 ||
		math.Abs(document.Result.StandardError-math.Sqrt(.0125)) > 1e-12 ||
		document.CostUSD.Baseline.Midpoint != 2 ||
		document.CostUSD.Treatment.Midpoint != 5 ||
		document.CostUSD.Treatment.Minimum != 4.8 ||
		document.CostUSD.Treatment.Maximum != 5.2 ||
		document.TreatmentDiagnostics.InvocationCount != 2 ||
		document.TreatmentDiagnostics.DispatchConformantRuns != 1 ||
		document.Experiment.AgentTimeoutMultiplier != skillsAgentTimeoutFactor ||
		document.Tasks[0].BaselineVerifierBuildFailedRuns != 1 ||
		document.Tasks[0].TreatmentVerifierBuildFailedRuns != 1 {
		t.Fatalf("unexpected derived analysis: %#v", document)
	}
	if math.Abs(studentTCritical(.05, 9.5532538268779)-2.242343579751151) > 1e-12 {
		t.Fatalf("Student t implementation no longer matches the completed campaign")
	}
}

func TestBuildCampaignReportRejectsMissingCostAxis(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	data, err := os.ReadFile(planPath) // #nosec G304 -- planPath is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	delete(plan, "costAxis")
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = BuildCampaignReport(CampaignReportOptions{
		RepoRoot: root, PlanPath: planPath, Execution: "luna:low",
	})
	if err == nil || !strings.Contains(err.Error(), "no cost-axis provenance") {
		t.Fatalf("missing cost-axis error = %v", err)
	}
}

func TestBuildCampaignReportRequiresExecutionForSchemaVersionTwo(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	if _, err := BuildCampaignReport(CampaignReportOptions{
		RepoRoot: root, PlanPath: planPath,
	}); err == nil || !strings.Contains(err.Error(), "require --execution") {
		t.Fatalf("missing schema version 2 execution error = %v", err)
	}
}

func addCostAxisToPlanFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["costAxis"] = map[string]any{
		"valid": true, "scale": "logarithmic",
		"referenceConfiguration":       observedCostAxisReference,
		"referenceSnapshotSha256":      strings.Repeat("a", 64),
		"referenceEstimatedArmCostUsd": 310.61,
		"roundingIncrementUsd":         observedCostAxisRoundingUSD,
		"maximumUsd":                   350.0,
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCampaignAttemptFixture(t *testing.T, root string, loaded loadedBenchmarkPlan, task string, attempt int, score, midpoint, minimum, maximum float64, conformant, verifierBuildFailed bool, checksum string) {
	t.Helper()
	duration := 1.0
	passed := int(math.Round(score * 10))
	result := AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: strings.Repeat("e", 16), Attempt: attempt,
		Task: task, Status: statusSuccess, F2PPassed: passed, F2PTotal: 10, F2PScore: score,
		CostUSD: &midpoint, CostKind: costKindProviderReported, DurationSeconds: &duration,
		TaskChecksum: checksum, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		Provider: loaded.Model.Adapter, PublishedModel: loaded.Model.PublishedIdentifier,
		RuntimeModel: loaded.Model.RuntimeIdentifier, ReasoningEffort: loaded.Effort,
		ProviderClientVersion: loaded.Model.ProviderClientVersion, DispatchConformant: conformant,
		VerifierBuildFailed: verifierBuildFailed,
	}
	if maximum > 0 {
		result.CostKind = costKindProviderUsage + "-range"
		result.CostMinUSD = &minimum
		result.CostMaxUSD = &maximum
		result.InvocationCount = 1
		zero := 0.0
		result.CoordinatorCostUSD = &midpoint
		result.CoordinatorCostMinUSD = &minimum
		result.CoordinatorCostMaxUSD = &maximum
		result.ChildCostUSD = &zero
		result.ChildCostMinUSD = &zero
		result.ChildCostMaxUSD = &zero
	}
	if err := writeJSON(armResultPath(root, task, attempt), result); err != nil {
		t.Fatal(err)
	}
}
