package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportEnvironmentIdentitiesAreDerivedWithoutDockerOrNetwork(t *testing.T) {
	checkout := t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	installAuditCheckout(t, checkout)
	dockerCommands := 0
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"),
		func(context.Context, ...string) ([]byte, error) {
			dockerCommands++
			return nil, nil
		})
	tasks := []benchmarkPlanTask{{ID: "first-task"}, {ID: "second-task"}}

	identities, err := identifyPlanTaskEnvironments(context.Background(), t.TempDir(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	// Reports are regenerated long after the paid run, often on a machine with
	// no Docker daemon. Deriving the identity has to be a pure function of the
	// pinned checkout and the readiness contract.
	if dockerCommands != 0 {
		t.Fatalf("report identity derivation ran %d Docker commands", dockerCommands)
	}
	if len(identities) != 2 {
		t.Fatalf("identities = %#v", identities)
	}
	for task, identity := range identities {
		if len(identity) != 64 {
			t.Fatalf("task %s identity = %q", task, identity)
		}
	}
	if identities["first-task"] == identities["second-task"] {
		t.Fatal("two different tasks share one environment identity")
	}

	// Certifying the same tasks must agree with the identity the report derives,
	// or a completed campaign would look uncertified at report time.
	certified, err := certifyPlanTaskEnvironments(
		context.Background(), t.TempDir(), checkout, tasks,
		map[string]string{},
	)
	if err == nil {
		t.Fatalf("certification without task checksums succeeded: %#v", certified)
	}
	if !strings.Contains(err.Error(), "no valid task checksum") {
		t.Fatalf("unchecksummed certification error = %v", err)
	}
}

func TestReportEnvironmentIdentityFailsWhenTheTaskTreeIsUnreadable(t *testing.T) {
	checkout := t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task")
	installAuditCheckout(t, checkout)
	installReadinessTestBoundaries(t, auditContractsFixture("first-task"),
		func(context.Context, ...string) ([]byte, error) { return nil, nil })

	// A task named in the plan but absent from the pinned checkout means the
	// report cannot state which environment produced the evidence.
	_, err := identifyPlanTaskEnvironments(context.Background(), t.TempDir(), []benchmarkPlanTask{{ID: "absent-task"}})
	if err == nil || !strings.Contains(err.Error(), "checksum benchmark task absent-task") {
		t.Fatalf("absent task error = %v", err)
	}
}

func TestMatrixReportRefusesInconsistentArmEvidence(t *testing.T) {
	mutateStoredResult := func(t *testing.T, arm *matrixArm, task string, mutate func(*AttemptResult)) {
		t.Helper()
		path := armResultPath(arm.StateDir, task, 1)
		var result AttemptResult
		if err := readCampaignJSON(path, &result); err != nil {
			t.Fatal(err)
		}
		mutate(&result)
		if err := writeJSON(path, result); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		name   string
		damage func(t *testing.T, arm *matrixArm)
		wanted string
	}{
		{
			"evidence certified under an environment the report cannot name",
			func(t *testing.T, arm *matrixArm) {
				mutateStoredResult(t, arm, "first-task", func(r *AttemptResult) {
					r.EnvironmentIdentity = strings.Repeat("e", 64)
				})
			},
			"mismatched execution evidence",
		},
		{
			"arm spans two provider client versions",
			func(t *testing.T, arm *matrixArm) {
				mutateStoredResult(t, arm, "second-task", func(r *AttemptResult) {
					r.ProviderClientVersion = "0.0.1"
				})
			},
			"mixes provider client versions",
		},
		{
			"a paid cell is missing",
			func(t *testing.T, arm *matrixArm) {
				if err := os.Remove(armResultPath(arm.StateDir, "second-task", 1)); err != nil {
					t.Fatal(err)
				}
			},
			"missing runs",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			selection := matrixSelectionFixture()
			selectionID, err := hashCanonical(selection)
			if err != nil {
				t.Fatal(err)
			}
			model, effort, err := ParseModelSelection("luna:low")
			if err != nil {
				t.Fatal(err)
			}
			tasks := []benchmarkPlanTask{
				{ID: "first-task", RepetitionsPerArm: 1},
				{ID: "second-task", RepetitionsPerArm: 1},
			}
			checksums := map[string]string{
				"first-task": "first-checksum", "second-task": "second-checksum",
			}
			arm := matrixArmFixture(root, "Bare luna low", ArmBaseline, model, effort, tasks)
			writeMatrixAttempt(t, &arm, "first-task", checksums["first-task"], .2, 1)
			writeMatrixAttempt(t, &arm, "second-task", checksums["second-task"], .6, 2)
			test.damage(t, &arm)

			// The report is the artifact a reader trusts. Publishing an arm whose
			// stored results disagree about the model, the provider client, or
			// how many runs happened would present a comparison that never
			// actually took place.
			_, err = buildMatrixReport(matrixPreparation{
				selection: selection, selectionID: selectionID,
				stateDir: filepath.Join(root, "matrix"), tasks: tasks,
				checksums: checksums, arms: []matrixArm{arm},
			})
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}
}

func TestMatrixExecutionStopsOnUnusableProviderEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		result func(ExecutionRequest) AttemptResult
		wanted string
	}{
		{
			"provider returned incomplete evidence",
			func(request ExecutionRequest) AttemptResult {
				result := validAttemptResultFixture()
				result.EventID, result.Task, result.TaskChecksum = request.EventID, request.Task, ""
				return result
			},
			"returned invalid evidence",
		},
		{
			"provider reported a failed run",
			func(request ExecutionRequest) AttemptResult {
				result := validAttemptResultFixture()
				result.EventID, result.Task = request.EventID, request.Task
				result.TaskChecksum = request.TaskChecksum
				result.RuntimeModel = request.Model.RuntimeIdentifier
				result.PublishedModel = request.Model.PublishedIdentifier
				result.ReasoningEffort = request.Effort
				result.Status, result.Error = statusFailed, "container exited before the patch was written"
				return result
			},
			"execution failed: container exited",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			model, effort, err := ParseModelSelection("luna:low")
			if err != nil {
				t.Fatal(err)
			}
			tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}}
			arms := []matrixArm{matrixArmFixture(root, "bare", ArmBaseline, model, effort, tasks)}

			err = executeMatrix(
				context.Background(), root,
				map[string]string{"first-task": "first-checksum"},
				map[string]string{"first-task": "first-environment"},
				arms, nil, 1, resultExecutor(test.result),
			)
			// Storing a result the analysis cannot use would leave the arm
			// looking complete while its report silently misrepresents the run.
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
			if _, statErr := os.Stat(armResultPath(arms[0].StateDir, "first-task", 1)); !os.IsNotExist(statErr) {
				t.Fatalf("%s was recorded as evidence: %v", test.name, statErr)
			}
		})
	}
}

// resultExecutor turns a per-request result into a TaskExecutor.
type resultExecutor func(ExecutionRequest) AttemptResult

func (executor resultExecutor) Execute(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
	return executor(request), nil
}
