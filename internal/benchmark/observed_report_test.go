package benchmark

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBuildObservedReportValidatesAndRendersExecutiveResult(t *testing.T) {
	document := observedAnalysisDocument{
		Schema:        observedAnalysisSchema,
		SchemaVersion: observedAnalysisSchemaVersion,
		GeneratedAt:   time.Date(2026, 7, 27, 15, 37, 0, 0, time.UTC),
		PlanID:        strings.Repeat("a", 64),
		CampaignID:    strings.Repeat("c", 64),
		Tasks: []ObservedTaskReport{{
			Task:                             "example-task",
			RepetitionsPerArm:                2,
			BaselineScores:                   []float64{.4, .6},
			TreatmentScores:                  []float64{.7, .8},
			BaselineMean:                     .5,
			TreatmentMean:                    .75,
			Difference:                       .25,
			BaselineSampleVariance:           .02,
			TreatmentSampleVariance:          .005,
			BaselineVerifierBuildFailedRuns:  1,
			TreatmentVerifierBuildFailedRuns: 1,
		}},
		Limitations: []string{"</script><img src=x>"},
	}
	document.Experiment.Model = "gpt-5-6-luna"
	document.Experiment.Reasoning = effortLow
	document.Experiment.CalibrationReference = "gpt-5-6-luna::medium"
	document.Experiment.CalibrationContrast = "gpt-5-6-luna::high"
	document.Experiment.Baseline = "bare Codex"
	document.Experiment.Treatment = "Agent Layer instructions and skills"
	document.Experiment.Tasks = 1
	document.Experiment.RepetitionsPerArm = map[string]int{"example-task": 2}
	document.Experiment.TwoSidedSignificanceLevel = .05
	document.Experiment.EqualTaskWeighting = true
	document.Experiment.AgentTimeoutMultiplier = skillsAgentTimeoutFactor
	document.Result.Verdict = "better"
	document.Result.BaselineMean = .5
	document.Result.TreatmentMean = .75
	document.Result.ObservedDifference = .25
	document.Result.StandardError = .1
	document.Result.TCriticalValue = 2
	document.Result.DecisionThreshold = .2
	document.Result.EffectiveDegreesOfFreedom = 4
	document.CostUSD.Baseline = ObservedCostRange{Midpoint: 1, Minimum: 1, Maximum: 1}
	document.CostUSD.Treatment = ObservedCostRange{Midpoint: 2, Minimum: 1.9, Maximum: 2.1}
	document.CostUSD.Total = ObservedCostRange{Midpoint: 3, Minimum: 2.9, Maximum: 3.1}
	document.TreatmentDiagnostics.InvocationCount = 8
	document.TreatmentDiagnostics.DispatchConformantRuns = 1
	document.TreatmentDiagnostics.TotalRuns = 2
	document.CostAxis.Scale = "logarithmic"
	document.CostAxis.ReferenceConfiguration = observedCostAxisReference
	document.CostAxis.ReferenceSnapshotSHA256 = strings.Repeat("b", 64)
	document.CostAxis.ReferenceEstimatedArmCostUSD = 310.61
	document.CostAxis.RoundingIncrementUSD = observedCostAxisRoundingUSD
	document.CostAxis.MaximumUSD = 350

	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildObservedCampaignReport(data)
	if err != nil {
		t.Fatal(err)
	}
	campaign := report.ObservedCampaign
	if campaign == nil || len(campaign.Versions) != 1 {
		t.Fatalf("unexpected campaign: %#v", campaign)
	}
	if report.ComparisonID != document.CampaignID {
		t.Fatalf("report comparison identity = %q; want campaign %q", report.ComparisonID, document.CampaignID)
	}
	version := campaign.Versions[0]
	if math.Abs(campaign.BaselineStandardError-.1) > 1e-12 ||
		math.Abs(version.StandardError-.05) > 1e-12 {
		t.Fatalf("unexpected arm score uncertainty: %#v", campaign)
	}
	if version.CostMultiple != 2 ||
		version.CostMultipleMinimum != 1.9 ||
		version.CostMultipleMaximum != 2.1 ||
		campaign.CostAxis.MaximumUSD != 350 {
		t.Fatalf("unexpected cost contract: %#v", campaign)
	}
	html, err := RenderHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	if strings.Contains(text, "</script><img") {
		t.Fatalf("observed report escaped embedded data as executable markup: %s", text)
	}
	for _, content := range []string{
		"Better", "&#43;25.0 pts", "±20.0-point threshold", "Score and cost",
		"Cost-accounting range", "Score uncertainty",
		"2.0×", "example-task", "$3.00", "$350", strings.Repeat("a", 64),
		"Verifier build failures: baseline 1, version 1", "4.0× agent timeout",
	} {
		if !strings.Contains(text, content) {
			t.Fatalf("observed report is missing %q: %s", content, text)
		}
	}

	second := document
	second.GeneratedAt = document.GeneratedAt.Add(time.Minute)
	second.Experiment.Treatment = "Agent Layer cost-reduction iteration"
	secondData, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	campaignReport, err := BuildObservedCampaignReport(data, secondData)
	if err != nil {
		t.Fatal(err)
	}
	if len(campaignReport.ObservedCampaign.Versions) != 2 ||
		campaignReport.ObservedCampaign.CampaignCost.Midpoint != 5 ||
		len(campaignReport.ObservedCampaign.BaselineTasks) != 1 {
		t.Fatalf("campaign did not reuse one baseline across ordered versions: %#v", campaignReport.ObservedCampaign)
	}

	second.Experiment.Baseline = "different baseline"
	differentBaseline, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildObservedCampaignReport(data, differentBaseline); err == nil {
		t.Fatal("campaign accepted a version with a different baseline")
	}

	document.Result.Verdict = "worse"
	invalid, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildObservedCampaignReport(invalid); err == nil {
		t.Fatal("observed report accepted a verdict that contradicted its threshold")
	}
}
