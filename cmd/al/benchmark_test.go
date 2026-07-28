package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

func TestBenchmarkRequiresWebsitePlan(t *testing.T) {
	for _, subcommand := range []string{"baseline", "treatment", "report"} {
		root := newRootCmd()
		root.SetArgs([]string{"benchmark", subcommand})
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "requires --plan") {
			t.Fatalf("benchmark %s error = %v", subcommand, err)
		}
	}
}

func TestBenchmarkBaselineAcceptsPipedWebsiteJSON(t *testing.T) {
	original := checkBaseline
	checkBaseline = func(_ context.Context, options bench.BaselineOptions) (bench.BaselineOutcome, error) {
		if options.PlanPath != "-" || string(options.PlanJSON) != `{"plan":true}` {
			t.Fatalf("unexpected plan input: %#v", options)
		}
		return bench.BaselineOutcome{PlanID: strings.Repeat("a", 64), Required: 4, Completed: 1, EstimatedUSD: 2.5}, nil
	}
	t.Cleanup(func() { checkBaseline = original })

	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "baseline", "--check", "--plan", "-"})
	root.SetIn(strings.NewReader(`{"plan":true}`))
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "3 paid calls missing") ||
		!strings.Contains(output.String(), "No provider call was made") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func TestBenchmarkTreatmentCheckDoesNotRunTreatment(t *testing.T) {
	original := checkTreatment
	checkTreatment = func(_ context.Context, options bench.TreatmentOptions) (bench.TreatmentOutcome, error) {
		if options.Label != "Iteration 2" {
			t.Fatalf("label = %q", options.Label)
		}
		return bench.TreatmentOutcome{
			TreatmentID: strings.Repeat("b", 64), Label: options.Label,
			Required: 12, Completed: 8, Missing: 4,
		}, nil
	}
	t.Cleanup(func() { checkTreatment = original })

	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "treatment", "--check", "--plan", "plan.json", "--label", "Iteration 2"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "4 paid calls missing") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}
