package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

func bundleManifestHash(bundle *TreatmentBundle) string {
	if bundle == nil {
		return ""
	}
	return bundle.ManifestHash
}

const (
	studyReportSchema = "deepswe-benchmark-study-report-v1"
	studyReportCLI    = "Agent Layer (al)"
)

// StudyReport is deliberately independent from MatrixReport.  A study is the
// public reproducibility boundary: it records exactly its declared experiments
// and every task in its fixed selection, including cells that are still missing.
type StudyReport struct {
	SchemaVersion string                   `json:"schema_version"`
	StudyID       string                   `json:"study_id"`
	SelectionID   string                   `json:"selection_id"`
	GeneratedAt   time.Time                `json:"generated_at"`
	Execution     StudyExecutionProvenance `json:"execution"`
	Selection     StudySelectionProvenance `json:"selection"`
	Experiments   []StudyExperimentReport  `json:"experiments"`
	Comparisons   []StudyComparisonReport  `json:"comparisons"`
	HolmFamily    StudyHolmFamily          `json:"holm_family"`
	Limitations   []string                 `json:"limitations"`
}

// StudyExecutionProvenance records the public runner and pinned evaluation harness.
type StudyExecutionProvenance struct {
	Command                   string `json:"reproduction_command"`
	CLI                       string `json:"cli"`
	Harness                   string `json:"harness"`
	HarnessVersion            string `json:"harness_version"`
	TaskConcurrency           int    `json:"task_concurrency"`
	TaskContainerArchitecture string `json:"task_container_architecture"`
}

// StudySelectionProvenance records the immutable selection inputs used by a study.
type StudySelectionProvenance struct {
	Model         string            `json:"model"`
	Reasoning     string            `json:"reasoning"`
	SnapshotURL   string            `json:"snapshot_url"`
	SnapshotSHA   string            `json:"snapshot_sha256"`
	TaskChecksums map[string]string `json:"task_checksums"`
	Environments  map[string]string `json:"task_environment_identities"`
}

// StudyExperimentReport records one content-addressed experiment and its cells.
type StudyExperimentReport struct {
	Name                      string                   `json:"name"`
	Identity                  string                   `json:"identity"`
	Model                     string                   `json:"model"`
	Reasoning                 string                   `json:"reasoning"`
	InputHashes               map[string]string        `json:"immutable_inputs"`
	ResourceContract          map[string]any           `json:"resource_contract"`
	ProviderClients           []string                 `json:"provider_client_versions"`
	AuthenticationPreflight   *AuthenticationPreflight `json:"authentication_preflight,omitempty"`
	WorkerCountObserved       int                      `json:"observed_worker_count"`
	CompletedCells            int                      `json:"completed_cells"`
	RequiredCells             int                      `json:"required_cells"`
	Score                     *float64                 `json:"calibrated_score,omitempty"`
	ObservedCost              ObservedCostRange        `json:"observed_cost"`
	InvocationCount           int                      `json:"invocation_count"`
	DispatchConformantRuns    int                      `json:"dispatch_conformant_runs"`
	WorkflowNoncomplianceRuns int                      `json:"workflow_noncompliance_runs"`
	VerifierTestTimeoutRuns   int                      `json:"verifier_test_timeout_runs"`
	Tasks                     []StudyTaskReport        `json:"tasks"`
	BundleManifest            *TreatmentManifest       `json:"immutable_bundle_manifest,omitempty"`
	LinuxBinarySHA256         string                   `json:"linux_binary_sha256,omitempty"`
	AdapterSHA256             string                   `json:"adapter_sha256,omitempty"`
	SourceCommit              string                   `json:"source_commit,omitempty"`
	SourceDirty               bool                     `json:"source_dirty,omitempty"`
	ComparabilityWarnings     []string                 `json:"comparability_warnings"`
}

// AuthenticationPreflight is invocation provenance for a successful provider
// credential check. It is not part of study, arm, or treatment identity.
type AuthenticationPreflight struct {
	Provider             string    `json:"provider"`
	Check                string    `json:"check"`
	AuthenticationMethod string    `json:"authentication_method"`
	VerifiedAt           time.Time `json:"verified_at"`
}

// StudyTaskReport records one selected task's repetitions and score evidence.
type StudyTaskReport struct {
	Task                 string                  `json:"task"`
	RepetitionsRequired  int                     `json:"repetitions_required"`
	RepetitionsCompleted int                     `json:"repetitions_completed"`
	F2PMean              *float64                `json:"f2p_mean,omitempty"`
	SampleVariance       *float64                `json:"sample_variance,omitempty"`
	CalibrationIntercept float64                 `json:"calibration_intercept"`
	CalibrationSlope     float64                 `json:"calibration_slope"`
	Weight               float64                 `json:"weight"`
	EffectiveCoefficient float64                 `json:"effective_coefficient"`
	CalibratedMean       *float64                `json:"calibrated_mean,omitempty"`
	WeightedContribution *float64                `json:"weighted_contribution,omitempty"`
	ObservedCost         ObservedCostRange       `json:"observed_cost"`
	VerifierTestTimeouts int                     `json:"verifier_test_timeouts"`
	MissingAttempts      []int                   `json:"missing_attempts,omitempty"`
	PublishedVariance    *StudyPublishedVariance `json:"published_variance,omitempty"`
}

// StudyPublishedVariance is pinned published-run evidence copied from a
// schema-v3 selection. Report generation never fetches planner data.
type StudyPublishedVariance struct {
	ConfigurationID     string  `json:"configuration_id"`
	PublishedModel      string  `json:"published_model"`
	PublishedReasoning  string  `json:"published_reasoning"`
	SampleSize          int     `json:"sample_size"`
	SampleVariance      float64 `json:"sample_variance"`
	VarianceEstimator   string  `json:"variance_estimator"`
	VarianceDenominator string  `json:"variance_denominator"`
}

const (
	inferenceSourceObserved       = "observed"
	inferenceSourcePublishedProxy = "published_proxy"
)

// StudyComparisonReport records the fixed-selection comparison of two experiments.
type StudyComparisonReport struct {
	Left               string                         `json:"left"`
	Right              string                         `json:"right"`
	Available          bool                           `json:"available"`
	UnavailableReason  string                         `json:"unavailable_reason,omitempty"`
	InferenceSource    string                         `json:"inference_source,omitempty"`
	PublishedProxy     *StudyPublishedProxyProvenance `json:"published_proxy,omitempty"`
	Difference         *float64                       `json:"difference,omitempty"`
	Variance           *float64                       `json:"variance,omitempty"`
	StandardError      *float64                       `json:"standard_error,omitempty"`
	DegreesOfFreedom   *float64                       `json:"degrees_of_freedom,omitempty"`
	Statistic          *float64                       `json:"statistic,omitempty"`
	RawTwoSidedPValue  *float64                       `json:"raw_two_sided_p_value,omitempty"`
	HolmAdjustedPValue *float64                       `json:"holm_adjusted_p_value,omitempty"`
}

// StudyPublishedProxyProvenance records the pinned published evidence used for
// published_proxy pairwise inference.
type StudyPublishedProxyProvenance struct {
	ConfigurationID     string `json:"configuration_id"`
	PublishedModel      string `json:"published_model"`
	PublishedReasoning  string `json:"published_reasoning"`
	VarianceEstimator   string `json:"variance_estimator"`
	VarianceDenominator string `json:"variance_denominator"`
	SnapshotSHA         string `json:"snapshot_sha256"`
}

// StudyHolmFamily describes the multiple-comparison adjustment applied to a study.
type StudyHolmFamily struct {
	Method  string   `json:"method"`
	Size    int      `json:"size"`
	Members []string `json:"members"`
}

func buildStudyReport(study preparedStudy, preparation matrixPreparation) (StudyReport, string, string, error) {
	report := StudyReport{
		SchemaVersion: studyReportSchema, StudyID: study.studyID, SelectionID: study.selectionID,
		GeneratedAt: time.Now().UTC(),
		Execution: StudyExecutionProvenance{
			Command: fmt.Sprintf("al benchmark run <study.toml> --task-concurrency %d", preparation.taskConcurrency),
			CLI:     studyReportCLI, Harness: "DataCurve Pier", HarnessVersion: PierVersion,
			TaskConcurrency: preparation.taskConcurrency, TaskContainerArchitecture: benchmarkTaskContainerArchitecture,
		},
		Selection: StudySelectionProvenance{Model: study.selection.Selector.Model, Reasoning: study.selection.Selector.Reasoning,
			SnapshotURL: study.selection.Snapshot.URL, SnapshotSHA: study.selection.Snapshot.SHA256,
			TaskChecksums: copyStringMap(preparation.checksums), Environments: copyStringMap(preparation.environments)},
		HolmFamily: StudyHolmFamily{Method: "Holm step-down adjustment over available unique experiment pairs"},
		Limitations: []string{
			studyReportRepetitionLimitation(study.selection),
			"Inference is conditional on this fixed selected task allocation, calibration slopes, and normalized weights. The selected tasks are not a random sample, so cross-task dispersion is not substituted for run variance.",
			"Published selector evidence is a planning anchor. The selected task is part of its full-configuration target, which induces a small part-whole correlation bias.",
		},
	}
	for index, experiment := range study.experiments {
		arm := preparation.arms[index]
		item, err := buildStudyExperimentReport(experiment, arm, study.selection, preparation, preparation.taskConcurrency)
		if err != nil {
			return StudyReport{}, "", "", err
		}
		report.Experiments = append(report.Experiments, item)
	}
	testTimeoutRuns := 0
	for _, experiment := range report.Experiments {
		testTimeoutRuns += experiment.VerifierTestTimeoutRuns
	}
	if testTimeoutRuns > 0 {
		report.Limitations = append(report.Limitations, fmt.Sprintf(
			"%d completed run(s) exhausted the candidate test-execution timeout and were recorded explicitly as zero-score verifier outcomes.",
			testTimeoutRuns,
		))
	}
	if len(report.Experiments) == 1 {
		report.Limitations = append(report.Limitations, "This study declares one experiment, so there are no pairwise comparisons.")
	}
	for i := range report.Experiments {
		for j := i + 1; j < len(report.Experiments); j++ {
			if len(report.Experiments[i].ProviderClients) > 0 && len(report.Experiments[j].ProviderClients) > 0 &&
				strings.Join(report.Experiments[i].ProviderClients, "\x00") != strings.Join(report.Experiments[j].ProviderClients, "\x00") {
				warning := fmt.Sprintf("Provider client provenance differs from experiment %q; comparisons remain descriptive across this runtime difference.", report.Experiments[j].Name)
				report.Experiments[i].ComparabilityWarnings = append(report.Experiments[i].ComparabilityWarnings, warning)
				warning = fmt.Sprintf("Provider client provenance differs from experiment %q; comparisons remain descriptive across this runtime difference.", report.Experiments[i].Name)
				report.Experiments[j].ComparabilityWarnings = append(report.Experiments[j].ComparabilityWarnings, warning)
			}
		}
	}
	report.Comparisons = buildStudyComparisons(&report)
	if usesPublishedProxy(report.Comparisons) {
		report.Limitations = append([]string{
			"published_proxy inference estimates uncertainty from pinned published DeepSWE runs, not from the executed repetitions in this study.",
		}, report.Limitations...)
	}
	members := make([]string, 0)
	for _, comparison := range report.Comparisons {
		if comparison.Available {
			members = append(members, comparison.Left+" vs "+comparison.Right)
		}
	}
	report.HolmFamily.Size, report.HolmFamily.Members = len(members), members
	applyHolm(report.Comparisons)
	reportDir := filepath.Join(preparation.stateDir, "report")
	jsonPath, htmlPath := filepath.Join(reportDir, "report.json"), filepath.Join(reportDir, "report.html")
	if err := writeJSON(jsonPath, report); err != nil {
		return StudyReport{}, "", "", err
	}
	htmlDocument, err := renderStudyReportHTML(report)
	if err != nil {
		return StudyReport{}, "", "", err
	}
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		return StudyReport{}, "", "", err
	}
	if err := fsutil.WriteFileAtomic(htmlPath, htmlDocument, 0o600); err != nil {
		return StudyReport{}, "", "", err
	}
	return report, jsonPath, htmlPath, nil
}

func buildStudyExperimentReport(experiment preparedStudyExperiment, arm matrixArm, selection matrixSelection, preparation matrixPreparation, workers int) (StudyExperimentReport, error) {
	item := StudyExperimentReport{Name: experiment.Name, Identity: experiment.identity, Model: experiment.model.PublishedIdentifier, Reasoning: experiment.effort,
		InputHashes: copyStringMap(experiment.inputHashes), ResourceContract: studyResourceContract(), AdapterSHA256: arm.AdapterSHA256}
	if evidence, ok := preparation.authentication[experiment.model.Adapter]; ok {
		evidence.VerifiedAt = evidence.VerifiedAt.UTC()
		item.AuthenticationPreflight = &evidence
	}
	if arm.Bundle != nil {
		manifest := arm.Bundle.Manifest
		item.BundleManifest = &manifest
		item.LinuxBinarySHA256 = arm.Bundle.LinuxBinarySHA256
		item.SourceCommit = arm.Bundle.TemplatesCommit
		item.SourceDirty = arm.Bundle.TemplatesDirty
	}
	clientSet := map[string]bool{}
	complete := true
	for _, selected := range selection.Tasks {
		task := StudyTaskReport{Task: selected.ID, RepetitionsRequired: selected.Repetitions, CalibrationIntercept: selected.Calibration.Intercept, CalibrationSlope: selected.Calibration.Slope, Weight: selected.Weight, EffectiveCoefficient: selected.Weight * selected.Calibration.Slope, PublishedVariance: studyPublishedVariance(selected.PublishedVariance)}
		var scores []float64
		for attempt := 1; attempt <= selected.Repetitions; attempt++ {
			state, result, err := inspectStudyCell(arm, selected.ID, attempt, preparation.checksums[selected.ID], preparation.environments[selected.ID])
			if err != nil {
				return item, fmt.Errorf("read immutable evidence for %s repetition %d: %w", selected.ID, attempt, err)
			}
			if state == studyCellMissing {
				task.MissingAttempts = append(task.MissingAttempts, attempt)
				complete = false
				continue
			}
			scores = append(scores, result.F2PScore)
			task.RepetitionsCompleted++
			item.CompletedCells++
			clientSet[result.ProviderClientVersion] = true
			item.InvocationCount += result.InvocationCount
			if result.InvocationWorkers > item.WorkerCountObserved {
				item.WorkerCountObserved = result.InvocationWorkers
			}
			if result.DispatchConformant {
				item.DispatchConformantRuns++
			}
			if !result.DispatchConformant && arm.Bundle != nil {
				item.WorkflowNoncomplianceRuns++
			}
			if result.VerifierOutcome == verifierOutcomeTestTimeout {
				task.VerifierTestTimeouts++
				item.VerifierTestTimeoutRuns++
			}
			minimum, maximum, costErr := result.CostBounds()
			if costErr != nil {
				return item, costErr
			}
			task.ObservedCost.Midpoint += *result.CostUSD
			task.ObservedCost.Minimum += minimum
			task.ObservedCost.Maximum += maximum
		}
		item.RequiredCells += selected.Repetitions
		if len(scores) > 0 {
			mean := average(scores)
			task.F2PMean = &mean
			calibrated := selected.Calibration.Intercept + selected.Calibration.Slope*mean
			task.CalibratedMean = &calibrated
			contribution := selected.Weight * calibrated
			task.WeightedContribution = &contribution
			if len(scores) > 1 {
				variance := sampleVariance(scores)
				task.SampleVariance = &variance
			}
			item.ObservedCost = addObservedCost(item.ObservedCost, task.ObservedCost)
		}
		item.Tasks = append(item.Tasks, task)
	}
	// Old evidence did not carry worker provenance. Do not substitute the
	// current command option during report regeneration; zero explicitly means
	// unavailable rather than inventing historical concurrency.
	_ = workers
	for client := range clientSet {
		item.ProviderClients = append(item.ProviderClients, client)
	}
	sort.Strings(item.ProviderClients)
	if len(item.ProviderClients) > 1 {
		item.ComparabilityWarnings = append(item.ComparabilityWarnings, "Evidence contains more than one provider client version.")
	}
	if item.WorkflowNoncomplianceRuns > 0 {
		item.ComparabilityWarnings = append(item.ComparabilityWarnings, "Workflow-noncompliant completed runs are retained as scored evidence; statistical comparisons involving this experiment are unavailable.")
	}
	if item.Model != selection.Selector.Model || item.Reasoning != selection.Selector.Reasoning {
		item.ComparabilityWarnings = append(item.ComparabilityWarnings, fmt.Sprintf(
			"Selector/executed mismatch: executed model/reasoning is %s/%s; selector published evidence is from %s/%s.",
			item.Model, item.Reasoning, selection.Selector.Model, selection.Selector.Reasoning,
		))
	}
	if complete {
		score := 0.0
		for _, task := range item.Tasks {
			score += *task.WeightedContribution
		}
		item.Score = &score
	}
	return item, nil
}

func readStudyResult(path, task string, attempt int, checksum, environment string, arm matrixArm) (AttemptResult, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a validated study evidence path.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AttemptResult{}, os.ErrNotExist
		}
		return AttemptResult{}, fmt.Errorf("read result %s: %w", path, err)
	}
	var result AttemptResult
	if err := json.Unmarshal(raw, &result); err != nil || !validPlanAttemptResult(result, task, attempt, checksum, arm.Loaded.Model, arm.Loaded.Effort, arm.Mode == ArmTreatment) {
		return AttemptResult{}, fmt.Errorf("malformed or conflicting result evidence %s", path)
	}
	if result.EnvironmentIdentity != environment {
		return AttemptResult{}, fmt.Errorf("result environment identity %q does not match certified environment %q", result.EnvironmentIdentity, environment)
	}
	receiptPath := filepath.Join(filepath.Dir(path), benchmarkArtifactsDir, result.EventID, "execution-receipt.json")
	var receipt pierExecutionReceipt
	if err := readStudyJSON(receiptPath, &receipt); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AttemptResult{}, fmt.Errorf("immutable execution receipt is missing for existing result: %w", err)
		}
		return AttemptResult{}, fmt.Errorf("read immutable execution receipt: %w", err)
	}
	if receipt.SchemaVersion != pierExecutionReceiptSchema || !receipt.Succeeded || !receipt.CleanupSucceeded || receipt.EventID != result.EventID || receipt.Attempt != attempt || receipt.Task != task || receipt.TaskChecksum != checksum || receipt.EnvironmentIdentity != environment || receipt.Arm != arm.Mode || receipt.RuntimeModel != arm.Loaded.Model.RuntimeIdentifier || receipt.ReasoningEffort != arm.Loaded.Effort || receipt.TreatmentHash != bundleManifestHash(arm.Bundle) {
		return AttemptResult{}, fmt.Errorf("execution receipt does not match immutable result evidence")
	}
	// Historical correction is an intentionally narrow read boundary. It
	// leaves every native study and private campaign result untouched, including
	// when a stray canonical-result.json happens to sit beside the evidence.
	result, err = canonicalizeAttemptResult(path, raw, result, arm)
	if err != nil {
		return AttemptResult{}, err
	}
	return result, nil
}

func buildStudyComparisons(report *StudyReport) []StudyComparisonReport {
	var comparisons []StudyComparisonReport
	for i := 0; i < len(report.Experiments); i++ {
		for j := i + 1; j < len(report.Experiments); j++ {
			comparisons = append(comparisons, compareStudyExperiments(report.Experiments[i], report.Experiments[j], report.Selection))
		}
	}
	return comparisons
}

func compareStudyExperiments(left, right StudyExperimentReport, selection StudySelectionProvenance) StudyComparisonReport {
	comparison := StudyComparisonReport{Left: left.Name, Right: right.Name}
	if left.Score == nil || right.Score == nil {
		comparison.UnavailableReason = "requires every selected task cell to be complete in both experiments"
		return comparison
	}
	if left.WorkflowNoncomplianceRuns > 0 || right.WorkflowNoncomplianceRuns > 0 {
		names := make([]string, 0, 2)
		if left.WorkflowNoncomplianceRuns > 0 {
			names = append(names, left.Name)
		}
		if right.WorkflowNoncomplianceRuns > 0 {
			names = append(names, right.Name)
		}
		verb := "has"
		if len(names) > 1 {
			verb = "have"
		}
		comparison.UnavailableReason = fmt.Sprintf("%s %s workflow-noncompliant completed runs; statistical comparison is unavailable while candidate evidence is retained", strings.Join(names, " and "), verb)
		return comparison
	}
	if canUseObservedInference(left, right) {
		moments, reason := observedWelchMoments(left.Tasks, right.Tasks)
		return finishStudyComparison(comparison, left, right, inferenceSourceObserved, nil, moments, reason)
	}
	if canUsePublishedProxyInference(left, right) {
		moments, reason := publishedProxyMoments(left.Tasks, right.Tasks)
		return finishStudyComparison(comparison, left, right, inferenceSourcePublishedProxy, publishedProxyProvenance(left.Tasks, selection), moments, reason)
	}
	comparison.UnavailableReason = unavailableInferenceReason(left, right)
	return comparison
}

func canUseObservedInference(left, right StudyExperimentReport) bool {
	if len(left.Tasks) == 0 || len(left.Tasks) != len(right.Tasks) {
		return false
	}
	for index := range left.Tasks {
		a, b := left.Tasks[index], right.Tasks[index]
		if a.RepetitionsCompleted < 2 || b.RepetitionsCompleted < 2 || a.SampleVariance == nil || b.SampleVariance == nil {
			return false
		}
	}
	return true
}

func canUsePublishedProxyInference(left, right StudyExperimentReport) bool {
	if len(left.Tasks) == 0 || len(left.Tasks) != len(right.Tasks) {
		return false
	}
	for index := range left.Tasks {
		a, b := left.Tasks[index], right.Tasks[index]
		if a.RepetitionsCompleted < 1 || b.RepetitionsCompleted < 1 || !validStudyPublishedVariance(a.PublishedVariance) || !samePublishedVariance(a.PublishedVariance, b.PublishedVariance) {
			return false
		}
	}
	return true
}

func validStudyPublishedVariance(evidence *StudyPublishedVariance) bool {
	return evidence != nil &&
		evidence.SampleSize >= 2 &&
		finite(evidence.SampleVariance) && evidence.SampleVariance >= 0 &&
		evidence.VarianceEstimator == publishedVarianceEstimator &&
		evidence.VarianceDenominator == publishedVarianceDenominator &&
		evidence.ConfigurationID != "" && evidence.PublishedModel != "" && evidence.PublishedReasoning != "" &&
		evidence.ConfigurationID == evidence.PublishedModel+"::"+evidence.PublishedReasoning
}

func samePublishedVariance(left, right *StudyPublishedVariance) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ConfigurationID == right.ConfigurationID &&
		left.PublishedModel == right.PublishedModel &&
		left.PublishedReasoning == right.PublishedReasoning &&
		left.SampleSize == right.SampleSize &&
		left.SampleVariance == right.SampleVariance &&
		left.VarianceEstimator == right.VarianceEstimator &&
		left.VarianceDenominator == right.VarianceDenominator
}

func unavailableInferenceReason(left, right StudyExperimentReport) string {
	hasPublishedEvidence := false
	for index := range left.Tasks {
		if validStudyPublishedVariance(left.Tasks[index].PublishedVariance) || validStudyPublishedVariance(right.Tasks[index].PublishedVariance) {
			hasPublishedEvidence = true
			break
		}
	}
	for index := range left.Tasks {
		a, b := left.Tasks[index], right.Tasks[index]
		if a.RepetitionsCompleted < 2 {
			if hasPublishedEvidence {
				return fmt.Sprintf("%s/%s requires at least two completed repetitions, or complete schema-v3 published variance evidence for published_proxy inference", left.Name, a.Task)
			}
			return fmt.Sprintf("%s/%s requires at least two completed repetitions", left.Name, a.Task)
		}
		if b.RepetitionsCompleted < 2 {
			if hasPublishedEvidence {
				return fmt.Sprintf("%s/%s requires at least two completed repetitions, or complete schema-v3 published variance evidence for published_proxy inference", right.Name, b.Task)
			}
			return fmt.Sprintf("%s/%s requires at least two completed repetitions", right.Name, b.Task)
		}
	}
	return "published_proxy inference requires complete pinned published sample size and variance for every selected task"
}

type pairwiseMoments struct {
	variance    float64
	denominator float64
}

func observedWelchMoments(left, right []StudyTaskReport) (pairwiseMoments, string) {
	var moments pairwiseMoments
	for index := range left {
		a, b := left[index], right[index]
		if a.SampleVariance == nil || b.SampleVariance == nil {
			return pairwiseMoments{}, "missing within-cell sample variance"
		}
		for _, cell := range []StudyTaskReport{a, b} {
			component := cell.EffectiveCoefficient * cell.EffectiveCoefficient * *cell.SampleVariance / float64(cell.RepetitionsCompleted)
			if !finite(component) || component < 0 {
				return pairwiseMoments{}, "non-finite within-cell variance contribution"
			}
			moments.variance += component
			moments.denominator += component * component / float64(cell.RepetitionsCompleted-1)
		}
	}
	return moments, ""
}

func publishedProxyMoments(left, right []StudyTaskReport) (pairwiseMoments, string) {
	var moments pairwiseMoments
	for index := range left {
		a, b := left[index], right[index]
		if a.Task != b.Task {
			return pairwiseMoments{}, "task identities do not align for published_proxy inference"
		}
		if math.Abs(a.EffectiveCoefficient-b.EffectiveCoefficient) > 1e-12 {
			return pairwiseMoments{}, "task coefficients do not align for published_proxy inference"
		}
		if a.RepetitionsCompleted < 1 || b.RepetitionsCompleted < 1 {
			return pairwiseMoments{}, "published_proxy inference requires at least one completed repetition in each experiment"
		}
		if !validStudyPublishedVariance(a.PublishedVariance) || !samePublishedVariance(a.PublishedVariance, b.PublishedVariance) {
			return pairwiseMoments{}, "published_proxy inference requires complete pinned published sample size and variance for every selected task"
		}
		coefficient := a.EffectiveCoefficient
		combined := coefficient * coefficient * a.PublishedVariance.SampleVariance * (1/float64(a.RepetitionsCompleted) + 1/float64(b.RepetitionsCompleted))
		if !finite(combined) || combined < 0 {
			return pairwiseMoments{}, "non-finite published_proxy variance contribution"
		}
		moments.variance += combined
		moments.denominator += combined * combined / float64(a.PublishedVariance.SampleSize-1)
	}
	return moments, ""
}

func publishedProxyProvenance(tasks []StudyTaskReport, selection StudySelectionProvenance) *StudyPublishedProxyProvenance {
	if len(tasks) == 0 || tasks[0].PublishedVariance == nil {
		return nil
	}
	evidence := tasks[0].PublishedVariance
	return &StudyPublishedProxyProvenance{
		ConfigurationID:     evidence.ConfigurationID,
		PublishedModel:      evidence.PublishedModel,
		PublishedReasoning:  evidence.PublishedReasoning,
		VarianceEstimator:   evidence.VarianceEstimator,
		VarianceDenominator: evidence.VarianceDenominator,
		SnapshotSHA:         selection.SnapshotSHA,
	}
}

func finishStudyComparison(comparison StudyComparisonReport, left, right StudyExperimentReport, source string, provenance *StudyPublishedProxyProvenance, moments pairwiseMoments, reason string) StudyComparisonReport {
	if reason != "" {
		comparison.UnavailableReason = reason
		return comparison
	}
	if !finite(moments.variance) || moments.variance <= 0 {
		comparison.UnavailableReason = "non-positive total variance"
		return comparison
	}
	if !finite(moments.denominator) || moments.denominator <= 0 {
		comparison.UnavailableReason = "non-positive Welch-Satterthwaite denominator"
		return comparison
	}
	df := moments.variance * moments.variance / moments.denominator
	if !finite(df) || df <= 0 {
		comparison.UnavailableReason = "non-finite positive Welch-Satterthwaite degrees of freedom"
		return comparison
	}
	difference := *left.Score - *right.Score
	standardError := math.Sqrt(moments.variance)
	statistic := math.Abs(difference) / standardError
	p := studentTwoSidedP(statistic, df)
	if !finite(p) {
		comparison.UnavailableReason = "non-finite two-sided p-value"
		return comparison
	}
	comparison.Available = true
	comparison.InferenceSource = source
	comparison.PublishedProxy = provenance
	comparison.Difference, comparison.Variance, comparison.StandardError, comparison.DegreesOfFreedom, comparison.Statistic, comparison.RawTwoSidedPValue = &difference, &moments.variance, &standardError, &df, &statistic, &p
	return comparison
}

func studyReportRepetitionLimitation(selection matrixSelection) string {
	if selection.SchemaVersion == matrixSelectionSchemaVersion {
		return "Observed-repetition Welch inference is used when every required task has at least two completed independent repetitions in both experiments; three or more are preferable for stability. One-repetition studies may use clearly labeled published_proxy inference when the selection pins complete published sample sizes and variances."
	}
	return "One repetition is descriptive only. At least two completed independent repetitions in every selected task and experiment enable the declared Welch inference; three or more are preferable for stability."
}

func usesPublishedProxy(comparisons []StudyComparisonReport) bool {
	for _, comparison := range comparisons {
		if comparison.Available && comparison.InferenceSource == inferenceSourcePublishedProxy {
			return true
		}
	}
	return false
}

func studyPublishedVariance(evidence *matrixPublishedTaskVariance) *StudyPublishedVariance {
	if evidence == nil {
		return nil
	}
	copied := StudyPublishedVariance{
		ConfigurationID:     evidence.ConfigurationID,
		PublishedModel:      evidence.PublishedModel,
		PublishedReasoning:  evidence.PublishedReasoning,
		SampleSize:          evidence.SampleSize,
		SampleVariance:      evidence.SampleVariance,
		VarianceEstimator:   evidence.VarianceEstimator,
		VarianceDenominator: evidence.VarianceDenominator,
	}
	return &copied
}

func applyHolm(comparisons []StudyComparisonReport) {
	var indexes []int
	for i := range comparisons {
		if comparisons[i].Available {
			indexes = append(indexes, i)
		}
	}
	sort.Slice(indexes, func(i, j int) bool {
		return *comparisons[indexes[i]].RawTwoSidedPValue < *comparisons[indexes[j]].RawTwoSidedPValue
	})
	previous := 0.0
	count := len(indexes)
	for rank, index := range indexes {
		adjusted := math.Min(1, float64(count-rank)**comparisons[index].RawTwoSidedPValue)
		if adjusted < previous {
			adjusted = previous
		}
		previous = adjusted
		comparisons[index].HolmAdjustedPValue = &adjusted
	}
}

func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
func sampleVariance(values []float64) float64 {
	mean := average(values)
	total := 0.0
	for _, value := range values {
		total += (value - mean) * (value - mean)
	}
	return total / float64(len(values)-1)
}

func studyReportCost(report StudyReport) ObservedCostRange {
	var total ObservedCostRange
	for _, experiment := range report.Experiments {
		total = addObservedCost(total, experiment.ObservedCost)
	}
	return total
}
func subtractObservedCost(total, prior ObservedCostRange) ObservedCostRange {
	return ObservedCostRange{Midpoint: total.Midpoint - prior.Midpoint, Minimum: total.Minimum - prior.Minimum, Maximum: total.Maximum - prior.Maximum}
}

// This evaluates the two-sided Student-t tail through I_x(a, b).
func studentTwoSidedP(statistic, df float64) float64 {
	return regularizedBeta(df/2, .5, df/(df+statistic*statistic))
}
