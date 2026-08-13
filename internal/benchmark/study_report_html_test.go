package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestRenderStudyReportHTMLPresentsResultsAndPreservesSafeBoundaries(t *testing.T) {
	scoreA, scoreB := .72, .64
	mean, calibrated, difference := .8, .72, .08
	p, adjusted, degrees := .018, .072, 7.4
	report := StudyReport{
		StudyID: strings.Repeat("a", 64), SelectionID: strings.Repeat("b", 64), GeneratedAt: time.Date(2026, time.August, 11, 14, 30, 0, 0, time.UTC),
		Selection: StudySelectionProvenance{Model: publishedLuna, Reasoning: effortLow, SnapshotURL: "https://example.com/snapshot.json", SnapshotSHA: strings.Repeat("c", 64)},
		Execution: StudyExecutionProvenance{Command: "al benchmark run study.toml --task-concurrency 2", CLI: "Agent Layer (al)", Harness: "DataCurve Pier", HarnessVersion: "0.3.0", TaskConcurrency: 2, TaskContainerArchitecture: "amd64"},
		Experiments: []StudyExperimentReport{
			{Name: "Bare", Model: publishedLuna, Reasoning: effortLow, CompletedCells: 3, RequiredCells: 3, Score: &scoreB, ObservedCost: ObservedCostRange{Midpoint: .8, Minimum: .7, Maximum: .9}, Tasks: []StudyTaskReport{{Task: "representative-task", RepetitionsRequired: 3, RepetitionsCompleted: 3, Weight: 1, F2PMean: &mean, CalibratedMean: &calibrated}}},
			{Name: "Agent <Layer>", Model: publishedLuna, Reasoning: effortLow, CompletedCells: 3, RequiredCells: 3, Score: &scoreA, InvocationCount: 3, WorkerCountObserved: 2, ProviderClients: []string{"codex 1.2"}, ObservedCost: ObservedCostRange{Midpoint: 1.2, Minimum: 1.1, Maximum: 1.3}, ComparabilityWarnings: []string{"Provider provenance differs."}, Tasks: []StudyTaskReport{{Task: "representative-task", RepetitionsRequired: 3, RepetitionsCompleted: 3, Weight: 1, F2PMean: &mean, CalibratedMean: &calibrated}}},
		},
		Comparisons: []StudyComparisonReport{{Left: "Agent <Layer>", Right: "Bare", Available: true, Difference: &difference, DegreesOfFreedom: &degrees, RawTwoSidedPValue: &p, HolmAdjustedPValue: &adjusted}},
		HolmFamily:  StudyHolmFamily{Method: "Holm step-down", Size: 1}, Limitations: []string{"Fixed selection only."},
	}

	html, err := renderStudyReportHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	document := string(html)
	for _, want := range []string{"Benchmark Results", "Compare experiment quality, cost, and statistical evidence across a shared task set.", "Score versus cost", "plotly.js (basic - minified) v3.7.0", "error_x", "Fit to results", "Fixed · $0.10–$1,000", "Compare two experiments", "Choose both experiments", "Agent &lt;Layer&gt;", "Pairwise score evidence", "data-raw-p=\"0.018\"", "data-adjusted-p=\"0.072\"", "Holm-adjusted p = ", "raw p = ", "0.072", "raw 0.018", "adjustedSignificant", "if(adjustedSignificant)", "Task evidence", "raw 80.0%", "best-badge", "× baseline", "Comparability warning", "Provider provenance differs.", "al benchmark run study.toml --task-concurrency 2", "DataCurve Pier 0.3.0", "Download canonical report.json", "Fixed selection only."} {
		if !strings.Contains(document, want) {
			t.Errorf("rendered report missing %q", want)
		}
	}
	if strings.Contains(document, "Agent <Layer>") {
		t.Fatal("rendered report contains unescaped experiment name")
	}
	if strings.Index(document, "Bare") > strings.Index(document, "Agent &lt;Layer&gt;") {
		t.Fatal("experiment summary does not preserve declared study order")
	}
	if strings.Contains(document, "DeepSWE benchmark") || strings.Contains(document, "Study report") {
		t.Fatal("rendered report uses the rejected report labeling")
	}
	if !strings.Contains(document, `data-baseline="1" data-comparison="2"`) {
		t.Fatal("rendered report is missing interactive pairwise comparison data")
	}
	if strings.Contains(document, `rawSignificant`) {
		t.Fatal("pairwise picker still derives its verdict from the raw p-value")
	}
	if strings.Contains(document, "pts contribution") {
		t.Fatal("task table repeats derivable weighted contributions")
	}
	if !strings.Contains(document, `let baseline=null,comparison=null,role=null`) || strings.Contains(document, `class="selection-slot role-button" data-role="baseline" aria-pressed="true"`) || strings.Contains(document, `id="comparison-results" hidden`) {
		t.Fatal("interactive comparison does not start with an empty selection")
	}
}

func TestStudyReportHTMLKeepsScoresIndependentOfChartCostEligibility(t *testing.T) {
	score := 1.2
	view := newStudyReportHTMLView(StudyReport{Experiments: []StudyExperimentReport{{Name: "Zero cost", Score: &score}}})
	if len(view.Experiments) != 1 || !view.Experiments[0].HasScore || view.Experiments[0].Score != "120.0%" {
		t.Fatalf("score summary = %+v, want completed 120.0%% score", view.Experiments)
	}
	if len(view.ChartExperiments) != 0 {
		t.Fatalf("chart experiments = %+v, want zero without positive log-scale cost", view.ChartExperiments)
	}
	document, err := renderStudyReportHTML(StudyReport{Experiments: []StudyExperimentReport{{Name: "Out of range", Score: &score, ObservedCost: ObservedCostRange{Midpoint: 1, Minimum: .9, Maximum: 1.1}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-score="1.2"`, `rawScoreMax=Math.max(1,...scores)`, `range:scoreRange`} {
		if !strings.Contains(string(document), want) {
			t.Errorf("out-of-range score report missing %q", want)
		}
	}
}

func TestPinnedPlotlyBasicAsset(t *testing.T) {
	sum := sha256.Sum256([]byte(plotlyBasicJS))
	if got := hex.EncodeToString(sum[:]); got != plotlyBasicSHA256 {
		t.Fatalf("Plotly Basic %s checksum = %s, want %s", plotlyBasicVersion, got, plotlyBasicSHA256)
	}
	if !strings.Contains(plotlyBasicJS, "plotly.js (basic - minified) v"+plotlyBasicVersion) {
		t.Fatalf("embedded asset does not identify itself as Plotly Basic %s", plotlyBasicVersion)
	}
}

func TestRenderStudyReportHTMLDoesNotOfferComparisonControlsForOneExperiment(t *testing.T) {
	score := .5
	report := StudyReport{StudyID: "single", Experiments: []StudyExperimentReport{{Name: "Only experiment", Score: &score}}}
	document, err := renderStudyReportHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Compare two experiments", `class="chart-choice"`} {
		if strings.Contains(string(document), unwanted) {
			t.Errorf("single-experiment report contains %q", unwanted)
		}
	}
}

func TestRenderStudyReportHTMLExplainsPartialEvidence(t *testing.T) {
	report := StudyReport{StudyID: "unfinished", Experiments: []StudyExperimentReport{{Name: "Incomplete", CompletedCells: 1, RequiredCells: 2, Tasks: []StudyTaskReport{{Task: "task", RepetitionsRequired: 2, RepetitionsCompleted: 1}}}}, HolmFamily: StudyHolmFamily{Method: "Holm"}}
	document, err := renderStudyReportHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 of 2 runs complete", "Pending", "1/2 runs", "— calibrated", "SD unavailable"} {
		if !strings.Contains(string(document), want) {
			t.Errorf("partial report missing %q", want)
		}
	}
}
