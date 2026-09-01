package benchmark

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
)

const (
	studyReportNotRecorded   = "Not recorded"
	studyReportStatusPartial = "partial"
	plotlyBasicVersion       = "3.7.0"
	plotlyBasicSHA256        = "89eccc5a8b851af5180a5fb4c1d52027e14aa4ca11e32d8372891119c21cf833"
)

// plotlyBasicJS is the pinned official Plotly Basic distribution. Embedding it
// keeps generated reports self-contained and usable without network access.
//
//go:embed assets/plotly-basic-3.7.0.min.js
var plotlyBasicJS string

var studyReportColors = []string{"#3d74ac", "#1591a8", "#7c5bb8", "#d17b32", "#2f855a", "#be5f8a", "#64748b", "#9a6b3f"}

type studyReportHTMLView struct {
	StudyID             string
	StudyIDShort        string
	SelectionID         string
	Generated           string
	Status              string
	StatusClass         string
	CompletedCells      int
	RequiredCells       int
	TaskCount           int
	ExperimentCount     int
	ObservedCost        string
	Experiments         []studyExperimentHTMLView
	ChartExperiments    []studyExperimentHTMLView
	TaskRows            []studyTaskRowHTMLView
	PValueRows          []studyPValueRowHTMLView
	Comparisons         []studyComparisonHTMLView
	Warnings            []string
	HasComparisons      bool
	HolmMethod          string
	HolmSize            int
	Limitations         []string
	SelectorModel       string
	SelectorReasoning   string
	SnapshotURL         string
	SnapshotSHA         string
	ReproductionCommand string
	CLI                 string
	Harness             string
	TaskConcurrency     int
	TaskArchitecture    string
	StatisticalMethod   string
	InferenceWarnings   []string
	PlotlyJS            template.JS
}

type studyExperimentHTMLView struct {
	Number         int
	Color          string
	Name           string
	Model          string
	Reasoning      string
	Score          string
	ScoreValue     string
	HasScore       bool
	CompletedCells int
	RequiredCells  int
	Completion     string
	ObservedCost   string
	CostMidpoint   string
	CostMinimum    string
	CostMaximum    string
}

type studyComparisonHTMLView struct {
	Baseline        int
	Comparison      int
	Available       bool
	Difference      string
	DifferenceValue string
	RawP            string
	RawPValue       string
	AdjustedP       string
	AdjustedPValue  string
	Reason          string
	InferenceSource string
}

type studyTaskRowHTMLView struct {
	Task   string
	Weight string
	Cells  []studyTaskCellHTMLView
}

type studyTaskCellHTMLView struct {
	Raw         string
	Calibrated  string
	StandardDev string
	Runs        string
	Cost        string
	Complete    bool
	Winner      bool
}

type studyPValueRowHTMLView struct {
	Number int
	Color  string
	Name   string
	Cells  []studyPValueCellHTMLView
}

type studyPValueCellHTMLView struct {
	Value string
	Raw   string
	Class string
	Title string
}

func renderStudyReportHTML(report StudyReport) ([]byte, error) {
	view := newStudyReportHTMLView(report)
	// #nosec G203 -- plotlyBasicJS is a pinned, checksummed embedded asset, not user input.
	view.PlotlyJS = template.JS(plotlyBasicJS)
	var output bytes.Buffer
	if err := studyReportTemplate.Execute(&output, view); err != nil {
		return nil, fmt.Errorf("render benchmark study HTML: %w", err)
	}
	return output.Bytes(), nil
}

func newStudyReportHTMLView(report StudyReport) studyReportHTMLView {
	view := studyReportHTMLView{
		StudyID: report.StudyID, StudyIDShort: shortIdentifier(report.StudyID), SelectionID: report.SelectionID,
		Generated:       report.GeneratedAt.Format("Jan 2, 2006 at 3:04 PM MST"),
		ExperimentCount: len(report.Experiments), HolmMethod: report.HolmFamily.Method, HolmSize: report.HolmFamily.Size,
		Limitations: report.Limitations, SelectorModel: report.Selection.Model, SelectorReasoning: report.Selection.Reasoning,
		SnapshotURL: report.Selection.SnapshotURL, SnapshotSHA: report.Selection.SnapshotSHA,
		ReproductionCommand: report.Execution.Command, CLI: report.Execution.CLI,
		Harness:         strings.TrimSpace(report.Execution.Harness + " " + report.Execution.HarnessVersion),
		TaskConcurrency: report.Execution.TaskConcurrency, TaskArchitecture: report.Execution.TaskContainerArchitecture,
		StatisticalMethod: studyReportStatisticalMethod(false),
	}
	if view.ReproductionCommand == "" {
		view.ReproductionCommand = "al benchmark run <study.toml>"
	}
	if view.CLI == "" {
		view.CLI = studyReportCLI
	}
	if view.Harness == "" {
		view.Harness = "DataCurve Pier " + PierVersion
	}
	if view.TaskArchitecture == "" {
		view.TaskArchitecture = benchmarkTaskContainerArchitecture
	}
	if report.GeneratedAt.IsZero() {
		view.Generated = studyReportNotRecorded
	}
	if len(report.Experiments) > 0 {
		view.TaskCount = len(report.Experiments[0].Tasks)
	}
	totalCost := ObservedCostRange{}
	for _, experiment := range report.Experiments {
		view.CompletedCells += experiment.CompletedCells
		view.RequiredCells += experiment.RequiredCells
		totalCost = addObservedCost(totalCost, experiment.ObservedCost)
	}
	view.ObservedCost = formatCostRange(totalCost)
	if view.RequiredCells > 0 && view.CompletedCells == view.RequiredCells {
		view.Status, view.StatusClass = "Complete", "complete"
	} else {
		view.Status, view.StatusClass = fmt.Sprintf("%d of %d runs complete", view.CompletedCells, view.RequiredCells), studyReportStatusPartial
	}

	for index, experiment := range report.Experiments {
		color := studyReportColors[index%len(studyReportColors)]
		item := studyExperimentHTMLView{
			Number: index + 1, Color: color, Name: experiment.Name, Model: experiment.Model, Reasoning: experiment.Reasoning,
			CompletedCells: experiment.CompletedCells, RequiredCells: experiment.RequiredCells,
			Completion:   fmt.Sprintf("%d / %d", experiment.CompletedCells, experiment.RequiredCells),
			ObservedCost: formatCostRange(experiment.ObservedCost),
			CostMidpoint: machineNumber(experiment.ObservedCost.Midpoint), CostMinimum: machineNumber(experiment.ObservedCost.Minimum), CostMaximum: machineNumber(experiment.ObservedCost.Maximum),
		}
		for _, warning := range experiment.ComparabilityWarnings {
			view.Warnings = append(view.Warnings, fmt.Sprintf("%s: %s", experiment.Name, warning))
		}
		if experiment.Score != nil {
			item.HasScore = true
			item.Score = formatPercent(*experiment.Score)
			item.ScoreValue = machineNumber(*experiment.Score)
		} else {
			item.Score = "Pending"
		}
		view.Experiments = append(view.Experiments, item)
		if item.HasScore && experiment.ObservedCost.Midpoint > 0 && experiment.ObservedCost.Minimum > 0 && experiment.ObservedCost.Maximum > 0 {
			view.ChartExperiments = append(view.ChartExperiments, item)
		}
	}

	if len(report.Experiments) > 0 {
		for taskIndex, task := range report.Experiments[0].Tasks {
			row := studyTaskRowHTMLView{Task: task.Task, Weight: fmt.Sprintf("%.1f%%", task.Weight*100)}
			bestCalibrated := math.Inf(-1)
			for _, experiment := range report.Experiments {
				if taskIndex < len(experiment.Tasks) && experiment.Tasks[taskIndex].CalibratedMean != nil {
					bestCalibrated = math.Max(bestCalibrated, *experiment.Tasks[taskIndex].CalibratedMean)
				}
			}
			for _, experiment := range report.Experiments {
				if taskIndex >= len(experiment.Tasks) {
					row.Cells = append(row.Cells, studyTaskCellHTMLView{Calibrated: "—", StandardDev: "—", Runs: "Missing", Cost: "—"})
					continue
				}
				cell := experiment.Tasks[taskIndex]
				item := studyTaskCellHTMLView{
					Calibrated: formatOptionalPercent(cell.CalibratedMean), StandardDev: formatVarianceSD(cell.SampleVariance),
					Runs: fmt.Sprintf("%d/%d runs", cell.RepetitionsCompleted, cell.RepetitionsRequired),
					Cost: formatCostRange(cell.ObservedCost), Complete: cell.RepetitionsCompleted == cell.RepetitionsRequired,
				}
				if cell.F2PMean != nil && cell.CalibratedMean != nil && math.Abs(*cell.F2PMean-*cell.CalibratedMean) >= .0005 {
					item.Raw = formatPercent(*cell.F2PMean)
				}
				item.Winner = cell.CalibratedMean != nil && math.Abs(*cell.CalibratedMean-bestCalibrated) < 1e-12
				row.Cells = append(row.Cells, item)
			}
			view.TaskRows = append(view.TaskRows, row)
		}
	}

	comparisonByPair := make(map[string]StudyComparisonReport, len(report.Comparisons))
	for _, comparison := range report.Comparisons {
		comparisonByPair[comparison.Left+"\x00"+comparison.Right] = comparison
		comparisonByPair[comparison.Right+"\x00"+comparison.Left] = reverseStudyComparison(comparison)
	}
	view.HasComparisons = len(report.Experiments) > 1
	for baselineIndex, baseline := range report.Experiments {
		for comparisonIndex, comparisonExperiment := range report.Experiments {
			if baselineIndex == comparisonIndex {
				continue
			}
			comparison, found := comparisonByPair[comparisonExperiment.Name+"\x00"+baseline.Name]
			item := studyComparisonHTMLView{Baseline: baselineIndex + 1, Comparison: comparisonIndex + 1}
			if !found || !comparison.Available {
				item.Reason = comparison.UnavailableReason
				if item.Reason == "" {
					item.Reason = "Comparison unavailable"
				}
				view.Comparisons = append(view.Comparisons, item)
				continue
			}
			item.Available = true
			if comparison.Difference != nil {
				item.Difference = formatSignedPoints(*comparison.Difference)
				item.DifferenceValue = machineNumber(*comparison.Difference)
			}
			item.RawP = formatPValue(comparison.RawTwoSidedPValue)
			if comparison.RawTwoSidedPValue != nil {
				item.RawPValue = machineNumber(*comparison.RawTwoSidedPValue)
			}
			item.AdjustedP = formatPValue(comparison.HolmAdjustedPValue)
			if comparison.HolmAdjustedPValue != nil {
				item.AdjustedPValue = machineNumber(*comparison.HolmAdjustedPValue)
			}
			item.InferenceSource = comparison.InferenceSource
			view.Comparisons = append(view.Comparisons, item)
		}
	}
	for rowIndex, rowExperiment := range report.Experiments {
		row := studyPValueRowHTMLView{Number: rowIndex + 1, Color: studyReportColors[rowIndex%len(studyReportColors)], Name: rowExperiment.Name}
		for columnIndex, columnExperiment := range report.Experiments {
			if rowIndex == columnIndex {
				row.Cells = append(row.Cells, studyPValueCellHTMLView{Value: "—", Class: "diagonal", Title: "Same experiment"})
				continue
			}
			comparison, found := comparisonByPair[rowExperiment.Name+"\x00"+columnExperiment.Name]
			if !found || !comparison.Available {
				reason := comparison.UnavailableReason
				if reason == "" {
					reason = "comparison unavailable"
				}
				row.Cells = append(row.Cells, studyPValueCellHTMLView{Value: "N/A", Class: "unavailable", Title: reason})
				continue
			}
			className := ""
			if comparison.HolmAdjustedPValue != nil && *comparison.HolmAdjustedPValue < .05 {
				className = "significant"
			}
			difference := "—"
			if comparison.Difference != nil {
				difference = formatSignedPoints(*comparison.Difference)
			}
			source := comparison.InferenceSource
			if source == "" {
				source = inferenceSourceObserved
			}
			row.Cells = append(row.Cells, studyPValueCellHTMLView{
				Value: formatPValue(comparison.HolmAdjustedPValue), Raw: formatPValue(comparison.RawTwoSidedPValue), Class: className,
				Title: fmt.Sprintf("Row minus column: %s; raw p = %s; adjusted p = %s; Welch df %s; inference %s", difference, formatPValue(comparison.RawTwoSidedPValue), formatPValue(comparison.HolmAdjustedPValue), formatOptionalFloat(comparison.DegreesOfFreedom, 2), source),
			})
		}
		view.PValueRows = append(view.PValueRows, row)
	}
	usesProxy := usesPublishedProxy(report.Comparisons)
	view.StatisticalMethod = studyReportStatisticalMethod(usesProxy)
	if usesProxy {
		view.InferenceWarnings = append(view.InferenceWarnings, "published_proxy inference estimates uncertainty from pinned published DeepSWE runs, not from the executed repetitions in this study.")
		if provenance := firstPublishedProxyProvenance(report.Comparisons); provenance != nil {
			view.InferenceWarnings = append(view.InferenceWarnings, fmt.Sprintf(
				"Published proxy evidence is configuration %s (%s/%s) using %s with denominator %s from snapshot %s.",
				provenance.ConfigurationID, provenance.PublishedModel, provenance.PublishedReasoning, provenance.VarianceEstimator, provenance.VarianceDenominator, provenance.SnapshotSHA,
			))
		}
	}
	return view
}

func firstPublishedProxyProvenance(comparisons []StudyComparisonReport) *StudyPublishedProxyProvenance {
	for _, comparison := range comparisons {
		if comparison.Available && comparison.InferenceSource == inferenceSourcePublishedProxy && comparison.PublishedProxy != nil {
			return comparison.PublishedProxy
		}
	}
	return nil
}

func studyReportStatisticalMethod(usesPublishedProxy bool) string {
	if usesPublishedProxy {
		return "Scores combine calibrated task means using the fixed selection weights. This report uses published_proxy inference: the same pinned published sample variance is shared by both experimental arms as combined = (weight × slope)² × s² × (1/rL + 1/rR), with one Satterthwaite term per task. That estimates uncertainty from published runs, not from the executed repetitions. Observed-repetition Welch inference is preferred when every required task has at least two completed repetitions in both experiments. Comparisons use two-sided Student-t p-values and Holm step-down adjustment across all available unique experiment pairs."
	}
	return "Scores combine calibrated task means using the fixed selection weights. Pairwise variance is the sum of each task’s observed within-cell sample variance scaled by its weight and calibration slope. Comparisons use Welch–Satterthwaite degrees of freedom, two-sided Student-t p-values, and Holm step-down adjustment across all available unique experiment pairs."
}

func shortIdentifier(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func machineNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func formatPercent(value float64) string { return fmt.Sprintf("%.1f%%", value*100) }

func formatOptionalPercent(value *float64) string {
	if value == nil {
		return "—"
	}
	return formatPercent(*value)
}

func formatSignedPoints(value float64) string {
	return fmt.Sprintf("%+.1f points", value*100)
}

func formatVarianceSD(value *float64) string {
	if value == nil {
		return "SD unavailable"
	}
	return fmt.Sprintf("SD %.1f pts", math.Sqrt(math.Max(0, *value))*100)
}

func formatOptionalFloat(value *float64, precision int) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.*f", precision, *value)
}

func formatPValue(value *float64) string {
	if value == nil {
		return "—"
	}
	if *value < .001 {
		return "<0.001"
	}
	return fmt.Sprintf("%.3f", *value)
}

func formatCostRange(cost ObservedCostRange) string {
	if math.Abs(cost.Minimum-cost.Maximum) < .0005 {
		return fmt.Sprintf("$%.2f", cost.Midpoint)
	}
	return fmt.Sprintf("$%.2f–$%.2f", cost.Minimum, cost.Maximum)
}

func reverseStudyComparison(comparison StudyComparisonReport) StudyComparisonReport {
	comparison.Left, comparison.Right = comparison.Right, comparison.Left
	if comparison.Difference != nil {
		difference := -*comparison.Difference
		comparison.Difference = &difference
	}
	return comparison
}

var studyReportTemplate = template.Must(template.New("study-report").Parse(studyReportHTMLDocumentV2))
