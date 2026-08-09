package benchmark

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPublishedObservedAnalysisMustBeInternallyConsistent(t *testing.T) {
	if err := validObservedAnalysisFixture().validate(); err != nil {
		t.Fatalf("consistent analysis rejected: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*observedAnalysisDocument)
		wanted string
	}{
		{
			"foreign schema",
			func(d *observedAnalysisDocument) { d.Schema = "observed-analysis-v0" },
			"unsupported or incomplete identity",
		},
		{
			"unstamped",
			func(d *observedAnalysisDocument) { d.GeneratedAt = time.Time{} },
			"unsupported or incomplete identity",
		},
		{
			"unidentified plan",
			func(d *observedAnalysisDocument) { d.PlanID = "short" },
			"unsupported or incomplete identity",
		},
		{
			"task count contradicts the task list",
			func(d *observedAnalysisDocument) { d.Experiment.Tasks = 2 },
			"invalid experiment contract",
		},
		{
			"unequal task weighting",
			func(d *observedAnalysisDocument) { d.Experiment.EqualTaskWeighting = false },
			"invalid experiment contract",
		},
		{
			"significance level outside (0,1)",
			func(d *observedAnalysisDocument) { d.Experiment.TwoSidedSignificanceLevel = 0 },
			"invalid experiment contract",
		},
		{
			"unnamed treatment",
			func(d *observedAnalysisDocument) { d.Experiment.Treatment = "" },
			"invalid experiment contract",
		},
		{
			"cost axis is not the published reference",
			func(d *observedAnalysisDocument) { d.CostAxis.ReferenceConfiguration = "some-other-model::low" },
			"invalid cost-axis contract",
		},
		{
			"cost axis maximum is not the rounded reference",
			func(d *observedAnalysisDocument) { d.CostAxis.MaximumUSD = 311 },
			"invalid cost-axis contract",
		},
		{
			"threshold contradicts the standard error",
			func(d *observedAnalysisDocument) { d.Result.DecisionThreshold = .5 },
			"inconsistent decision statistics",
		},
		{
			"difference contradicts the arm means",
			func(d *observedAnalysisDocument) { d.Result.ObservedDifference = .1 },
			"inconsistent decision statistics",
		},
		{
			"mean outside the score range",
			func(d *observedAnalysisDocument) {
				d.Result.TreatmentMean = 1.5
				d.Result.ObservedDifference = 1
			},
			"inconsistent decision statistics",
		},
		{
			"verdict contradicts the threshold",
			func(d *observedAnalysisDocument) { d.Result.Verdict = verdictWorse },
			"verdict does not match its threshold",
		},
		{
			"total cost does not reconcile",
			func(d *observedAnalysisDocument) {
				d.CostUSD.Total = ObservedCostRange{Midpoint: 4, Minimum: 3.9, Maximum: 4.1}
			},
			"total cost does not reconcile",
		},
		{
			"baseline cost range is inverted",
			func(d *observedAnalysisDocument) { d.CostUSD.Baseline.Minimum = 2 },
			"observed baseline cost",
		},
		{
			"free baseline cannot produce a cost multiple",
			func(d *observedAnalysisDocument) {
				d.CostUSD.Baseline = ObservedCostRange{}
				d.CostUSD.Total = ObservedCostRange{Midpoint: 2, Minimum: 1.9, Maximum: 2.1}
			},
			"must be positive to calculate the treatment cost multiple",
		},
		{
			"more conformant runs than runs",
			func(d *observedAnalysisDocument) { d.TreatmentDiagnostics.DispatchConformantRuns = 3 },
			"invalid treatment metadata",
		},
		{
			"no recorded invocations",
			func(d *observedAnalysisDocument) { d.TreatmentDiagnostics.InvocationCount = 0 },
			"invalid treatment metadata",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := validObservedAnalysisFixture()
			test.mutate(&document)
			// This document is the published comparison. Rendering one whose
			// verdict, threshold, or costs disagree with its own numbers would
			// put a claim on the website that its own evidence contradicts.
			err := document.validate()
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}

			data, marshalErr := json.Marshal(document)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, err := BuildObservedCampaignReport(data); err == nil {
				t.Fatalf("%s was rendered into a report anyway", test.name)
			}
		})
	}
}

func TestObservedCampaignVersionsMustShareOneBaseline(t *testing.T) {
	first := validObservedAnalysisFixture()
	firstData, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*observedAnalysisDocument)
	}{
		{"different baseline mean", func(d *observedAnalysisDocument) { d.Result.BaselineMean = .4 }},
		{"different baseline cost", func(d *observedAnalysisDocument) { d.CostUSD.Baseline.Midpoint = 1.5 }},
		{"different campaign", func(d *observedAnalysisDocument) { d.CampaignID = strings.Repeat("d", 64) }},
		{"different task allocation", func(d *observedAnalysisDocument) {
			d.Tasks[0].BaselineScores = []float64{.3, .7}
			d.Tasks[0].Task = "other-task"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			second := validObservedAnalysisFixture()
			second.GeneratedAt = first.GeneratedAt.Add(time.Minute)
			second.Experiment.Treatment = "Agent Layer iteration two"
			test.mutate(&second)
			secondData, marshalErr := json.Marshal(second)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			// Versions are plotted against one shared baseline. If two versions
			// disagree about what the baseline was, the chart would compare them
			// to different things while presenting one axis.
			if _, err := BuildObservedCampaignReport(firstData, secondData); err == nil {
				t.Fatalf("%s was accepted as one campaign", test.name)
			}
		})
	}
}
