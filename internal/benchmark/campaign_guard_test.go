package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// campaignFixture is a plan whose baseline arm is complete, so a treatment can
// legitimately be checked against it.
type campaignFixture struct {
	root        string
	planPath    string
	loaded      loadedBenchmarkPlan
	baselineDir string
	checksums   map[string]string
}

func newCampaignFixture(t *testing.T) campaignFixture {
	t.Helper()
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	writeBenchmarkPlanFixture(t, planPath)
	addCostAxisToPlanFixture(t, planPath)
	raw, err := os.ReadFile(planPath) // #nosec G304 -- planPath is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBenchmarkPlanJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{
		"first-benchmark-task":  strings.Repeat("b", 64),
		"second-benchmark-task": strings.Repeat("c", 64),
	}
	baselineDir := baselineStateDir(root, loaded.ID)
	if err := writeJSON(filepath.Join(baselineDir, "manifest.json"), baselineManifest{
		SchemaVersion: baselineStateSchema,
		PlanID:        loaded.ID,
		PlanSnapshot:  loaded.Plan.Snapshot.SHA256,
		Model:         loaded.Plan.Target.Model,
		Reasoning:     loaded.Plan.Target.Reasoning,
		DeepSWECommit: DeepSWECommit,
		PierVersion:   PierVersion,
		TaskChecksums: checksums,
		Repetitions:   repetitionsForPlan(loaded.Plan),
	}); err != nil {
		t.Fatal(err)
	}
	for task, scores := range map[string][]float64{
		"first-benchmark-task":  {.2, .4},
		"second-benchmark-task": {.6, .8},
	} {
		for index, score := range scores {
			writeCampaignAttemptFixture(t, baselineDir, loaded, task, index+1, score, .1, 0, 0, true, false, checksums[task])
		}
	}
	return campaignFixture{root: root, planPath: planPath, loaded: loaded, baselineDir: baselineDir, checksums: checksums}
}

// stubCampaignPreflight replaces the environment probes a treatment performs
// before it is allowed to spend money. Each returns nil unless a test overrides
// it, so a single failing probe can be isolated.
//
// These are package-level function variables restored with t.Cleanup, so every
// test in this package must run sequentially. Do not call t.Parallel() in a test
// that uses this helper, or in any test that could run beside one: a parallel
// test would observe another test's stub.
func stubCampaignPreflight(t *testing.T) {
	t.Helper()
	originalPreflight := preflightBenchmark
	originalPier := verifyBenchmarkPier
	originalAuth := validateBenchmarkAuthentication
	originalBundle := buildCampaignTreatmentBundle
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(string, []parsedSelection) error { return nil }
	buildCampaignTreatmentBundle = func(_ string, _ string, mode string, _ Model, _ string) (*TreatmentBundle, error) {
		return &TreatmentBundle{
			Root:          t.TempDir(),
			ManifestHash:  strings.Repeat("d", 64),
			AdapterSHA256: strings.Repeat("e", 64),
			Manifest: TreatmentManifest{
				SchemaVersion:          TreatmentSchemaVersion,
				Mode:                   mode,
				AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
				RequiredRoles:          []string{requiredRolePlanReviewer, requiredRoleImplementer, requiredRoleCodeReviewer},
			},
		}, nil
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalPier
		validateBenchmarkAuthentication = originalAuth
		buildCampaignTreatmentBundle = originalBundle
	})
}

// TestTreatmentRefusesToRunAgainstAnUnusableBaseline covers the precondition
// every published comparison rests on: a treatment arm is only meaningful next
// to a complete baseline collected under the same plan, model, and repetition
// counts. Each case makes the comparison invalid in a different way, and none of
// them may reach a provider call.
func TestTreatmentRefusesToRunAgainstAnUnusableBaseline(t *testing.T) {
	probeFailure := errors.New("probe failed")

	tests := []struct {
		name    string
		breakIt func(t *testing.T, fixture *campaignFixture, options *TreatmentOptions)
	}{
		{"task concurrency beyond the supported range", func(_ *testing.T, _ *campaignFixture, options *TreatmentOptions) {
			options.TaskConcurrency = 9
		}},
		{"no plan", func(_ *testing.T, _ *campaignFixture, options *TreatmentOptions) {
			options.PlanPath = ""
		}},
		{"no repository root", func(_ *testing.T, _ *campaignFixture, options *TreatmentOptions) {
			options.RepoRoot = ""
		}},
		{"baseline was never completed", func(t *testing.T, fixture *campaignFixture, _ *TreatmentOptions) {
			if err := os.Remove(filepath.Join(fixture.baselineDir, "manifest.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"baseline was collected for another model", func(t *testing.T, fixture *campaignFixture, _ *TreatmentOptions) {
			manifest := readBaselineManifestFixture(t, fixture)
			manifest.Model = "some-other-model"
			writeBaselineManifestFixture(t, fixture, manifest)
		}},
		{"baseline used different repetition counts", func(t *testing.T, fixture *campaignFixture, _ *TreatmentOptions) {
			manifest := readBaselineManifestFixture(t, fixture)
			manifest.Repetitions["first-benchmark-task"] = 5
			writeBaselineManifestFixture(t, fixture, manifest)
		}},
		{"baseline task checksum no longer matches its evidence", func(t *testing.T, fixture *campaignFixture, _ *TreatmentOptions) {
			manifest := readBaselineManifestFixture(t, fixture)
			manifest.TaskChecksums["first-benchmark-task"] = strings.Repeat("9", 64)
			writeBaselineManifestFixture(t, fixture, manifest)
		}},
		{"required tooling is missing", func(t *testing.T, _ *campaignFixture, _ *TreatmentOptions) {
			preflightBenchmark = func([]parsedSelection) error { return probeFailure }
		}},
		{"the pinned Pier version cannot be verified", func(t *testing.T, _ *campaignFixture, _ *TreatmentOptions) {
			verifyBenchmarkPier = func(context.Context) error { return probeFailure }
		}},
		{"provider authentication is unusable", func(t *testing.T, _ *campaignFixture, _ *TreatmentOptions) {
			validateBenchmarkAuthentication = func(string, []parsedSelection) error { return probeFailure }
		}},
		{"the treatment bundle cannot be built", func(t *testing.T, _ *campaignFixture, _ *TreatmentOptions) {
			buildCampaignTreatmentBundle = func(string, string, string, Model, string) (*TreatmentBundle, error) {
				return nil, probeFailure
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCampaignFixture(t)
			stubCampaignPreflight(t)
			options := TreatmentOptions{
				RepoRoot: fixture.root, PlanPath: fixture.planPath,
				Label: "Iteration 1", TaskConcurrency: 2, Confirmed: true,
			}
			test.breakIt(t, &fixture, &options)

			executor := &baselineFakeExecutor{}
			if _, err := RunTreatment(context.Background(), options, executor); err == nil {
				t.Fatal("treatment proceeded against an unusable baseline")
			}
			if len(executor.calls) != 0 {
				t.Fatalf("treatment made %d provider calls before failing", len(executor.calls))
			}
		})
	}
}

func readBaselineManifestFixture(t *testing.T, fixture *campaignFixture) baselineManifest {
	t.Helper()
	var manifest baselineManifest
	if err := readCampaignJSON(filepath.Join(fixture.baselineDir, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeBaselineManifestFixture(t *testing.T, fixture *campaignFixture, manifest baselineManifest) {
	t.Helper()
	if err := writeJSON(filepath.Join(fixture.baselineDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
}
