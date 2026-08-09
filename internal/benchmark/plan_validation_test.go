package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchmarkPlanDocument returns the website-exported plan as a mutable map so a
// test can invalidate exactly one part of the paid allocation contract.
func benchmarkPlanDocument(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	writeBenchmarkPlanFixture(t, path)
	data, err := os.ReadFile(path) // #nosec G304 -- path is inside this test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func loadPlanDocument(t *testing.T, plan map[string]any) (loadedBenchmarkPlan, error) {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return loadBenchmarkPlanJSON(data)
}

func TestBenchmarkPlanRefusesAllocationsItCannotExecuteFaithfully(t *testing.T) {
	if _, err := loadPlanDocument(t, benchmarkPlanDocument(t)); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
		wanted string
	}{
		{
			"unknown schema",
			func(p map[string]any) { p["schema"] = "deepswe-benchmark-plan-v9" },
			"unsupported or invalid DeepSWE benchmark plan",
		},
		{
			"website marked the plan unusable",
			func(p map[string]any) { p["result"].(map[string]any)["valid"] = false },
			"unsupported or invalid DeepSWE benchmark plan",
		},
		{
			"unpinned trials snapshot",
			func(p map[string]any) { p["snapshot"].(map[string]any)["sha256"] = "short" },
			"missing pinned DeepSWE snapshot provenance",
		},
		{
			"foreign trials source",
			func(p map[string]any) { p["snapshot"].(map[string]any)["url"] = "https://example.invalid/trials.json" },
			"missing pinned DeepSWE snapshot provenance",
		},
		{
			"calibration pair is one configuration",
			func(p map[string]any) { p["calibrationContrast"] = p["calibrationReference"] },
			"invalid calibration pair",
		},
		{
			"calibration identity contradicts its parts",
			func(p map[string]any) {
				p["calibrationContrast"].(map[string]any)["id"] = "gpt-5-6-luna::medium-ish"
			},
			"invalid calibration pair",
		},
		{
			"calibration harness is unrecorded",
			func(p map[string]any) { p["calibrationReference"].(map[string]any)["harnesses"] = []any{} },
			"invalid calibration pair",
		},
		{
			"no tasks",
			func(p map[string]any) { p["tasks"] = []any{} },
			"invalid budget, result, or task selection",
		},
		{
			"spend exceeds the approved budget",
			func(p map[string]any) { p["parameters"].(map[string]any)["calibrationReferenceBudgetUsd"] = .01 },
			"invalid budget, result, or task selection",
		},
		{
			"significance level outside (0,1)",
			func(p map[string]any) { p["parameters"].(map[string]any)["twoSidedSignificanceLevel"] = 1.0 },
			"invalid budget, result, or task selection",
		},
		{
			"duplicate task allocation",
			func(p map[string]any) {
				tasks := p["tasks"].([]any)
				p["tasks"] = []any{tasks[0], tasks[0]}
			},
			"invalid task allocation",
		},
		{
			"single repetition cannot support a variance",
			func(p map[string]any) {
				p["tasks"].([]any)[0].(map[string]any)["repetitionsPerArm"] = 1.0
			},
			"invalid task allocation",
		},
		{
			"published mean outside the score range",
			func(p map[string]any) {
				p["tasks"].([]any)[0].(map[string]any)["calibrationReference"] = map[string]any{"mean": 1.5}
			},
			"invalid task allocation",
		},
		{
			"task costs contradict the estimated spend",
			func(p map[string]any) {
				p["tasks"].([]any)[0].(map[string]any)["calibrationReferenceEstimatedBaselineCostUsd"] = .05
			},
			"task costs do not match its estimated baseline spend",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := benchmarkPlanDocument(t)
			test.mutate(plan)
			// Every number in the plan determines how much real money the
			// campaign spends and what the resulting comparison is allowed to
			// claim, so an internally inconsistent plan must never execute.
			_, err := loadPlanDocument(t, plan)
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}
}

func TestBenchmarkPlanInputMustBeOneBoundedDocument(t *testing.T) {
	if _, err := loadBenchmarkPlanJSON(nil); err == nil ||
		!strings.Contains(err.Error(), "non-empty and no larger than") {
		t.Fatalf("empty plan error = %v", err)
	}
	if _, err := loadBenchmarkPlanJSON([]byte("{")); err == nil ||
		!strings.Contains(err.Error(), "decode benchmark plan") {
		t.Fatalf("truncated plan error = %v", err)
	}

	directory := t.TempDir()
	if _, err := loadBenchmarkPlanInput(directory, nil); err == nil ||
		!strings.Contains(err.Error(), "must be a non-empty JSON file") {
		t.Fatalf("directory plan error = %v", err)
	}
	if _, err := loadBenchmarkPlanInput(filepath.Join(directory, "absent.json"), nil); err == nil ||
		!strings.Contains(err.Error(), "inspect benchmark plan") {
		t.Fatalf("missing plan error = %v", err)
	}

	path := filepath.Join(directory, "plan.json")
	writeBenchmarkPlanFixture(t, path)
	fromFile, err := loadBenchmarkPlanInput(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is inside this test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	fromStandardInput, err := loadBenchmarkPlanInput(path, data)
	if err != nil {
		t.Fatal(err)
	}
	// A plan piped in has to identify the same campaign as the same plan on
	// disk, otherwise the two entry points would build separate evidence trees
	// and re-run paid cells.
	if fromFile.ID != fromStandardInput.ID {
		t.Fatalf("plan identity differs by input source: %q != %q", fromFile.ID, fromStandardInput.ID)
	}
}

func TestLegacyBenchmarkPlansAreReportOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	writeLegacyBenchmarkPlanFixture(t, path, legacyBenchmarkPlanSchema)

	loaded, err := loadBenchmarkPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Legacy || len(loaded.Plan.Tasks) == 0 {
		t.Fatalf("legacy plan = %#v", loaded)
	}
	// Version 1 plans predate the recorded execution configuration, so a new
	// paid run from one could not state which model produced its evidence.
	if _, err := bindBenchmarkExecution(loaded, "luna:low"); err == nil ||
		!strings.Contains(err.Error(), "report-only") {
		t.Fatalf("legacy execution error = %v", err)
	}
}

func TestBenchmarkExecutionSelectionMustBeExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	writeBenchmarkPlanFixture(t, path)
	loaded, err := loadBenchmarkPlan(path)
	if err != nil {
		t.Fatal(err)
	}

	// The plan carries published calibrations, not an execution target. Guessing
	// one would silently benchmark a model the operator never chose.
	if _, err := bindBenchmarkExecution(loaded, ""); err == nil ||
		!strings.Contains(err.Error(), "execution configuration is required") {
		t.Fatalf("unset execution error = %v", err)
	}
	if _, err := bindBenchmarkExecution(loaded, "gemini:low"); err == nil ||
		!strings.Contains(err.Error(), "unsupported model") {
		t.Fatalf("unsupported execution error = %v", err)
	}

	bound, err := bindBenchmarkExecution(loaded, "luna:low")
	if err != nil {
		t.Fatal(err)
	}
	if bound.Model.PublishedIdentifier != publishedLuna || bound.Effort != effortLow {
		t.Fatalf("bound execution = %#v", bound)
	}
	// The campaign identity has to fold in the execution, so the same plan run
	// at two efforts keeps two separate immutable evidence trees.
	other, err := bindBenchmarkExecution(loaded, "luna:high")
	if err != nil {
		t.Fatal(err)
	}
	if bound.CampaignID == other.CampaignID || bound.CampaignID == "" {
		t.Fatalf("campaign identity ignored execution: %q and %q", bound.CampaignID, other.CampaignID)
	}
}
