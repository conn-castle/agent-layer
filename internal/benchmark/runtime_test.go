package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/gitenv"
)

func TestNormalizePierPreservesOutcomeCostAndDiagnostics(t *testing.T) {
	model, effort, err := ParseModelSelection("fable:high")
	if err != nil {
		t.Fatal(err)
	}
	stage := writePierStage(t, "task-checksum", .5, 3.5)
	request := ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmBaseline, TaskChecksum: "task-checksum",
	}
	result, err := normalizePier(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusSuccess || !result.DispatchConformant ||
		result.F2PScore != .5 || *result.CostUSD != 3.5 ||
		!result.VerifierBuildFailed || result.PatchBytes == 0 {
		t.Fatalf("normalized result = %#v", result)
	}

	request.TaskChecksum = "different"
	if _, err := normalizePier(stage, request); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched checksum error = %v", err)
	}
}

func TestCompletedPierExecutionIsRecoveredWithoutProviderRetry(t *testing.T) {
	model, effort, err := ParseModelSelection("fable:high")
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	evidence := t.TempDir()
	stage := writePierStage(t, "task-checksum", .5, 3.5)
	request := ExecutionRequest{
		RepoRoot: repository, EvidenceDir: evidence, EventID: "first-event",
		Attempt: 1, Task: "example-task", Model: model, Effort: effort,
		Arm: ArmBaseline, TaskChecksum: "task-checksum", EnvironmentIdentity: "environment-one",
	}
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	if err := writePierExecutionReceipt(request, nil, nil); err != nil {
		t.Fatal(err)
	}

	retry := request
	retry.EventID = "would-have-called-provider"
	result, found, err := recoverCompletedPierExecution(retry)
	if err != nil {
		t.Fatal(err)
	}
	if !found || result.EventID != "first-event" || result.EnvironmentIdentity != "environment-one" || result.F2PScore != .5 {
		t.Fatalf("recovered result = %#v, found = %t", result, found)
	}
	executorResult, err := (PierExecutor{}).Execute(context.Background(), retry)
	if err != nil || executorResult.EventID != "first-event" {
		t.Fatalf("executor did not recover before provider setup: result=%#v err=%v", executorResult, err)
	}

	retry.EnvironmentIdentity = "changed-environment"
	if _, found, err := recoverCompletedPierExecution(retry); err != nil || found {
		t.Fatalf("changed environment recovered old cell: found=%t err=%v", found, err)
	}
}

func TestCleanupPierDockerResourcesUsesExactComposeProject(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 1)
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum"}
	original := runBenchmarkDockerCommand
	originalOS := benchmarkHostOS
	benchmarkHostOS = platformDarwin
	var calls []string
	runBenchmarkDockerCommand = func(_ context.Context, arguments ...string) ([]byte, error) {
		call := strings.Join(arguments, " ")
		calls = append(calls, call)
		switch call {
		case `ps --all --format {{.ID}}\t{{.Label "com.docker.compose.project"}}`:
			return []byte("0123456789ab\texample-task__abc1234\n999999999999\tunrelated\n"), nil
		case `network ls --format {{.ID}}\t{{.Label "com.docker.compose.project"}}`:
			return []byte("abcdef012345\texample-task__abc1234__verifier__trial\n"), nil
		case `volume ls --format {{.Name}}\t{{.Label "com.docker.compose.project"}}`:
			return nil, nil
		case `image ls --filter reference=example-task__abc1234-main:latest --format {{.ID}}`:
			return []byte("fedcba987654\n"), nil
		case `image ls --filter reference=example-task__abc1234__verifier__*-main:latest --format {{.ID}}`:
			return []byte("111111111111\n"), nil
		case "rm --force 0123456789ab", "network rm abcdef012345", "image rm fedcba987654 111111111111":
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected Docker command %q", call)
		}
	}
	t.Cleanup(func() {
		runBenchmarkDockerCommand = original
		benchmarkHostOS = originalOS
	})

	if err := cleanupPierDockerResources(stage, request); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		`ps --all --format {{.ID}}\t{{.Label "com.docker.compose.project"}}`,
		"rm --force 0123456789ab",
		`network ls --format {{.ID}}\t{{.Label "com.docker.compose.project"}}`,
		"network rm abcdef012345",
		`volume ls --format {{.Name}}\t{{.Label "com.docker.compose.project"}}`,
		`image ls --filter reference=example-task__abc1234-main:latest --format {{.ID}}`,
		`image ls --filter reference=example-task__abc1234__verifier__*-main:latest --format {{.ID}}`,
		"image rm fedcba987654 111111111111",
	}
	if strings.Join(calls, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("Docker cleanup calls = %#v", calls)
	}
}

func TestLinuxCleanupRepairsLogOwnershipBeforeContainerRemoval(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 1)
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum"}
	originalOS := benchmarkHostOS
	benchmarkHostOS = platformLinux
	t.Cleanup(func() { benchmarkHostOS = originalOS })
	var calls []string
	installDockerCleanupStub(t, func(_ context.Context, arguments ...string) ([]byte, error) {
		call := strings.Join(arguments, " ")
		calls = append(calls, call)
		switch arguments[0] {
		case "ps":
			return []byte("0123456789ab\texample-task__abc1234\n"), nil
		case "inspect":
			return []byte("true\tsha256:" + strings.Repeat("a", 64) + "\n"), nil
		case "network", "volume", "image":
			return nil, nil
		default:
			return nil, nil
		}
	})
	if err := cleanupPierDockerResources(stage, request); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	wantInspect := `inspect --format {{.State.Running}}{{"\t"}}{{.Image}} 0123456789ab`
	wantRepair := "exec --user 0 0123456789ab chown -R " + owner + " /logs"
	inspect, repair, removal := -1, -1, -1
	for index, call := range calls {
		if call == wantInspect {
			inspect = index
		}
		if call == wantRepair {
			repair = index
		}
		if call == "rm --force 0123456789ab" {
			removal = index
		}
	}
	if inspect < 0 || repair < 0 || removal < 0 || inspect >= repair || repair >= removal {
		t.Fatalf("ownership repair did not precede removal: %#v", calls)
	}
}

func TestLinuxCleanupRepairsStoppedContainerThroughItsPinnedImage(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 1)
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum"}
	originalOS := benchmarkHostOS
	benchmarkHostOS = platformLinux
	t.Cleanup(func() { benchmarkHostOS = originalOS })
	image := "sha256:" + strings.Repeat("a", 64)
	var calls []string
	installDockerCleanupStub(t, func(_ context.Context, arguments ...string) ([]byte, error) {
		call := strings.Join(arguments, " ")
		calls = append(calls, call)
		switch arguments[0] {
		case "ps":
			return []byte("0123456789ab\texample-task__abc1234\n"), nil
		case "inspect":
			return []byte("false\t" + image + "\n"), nil
		default:
			return nil, nil
		}
	})
	if err := cleanupPierDockerResources(stage, request); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	want := "run --rm --network none --user 0 --volumes-from 0123456789ab --entrypoint chown " + image + " -R " + owner + " /logs"
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, want+"\nrm --force 0123456789ab") {
		t.Fatalf("stopped-container ownership repair did not precede removal: %#v", calls)
	}
}

func TestCleanupPierDockerResourcesRejectsUnrelatedProject(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 1)
	resultPath := filepath.Join(stage, "jobs", "one", "result.json")
	data, err := os.ReadFile(resultPath) // #nosec G304 -- path is rooted in a test-owned temporary stage.
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	result["trial_name"] = "unrelated-task__Abc1234"
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	err = cleanupPierDockerResources(stage, ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum"})
	if err == nil || !strings.Contains(err.Error(), "does not match benchmark task") {
		t.Fatalf("unrelated cleanup error = %v", err)
	}
}

func TestPierCleanupIdentifiesTrialDirectoryAfterCancelledResult(t *testing.T) {
	stage := t.TempDir()
	trial := filepath.Join(stage, "jobs", "event", "example-task__Abc1234")
	if err := os.MkdirAll(trial, 0o700); err != nil {
		t.Fatal(err)
	}
	project, err := identifyPierComposeProject(stage, ExecutionRequest{
		Task: "example-task", TaskChecksum: "task-checksum",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project != "example-task__abc1234" {
		t.Fatalf("cancelled trial project = %q", project)
	}
}

func TestNormalizePierRejectsAmbiguousAndMalformedEvidence(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	jobs := filepath.Join(stage, "jobs", "job")
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmBaseline, TaskChecksum: "task-checksum",
	}
	path := filepath.Join(jobs, "result.json")
	for _, test := range []struct {
		data, wanted string
	}{
		{`{"id":"job"}`, "0 matching task results"},
		{`not-json`, "decode Pier result identity"},
		{`{"task_checksum":"task-checksum","started_at":"not-a-time"}`, "decode Pier task result"},
	} {
		if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := normalizePier(stage, request); err == nil ||
			!strings.Contains(err.Error(), test.wanted) {
			t.Fatalf("%q error = %v", test.wanted, err)
		}
	}
}

func TestDispatchConformanceMatchesRequiredTargetMultiset(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:high")
	if err != nil {
		t.Fatal(err)
	}
	opusModel, opusEffort, err := ParseModelSelection("opus:high")
	if err != nil {
		t.Fatal(err)
	}
	luna := TreatmentDispatchTarget{Agent: dispatchAgent(model), Model: dispatchModel(model), ReasoningEffort: effort}
	opus := TreatmentDispatchTarget{Agent: dispatchAgent(opusModel), Model: dispatchModel(opusModel), ReasoningEffort: opusEffort}
	skillsRequest := func(roles []string, config TreatmentDispatchConfig) ExecutionRequest {
		return ExecutionRequest{
			EventID: "event", Attempt: 1, Task: "example-task", Model: model,
			Effort: effort, Arm: ArmTreatment, TaskChecksum: "task-checksum",
			Bundle: &TreatmentBundle{Manifest: TreatmentManifest{
				Mode: TreatmentInstructionsAndSkills, RequiredRoles: roles, DispatchConfig: config,
			}},
		}
	}
	shared := defaultTreatmentDispatchConfig(model, effort)
	threeRoles := []string{requiredRolePlanReviewer, requiredRoleImplementer, requiredRoleCodeReviewer}
	completed := func(id, agent, modelName, reasoning, role string) dispatchConformanceRecord {
		return dispatchConformanceRecord{
			ID: id, Agent: agent, Model: modelName, ReasoningEffort: reasoning,
			Role: role, Mode: "fresh", State: "completed",
		}
	}
	lunaRecord := func(id, role string) dispatchConformanceRecord {
		return completed(id, luna.Agent, luna.Model, luna.ReasoningEffort, role)
	}
	protocol := []dispatchConformanceRecord{
		lunaRecord("run-0", requiredRolePlanReviewer),
		lunaRecord("run-1", requiredRoleImplementer),
		lunaRecord("run-2", requiredRoleCodeReviewer),
	}

	unconstrained := skillsRequest(nil, TreatmentDispatchConfig{})
	if conformant, err := dispatchConformance(t.TempDir(), unconstrained); err != nil || !conformant {
		t.Fatalf("unconstrained skills treatment = %t, %v", conformant, err)
	}

	stage := writePierStage(t, "task-checksum", .5, 1)
	request := skillsRequest(threeRoles, shared)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("missing lifecycle = %t, %v", conformant, err)
	}

	dispatchDir := filepath.Join(stage, "jobs", "one", dispatchEvidenceDir)
	writeDispatchRecords(t, dispatchDir, lunaRecord("run-0", ""), lunaRecord("run-1", ""), lunaRecord("run-2", ""))
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("role-less shared-target lifecycles = %t, %v", conformant, err)
	}
	writeDispatchRecords(t, dispatchDir,
		lunaRecord("run-0", requiredRolePlanReviewer),
		lunaRecord("run-1", requiredRolePlanReviewer),
		lunaRecord("run-2", requiredRolePlanReviewer),
	)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("repeated plan-review skill filled distinct roles = %t, %v", conformant, err)
	}
	implementerRequest := skillsRequest([]string{requiredRoleImplementer}, shared)
	writeDispatchRecords(t, dispatchDir, lunaRecord("run-0", requiredRolePlanReviewer))
	if conformant, err := dispatchConformance(stage, implementerRequest); err != nil || conformant {
		t.Fatalf("review-plan filled implementer = %t, %v", conformant, err)
	}
	writeDispatchRecords(t, dispatchDir, lunaRecord("run-0", requiredRoleImplementer))
	if conformant, err := dispatchConformance(stage, implementerRequest); err != nil || !conformant {
		t.Fatalf("implement-plan on configured target = %t, %v", conformant, err)
	}

	writeDispatchRecords(t, dispatchDir, protocol...)
	if conformant, err := dispatchConformance(stage, request); err != nil || !conformant {
		t.Fatalf("required target multiset = %t, %v", conformant, err)
	}
	for _, name := range []string{codexMCPPreflightEvidence, dispatchOptionsPreflightFile} {
		if err := os.WriteFile(filepath.Join(dispatchDir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if conformant, err := dispatchConformance(stage, request); err != nil || !conformant {
		t.Fatalf("lifecycle with preflight evidence = %t, %v", conformant, err)
	}

	writeDispatchRecords(t, dispatchDir, lunaRecord("run-0", requiredRoleImplementer))
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("one lifecycle cannot satisfy two roles = %t, %v", conformant, err)
	}
	writeDispatchRecords(t, dispatchDir,
		lunaRecord("run-0", requiredRolePlanReviewer),
		lunaRecord("run-0", requiredRoleImplementer),
		lunaRecord("run-0", requiredRoleCodeReviewer),
	)
	if _, err := dispatchConformance(stage, request); err == nil || !strings.Contains(err.Error(), "lifecycle \"run-0\" is duplicated") {
		t.Fatalf("duplicated lifecycle error = %v", err)
	}

	writeDispatchRecords(t, dispatchDir,
		completed("run-0", opus.Agent, opus.Model, opus.ReasoningEffort, requiredRolePlanReviewer),
		lunaRecord("run-1", requiredRoleImplementer),
		lunaRecord("run-2", requiredRoleCodeReviewer),
	)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("wrong target lifecycle = %t, %v", conformant, err)
	}

	failed := protocol[2]
	failed.State = "failed"
	writeDispatchRecords(t, dispatchDir, protocol[0], protocol[1], failed)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("incomplete lifecycle = %t, %v", conformant, err)
	}

	nested := lunaRecord("nested", requiredRoleImplementer)
	nested.ParentRunID = "run-0"
	continued := lunaRecord("continued", requiredRoleCodeReviewer)
	continued.Mode = "continued"
	protocolWith := func(extras ...dispatchConformanceRecord) []dispatchConformanceRecord {
		return append(append([]dispatchConformanceRecord{}, protocol...), extras...)
	}
	writeDispatchRecords(t, dispatchDir, protocolWith(nested, continued, completed("extra", opus.Agent, opus.Model, opus.ReasoningEffort, requiredRolePlanReviewer))...)
	if conformant, err := dispatchConformance(stage, request); err != nil || !conformant {
		t.Fatalf("nested extra records poisoned a valid multiset = %t, %v", conformant, err)
	}

	writeDispatchRecords(t, dispatchDir, nested, continued)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("nested records filled a required slot = %t, %v", conformant, err)
	}
	nestedFailed := nested
	nestedFailed.State = "failed"
	writeDispatchRecords(t, dispatchDir, protocolWith(nestedFailed)...)
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("failed nested lifecycle was ignored = %t, %v", conformant, err)
	}

	twoReviewers := shared
	twoReviewers.PlanReviewers = []TreatmentDispatchTarget{luna, opus}
	reviewerRequest := skillsRequest([]string{requiredRolePlanReviewer}, twoReviewers)
	writeDispatchRecords(t, dispatchDir,
		lunaRecord("review-luna", requiredRolePlanReviewer),
		completed("review-opus", opus.Agent, opus.Model, opus.ReasoningEffort, requiredRolePlanReviewer),
	)
	if conformant, err := dispatchConformance(stage, reviewerRequest); err != nil || !conformant {
		t.Fatalf("plan-reviewer target multiset = %t, %v", conformant, err)
	}
	writeDispatchRecords(t, dispatchDir,
		lunaRecord("review-luna", requiredRolePlanReviewer),
		lunaRecord("review-luna-2", requiredRolePlanReviewer),
	)
	if conformant, err := dispatchConformance(stage, reviewerRequest); err != nil || conformant {
		t.Fatalf("missing distinct reviewer target = %t, %v", conformant, err)
	}

	if _, err := dispatchConformance(stage, skillsRequest([]string{requiredRoleImplementer}, TreatmentDispatchConfig{})); err == nil ||
		!strings.Contains(err.Error(), "no configured implementer target") {
		t.Fatalf("missing configured target error = %v", err)
	}

	writeDispatchRecords(t, dispatchDir, protocol[0])
	if err := os.WriteFile(filepath.Join(dispatchDir, "bad.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchConformance(stage, request); err == nil ||
		!strings.Contains(err.Error(), "decode treatment dispatch evidence") {
		t.Fatalf("malformed lifecycle error = %v", err)
	}

	instructions := request
	instructions.Bundle = &TreatmentBundle{Manifest: TreatmentManifest{Mode: TreatmentInstructionsOnly}}
	if conformant, err := dispatchConformance(t.TempDir(), instructions); err == nil || conformant {
		t.Fatalf("missing jobs directory should fail visibly: %t, %v", conformant, err)
	}
}

func TestNormalizePierRetainsScoreWhenDispatchIsNonconformant(t *testing.T) {
	model, effort, err := ParseModelSelection("fable:high")
	if err != nil {
		t.Fatal(err)
	}
	stage := writePierStage(t, "task-checksum", .5, 3.5)
	request := ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmTreatment, TaskChecksum: "task-checksum",
		Bundle: &TreatmentBundle{Manifest: TreatmentManifest{
			Mode:           TreatmentInstructionsAndSkills,
			RequiredRoles:  []string{requiredRoleImplementer},
			DispatchConfig: defaultTreatmentDispatchConfig(model, effort),
		}},
	}
	result, err := normalizePier(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusSuccess || result.DispatchConformant || result.F2PScore != .5 || result.CostUSD == nil || *result.CostUSD != 3.5 {
		t.Fatalf("nonconformant normalized result = %#v", result)
	}
}

func writeDispatchRecords(t *testing.T, dir string, records ...dispatchConformanceRecord) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for index, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", index)), append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexCostUsesRequestLevelUsageAndReconcilesChildren(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	usage, err := parseCodexSessionCost(filepath.Join("testdata", "codex-session-cost.jsonl"), pricing)
	if err != nil {
		t.Fatal(err)
	}
	if usage.id != "shared-cost-session" ||
		math.Abs(usage.cost.minimum-.240304) > 1e-12 ||
		math.Abs(usage.cost.maximum-.290319) > 1e-12 {
		t.Fatalf("usage = %#v", usage)
	}
	exactUsage, err := parseCodexSessionCost(
		filepath.Join("testdata", "codex-session-cost-with-cache-writes.jsonl"),
		pricing,
	)
	if err != nil {
		t.Fatal(err)
	}
	if exactUsage.id != "exact-cost-session" ||
		math.Abs(exactUsage.cost.minimum-.290319) > 1e-12 ||
		exactUsage.cost.minimum != exactUsage.cost.maximum {
		t.Fatalf("exact usage = %#v", exactUsage)
	}
	exactFixture, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost-with-cache-writes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	zeroFixture := bytes.ReplaceAll(exactFixture, []byte(`"cache_write_input_tokens":60`), []byte(`"cache_write_input_tokens":0`))
	zeroFixture = bytes.ReplaceAll(zeroFixture, []byte(`"cache_write_input_tokens":100000`), []byte(`"cache_write_input_tokens":0`))
	zeroFixture = bytes.ReplaceAll(zeroFixture, []byte(`"cache_write_input_tokens":100060`), []byte(`"cache_write_input_tokens":0`))
	zeroPath := filepath.Join(t.TempDir(), "zero-cache-writes.jsonl")
	// #nosec G703 -- zeroPath is beneath a test-owned temporary directory.
	if err := os.WriteFile(zeroPath, zeroFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	zeroUsage, err := parseCodexSessionCost(zeroPath, pricing)
	if err != nil {
		t.Fatal(err)
	}
	if zeroUsage.cost.minimum == zeroUsage.cost.maximum {
		t.Fatalf("all-zero cache-write telemetry was treated as exact: %#v", zeroUsage)
	}

	stage := t.TempDir()
	sessions := filepath.Join(stage, "jobs", "job", "agent", "sessions")
	dispatch := filepath.Join(stage, "jobs", "job", "agent", "agent-layer-dispatch")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dispatch, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{
		"coordinator.jsonl": "shared-cost-session",
		"nested.jsonl":      "nested-session",
		"child.jsonl":       "child-session",
	} {
		data := bytes.Replace(fixture, []byte("shared-cost-session"), []byte(id), 1)
		if name == "nested.jsonl" {
			data = bytes.Replace(
				data,
				[]byte(`"source":"exec"`),
				[]byte(`"source":{"subagent":{"thread_spawn":{"parent_thread_id":"child-session"}}}`),
				1,
			)
			data = append(
				data,
				[]byte(`{"type":"session_meta","payload":{"id":"child-session","source":"exec"}}`+"\n")...,
			)
		}
		if err := os.WriteFile(filepath.Join(sessions, name), data, 0o600); err != nil { // #nosec G703 -- name comes from the fixed test fixture map.
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(dispatch, "child.json"),
		[]byte(`{"provider_session_id":"child-session","model":"gpt-5.6-luna"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	execOutput := `{"type":"turn.completed","usage":{"input_tokens":300000,"cached_input_tokens":200000,"cache_write_input_tokens":0,"output_tokens":10000}}` + "\n"
	if err := os.WriteFile(filepath.Join(dispatch, "child.stdout"), []byte(execOutput), 0o600); err != nil {
		t.Fatal(err)
	}
	cost, err := codexAttemptCost(stage)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 3 ||
		math.Abs(cost.total.minimum-.720912) > 1e-12 ||
		math.Abs(cost.total.maximum-.870957) > 1e-12 ||
		math.Abs(cost.child.maximum-.580638) > 1e-12 {
		t.Fatalf("cost = %#v", cost)
	}

	baselineStage := writePierStage(t, "task-checksum", .5, 99)
	baselineSessions := filepath.Join(baselineStage, "jobs", "one", "agent", "sessions")
	if err := os.MkdirAll(baselineSessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baselineSessions, "coordinator.jsonl"), fixture, 0o600); err != nil { // #nosec G703 -- baselineSessions is beneath a test-owned temporary directory.
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	result, err := normalizePier(baselineStage, ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmBaseline, TaskChecksum: "task-checksum",
	})
	if err != nil {
		t.Fatal(err)
	}
	if *result.CostUSD == 99 ||
		math.Abs(*result.CostMinUSD-.240304) > 1e-12 ||
		math.Abs(*result.CostMaxUSD-.290319) > 1e-12 ||
		result.CostKind != costKindProviderUsage+"-range" ||
		*result.ChildCostUSD != 0 {
		t.Fatalf("Codex baseline did not use the same token-derived cost basis: %#v", result)
	}
	if err := os.WriteFile(filepath.Join(baselineSessions, "coordinator.jsonl"), exactFixture, 0o600); err != nil { // #nosec G703 -- baselineSessions is beneath a test-owned temporary directory.
		t.Fatal(err)
	}
	exactResult, err := normalizePier(baselineStage, ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmBaseline, TaskChecksum: "task-checksum",
	})
	if err != nil {
		t.Fatal(err)
	}
	if exactResult.CostKind != costKindProviderUsage ||
		*exactResult.CostMinUSD != *exactResult.CostMaxUSD {
		t.Fatalf("Codex baseline did not use exact populated cache-write evidence: %#v", exactResult)
	}

	incomplete := filepath.Join(t.TempDir(), "session.jsonl")
	content := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"session\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"model\":\"gpt-5.6-luna\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":1}}}}\n"
	if err := os.WriteFile(incomplete, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCodexSessionCost(incomplete, pricing); err == nil ||
		!strings.Contains(err.Error(), "incomplete request-level") {
		t.Fatalf("incomplete usage error = %v", err)
	}
}

func TestCodexCostToleratesInterruptedNonBillingResponseItem(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	interrupted := []byte(`{"timestamp":"2026-08-03T20:00:03Z","type":"response_item","payload":{"type":"reasoning","encrypted_content":"partial` + "\n")
	if err := os.WriteFile(path, append(interrupted, fixture...), 0o600); err != nil { // #nosec G703 -- path is rooted in a test-owned temporary directory.
		t.Fatal(err)
	}
	usage, err := parseCodexSessionCost(path, pricing)
	if err != nil {
		t.Fatal(err)
	}
	if usage.id != "shared-cost-session" {
		t.Fatalf("usage = %#v", usage)
	}

	interruptedBilling := []byte(`{"timestamp":"2026-08-03T20:00:03Z","type":"event_msg","payload":{"type":"token_count"` + "\n")
	if err := os.WriteFile(path, append(interruptedBilling, fixture...), 0o600); err != nil { // #nosec G703 -- path is rooted in a test-owned temporary directory.
		t.Fatal(err)
	}
	if _, err := parseCodexSessionCost(path, pricing); err == nil ||
		!strings.Contains(err.Error(), "decode codex session") {
		t.Fatalf("interrupted billing error = %v", err)
	}
}

func TestCodexCostRejectsDispatchWithoutRequestLevelSessionEvidence(t *testing.T) {
	stage := t.TempDir()
	sessions := filepath.Join(stage, "jobs", "job", "agent", "sessions")
	dispatch := filepath.Join(stage, "jobs", "job", "agent", "agent-layer-dispatch")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dispatch, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "coordinator.jsonl"), fixture, 0o600); err != nil { // #nosec G703 -- sessions is beneath a test-owned temporary directory.
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dispatch, "missing.json"),
		[]byte(`{"provider_session_id":"missing-session","model":"gpt-5.6-luna"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := codexAttemptCost(stage); err == nil ||
		!strings.Contains(err.Error(), `dispatch session "missing-session" has no captured request-level session evidence`) {
		t.Fatalf("codexAttemptCost() error = %v", err)
	}
}

func TestCodexCostOmitsCallerCancelledSessionWithoutUsage(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	noUsage := []byte(`{"type":"session_meta","payload":{"id":"cancelled-session","source":"exec"}}` + "\n" +
		`{"type":"turn_context","payload":{"model":"gpt-5.6-luna"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":12.5}}}}` + "\n")
	parsed, err := parseCodexSessionCost(writeTempSession(t, noUsage), pricing)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.id != "cancelled-session" || parsed.hasCompleteUsage || parsed.cost != (costRange{}) {
		t.Fatalf("identified no-usage parse = %#v", parsed)
	}

	callerCancel := []byte(`{"provider_session_id":"cancelled-session","state":"cancelled","terminal_reason":"cancelled by caller"}`)
	stage := writeCodexCostStage(t, map[string][]byte{
		"coordinator.jsonl": coordinator,
		"cancelled.jsonl":   noUsage,
	}, map[string][]byte{
		"cancelled.json": callerCancel,
		"continued.json": callerCancel,
		"preflight.json": []byte(`{"id":"codex-mcp-preflight"}`),
	})
	cost, err := codexAttemptCost(stage)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 1 ||
		math.Abs(cost.total.minimum-.240304) > 1e-12 ||
		math.Abs(cost.total.maximum-.290319) > 1e-12 ||
		cost.child != (costRange{}) ||
		cost.coordinator != cost.total {
		t.Fatalf("omitted caller-cancelled session cost = %#v", cost)
	}
}

func TestCodexCostBillsCallerCancelledSessionWithCompleteUsage(t *testing.T) {
	coordinator, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	child := bytes.Replace(coordinator, []byte("shared-cost-session"), []byte("cancelled-session"), 1)
	stage := writeCodexCostStage(t, map[string][]byte{
		"coordinator.jsonl": coordinator,
		"cancelled.jsonl":   child,
	}, map[string][]byte{
		"cancelled.json": []byte(`{"provider_session_id":"cancelled-session","state":"cancelled","terminal_reason":"cancelled by caller"}`),
		"continued.json": []byte(`{"provider_session_id":"cancelled-session","state":"completed"}`),
	})
	cost, err := codexAttemptCost(stage)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 2 ||
		math.Abs(cost.total.minimum-.480608) > 1e-12 ||
		math.Abs(cost.total.maximum-.580638) > 1e-12 ||
		math.Abs(cost.child.minimum-.240304) > 1e-12 ||
		math.Abs(cost.child.maximum-.290319) > 1e-12 {
		t.Fatalf("billed cancelled session cost = %#v", cost)
	}
}

func TestCodexCostRejectsNoUsageWithoutProvenCallerCancellation(t *testing.T) {
	coordinator, err := os.ReadFile(filepath.Join("testdata", "codex-session-cost.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	noUsage := []byte(`{"type":"session_meta","payload":{"id":"cancelled-session","source":"exec"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}` + "\n")
	incomplete := []byte(`{"type":"session_meta","payload":{"id":"cancelled-session"}}` + "\n" +
		`{"type":"turn_context","payload":{"model":"gpt-5.6-luna"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1}}}}` + "\n")
	missingIdentity := []byte(`{"type":"turn_context","payload":{"model":"gpt-5.6-luna"}}` + "\n")
	malformed := []byte(`{"type":"session_meta","payload":{"id":"cancelled-session"}}` + "\n" +
		`{"timestamp":"2026-08-03T20:00:03Z","type":"event_msg","payload":{"type":"token_count"` + "\n")
	callerCancel := []byte(`{"provider_session_id":"cancelled-session","state":"cancelled","terminal_reason":"cancelled by caller"}`)
	for _, test := range []struct {
		name       string
		sessions   map[string][]byte
		dispatches map[string][]byte
		want       string
	}{
		{
			name: "missing dispatch",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   noUsage,
			},
			want: `session "cancelled-session" has no complete request-level usage and is not proven caller-cancelled before usage`,
		},
		{
			name: "mixed lifecycle records",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   noUsage,
			},
			dispatches: map[string][]byte{
				"cancelled.json": callerCancel,
				"continued.json": []byte(`{"provider_session_id":"cancelled-session","state":"completed"}`),
			},
			want: `session "cancelled-session" has no complete request-level usage and is not proven caller-cancelled before usage`,
		},
		{
			name: "failed dispatch",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   noUsage,
			},
			dispatches: map[string][]byte{
				"cancelled.json": []byte(`{"provider_session_id":"cancelled-session","state":"failed","terminal_reason":"cancelled by caller"}`),
			},
			want: `session "cancelled-session" has no complete request-level usage and is not proven caller-cancelled before usage`,
		},
		{
			name: "non-caller cancellation",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   noUsage,
			},
			dispatches: map[string][]byte{
				"cancelled.json": []byte(`{"provider_session_id":"cancelled-session","state":"cancelled","terminal_reason":"dispatch was interrupted before launching its worker"}`),
			},
			want: `session "cancelled-session" has no complete request-level usage and is not proven caller-cancelled before usage`,
		},
		{
			name: "incomplete non-null usage",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   incomplete,
			},
			dispatches: map[string][]byte{"cancelled.json": callerCancel},
			want:       "incomplete request-level token usage",
		},
		{
			name: "missing identity",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   missingIdentity,
			},
			dispatches: map[string][]byte{"cancelled.json": callerCancel},
			want:       "incomplete identity or billing evidence",
		},
		{
			name: "malformed billing event",
			sessions: map[string][]byte{
				"coordinator.jsonl": coordinator,
				"cancelled.jsonl":   malformed,
			},
			dispatches: map[string][]byte{"cancelled.json": callerCancel},
			want:       "decode codex session",
		},
	} {
		if _, err := codexAttemptCost(writeCodexCostStage(t, test.sessions, test.dispatches)); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s error = %v", test.name, err)
		}
	}
}

func TestClaudeCostUsesProviderReportedCoordinatorAndDispatchTotals(t *testing.T) {
	stage := t.TempDir()
	dispatch := filepath.Join(stage, "jobs", "job", "agent", "agent-layer-dispatch")
	if err := os.MkdirAll(dispatch, 0o700); err != nil {
		t.Fatal(err)
	}
	for index, item := range []struct {
		session string
		cost    float64
	}{
		{session: "11111111-1111-4111-8111-111111111111", cost: 1.25},
		{session: "22222222-2222-4222-8222-222222222222", cost: .75},
	} {
		record := fmt.Sprintf(`{"provider_session_id":%q}`, item.session)
		if err := os.WriteFile(filepath.Join(dispatch, fmt.Sprintf("%d.json", index)), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
		output := fmt.Sprintf(
			"{\"type\":\"stream_event\"}\n{\"type\":\"result\",\"session_id\":%q,\"total_cost_usd\":%.2f}\n",
			item.session,
			item.cost,
		)
		if err := os.WriteFile(filepath.Join(dispatch, fmt.Sprintf("%d.stdout", index)), []byte(output), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	coordinator := 3.0
	cost, err := treatmentClaudeCost(stage, &coordinator)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 3 ||
		cost.coordinator.minimum != 3 ||
		cost.child.minimum != 2 ||
		cost.total.minimum != 5 ||
		cost.total.maximum != 5 {
		t.Fatalf("Claude treatment cost = %#v", cost)
	}

	if err := os.Remove(filepath.Join(dispatch, "1.stdout")); err != nil {
		t.Fatal(err)
	}
	if _, err := treatmentClaudeCost(stage, &coordinator); err == nil ||
		!strings.Contains(err.Error(), "1 of 2 dispatch sessions") {
		t.Fatalf("incomplete Claude billing error = %v", err)
	}
}

func TestAuthenticationPreflightAndDockerIsolationFailLoud(t *testing.T) {
	repository := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selection := []parsedSelection{{model: model, effort: effort}}
	if _, err := validateAuthentication(context.Background(), repository, selection); err == nil {
		t.Fatal("missing credentials accepted")
	}
	auth := filepath.Join(repository, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateAuthentication(context.Background(), repository, selection); err == nil {
		t.Fatal("malformed credentials accepted")
	}
	if err := os.WriteFile(auth, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	if _, err := validateAuthentication(context.Background(), repository, selection); err != nil {
		t.Fatal(err)
	}
	if _, err := validateAuthentication(context.Background(), repository, []parsedSelection{{model: Model{Adapter: "unknown"}}}); err == nil {
		t.Fatal("unknown provider accepted")
	}

	bin := t.TempDir()
	for _, name := range []string{"git", "docker", "uvx", "codex"} {
		body := "#!/bin/sh\nexit 0\n"
		if name == "docker" {
			body = "#!/bin/sh\nprintf 'server\\n'\n"
		}
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable test stub.
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if err := preflight(selection); err != nil {
		t.Fatal(err)
	}
	if err := verifyPinnedPier(context.Background()); err != nil {
		t.Fatal(err)
	}

	dockerSource := t.TempDir()
	for _, name := range []string{dockerBuildxPlugin, dockerComposePlugin} {
		path := filepath.Join(dockerSource, "cli-plugins", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable Docker plugin fixture.
			t.Fatal(err)
		}
	}
	t.Setenv("DOCKER_CONFIG", dockerSource)
	target, err := prepareBenchmarkDockerConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(target, "config.json")) // #nosec G304 -- target is test-owned.
	if err != nil || string(config) != "{\"auths\":{}}\n" {
		t.Fatalf("Docker config = %q, %v", config, err)
	}
}

func TestArtifactSanitizationUsesVersionedEvidenceRoot(t *testing.T) {
	repository := t.TempDir()
	stage := filepath.Join(repository, ".agent-layer", "tmp", "stage")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := "top-secret"
	if err := os.WriteFile(filepath.Join(repository, ".codex", "auth.json"), []byte(`{"token":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	claudeCredentials := filepath.Join(repository, ".claude-config", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(claudeCredentials), 0o700); err != nil {
		t.Fatal(err)
	}
	claudeSecret := "claude-secret"
	if err := os.WriteFile(claudeCredentials, []byte(
		`{"accessToken":"`+claudeSecret+`","subscriptionType":"max","scopes":["user:inference"]}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(stage, "output.log"),
		[]byte(secret+" "+claudeSecret+" max user:inference "+repository),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(repository, ".agent-layer", "state", "benchmarks", "deepswe", "campaigns", strings.Repeat("a", 64), "treatments", strings.Repeat("b", 64))
	request := ExecutionRequest{
		RepoRoot: repository, EvidenceDir: evidence, EventID: "event",
		Attempt: 2, Task: "example-task",
	}
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(filepath.Join(evidence, "attempts", "2", "tasks", "example-task", "artifacts", "event", "output.log")) // #nosec G304 -- evidence is test-owned.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), secret) ||
		strings.Contains(string(output), claudeSecret) ||
		strings.Contains(string(output), repository) {
		t.Fatalf("artifact was not sanitized: %q", output)
	}
	if !strings.Contains(string(output), "max user:inference") {
		t.Fatalf("non-secret credential metadata was corrupted: %q", output)
	}
	request.EventID = "../escape"
	if _, err := artifactDestination(request); err == nil {
		t.Fatal("unsafe artifact identity accepted")
	}
}

func TestProviderCapacityRequiresExactProviderTranscriptEvidence(t *testing.T) {
	stage := t.TempDir()
	transcript := filepath.Join(stage, "jobs", "one", "agent", "codex.txt")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		transcript,
		[]byte(`{"type":"error","message":"`+providerCapacityMessage+`"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if capacity, err := hasProviderCapacityEvidence(stage); err != nil || !capacity {
		t.Fatalf("capacity evidence = %t, %v", capacity, err)
	}
	if err := os.WriteFile(transcript, []byte("generic provider failure\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if capacity, err := hasProviderCapacityEvidence(stage); err != nil || capacity {
		t.Fatalf("generic failure classified as capacity = %t, %v", capacity, err)
	}
}

func TestTreatmentRuntimePreflightRequiresEvidenceWithoutProviderSession(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 0)
	evidence := filepath.Join(stage, "jobs", "one", "agent", dispatchEvidenceDir)
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex-mcp-preflight.json", "dispatch-options-preflight.json"} {
		if err := os.WriteFile(filepath.Join(evidence, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort,
		Bundle: &TreatmentBundle{Manifest: TreatmentManifest{Mode: TreatmentInstructionsAndSkills}},
	}
	if err := validatePierTreatmentPreflight(stage, request); err != nil {
		t.Fatalf("valid runtime preflight: %v", err)
	}
	session := filepath.Join(stage, "jobs", "one", "agent", "sessions", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePierTreatmentPreflight(stage, request); err == nil ||
		!strings.Contains(err.Error(), "unexpectedly invoked the provider") {
		t.Fatalf("provider session accepted in runtime preflight: %v", err)
	}
}

func TestInstructionsOnlyRuntimePreflightDoesNotRequireDispatchEvidence(t *testing.T) {
	stage := writePierStage(t, "task-checksum", .5, 0)
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort,
		Bundle: &TreatmentBundle{Manifest: TreatmentManifest{Mode: TreatmentInstructionsOnly}},
	}
	if err := validatePierTreatmentPreflight(stage, request); err != nil {
		t.Fatalf("valid instructions-only runtime preflight: %v", err)
	}
}

func TestPinnedCheckoutValidationRejectsMissingAndWrongRepositoryState(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	if valid, err := validateExistingPinnedCheckout(context.Background(), checkout); err != nil || valid {
		t.Fatalf("missing checkout = %t, %v", valid, err)
	}
	if err := os.WriteFile(checkout, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExistingPinnedCheckout(context.Background(), checkout); err == nil ||
		!strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file checkout error = %v", err)
	}
	if err := os.Remove(checkout); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", checkout}, arguments...)...) // #nosec G204 -- fixed git operations below a test-owned path.
		// Resolve the repository from the path above, never from an inherited
		// GIT_DIR: git exports it to hooks, so under pre-commit this fixture would
		// otherwise operate on the developer's own checkout.
		command.Env = gitenv.WithoutDiscovery()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	run("init", "--quiet")
	run("config", "user.email", "benchmark@local.invalid")
	run("config", "user.name", "Benchmark")
	if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "--quiet", "-m", "fixture")
	if err := os.WriteFile(filepath.Join(checkout, "untracked.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePinnedCheckoutClean(context.Background(), checkout); err == nil ||
		!strings.Contains(err.Error(), "must be clean") {
		t.Fatalf("dirty checkout error = %v", err)
	}
	if err := os.Remove(filepath.Join(checkout, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExistingPinnedCheckout(context.Background(), checkout); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong revision error = %v", err)
	}
	if _, err := (PierExecutor{}).Execute(context.Background(), ExecutionRequest{}); err == nil ||
		!strings.Contains(err.Error(), "invalid Pier execution request") {
		t.Fatalf("invalid Pier request error = %v", err)
	}
}

func writeTempSession(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil { // #nosec G703 -- path is rooted in a test-owned temporary directory.
		t.Fatal(err)
	}
	return path
}

func writeCodexCostStage(t *testing.T, sessions, dispatches map[string][]byte) string {
	t.Helper()
	stage := t.TempDir()
	sessionDir := filepath.Join(stage, "jobs", "job", "agent", "sessions")
	dispatchDir := filepath.Join(stage, "jobs", "job", "agent", dispatchEvidenceDir)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dispatchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range sessions {
		if err := os.WriteFile(filepath.Join(sessionDir, name), data, 0o600); err != nil { // #nosec G703 -- sessionDir is beneath a test-owned temporary directory.
			t.Fatal(err)
		}
	}
	for name, data := range dispatches {
		if err := os.WriteFile(filepath.Join(dispatchDir, name), data, 0o600); err != nil { // #nosec G703 -- dispatchDir is beneath a test-owned temporary directory.
			t.Fatal(err)
		}
	}
	return stage
}

func writePierStage(t *testing.T, checksum string, score, cost float64) string {
	t.Helper()
	stage := t.TempDir()
	jobs := filepath.Join(stage, "jobs", "one")
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Second)
	raw := map[string]any{
		"trial_name":    "example-task__Abc1234",
		"task_checksum": checksum, "started_at": started, "finished_at": started.Add(time.Second),
		"agent_info":   map[string]any{"model_info": map[string]any{"provider": "openai"}},
		"agent_result": map[string]any{"cost_usd": cost},
		"verifier_result": map[string]any{"rewards": map[string]any{
			"reward": score, "f2p_total": 10, "f2p_passed": int(score * 10),
			"f2p": score, "partial": score,
		}},
		"exception_info": nil,
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs, "result.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(jobs, "artifacts", "model.patch")
	if err := os.MkdirAll(filepath.Dir(patch), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch, []byte("diff --git a/a b/a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier := filepath.Join(jobs, "verifier", "run.log")
	if err := os.MkdirAll(filepath.Dir(verifier), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verifier, []byte(`{"FailedBuild":"package"} [build failed]`), 0o600); err != nil {
		t.Fatal(err)
	}
	return stage
}
