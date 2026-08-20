package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promotePierAttempt stages one finished Pier execution into the evidence tree
// and records the receipt that marks the cell as already paid for.
func promotePierAttempt(t *testing.T, request ExecutionRequest, commandErr error) {
	t.Helper()
	stage := writePierStage(t, request.TaskChecksum, .5, 1.25)
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	if err := writePierExecutionReceipt(request, commandErr, nil); err != nil {
		t.Fatal(err)
	}
}

func recoveryRequestFixture(t *testing.T) ExecutionRequest {
	t.Helper()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	return ExecutionRequest{
		RepoRoot: t.TempDir(), EvidenceDir: t.TempDir(), EventID: "recorded-event",
		Attempt: 1, Task: "example-task", Model: model, Effort: effort,
		Arm: ArmBaseline, TaskChecksum: strings.Repeat("c", 64),
		EnvironmentIdentity: strings.Repeat("e", 64),
	}
}

func TestFailedPierExecutionIsNotSilentlyRetriedAtProviderExpense(t *testing.T) {
	request := recoveryRequestFixture(t)
	promotePierAttempt(t, request, errors.New("pier exited 1"))

	retry := request
	retry.EventID = "would-have-called-provider"
	// The provider was already paid for this cell and it failed. Re-running
	// automatically would spend again and could quietly turn a reproducible
	// failure into a passing arm, so the operator has to decide.
	_, found, err := recoverCompletedPierExecution(retry)
	if err == nil || !strings.Contains(err.Error(), "refusing an automatic paid retry") {
		t.Fatalf("failed-cell recovery error = %v, found = %t", err, found)
	}
	if _, err := (PierExecutor{}).Execute(context.Background(), retry); err == nil ||
		!strings.Contains(err.Error(), "refusing an automatic paid retry") {
		t.Fatalf("executor retried a failed paid cell: %v", err)
	}
}

func TestFailedPierExecutionResumesOnlyThroughANewStudyInvocation(t *testing.T) {
	request := recoveryRequestFixture(t)
	promotePierAttempt(t, request, errors.New("task environment unavailable"))

	resume := request
	resume.EventID = "fresh-event"
	resume.ResumeFailedInfrastructure = true
	if _, found, err := recoverCompletedPierExecution(resume); err != nil || found {
		t.Fatalf("explicit resume recovery = found=%t err=%v", found, err)
	}
	failed, err := failedPierExecutionIDs(resume)
	if err != nil || len(failed) != 1 || failed[0] != request.EventID {
		t.Fatalf("failed execution history = %#v, %v", failed, err)
	}
	resume.resumedFailedEventIDs = failed
	if err := writePierExecutionReceipt(resume, errors.New("second infrastructure failure"), nil); err != nil {
		t.Fatal(err)
	}
	destination, err := artifactDestination(resume)
	if err != nil {
		t.Fatal(err)
	}
	var receipt pierExecutionReceipt
	if err := readStudyJSON(filepath.Join(destination, "execution-receipt.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.ResumedFailedEventIDs) != 1 || receipt.ResumedFailedEventIDs[0] != request.EventID {
		t.Fatalf("resume transition = %#v", receipt)
	}
	// The predecessor receipt remains immutable and attributable; a resumption
	// always receives a distinct event directory.
	previous, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(previous, "execution-receipt.json")); err != nil {
		t.Fatalf("original failed receipt was overwritten: %v", err)
	}
}

func TestCompletedPierExecutionIsNotSharedAcrossArmsOrConfigurations(t *testing.T) {
	request := recoveryRequestFixture(t)
	promotePierAttempt(t, request, nil)

	for _, test := range []struct {
		name   string
		mutate func(*ExecutionRequest)
	}{
		{"other arm", func(r *ExecutionRequest) { r.Arm = ArmTreatment }},
		{"other reasoning effort", func(r *ExecutionRequest) { r.Effort = effortHigh }},
		{"other model", func(r *ExecutionRequest) {
			model, _, err := ParseModelSelection("sol:low")
			if err != nil {
				t.Fatal(err)
			}
			r.Model = model
		}},
		{"other task tree", func(r *ExecutionRequest) { r.TaskChecksum = strings.Repeat("d", 64) }},
		{"other certified environment", func(r *ExecutionRequest) { r.EnvironmentIdentity = strings.Repeat("f", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			retry := request
			retry.EventID = "fresh-event"
			test.mutate(&retry)
			// Recovery is keyed on the full execution identity. Reusing a result
			// recorded under a different arm or configuration would fabricate
			// evidence for a cell that was never executed.
			_, found, err := recoverCompletedPierExecution(retry)
			if err != nil || found {
				t.Fatalf("%s recovered foreign evidence: found=%t err=%v", test.name, found, err)
			}
		})
	}
}

func TestPierExecutionReceiptMustDescribeTheCellItSitsIn(t *testing.T) {
	request := recoveryRequestFixture(t)
	promotePierAttempt(t, request, nil)
	destination, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(destination, "execution-receipt.json")

	var receipt pierExecutionReceipt
	if err := readStudyJSON(receiptPath, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.EventID = "some-other-event"
	if err := writeJSON(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}

	// A receipt that does not describe its own directory means the evidence tree
	// was edited or copied between campaigns. Skipping it would re-run a paid
	// cell; trusting it would attribute someone else's result to this one.
	if _, _, err := recoverCompletedPierExecution(request); err == nil ||
		!strings.Contains(err.Error(), "does not match its benchmark cell") {
		t.Fatalf("mismatched receipt error = %v", err)
	}
}

func TestPierArtifactsWithoutAReceiptAreNotTreatedAsCompleted(t *testing.T) {
	request := recoveryRequestFixture(t)
	stage := writePierStage(t, request.TaskChecksum, .5, 1.25)
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}

	// Artifacts are promoted before the receipt is written, so a run interrupted
	// in that window has partial evidence. Only the receipt proves the provider
	// call actually completed.
	_, found, err := recoverCompletedPierExecution(request)
	if err != nil || found {
		t.Fatalf("receiptless artifacts recovered: found=%t err=%v", found, err)
	}
}

func TestPierCleanupRefusesResourcesItCannotAttribute(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		wanted   string
	}{
		{
			"missing ownership column",
			"0123456789ab\n",
			"Docker returned malformed record",
		},
		{
			"unusable resource identity",
			"not a container id\texample-task__abc1234\n",
			"Docker returned invalid ID",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stage := writePierStage(t, "task-checksum", .5, 1)
			var removals int
			installDockerCleanupStub(t, func(_ context.Context, arguments ...string) ([]byte, error) {
				if arguments[0] == "ps" {
					return []byte(test.response), nil
				}
				removals++
				return nil, nil
			})

			// Cleanup issues forced removals, so an ownership record it cannot
			// parse must stop the sweep rather than guess which resources belong
			// to this trial.
			err := cleanupPierDockerResources(stage, ExecutionRequest{
				Task: "example-task", TaskChecksum: "task-checksum",
			})
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
			if removals != 0 {
				t.Fatalf("%s removed %d resources anyway", test.name, removals)
			}
		})
	}
}

func TestPierCleanupSurfacesDockerFailures(t *testing.T) {
	t.Run("listing", func(t *testing.T) {
		stage := writePierStage(t, "task-checksum", .5, 1)
		installDockerCleanupStub(t, func(context.Context, ...string) ([]byte, error) {
			return []byte("Cannot connect to the Docker daemon"), errors.New("exit status 1")
		})
		err := cleanupPierDockerResources(stage, ExecutionRequest{
			Task: "example-task", TaskChecksum: "task-checksum",
		})
		if err == nil || !strings.Contains(err.Error(), "list Pier containers") {
			t.Fatalf("list failure error = %v", err)
		}
	})

	t.Run("removal", func(t *testing.T) {
		stage := writePierStage(t, "task-checksum", .5, 1)
		installDockerCleanupStub(t, func(_ context.Context, arguments ...string) ([]byte, error) {
			if arguments[0] == "ps" {
				return []byte("0123456789ab\texample-task__abc1234\n"), nil
			}
			return []byte("device or resource busy"), errors.New("exit status 1")
		})
		// A container that survives cleanup keeps a Docker network and volume
		// alive; reporting success would let the next trial inherit them.
		err := cleanupPierDockerResources(stage, ExecutionRequest{
			Task: "example-task", TaskChecksum: "task-checksum",
		})
		if err == nil || !strings.Contains(err.Error(), "remove Pier containers") {
			t.Fatalf("removal failure error = %v", err)
		}
	})
}

func TestPierCleanupCannotIdentifyAnAmbiguousTrial(t *testing.T) {
	stage := t.TempDir()
	for _, trial := range []string{"example-task__Abc1234", "example-task__Zyx9876"} {
		if err := os.MkdirAll(filepath.Join(stage, "jobs", "event", trial), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Two candidate trials means cleanup cannot prove which Compose project it
	// owns, and removing the wrong one would destroy a concurrent task's
	// containers.
	_, err := identifyPierComposeProject(stage, ExecutionRequest{
		Task: "example-task", TaskChecksum: "task-checksum",
	})
	if err == nil || !strings.Contains(err.Error(), "found 2 matching trial directories") {
		t.Fatalf("ambiguous trial error = %v", err)
	}
}

func TestTreatmentPierArgumentsRequireAnImmutableBundleAndCredentials(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	request := ExecutionRequest{
		RepoRoot: repository, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmTreatment,
	}

	// The bundle is the treatment's entire identity; running without one would
	// produce a "treatment" arm indistinguishable from the baseline.
	if _, err := treatmentPierArguments(request); err == nil ||
		!strings.Contains(err.Error(), "requires an immutable treatment bundle") {
		t.Fatalf("bundle-less treatment error = %v", err)
	}

	request.Bundle = &TreatmentBundle{
		Root: filepath.Join(repository, "bundle"),
		Manifest: TreatmentManifest{
			Mode: TreatmentInstructionsAndSkills, AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
			RequiredRoles: []string{requiredRoleImplementer},
		},
	}
	if _, err := treatmentPierArguments(request); err == nil ||
		!strings.Contains(err.Error(), "codex authentication is required") {
		t.Fatalf("unauthenticated treatment error = %v", err)
	}

	authentication := filepath.Join(repository, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authentication), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authentication, []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments, err := treatmentPierArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	// Pier has to receive the timeout multiplier, the bundle, and the required
	// dispatch roles, because those are the treatment conditions the report
	// later claims were in force.
	for _, wanted := range []string{
		"--agent-timeout-multiplier 4",
		"treatment_bundle=" + request.Bundle.Root,
		"treatment_mode=" + TreatmentInstructionsAndSkills,
		"required_dispatch_roles=" + requiredRoleImplementer,
		"codex_credentials_path=" + authentication,
	} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("treatment arguments missing %q: %s", wanted, joined)
		}
	}
	if strings.Contains(joined, "preflight_only") {
		t.Fatalf("paid treatment run requested preflight mode: %s", joined)
	}

	request.PreflightOnly = true
	preflightArguments, err := treatmentPierArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(preflightArguments, " "), "preflight_only=true") {
		t.Fatalf("preflight arguments = %v", preflightArguments)
	}
}

func TestBenchmarkAuthenticationRejectsUnusableCredentials(t *testing.T) {
	repository := t.TempDir()
	codex := []parsedSelection{{model: supportedModels[0], effort: effortLow}}
	path := filepath.Join(repository, ".codex", "auth.json")

	if _, err := validateAuthentication(context.Background(), repository, codex); err == nil ||
		!strings.Contains(err.Error(), "must be a non-empty JSON file") {
		t.Fatalf("missing credential error = %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A malformed credential file fails deep inside the container minutes later,
	// after Docker work has already been spent.
	if _, err := validateAuthentication(context.Background(), repository, codex); err == nil ||
		!strings.Contains(err.Error(), "must be non-empty JSON") {
		t.Fatalf("malformed credential error = %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	if _, err := validateAuthentication(context.Background(), repository, codex); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}

	if _, err := validateAuthentication(context.Background(), repository, []parsedSelection{{model: Model{Adapter: "gemini"}}}); err == nil ||
		!strings.Contains(err.Error(), "unsupported benchmark provider adapter") {
		t.Fatalf("unsupported adapter error = %v", err)
	}
}

func installDockerCleanupStub(t *testing.T, run func(context.Context, ...string) ([]byte, error)) {
	t.Helper()
	original := runBenchmarkDockerCommand
	runBenchmarkDockerCommand = run
	t.Cleanup(func() { runBenchmarkDockerCommand = original })
}

func TestBenchmarkDockerConfigWithholdsRegistryCredentials(t *testing.T) {
	host := t.TempDir()
	if err := os.MkdirAll(filepath.Join(host, "cli-plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(host, "config.json"),
		[]byte(`{"auths":{"registry.example":{"auth":"c2VjcmV0"}}}`), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "cli-plugins", dockerComposePlugin), []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- an executable stand-in for the host Compose plugin.
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", host)

	stage := t.TempDir()
	config, err := prepareBenchmarkDockerConfig(stage)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config, "config.json")) // #nosec G304 -- path is inside this test's staging directory.
	if err != nil {
		t.Fatal(err)
	}
	// Benchmark containers run untrusted task code. Inheriting the operator's
	// registry credentials would expose them to every task image build.
	if strings.Contains(string(data), "c2VjcmV0") || !strings.Contains(string(data), `"auths":{}`) {
		t.Fatalf("benchmark Docker configuration = %s", data)
	}
	// Compose still has to work, so a plugin the host provides is linked through.
	if _, err := os.Stat(filepath.Join(config, "cli-plugins", dockerComposePlugin)); err != nil {
		t.Fatalf("Compose plugin was not linked: %v", err)
	}
	// A plugin the host does not have is simply absent rather than fatal.
	if _, err := os.Lstat(filepath.Join(config, "cli-plugins", dockerBuildxPlugin)); !os.IsNotExist(err) {
		t.Fatalf("absent buildx plugin = %v", err)
	}
}
