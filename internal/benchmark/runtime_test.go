package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestDispatchConformanceUsesObservedLifecycleWithoutAffectingScore(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:high")
	if err != nil {
		t.Fatal(err)
	}
	stage := writePierStage(t, "task-checksum", .5, 1)
	request := ExecutionRequest{
		EventID: "event", Attempt: 1, Task: "example-task", Model: model,
		Effort: effort, Arm: ArmTreatment, TaskChecksum: "task-checksum",
		Bundle: &TreatmentBundle{Manifest: TreatmentManifest{
			Mode: TreatmentInstructionsAndSkills,
			RequiredRoles: []string{
				requiredRolePlanReviewer, requiredRoleImplementer, requiredRoleCodeReviewer,
			},
		}},
	}
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("missing lifecycle = %t, %v", conformant, err)
	}
	dispatchDir := filepath.Join(stage, "jobs", "one", "agent-layer-dispatch")
	if err := os.MkdirAll(dispatchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for index, skill := range []string{"review-plan", "implement-plan", "review-uncommitted-code"} {
		record := fmt.Sprintf(
			`{"id":"run-%d","agent":"codex","model":"gpt-5.6-luna","reasoning_effort":"high","skill":"%s","mode":"fresh","state":"completed"}`,
			index, skill,
		)
		if err := os.WriteFile(filepath.Join(dispatchDir, fmt.Sprintf("%d.json", index)), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if conformant, err := dispatchConformance(stage, request); err != nil || !conformant {
		t.Fatalf("complete lifecycle = %t, %v", conformant, err)
	}
	if err := os.WriteFile(filepath.Join(dispatchDir, "2.json"), []byte(`{"id":"run-2","agent":"codex","model":"gpt-5.6-luna","reasoning_effort":"high","skill":"implement-plan","mode":"fresh","state":"completed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if conformant, err := dispatchConformance(stage, request); err != nil || conformant {
		t.Fatalf("duplicated role lifecycle = %t, %v", conformant, err)
	}
	if err := os.WriteFile(filepath.Join(dispatchDir, "2.json"), []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchConformance(stage, request); err == nil ||
		!strings.Contains(err.Error(), "decode treatment dispatch evidence") {
		t.Fatalf("malformed lifecycle error = %v", err)
	}

	request.Bundle.Manifest.Mode = TreatmentInstructionsOnly
	request.Bundle.Manifest.RequiredRoles = nil
	if conformant, err := dispatchConformance(t.TempDir(), request); err == nil || conformant {
		t.Fatalf("missing jobs directory should fail visibly: %t, %v", conformant, err)
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
		"second.jsonl":      "second-session",
		"child.jsonl":       "child-session",
	} {
		data := bytes.Replace(fixture, []byte("shared-cost-session"), []byte(id), 1)
		if err := os.WriteFile(filepath.Join(sessions, name), data, 0o600); err != nil { // #nosec G703 -- name comes from the fixed test fixture map.
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dispatch, "child.json"), []byte(`{"provider_session_id":"child-session"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cost, err := codexAttemptCost(stage)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 3 ||
		math.Abs(cost.total.minimum-.720912) > 1e-12 ||
		math.Abs(cost.total.maximum-.870957) > 1e-12 ||
		math.Abs(cost.child.maximum-.290319) > 1e-12 {
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
	if err := validateAuthentication(repository, selection); err == nil {
		t.Fatal("missing credentials accepted")
	}
	auth := filepath.Join(repository, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateAuthentication(repository, selection); err == nil {
		t.Fatal("malformed credentials accepted")
	}
	if err := os.WriteFile(auth, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateAuthentication(repository, selection); err != nil {
		t.Fatal(err)
	}
	if err := validateAuthentication(repository, []parsedSelection{{model: Model{Adapter: "unknown"}}}); err == nil {
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

func TestArmExecutorPreservesSuccessesAndReturnsEveryFailure(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	plan := benchmarkPlan{}
	plan.Tasks = []benchmarkPlanTask{
		{ID: "first-task", RepetitionsPerArm: 2},
		{ID: "second-task", RepetitionsPerArm: 2},
	}
	loaded := loadedBenchmarkPlan{
		ID: strings.Repeat("a", 64), Plan: plan, Model: model, Effort: effort, RunCount: 4,
	}
	executor := &selectiveFailureExecutor{failures: map[string]bool{
		"first-task:1": true, "second-task:2": true,
	}}
	root := t.TempDir()
	execution := armExecution{
		repoRoot: root, stateDir: filepath.Join(root, "evidence"),
		arm: ArmBaseline, concurrency: 2, loaded: loaded,
		checksums: map[string]string{"first-task": "first", "second-task": "second"},
	}
	err = executePlanArm(context.Background(), execution, executor)
	if err == nil || !strings.Contains(err.Error(), "first-task repetition 1") ||
		!strings.Contains(err.Error(), "second-task repetition 2") {
		t.Fatalf("aggregate failures = %v", err)
	}
	if missing := missingPlanCells(execution); len(missing) != 2 {
		t.Fatalf("missing cells = %#v; completed successes were not preserved", missing)
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

type selectiveFailureExecutor struct {
	mutex    sync.Mutex
	failures map[string]bool
}

func (executor *selectiveFailureExecutor) Execute(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
	key := fmt.Sprintf("%s:%d", request.Task, request.Attempt)
	executor.mutex.Lock()
	fails := executor.failures[key]
	executor.mutex.Unlock()
	if fails {
		return AttemptResult{}, errors.New("observed failure")
	}
	cost, duration := .1, 1.0
	now := time.Now().UTC()
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, Status: statusSuccess,
		F2PPassed: 1, F2PTotal: 2, F2PScore: .5,
		CostUSD: &cost, CostKind: costKindProviderReported,
		DurationSeconds: &duration, TaskChecksum: request.TaskChecksum,
		StartedAt: now, FinishedAt: now.Add(time.Second),
		Provider: request.Model.Adapter, PublishedModel: request.Model.PublishedIdentifier,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		ProviderClientVersion: request.Model.ProviderClientVersion,
	}, nil
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
