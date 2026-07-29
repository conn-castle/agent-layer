package benchmark

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

const observedAnalysisSchema = "deepswe-observed-arm-analysis"
const observedAnalysisSchemaVersion = 3
const legacyObservedAnalysisSchemaVersion = 2
const observedCostAxisReference = "claude-fable-5::max"
const observedCostAxisRoundingUSD = 50.0

// ObservedCostRange is the bounded provider cost for one report scope.
type ObservedCostRange struct {
	Midpoint float64 `json:"midpoint"`
	Minimum  float64 `json:"minimum"`
	Maximum  float64 `json:"maximum"`
}

// ObservedTaskReport contains the repeated score evidence for one selected task
// in the source analysis artifact.
type ObservedTaskReport struct {
	Task                             string    `json:"task"`
	RepetitionsPerArm                int       `json:"repetitionsPerArm"`
	BaselineScores                   []float64 `json:"baselineScores"`
	TreatmentScores                  []float64 `json:"treatmentScores"`
	BaselineMean                     float64   `json:"baselineMean"`
	TreatmentMean                    float64   `json:"treatmentMean"`
	Difference                       float64   `json:"difference"`
	BaselineSampleVariance           float64   `json:"baselineSampleVariance"`
	TreatmentSampleVariance          float64   `json:"treatmentSampleVariance"`
	BaselineVerifierBuildFailedRuns  int       `json:"baselineVerifierBuildFailedRuns"`
	TreatmentVerifierBuildFailedRuns int       `json:"treatmentVerifierBuildFailedRuns"`
}

// ObservedCostAxis records the campaign-specific, reproducible cost domain.
type ObservedCostAxis struct {
	Scale                        string  `json:"scale"`
	ReferenceConfiguration       string  `json:"reference_configuration"`
	ReferenceSnapshotSHA256      string  `json:"reference_snapshot_sha256"`
	ReferenceEstimatedArmCostUSD float64 `json:"reference_estimated_arm_cost_usd"`
	RoundingIncrementUSD         float64 `json:"rounding_increment_usd"`
	MaximumUSD                   float64 `json:"maximum_usd"`
}

// ObservedBaselineTaskReport is the shared bare-model evidence for one task.
type ObservedBaselineTaskReport struct {
	Task                    string    `json:"task"`
	Repetitions             int       `json:"repetitions"`
	Scores                  []float64 `json:"scores"`
	Mean                    float64   `json:"mean"`
	SampleVariance          float64   `json:"sample_variance"`
	VerifierBuildFailedRuns int       `json:"verifier_build_failed_runs"`
}

// ObservedVersionTaskReport is one campaign version's evidence for one task.
type ObservedVersionTaskReport struct {
	Task                    string    `json:"task"`
	Scores                  []float64 `json:"scores"`
	Mean                    float64   `json:"mean"`
	Difference              float64   `json:"difference"`
	SampleVariance          float64   `json:"sample_variance"`
	VerifierBuildFailedRuns int       `json:"verifier_build_failed_runs"`
}

// ObservedVersionReport is one skills/instructions version compared with the
// campaign's shared bare-model baseline.
type ObservedVersionReport struct {
	GeneratedAt               time.Time                   `json:"generated_at"`
	Label                     string                      `json:"label"`
	Verdict                   string                      `json:"verdict"`
	Mean                      float64                     `json:"mean"`
	StandardError             float64                     `json:"standard_error"`
	ObservedDifference        float64                     `json:"observed_difference"`
	DecisionThreshold         float64                     `json:"decision_threshold"`
	DifferenceStandardError   float64                     `json:"difference_standard_error"`
	EffectiveDegreesOfFreedom float64                     `json:"effective_degrees_of_freedom"`
	TCriticalValue            float64                     `json:"t_critical_value"`
	Cost                      ObservedCostRange           `json:"cost"`
	CostMultiple              float64                     `json:"cost_multiple"`
	CostMultipleMinimum       float64                     `json:"cost_multiple_minimum"`
	CostMultipleMaximum       float64                     `json:"cost_multiple_maximum"`
	InvocationCount           int                         `json:"invocation_count"`
	DispatchConformantRuns    int                         `json:"dispatch_conformant_runs"`
	TotalRuns                 int                         `json:"total_runs"`
	AgentTimeoutMultiplier    float64                     `json:"agent_timeout_multiplier"`
	Tasks                     []ObservedVersionTaskReport `json:"tasks"`
	Limitations               []string                    `json:"limitations"`
}

// ObservedCampaignReport is the executive report model for a shared baseline
// and an ordered series of skills/instructions versions.
type ObservedCampaignReport struct {
	PlanID                string                       `json:"plan_id"`
	CampaignID            string                       `json:"campaign_id"`
	Model                 string                       `json:"model"`
	Reasoning             string                       `json:"reasoning"`
	CalibrationReference  string                       `json:"calibration_reference"`
	CalibrationContrast   string                       `json:"calibration_contrast"`
	BaselineLabel         string                       `json:"baseline_label"`
	TaskCount             int                          `json:"task_count"`
	RunsPerArm            int                          `json:"runs_per_arm"`
	SignificanceLevel     float64                      `json:"two_sided_significance_level"`
	EqualTaskWeighting    bool                         `json:"equal_task_weighting"`
	BaselineMean          float64                      `json:"baseline_mean"`
	BaselineStandardError float64                      `json:"baseline_standard_error"`
	BaselineCost          ObservedCostRange            `json:"baseline_cost"`
	CampaignCost          ObservedCostRange            `json:"campaign_cost"`
	CostAxis              ObservedCostAxis             `json:"cost_axis"`
	BaselineTasks         []ObservedBaselineTaskReport `json:"baseline_tasks"`
	Versions              []ObservedVersionReport      `json:"versions"`
	Warnings              []string                     `json:"warnings,omitempty"`
}

type observedAnalysisDocument struct {
	Schema        string    `json:"schema"`
	SchemaVersion int       `json:"schemaVersion"`
	GeneratedAt   time.Time `json:"generatedAt"`
	PlanID        string    `json:"planId"`
	CampaignID    string    `json:"campaignId"`
	Experiment    struct {
		Model                     string         `json:"model"`
		Reasoning                 string         `json:"reasoning"`
		CalibrationReference      string         `json:"calibrationReference"`
		CalibrationContrast       string         `json:"calibrationContrast"`
		Baseline                  string         `json:"baseline"`
		Treatment                 string         `json:"treatment"`
		Tasks                     int            `json:"tasks"`
		RepetitionsPerArm         map[string]int `json:"repetitionsPerArm"`
		TwoSidedSignificanceLevel float64        `json:"twoSidedSignificanceLevel"`
		EqualTaskWeighting        bool           `json:"equalTaskWeighting"`
		AgentTimeoutMultiplier    float64        `json:"agentTimeoutMultiplier,omitempty"`
	} `json:"experiment"`
	Result struct {
		Verdict                   string  `json:"verdict"`
		BaselineMean              float64 `json:"baselineMean"`
		TreatmentMean             float64 `json:"treatmentMean"`
		ObservedDifference        float64 `json:"observedDifference"`
		DecisionThreshold         float64 `json:"decisionThreshold"`
		StandardError             float64 `json:"standardError"`
		EffectiveDegreesOfFreedom float64 `json:"effectiveDegreesOfFreedom"`
		TCriticalValue            float64 `json:"tCriticalValue"`
	} `json:"result"`
	CostUSD struct {
		Baseline  ObservedCostRange `json:"baseline"`
		Treatment ObservedCostRange `json:"treatment"`
		Total     ObservedCostRange `json:"total"`
	} `json:"costUsd"`
	TreatmentDiagnostics struct {
		InvocationCount        int `json:"invocationCount"`
		DispatchConformantRuns int `json:"dispatchConformantRuns"`
		TotalRuns              int `json:"totalRuns"`
	} `json:"treatmentDiagnostics"`
	CostAxis struct {
		Scale                        string  `json:"scale"`
		ReferenceConfiguration       string  `json:"referenceConfiguration"`
		ReferenceSnapshotSHA256      string  `json:"referenceSnapshotSha256"`
		ReferenceEstimatedArmCostUSD float64 `json:"referenceEstimatedArmCostUsd"`
		RoundingIncrementUSD         float64 `json:"roundingIncrementUsd"`
		MaximumUSD                   float64 `json:"maximumUsd"`
	} `json:"costAxis"`
	Tasks       []ObservedTaskReport `json:"tasks"`
	Limitations []string             `json:"limitations"`
}

func upgradeLegacyObservedAnalysis(data []byte, loaded loadedBenchmarkPlan) ([]byte, error) {
	var source observedAnalysisDocument
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("decode observed benchmark analysis: %w", err)
	}
	if source.Schema != observedAnalysisSchema || source.PlanID != loaded.ID {
		return nil, fmt.Errorf("analysis does not match the selected schema version 1 plan")
	}
	switch source.SchemaVersion {
	case legacyObservedAnalysisSchemaVersion:
		source.SchemaVersion = observedAnalysisSchemaVersion
		source.CampaignID = loaded.CampaignID
		source.Experiment.CalibrationReference = loaded.Plan.CalibrationReference.ID
		source.Experiment.CalibrationContrast = loaded.Plan.CalibrationContrast.ID
	case observedAnalysisSchemaVersion:
		if source.CampaignID != loaded.CampaignID ||
			source.Experiment.CalibrationReference != loaded.Plan.CalibrationReference.ID ||
			source.Experiment.CalibrationContrast != loaded.Plan.CalibrationContrast.ID {
			return nil, fmt.Errorf("current analysis does not match the selected schema version 1 campaign")
		}
	default:
		return nil, fmt.Errorf("analysis uses unsupported schema version %d", source.SchemaVersion)
	}
	if err := source.validate(); err != nil {
		return nil, err
	}
	upgraded, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode upgraded observed benchmark analysis: %w", err)
	}
	return upgraded, nil
}

// BuildObservedCampaignReport validates ordered observed-arm analyses sharing
// one baseline and converts them into the canonical campaign report.
func BuildObservedCampaignReport(documents ...[]byte) (Report, error) {
	if len(documents) == 0 {
		return Report{}, fmt.Errorf("observed benchmark campaign requires at least one version")
	}
	sources := make([]observedAnalysisDocument, 0, len(documents))
	for _, data := range documents {
		var source observedAnalysisDocument
		if err := json.Unmarshal(data, &source); err != nil {
			return Report{}, fmt.Errorf("decode observed benchmark analysis: %w", err)
		}
		if err := source.validate(); err != nil {
			return Report{}, err
		}
		sources = append(sources, source)
	}
	first := sources[0]
	runsPerArm := 0
	for _, task := range first.Tasks {
		runsPerArm += task.RepetitionsPerArm
	}
	baselineStandardError := observedArmStandardError(first.Tasks, func(task ObservedTaskReport) float64 {
		return task.BaselineSampleVariance
	})
	campaign := &ObservedCampaignReport{
		PlanID:                first.PlanID,
		CampaignID:            first.CampaignID,
		Model:                 first.Experiment.Model,
		Reasoning:             first.Experiment.Reasoning,
		CalibrationReference:  first.Experiment.CalibrationReference,
		CalibrationContrast:   first.Experiment.CalibrationContrast,
		BaselineLabel:         first.Experiment.Baseline,
		TaskCount:             first.Experiment.Tasks,
		RunsPerArm:            runsPerArm,
		SignificanceLevel:     first.Experiment.TwoSidedSignificanceLevel,
		EqualTaskWeighting:    first.Experiment.EqualTaskWeighting,
		BaselineMean:          first.Result.BaselineMean,
		BaselineStandardError: baselineStandardError,
		BaselineCost:          first.CostUSD.Baseline,
		CampaignCost:          first.CostUSD.Baseline,
		CostAxis: ObservedCostAxis{
			Scale:                        first.CostAxis.Scale,
			ReferenceConfiguration:       first.CostAxis.ReferenceConfiguration,
			ReferenceSnapshotSHA256:      first.CostAxis.ReferenceSnapshotSHA256,
			ReferenceEstimatedArmCostUSD: first.CostAxis.ReferenceEstimatedArmCostUSD,
			RoundingIncrementUSD:         first.CostAxis.RoundingIncrementUSD,
			MaximumUSD:                   first.CostAxis.MaximumUSD,
		},
	}
	for _, task := range first.Tasks {
		campaign.BaselineTasks = append(campaign.BaselineTasks, ObservedBaselineTaskReport{
			Task: task.Task, Repetitions: task.RepetitionsPerArm,
			Scores: append([]float64(nil), task.BaselineScores...),
			Mean:   task.BaselineMean, SampleVariance: task.BaselineSampleVariance,
			VerifierBuildFailedRuns: task.BaselineVerifierBuildFailedRuns,
		})
	}
	seenLabels := make(map[string]bool, len(sources))
	for index, source := range sources {
		if seenLabels[source.Experiment.Treatment] {
			return Report{}, fmt.Errorf("observed benchmark campaign version labels must be unique")
		}
		seenLabels[source.Experiment.Treatment] = true
		if index > 0 {
			if err := validateSharedObservedBaseline(first, source); err != nil {
				return Report{}, fmt.Errorf("observed benchmark campaign version %d: %w", index+1, err)
			}
			if !source.GeneratedAt.After(sources[index-1].GeneratedAt) {
				return Report{}, fmt.Errorf("observed benchmark campaign versions must be in chronological order")
			}
		}
		timeoutMultiplier := source.Experiment.AgentTimeoutMultiplier
		if timeoutMultiplier == 0 {
			timeoutMultiplier = 1
		}
		version := ObservedVersionReport{
			GeneratedAt:               source.GeneratedAt.UTC(),
			Label:                     source.Experiment.Treatment,
			Verdict:                   source.Result.Verdict,
			Mean:                      source.Result.TreatmentMean,
			StandardError:             observedArmStandardError(source.Tasks, func(task ObservedTaskReport) float64 { return task.TreatmentSampleVariance }),
			ObservedDifference:        source.Result.ObservedDifference,
			DecisionThreshold:         source.Result.DecisionThreshold,
			DifferenceStandardError:   source.Result.StandardError,
			EffectiveDegreesOfFreedom: source.Result.EffectiveDegreesOfFreedom,
			TCriticalValue:            source.Result.TCriticalValue,
			Cost:                      source.CostUSD.Treatment,
			CostMultiple:              source.CostUSD.Treatment.Midpoint / source.CostUSD.Baseline.Midpoint,
			CostMultipleMinimum:       source.CostUSD.Treatment.Minimum / source.CostUSD.Baseline.Maximum,
			CostMultipleMaximum:       source.CostUSD.Treatment.Maximum / source.CostUSD.Baseline.Minimum,
			InvocationCount:           source.TreatmentDiagnostics.InvocationCount,
			DispatchConformantRuns:    source.TreatmentDiagnostics.DispatchConformantRuns,
			TotalRuns:                 source.TreatmentDiagnostics.TotalRuns,
			AgentTimeoutMultiplier:    timeoutMultiplier,
			Limitations:               append([]string(nil), source.Limitations...),
		}
		for _, task := range source.Tasks {
			version.Tasks = append(version.Tasks, ObservedVersionTaskReport{
				Task: task.Task, Scores: append([]float64(nil), task.TreatmentScores...),
				Mean: task.TreatmentMean, Difference: task.Difference,
				SampleVariance:          task.TreatmentSampleVariance,
				VerifierBuildFailedRuns: task.TreatmentVerifierBuildFailedRuns,
			})
		}
		campaign.Versions = append(campaign.Versions, version)
		campaign.CampaignCost = addObservedCost(campaign.CampaignCost, source.CostUSD.Treatment)
	}
	last := sources[len(sources)-1]
	return Report{
		SchemaVersion:    ReportSchemaVersion,
		ComparisonID:     first.CampaignID,
		GeneratedAt:      last.GeneratedAt.UTC(),
		ObservedCampaign: campaign,
		Limitations:      append([]string(nil), last.Limitations...),
	}, nil
}

func addObservedCost(left, right ObservedCostRange) ObservedCostRange {
	return ObservedCostRange{
		Midpoint: left.Midpoint + right.Midpoint,
		Minimum:  left.Minimum + right.Minimum,
		Maximum:  left.Maximum + right.Maximum,
	}
}

func validateSharedObservedBaseline(first, next observedAnalysisDocument) error {
	if first.PlanID != next.PlanID ||
		first.CampaignID != next.CampaignID ||
		first.Experiment.Model != next.Experiment.Model ||
		first.Experiment.Reasoning != next.Experiment.Reasoning ||
		first.Experiment.CalibrationReference != next.Experiment.CalibrationReference ||
		first.Experiment.CalibrationContrast != next.Experiment.CalibrationContrast ||
		first.Experiment.Baseline != next.Experiment.Baseline ||
		first.Experiment.Tasks != next.Experiment.Tasks ||
		first.Experiment.TwoSidedSignificanceLevel != next.Experiment.TwoSidedSignificanceLevel ||
		first.Experiment.EqualTaskWeighting != next.Experiment.EqualTaskWeighting ||
		!closeEnough(first.Result.BaselineMean, next.Result.BaselineMean) ||
		first.CostUSD.Baseline != next.CostUSD.Baseline ||
		first.CostAxis != next.CostAxis ||
		len(first.Tasks) != len(next.Tasks) {
		return fmt.Errorf("version does not share the campaign baseline contract")
	}
	for index := range first.Tasks {
		left, right := first.Tasks[index], next.Tasks[index]
		if left.Task != right.Task ||
			left.RepetitionsPerArm != right.RepetitionsPerArm ||
			left.BaselineVerifierBuildFailedRuns != right.BaselineVerifierBuildFailedRuns ||
			!closeEnough(left.BaselineMean, right.BaselineMean) ||
			!closeEnough(left.BaselineSampleVariance, right.BaselineSampleVariance) ||
			len(left.BaselineScores) != len(right.BaselineScores) {
			return fmt.Errorf("version does not share baseline task %q", left.Task)
		}
		for scoreIndex := range left.BaselineScores {
			if !closeEnough(left.BaselineScores[scoreIndex], right.BaselineScores[scoreIndex]) {
				return fmt.Errorf("version does not share baseline scores for task %q", left.Task)
			}
		}
	}
	return nil
}

func (source observedAnalysisDocument) validate() error {
	if source.Schema != observedAnalysisSchema || source.SchemaVersion != observedAnalysisSchemaVersion ||
		source.GeneratedAt.IsZero() || len(source.PlanID) != 64 ||
		len(source.CampaignID) != 64 {
		return fmt.Errorf("observed benchmark analysis has an unsupported or incomplete identity")
	}
	if source.Experiment.Model == "" || source.Experiment.Reasoning == "" ||
		source.Experiment.CalibrationReference == "" ||
		source.Experiment.CalibrationContrast == "" ||
		source.Experiment.Baseline == "" || source.Experiment.Treatment == "" ||
		source.Experiment.Tasks < 1 || source.Experiment.Tasks != len(source.Tasks) ||
		!source.Experiment.EqualTaskWeighting ||
		source.Experiment.AgentTimeoutMultiplier < 0 ||
		source.Experiment.TwoSidedSignificanceLevel <= 0 ||
		source.Experiment.TwoSidedSignificanceLevel >= 1 {
		return fmt.Errorf("observed benchmark analysis has an invalid experiment contract")
	}
	if source.CostAxis.Scale != costAxisLogarithmic ||
		source.CostAxis.ReferenceConfiguration != observedCostAxisReference ||
		len(source.CostAxis.ReferenceSnapshotSHA256) != 64 ||
		!finite(source.CostAxis.ReferenceEstimatedArmCostUSD) ||
		source.CostAxis.ReferenceEstimatedArmCostUSD <= 0 ||
		source.CostAxis.RoundingIncrementUSD != observedCostAxisRoundingUSD ||
		!finite(source.CostAxis.MaximumUSD) ||
		!closeEnough(source.CostAxis.MaximumUSD, math.Ceil(source.CostAxis.ReferenceEstimatedArmCostUSD/observedCostAxisRoundingUSD)*observedCostAxisRoundingUSD) {
		return fmt.Errorf("observed benchmark analysis has an invalid cost-axis contract")
	}
	for _, value := range []float64{
		source.Result.BaselineMean,
		source.Result.TreatmentMean,
		source.Result.DecisionThreshold,
		source.Result.StandardError,
		source.Result.EffectiveDegreesOfFreedom,
		source.Result.TCriticalValue,
	} {
		if !finite(value) {
			return fmt.Errorf("observed benchmark analysis contains a non-finite result")
		}
	}
	if source.Result.BaselineMean < 0 || source.Result.BaselineMean > 1 ||
		source.Result.TreatmentMean < 0 || source.Result.TreatmentMean > 1 ||
		source.Result.DecisionThreshold <= 0 || source.Result.StandardError <= 0 ||
		source.Result.EffectiveDegreesOfFreedom <= 0 || source.Result.TCriticalValue <= 0 ||
		!closeEnough(source.Result.ObservedDifference, source.Result.TreatmentMean-source.Result.BaselineMean) ||
		!closeEnough(source.Result.DecisionThreshold, source.Result.StandardError*source.Result.TCriticalValue) {
		return fmt.Errorf("observed benchmark analysis has inconsistent decision statistics")
	}
	expectedVerdict := verdictInconclusive
	if source.Result.ObservedDifference > source.Result.DecisionThreshold {
		expectedVerdict = verdictBetter
	} else if source.Result.ObservedDifference < -source.Result.DecisionThreshold {
		expectedVerdict = verdictWorse
	}
	if source.Result.Verdict != expectedVerdict {
		return fmt.Errorf("observed benchmark analysis verdict does not match its threshold")
	}
	if err := validateObservedCost(source.CostUSD.Baseline); err != nil {
		return fmt.Errorf("observed baseline cost: %w", err)
	}
	if err := validateObservedCost(source.CostUSD.Treatment); err != nil {
		return fmt.Errorf("observed treatment cost: %w", err)
	}
	if err := validateObservedCost(source.CostUSD.Total); err != nil {
		return fmt.Errorf("observed total cost: %w", err)
	}
	if source.CostUSD.Baseline.Minimum <= 0 {
		return fmt.Errorf("observed baseline cost must be positive to calculate the treatment cost multiple")
	}
	for _, field := range []struct {
		total, baseline, treatment float64
	}{
		{source.CostUSD.Total.Midpoint, source.CostUSD.Baseline.Midpoint, source.CostUSD.Treatment.Midpoint},
		{source.CostUSD.Total.Minimum, source.CostUSD.Baseline.Minimum, source.CostUSD.Treatment.Minimum},
		{source.CostUSD.Total.Maximum, source.CostUSD.Baseline.Maximum, source.CostUSD.Treatment.Maximum},
	} {
		if !closeEnough(field.total, field.baseline+field.treatment) {
			return fmt.Errorf("observed benchmark total cost does not reconcile")
		}
	}
	if source.TreatmentDiagnostics.InvocationCount < 1 ||
		source.TreatmentDiagnostics.TotalRuns < 1 ||
		source.TreatmentDiagnostics.DispatchConformantRuns < 0 ||
		source.TreatmentDiagnostics.DispatchConformantRuns > source.TreatmentDiagnostics.TotalRuns {
		return fmt.Errorf("observed benchmark analysis has invalid treatment metadata")
	}
	baselineMeans := make([]float64, 0, len(source.Tasks))
	treatmentMeans := make([]float64, 0, len(source.Tasks))
	runs := 0
	for _, task := range source.Tasks {
		repetitions, ok := source.Experiment.RepetitionsPerArm[task.Task]
		if !ok || task.Task == "" || repetitions < 2 || task.RepetitionsPerArm != repetitions ||
			len(task.BaselineScores) != repetitions || len(task.TreatmentScores) != repetitions ||
			task.BaselineVerifierBuildFailedRuns < 0 || task.BaselineVerifierBuildFailedRuns > repetitions ||
			task.TreatmentVerifierBuildFailedRuns < 0 || task.TreatmentVerifierBuildFailedRuns > repetitions {
			return fmt.Errorf("observed benchmark task %q has invalid repetition evidence", task.Task)
		}
		baselineMean, baselineVariance, err := observedMoments(task.BaselineScores)
		if err != nil {
			return fmt.Errorf("observed benchmark task %s baseline: %w", task.Task, err)
		}
		treatmentMean, treatmentVariance, err := observedMoments(task.TreatmentScores)
		if err != nil {
			return fmt.Errorf("observed benchmark task %s treatment: %w", task.Task, err)
		}
		if !closeEnough(task.BaselineMean, baselineMean) ||
			!closeEnough(task.TreatmentMean, treatmentMean) ||
			!closeEnough(task.Difference, treatmentMean-baselineMean) ||
			!closeEnough(task.BaselineSampleVariance, baselineVariance) ||
			!closeEnough(task.TreatmentSampleVariance, treatmentVariance) {
			return fmt.Errorf("observed benchmark task %s statistics do not match its scores", task.Task)
		}
		baselineMeans = append(baselineMeans, baselineMean)
		treatmentMeans = append(treatmentMeans, treatmentMean)
		runs += repetitions
	}
	if source.TreatmentDiagnostics.TotalRuns != runs ||
		!closeEnough(source.Result.BaselineMean, observedMean(baselineMeans)) ||
		!closeEnough(source.Result.TreatmentMean, observedMean(treatmentMeans)) {
		return fmt.Errorf("observed benchmark aggregate does not match its task evidence")
	}
	return nil
}

func validateObservedCost(cost ObservedCostRange) error {
	if !finite(cost.Midpoint) || !finite(cost.Minimum) || !finite(cost.Maximum) ||
		cost.Minimum < 0 || cost.Maximum < cost.Minimum ||
		cost.Midpoint < cost.Minimum || cost.Midpoint > cost.Maximum {
		return fmt.Errorf("invalid cost range")
	}
	return nil
}

func observedMoments(values []float64) (float64, float64, error) {
	if len(values) < 2 {
		return 0, 0, fmt.Errorf("at least two scores are required")
	}
	for _, value := range values {
		if !finite(value) || value < 0 || value > 1 {
			return 0, 0, fmt.Errorf("score is outside [0,1]")
		}
	}
	mean := observedMean(values)
	var sum float64
	for _, value := range values {
		delta := value - mean
		sum += delta * delta
	}
	return mean, sum / float64(len(values)-1), nil
}

func observedMean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func observedArmStandardError(tasks []ObservedTaskReport, variance func(ObservedTaskReport) float64) float64 {
	var propagatedVariance float64
	for _, task := range tasks {
		propagatedVariance += variance(task) / float64(task.RepetitionsPerArm)
	}
	return math.Sqrt(propagatedVariance / float64(len(tasks)*len(tasks)))
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func closeEnough(actual, expected float64) bool {
	return math.Abs(actual-expected) <= 1e-12
}
