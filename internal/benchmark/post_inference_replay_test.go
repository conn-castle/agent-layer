package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const replayGrokCellCostUSD = 0.18418582

type postInferenceReplayExecutor struct {
	rejectCalls bool
	mu          sync.Mutex
	calls       int
}

func (executor *postInferenceReplayExecutor) Execute(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
	executor.mu.Lock()
	executor.calls++
	executor.mu.Unlock()
	if executor.rejectCalls {
		return AttemptResult{}, errors.New("cached replay reached the provider boundary")
	}
	result, found, err := recoverCompletedPierExecution(request)
	if err != nil {
		return AttemptResult{}, err
	}
	if !found {
		return AttemptResult{}, errors.New("retained fixture inference was not recoverable without a provider call")
	}
	return result, nil
}

func writeRetainedGrokExecution(t *testing.T, request ExecutionRequest) {
	t.Helper()
	stage := writePierStage(t, request.TaskChecksum, .8, replayGrokCellCostUSD)
	rewriteReplayProvider(t, stage, adapterGrok)
	streamPath := filepath.Join(stage, "jobs", "one", "agent", "grok.jsonl")
	if err := os.MkdirAll(filepath.Dir(streamPath), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := `{"type":"usage","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":10,"cache_creation_input_tokens":0,"reasoning_tokens":5}}` + "\n" +
		`{"type":"end","sessionId":"` + request.EventID + `","stopReason":"end_turn","total_cost_usd":` + strconv.FormatFloat(replayGrokCellCostUSD, 'f', -1, 64) + `}` + "\n"
	if err := os.WriteFile(streamPath, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	if err := writePierExecutionReceipt(request, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func (executor *postInferenceReplayExecutor) callCount() int {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.calls
}

func rewriteReplayProvider(t *testing.T, stage, provider string) {
	t.Helper()
	path := filepath.Join(stage, "jobs", "one", "result.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path beneath a test-owned stage.
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	result["agent_info"] = map[string]any{"model_info": map[string]any{"provider": provider}}
	if err := writeJSON(path, result); err != nil {
		t.Fatal(err)
	}
}

func TestSuccessfulGrokInferenceReplayCompletesAndCachesThePostRunPipeline(t *testing.T) {
	root := t.TempDir()
	writeBareStudy(t, root, modelGrok45, "minimal")
	stubStudyInfrastructure(t, root)

	originalAuthentication := validateBenchmarkAuthentication
	originalRuntimePreflight := preflightTreatmentRuntime
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{adapterGrok: {
			Provider: adapterGrok, Check: authCheckJSONFilePresence,
			AuthenticationMethod: authMethodJSONFile, VerifiedAt: time.Now().UTC(),
		}}, nil
	}
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuthentication
		preflightTreatmentRuntime = originalRuntimePreflight
	})

	dry, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), TaskConcurrency: 1, DryRun: true,
	}, &postInferenceReplayExecutor{rejectCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", dry.StudyID, "study-manifest.json")
	var manifest immutableStudyManifest
	if err := readStudyJSON(manifestPath, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Arms) != 1 {
		t.Fatalf("fixture replay study arms=%#v", manifest.Arms)
	}
	model, effort, err := ParseModelSelection(modelGrok45 + ":minimal")
	if err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(filepath.Dir(manifestPath), "arms", manifest.Arms[0].ID)
	for index, task := range []string{"first-task", "second-task"} {
		writeRetainedGrokExecution(t, ExecutionRequest{
			RepoRoot: root, EvidenceDir: evidenceDir, EventID: "retained-grok-event-" + strconv.Itoa(index+1),
			Attempt: 1, Task: task, Model: model, Effort: effort, Arm: ArmBaseline,
			TaskChecksum: task + "-checksum", EnvironmentIdentity: task + "-env",
		})
	}

	executor := &postInferenceReplayExecutor{}
	outcome, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), TaskConcurrency: 1,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if executor.callCount() != 2 || outcome.Completed != 2 || outcome.Missing != 0 {
		t.Fatalf("fixture replay outcome=%#v provider-boundary calls=%d", outcome, executor.callCount())
	}
	wantCost := 2 * replayGrokCellCostUSD
	if math.Abs(outcome.ObservedInvocationCost.Midpoint-wantCost) > 1e-12 ||
		outcome.ObservedInvocationCost.Minimum != outcome.ObservedInvocationCost.Midpoint ||
		outcome.ObservedInvocationCost.Maximum != outcome.ObservedInvocationCost.Midpoint {
		t.Fatalf("fixture replay cost=%#v, want %.8f", outcome.ObservedInvocationCost, wantCost)
	}
	for _, path := range []string{outcome.JSONPath, outcome.HTMLPath} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("fixture replay report %q: info=%v err=%v", path, info, statErr)
		}
	}
	var report StudyReport
	if err := readStudyJSON(outcome.JSONPath, &report); err != nil {
		t.Fatal(err)
	}
	if report.StudyID != outcome.StudyID || len(report.Experiments) != 1 ||
		report.Experiments[0].CompletedCells != 2 || math.Abs(report.Experiments[0].ObservedCost.Midpoint-wantCost) > 1e-12 {
		t.Fatalf("fixture replay report=%#v", report)
	}
	html, err := os.ReadFile(outcome.HTMLPath) // #nosec G304 -- path returned by the test-owned study.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "Download canonical report.json") {
		t.Fatal("fixture replay HTML omitted the canonical report link")
	}

	stateDir := filepath.Dir(filepath.Dir(outcome.JSONPath))
	results, err := filepath.Glob(filepath.Join(stateDir, "arms", "*", "attempts", "1", "tasks", "*", "result.json"))
	if err != nil || len(results) != 2 {
		t.Fatalf("fixture replay normalized results=%#v err=%v", results, err)
	}
	for _, path := range results {
		var result AttemptResult
		if err := readStudyJSON(path, &result); err != nil {
			t.Fatal(err)
		}
		if result.Provider != adapterGrok || result.CostKind != costKindProviderTotal || result.InvocationCount != 1 ||
			result.CostUSD == nil || math.Abs(*result.CostUSD-replayGrokCellCostUSD) > 1e-12 {
			t.Fatalf("fixture replay normalized result=%#v", result)
		}
	}
	streams, err := filepath.Glob(filepath.Join(stateDir, "arms", "*", "attempts", "1", "tasks", "*", "artifacts", "*", "jobs", "one", "agent", "grok.jsonl"))
	if err != nil || len(streams) != 2 {
		t.Fatalf("fixture replay retained streams=%#v err=%v", streams, err)
	}

	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, errors.New("cached replay reached authentication")
	}
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error {
		return errors.New("cached replay reached runtime preflight")
	}
	cachedExecutor := &postInferenceReplayExecutor{rejectCalls: true}
	cached, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), TaskConcurrency: 1,
	}, cachedExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if cachedExecutor.callCount() != 0 || cached.Completed != 2 || cached.Missing != 0 ||
		cached.JSONPath == "" || cached.HTMLPath == "" || cached.ObservedInvocationCost.Midpoint != 0 {
		t.Fatalf("cached fixture replay=%#v provider-boundary calls=%d", cached, cachedExecutor.callCount())
	}
}
