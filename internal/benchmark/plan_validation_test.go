package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validBenchmarkPlanDocument returns the exported plan fixture as a mutable
// document so a single field can be invalidated per case.
func validBenchmarkPlanDocument(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	writeBenchmarkPlanFixture(t, path)
	addCostAxisToPlanFixture(t, path)
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func planTasksFixture(document map[string]any) []any {
	return document["tasks"].([]any)
}

func planTaskFixture(document map[string]any, index int) map[string]any {
	return planTasksFixture(document)[index].(map[string]any)
}

// TestBenchmarkPlanRejectsAnythingItCannotVouchFor covers the only gate between
// a JSON file the user pastes in and a run that spends real money against a
// provider. The plan carries the task selection, repetition counts, and spend
// estimate the campaign is later reported under, so a plan whose own numbers do
// not agree — or whose provenance is missing — must never be loaded.
func TestBenchmarkPlanRejectsAnythingItCannotVouchFor(t *testing.T) {
	if _, err := loadBenchmarkPlanJSON(nil); err == nil {
		t.Fatal("an empty plan was accepted")
	}
	if _, err := loadBenchmarkPlanJSON([]byte("not-json")); err == nil {
		t.Fatal("a plan that is not JSON was accepted")
	}

	tests := []struct {
		name    string
		breakIt func(map[string]any)
	}{
		{"unsupported schema", func(d map[string]any) { d["schema"] = "some-other-schema" }},
		{"unsupported schema version", func(d map[string]any) { d["schemaVersion"] = 99 }},
		{"the website never marked the result valid", func(d map[string]any) {
			d["result"].(map[string]any)["valid"] = false
		}},
		{"snapshot provenance points elsewhere", func(d map[string]any) {
			d["snapshot"].(map[string]any)["url"] = "https://example.com/trials.json"
		}},
		{"snapshot digest is not a full hash", func(d map[string]any) {
			d["snapshot"].(map[string]any)["sha256"] = strings.Repeat("a", 32)
		}},
		{"target model is not a supported selection", func(d map[string]any) {
			d["target"].(map[string]any)["model"] = "some-unreleased-model"
		}},
		{"no published harness", func(d map[string]any) {
			d["target"].(map[string]any)["harnesses"] = []any{}
		}},
		{"a published harness is unnamed", func(d map[string]any) {
			d["target"].(map[string]any)["harnesses"] = []any{""}
		}},
		{"no tasks selected", func(d map[string]any) { d["tasks"] = []any{} }},
		{"significance level is not a probability", func(d map[string]any) {
			d["parameters"].(map[string]any)["twoSidedSignificanceLevel"] = 1.0
		}},
		{"estimated spend exceeds the plan budget", func(d map[string]any) {
			d["parameters"].(map[string]any)["baselineBudgetUsd"] = .05
		}},
		{"decision threshold is outside the score range", func(d map[string]any) {
			d["result"].(map[string]any)["decisionThreshold"] = 1.5
		}},
		{"a task is repeated", func(d map[string]any) {
			planTaskFixture(d, 1)["id"] = planTaskFixture(d, 0)["id"]
		}},
		{"a task name is not a catalog identifier", func(d map[string]any) {
			planTaskFixture(d, 0)["id"] = "First Benchmark Task"
		}},
		{"a task has too few repetitions to estimate variance", func(d map[string]any) {
			planTaskFixture(d, 0)["repetitionsPerArm"] = 1
		}},
		{"a task has more repetitions than the plan allows", func(d map[string]any) {
			planTaskFixture(d, 0)["repetitionsPerArm"] = 5
		}},
		{"a task target mean is outside the score range", func(d map[string]any) {
			planTaskFixture(d, 0)["target"].(map[string]any)["mean"] = 1.5
		}},
		{"a task carries no cost estimate", func(d map[string]any) {
			planTaskFixture(d, 0)["targetEstimatedBaselineCostUsd"] = 0
		}},
		{"task costs do not sum to the estimated spend", func(d map[string]any) {
			planTaskFixture(d, 0)["targetEstimatedBaselineCostUsd"] = .05
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := validBenchmarkPlanDocument(t)
			test.breakIt(document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadBenchmarkPlanJSON(raw); err == nil {
				t.Fatal("an unusable benchmark plan was accepted for a paid run")
			}
		})
	}
}

// TestBenchmarkPlanIdentityIsContentAddressed covers the property that lets
// evidence be reused safely: a plan's ID is derived from its exact bytes, so a
// plan edited after a run cannot silently adopt the previous run's evidence.
func TestBenchmarkPlanIdentityIsContentAddressed(t *testing.T) {
	document := validBenchmarkPlanDocument(t)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	first, err := loadBenchmarkPlanJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := loadBenchmarkPlanJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID || len(first.ID) != 64 {
		t.Fatalf("plan identity = %q and %q, want one stable digest", first.ID, again.ID)
	}
	if first.RunCount != 4 {
		t.Fatalf("run count = %d, want the sum of the plan's repetitions", first.RunCount)
	}

	planTaskFixture(document, 0)["target"].(map[string]any)["mean"] = .30
	edited, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := loadBenchmarkPlanJSON(edited)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID == first.ID {
		t.Fatal("an edited plan reused the original plan's evidence identity")
	}
}
