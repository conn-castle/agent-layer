package benchmark

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStudyReportSchemaV2OneRepetitionLeavesInferenceUnavailable(t *testing.T) {
	report := twoArmStudyReport(t, matrixSelectionFixture(), map[string][]float64{
		"first-task":  {.8},
		"second-task": {.4},
	}, map[string][]float64{
		"first-task":  {.2},
		"second-task": {.6},
	}, Model{}, "")
	if len(report.Comparisons) != 1 || report.Comparisons[0].Available || report.Comparisons[0].InferenceSource != "" || report.Comparisons[0].HolmAdjustedPValue != nil {
		t.Fatalf("schema v2 comparison = %#v", report.Comparisons)
	}
	if !strings.Contains(report.Comparisons[0].UnavailableReason, "requires at least two completed repetitions") {
		t.Fatalf("unavailable reason = %q", report.Comparisons[0].UnavailableReason)
	}
}

func TestStudyReportOneRepetitionUsesPublishedProxy(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.SampleVariance = .16
	selection.Tasks[1].PublishedVariance.SampleVariance = .09
	report := twoArmStudyReport(t, selection, map[string][]float64{
		"first-task":  {.8},
		"second-task": {.4},
	}, map[string][]float64{
		"first-task":  {.2},
		"second-task": {.6},
	}, Model{}, "")
	if len(report.Comparisons) != 1 {
		t.Fatalf("comparisons = %#v", report.Comparisons)
	}
	comparison := report.Comparisons[0]
	if !comparison.Available || comparison.InferenceSource != inferenceSourcePublishedProxy || comparison.PublishedProxy == nil {
		t.Fatalf("proxy comparison = %#v", comparison)
	}
	if comparison.PublishedProxy.ConfigurationID != publishedLuna+"::"+effortLow || comparison.PublishedProxy.VarianceEstimator != publishedVarianceEstimator || comparison.PublishedProxy.VarianceDenominator != publishedVarianceDenominator || comparison.PublishedProxy.SnapshotSHA != selection.Snapshot.SHA256 {
		t.Fatalf("proxy provenance = %#v", comparison.PublishedProxy)
	}
	if comparison.HolmAdjustedPValue == nil || *comparison.HolmAdjustedPValue != *comparison.RawTwoSidedPValue {
		t.Fatalf("holm adjustment = %#v", comparison)
	}
	if !containsString(report.Limitations, "published_proxy inference estimates uncertainty from pinned published DeepSWE runs, not from the executed repetitions in this study.") {
		t.Fatalf("missing proxy limitation: %#v", report.Limitations)
	}
	firstCombined := (.25 * .8) * (.25 * .8) * .16 * 2
	secondCombined := (.75 * .5) * (.75 * .5) * .09 * 2
	wantVariance := firstCombined + secondCombined
	if comparison.Variance == nil || math.Abs(*comparison.Variance-wantVariance) > 1e-12 {
		t.Fatalf("proxy variance = %v, want %v", dereference(comparison.Variance), wantVariance)
	}
	wantDF := wantVariance * wantVariance / ((firstCombined*firstCombined + secondCombined*secondCombined) / 3)
	if comparison.DegreesOfFreedom == nil || math.Abs(*comparison.DegreesOfFreedom-wantDF) > 1e-12 {
		t.Fatalf("proxy df = %v, want %v", dereference(comparison.DegreesOfFreedom), wantDF)
	}
}

func TestStudyReportObservedInferenceSupersedesPublishedProxy(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	for index := range selection.Tasks {
		selection.Tasks[index].Repetitions = 2
	}
	report := twoArmStudyReport(t, selection, map[string][]float64{
		"first-task":  {.2, .4},
		"second-task": {.6, .8},
	}, map[string][]float64{
		"first-task":  {.4, .8},
		"second-task": {.1, .3},
	}, Model{}, "")
	if len(report.Comparisons) != 1 || report.Comparisons[0].InferenceSource != inferenceSourceObserved || report.Comparisons[0].PublishedProxy != nil {
		t.Fatalf("observed should supersede proxy: %#v", report.Comparisons)
	}
	if math.Abs(*report.Comparisons[0].Variance-.0048125) > 1e-12 {
		t.Fatalf("observed variance = %v", dereference(report.Comparisons[0].Variance))
	}
}

func TestStudyReportPublishedProxyAllowsZeroTaskVariance(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.SampleVariance = 0
	report := twoArmStudyReport(t, selection, map[string][]float64{
		"first-task":  {.8},
		"second-task": {.4},
	}, map[string][]float64{
		"first-task":  {.2},
		"second-task": {.6},
	}, Model{}, "")
	comparison := report.Comparisons[0]
	if !comparison.Available || comparison.InferenceSource != inferenceSourcePublishedProxy {
		t.Fatalf("zero-task-variance comparison = %#v", comparison)
	}
	wantVariance := (.75 * .5) * (.75 * .5) * .04 * 2
	if comparison.Variance == nil || math.Abs(*comparison.Variance-wantVariance) > 1e-12 {
		t.Fatalf("variance = %v, want %v", dereference(comparison.Variance), wantVariance)
	}
}

func TestStudyReportMismatchWarningsAppearInJSONAndHTML(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	sol, medium, err := ParseModelSelection("sol:medium")
	if err != nil {
		t.Fatal(err)
	}
	report := twoArmStudyReport(t, selection, map[string][]float64{
		"first-task":  {.8},
		"second-task": {.4},
	}, map[string][]float64{
		"first-task":  {.2},
		"second-task": {.6},
	}, sol, medium)
	want := "Selector/executed mismatch: executed model/reasoning is gpt-5-6-sol/medium; selector published evidence is from gpt-5-6-luna/low."
	if !containsString(report.Experiments[1].ComparabilityWarnings, want) {
		t.Fatalf("missing mismatch warning: %#v", report.Experiments[1].ComparabilityWarnings)
	}
	if containsString(report.Experiments[0].ComparabilityWarnings, "Selector/executed mismatch:") {
		t.Fatalf("matching arm was warned: %#v", report.Experiments[0].ComparabilityWarnings)
	}
	html, err := renderStudyReportHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	document := string(html)
	for _, needle := range []string{
		"Inference warning",
		"published_proxy inference estimates uncertainty from pinned published DeepSWE runs, not from the executed repetitions in this study.",
		want,
		"published_proxy",
		`data-inference-source="published_proxy"`,
	} {
		if !strings.Contains(document, needle) {
			t.Errorf("HTML missing %q", needle)
		}
	}
}

func TestStudyReportPublishedProxyKeepsHolmFamily(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": "first-checksum", "second-task": "second-checksum"}
	arms := []matrixArm{
		matrixArmFixture(root, "A", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "B", ArmBaseline, model, effort, tasks),
		matrixArmFixture(root, "C", ArmBaseline, model, effort, tasks),
	}
	rewriteStudyAttempt(t, arms[0], "first-task", 1, checksums["first-task"], .9, 1)
	rewriteStudyAttempt(t, arms[0], "second-task", 1, checksums["second-task"], .9, 1)
	rewriteStudyAttempt(t, arms[1], "first-task", 1, checksums["first-task"], .1, 1)
	rewriteStudyAttempt(t, arms[1], "second-task", 1, checksums["second-task"], .1, 1)
	rewriteStudyAttempt(t, arms[2], "first-task", 1, checksums["first-task"], .5, 1)
	rewriteStudyAttempt(t, arms[2], "second-task", 1, checksums["second-task"], .5, 1)
	report, _, _, err := buildStudyReport(preparedStudy{
		selection: selection, selectionID: "selection", studyID: strings.Repeat("p", 64),
		experiments: []preparedStudyExperiment{
			{studyExperiment: studyExperiment{Name: "A"}, model: model, effort: effort, identity: "a"},
			{studyExperiment: studyExperiment{Name: "B"}, model: model, effort: effort, identity: "b"},
			{studyExperiment: studyExperiment{Name: "C"}, model: model, effort: effort, identity: "c"},
		},
	}, matrixPreparation{
		selection: selection, selectionID: "selection", stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums,
		environments: map[string]string{"first-task": "env-1", "second-task": "env-2"}, arms: arms, taskConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Comparisons) != 3 || report.HolmFamily.Size != 3 {
		t.Fatalf("holm family = %#v comparisons = %#v", report.HolmFamily, report.Comparisons)
	}
	for _, comparison := range report.Comparisons {
		if !comparison.Available || comparison.InferenceSource != inferenceSourcePublishedProxy || comparison.HolmAdjustedPValue == nil {
			t.Fatalf("proxy holm comparison = %#v", comparison)
		}
	}
	adjusted := []float64{*report.Comparisons[0].HolmAdjustedPValue, *report.Comparisons[1].HolmAdjustedPValue, *report.Comparisons[2].HolmAdjustedPValue}
	if adjusted[0] < *report.Comparisons[0].RawTwoSidedPValue-1e-15 {
		t.Fatalf("holm did not adjust: %#v", report.Comparisons)
	}
}

func TestPublishedProxySharedVarianceMatchesApprovedGrokSensitivity(t *testing.T) {
	difference := 0.10657576471650215
	variance := 0.0011256732652892227
	standardError := math.Sqrt(variance)
	df := 8.059427612307923
	statistic := math.Abs(difference) / standardError
	p := studentTwoSidedP(statistic, df)
	if math.Abs(standardError-0.03355105460770529) > 1e-15 {
		t.Fatalf("standard error = %v", standardError)
	}
	if math.Abs(statistic-3.17652502917229) > 1e-12 {
		t.Fatalf("statistic = %v", statistic)
	}
	if math.Abs(p-0.012941455976861844) > 1e-15 {
		t.Fatalf("p-value = %v", p)
	}

	left, right := grokSensitivityTasks()
	moments, reason := publishedProxyMoments(left, right)
	if reason != "" {
		t.Fatal(reason)
	}
	if math.Abs(moments.variance-variance) > 1e-15 {
		t.Fatalf("shared variance = %v, want %v", moments.variance, variance)
	}
	gotDF := moments.variance * moments.variance / moments.denominator
	if math.Abs(gotDF-df) > 1e-12 {
		t.Fatalf("shared df = %v, want %v", gotDF, df)
	}
	independent := independentPublishedMoments(left, right)
	independentDF := independent.variance * independent.variance / independent.denominator
	independentP := studentTwoSidedP(math.Abs(difference)/math.Sqrt(independent.variance), independentDF)
	if math.Abs(independent.variance-variance) > 1e-15 {
		t.Fatalf("independent treatment changed total variance: %v", independent.variance)
	}
	if math.Abs(independentP-0.005816) > 5e-6 {
		t.Fatalf("independent p-value = %v, want about 0.005816", independentP)
	}
	if independentP >= p {
		t.Fatalf("independent p-value %v was not smaller than shared p-value %v", independentP, p)
	}
}

func grokSensitivityTasks() ([]StudyTaskReport, []StudyTaskReport) {
	// Reconstruct three n=4 published cells whose shared-proxy combined
	// contributions match the approved Grok one-repetition moments.
	variance := 0.0011256732652892227
	df := 8.059427612307923
	sumSquares := 3 * variance * variance / df
	// Solve 2a² + (V-2a)² = S2 for a two-equal-plus-one split.
	a := (4*variance - math.Sqrt(16*variance*variance-24*(variance*variance-sumSquares))) / 12
	combined := []float64{a, a, variance - 2*a}
	left := make([]StudyTaskReport, len(combined))
	right := make([]StudyTaskReport, len(combined))
	for index, value := range combined {
		evidence := &StudyPublishedVariance{
			ConfigurationID: deepSWEPublishedGrok46 + "::" + effortHigh, PublishedModel: deepSWEPublishedGrok46, PublishedReasoning: effortHigh,
			SampleSize: 4, SampleVariance: value / 2, VarianceEstimator: publishedVarianceEstimator, VarianceDenominator: publishedVarianceDenominator,
		}
		left[index] = StudyTaskReport{Task: "task-" + strings.Repeat("a", index+1), RepetitionsCompleted: 1, EffectiveCoefficient: 1, PublishedVariance: evidence}
		copied := *evidence
		right[index] = StudyTaskReport{Task: left[index].Task, RepetitionsCompleted: 1, EffectiveCoefficient: 1, PublishedVariance: &copied}
	}
	return left, right
}

func independentPublishedMoments(left, right []StudyTaskReport) pairwiseMoments {
	var moments pairwiseMoments
	for index := range left {
		a, b := left[index], right[index]
		coefficient := a.EffectiveCoefficient
		for _, repetitions := range []int{a.RepetitionsCompleted, b.RepetitionsCompleted} {
			component := coefficient * coefficient * a.PublishedVariance.SampleVariance / float64(repetitions)
			moments.variance += component
			moments.denominator += component * component / float64(a.PublishedVariance.SampleSize-1)
		}
	}
	return moments
}

func twoArmStudyReport(t *testing.T, selection matrixSelection, leftScores, rightScores map[string][]float64, rightModel Model, rightEffort string) StudyReport {
	t.Helper()
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	if rightModel.PublishedIdentifier == "" {
		rightModel, rightEffort = model, effort
	}
	tasks := make([]benchmarkPlanTask, 0, len(selection.Tasks))
	checksums := map[string]string{}
	environments := map[string]string{}
	for _, selected := range selection.Tasks {
		tasks = append(tasks, benchmarkPlanTask{ID: selected.ID, RepetitionsPerArm: selected.Repetitions})
		checksums[selected.ID] = selected.ID + "-checksum"
		if selected.ID == "second-task" {
			environments[selected.ID] = "env-2"
		} else {
			environments[selected.ID] = "env-1"
		}
	}
	left := matrixArmFixture(root, "Bare", ArmBaseline, model, effort, tasks)
	right := matrixArmFixture(root, "Agent Layer", ArmBaseline, rightModel, rightEffort, tasks)
	writeArmScores(t, left, leftScores, checksums)
	writeArmScores(t, right, rightScores, checksums)
	report, _, _, err := buildStudyReport(preparedStudy{
		selection: selection, selectionID: "selection", studyID: strings.Repeat("s", 64),
		experiments: []preparedStudyExperiment{
			{studyExperiment: studyExperiment{Name: "Bare"}, model: model, effort: effort, identity: "left"},
			{studyExperiment: studyExperiment{Name: "Agent Layer"}, model: rightModel, effort: rightEffort, identity: "right"},
		},
	}, matrixPreparation{
		selection: selection, selectionID: "selection", stateDir: filepath.Join(root, "study"), tasks: tasks, checksums: checksums,
		environments: environments, arms: []matrixArm{left, right}, taskConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func writeArmScores(t *testing.T, arm matrixArm, scores map[string][]float64, checksums map[string]string) {
	t.Helper()
	for task, values := range scores {
		for attempt, score := range values {
			rewriteStudyAttempt(t, arm, task, attempt+1, checksums[task], score, 1)
		}
	}
}

func TestCastleForgeGrok46PublishedProxyReport(t *testing.T) {
	studyDir := filepath.Join("..", "..", ".agent-layer", "tmp", "grok46-published-proxy", "study-ca0dcfd9")
	selectionPath := filepath.Join("..", "..", ".agent-layer", "tmp", "grok46-published-proxy", "selection.json")
	if _, err := os.Stat(filepath.Join(studyDir, "study-manifest.json")); err != nil {
		t.Skip("castle-forge grok study snapshot is not present")
	}
	selection, selectionID, err := loadMatrixSelection(selectionPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selection.SchemaVersion != matrixSelectionSchemaVersion {
		t.Fatalf("selection schema = %d", selection.SchemaVersion)
	}
	raw, err := os.ReadFile(filepath.Join(studyDir, "study-manifest.json")) // #nosec G304 -- local optional snapshot under .agent-layer/tmp.
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		StudyID                   string            `json:"study_id"`
		TaskChecksums             map[string]string `json:"task_checksums"`
		TaskEnvironmentIdentities map[string]string `json:"task_environment_identities"`
		Arms                      []struct {
			Name               string `json:"name"`
			ID                 string `json:"id"`
			BundleManifestHash string `json:"bundle_manifest_hash"`
		} `json:"arms"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("grok-4.6:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := make([]benchmarkPlanTask, 0, len(selection.Tasks))
	for _, task := range selection.Tasks {
		tasks = append(tasks, benchmarkPlanTask{ID: task.ID, RepetitionsPerArm: task.Repetitions})
	}
	arms := make([]matrixArm, 0, len(manifest.Arms))
	experiments := make([]preparedStudyExperiment, 0, len(manifest.Arms))
	for _, contract := range manifest.Arms {
		mode := ArmBaseline
		var bundle *TreatmentBundle
		if contract.BundleManifestHash != "" {
			mode = ArmTreatment
			bundle = &TreatmentBundle{ManifestHash: contract.BundleManifestHash}
		}
		loaded := loadedBenchmarkPlan{Model: model, Effort: effort}
		loaded.Plan.Tasks = append([]benchmarkPlanTask(nil), tasks...)
		arm := matrixArm{
			ID: contract.ID, Label: contract.Name, Mode: mode,
			StateDir: filepath.Join(studyDir, "arms", contract.ID), Loaded: loaded, Bundle: bundle,
		}
		arms = append(arms, arm)
		experiments = append(experiments, preparedStudyExperiment{
			studyExperiment: studyExperiment{Name: contract.Name}, model: model, effort: effort, identity: contract.ID,
		})
	}
	outDir := filepath.Join(filepath.Dir(studyDir), "generated-report")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	report, jsonPath, htmlPath, err := buildStudyReport(preparedStudy{
		selection: selection, selectionID: selectionID, studyID: manifest.StudyID,
		experiments: experiments,
	}, matrixPreparation{
		selection: selection, selectionID: selectionID, stateDir: outDir, tasks: tasks,
		checksums: manifest.TaskChecksums, environments: manifest.TaskEnvironmentIdentities,
		arms: arms, taskConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Comparisons) != 1 || !report.Comparisons[0].Available || report.Comparisons[0].InferenceSource != inferenceSourcePublishedProxy {
		t.Fatalf("generated comparison = %#v", report.Comparisons)
	}
	comparison := report.Comparisons[0]
	if math.Abs(*comparison.Difference+0.10657576471650215) > 1e-12 {
		t.Fatalf("difference = %v, want bare minus agent-layer", *comparison.Difference)
	}
	if math.Abs(*comparison.Variance-0.0011256732652892227) > 1e-15 {
		t.Fatalf("variance = %v", *comparison.Variance)
	}
	if math.Abs(*comparison.DegreesOfFreedom-8.059427612307923) > 1e-12 {
		t.Fatalf("df = %v", *comparison.DegreesOfFreedom)
	}
	if math.Abs(*comparison.RawTwoSidedPValue-0.012941455976861844) > 1e-15 {
		t.Fatalf("p = %v", *comparison.RawTwoSidedPValue)
	}
	t.Logf("wrote %s and %s p=%v source=%s", jsonPath, htmlPath, *comparison.RawTwoSidedPValue, comparison.InferenceSource)
}
