package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplayableVerifierFailureRequiresProviderAndVerifierBoundaries(t *testing.T) {
	completed := pierExecutionCheckpoint{ProviderCompletedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)}
	failed := pierTaskResult{ExceptionInfo: json.RawMessage(`{"type":"AgentTimeout"}`)}
	if replayableVerifierFailure("", &failed, completed) {
		t.Fatal("agent-phase failure was accepted for verifier replay")
	}
	failed.ExceptionInfo = json.RawMessage(`{"exception_type":"VerifierTimeoutError"}`)
	if !replayableVerifierFailure("", &failed, completed) {
		t.Fatal("proven verifier-phase failure was rejected")
	}
	if replayableVerifierFailure("", nil, pierExecutionCheckpoint{}) {
		t.Fatal("missing provider completion was accepted")
	}
}

func TestPierVerifierTimingAndExceptionRoundTripGateReplay(t *testing.T) {
	var result pierTaskResult
	data := []byte(`{"verifier":{"started_at":"2026-08-29T12:00:00Z","finished_at":"2026-08-29T12:30:00Z"},"exception_info":{"exception_type":"VerifierTimeoutError"}}`)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	checkpoint := pierExecutionCheckpoint{ProviderCompletedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)}
	if result.VerifierExecution == nil || result.VerifierExecution.StartedAt.IsZero() || !replayableVerifierFailure("", &result, checkpoint) {
		t.Fatalf("Pier verifier timing did not gate replay: %#v", result)
	}
	result.ExceptionInfo = json.RawMessage(`{"exception_type":"AgentTimeoutError"}`)
	if replayableVerifierFailure("", &result, checkpoint) {
		t.Fatal("agent timeout with verifier timing was accepted for replay")
	}
}

func TestIncompleteResultRequiresAdapterProviderCheckpoint(t *testing.T) {
	agentDir := t.TempDir()
	checkpoint := pierExecutionCheckpoint{ProviderCompletedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)}
	if replayableVerifierFailure(agentDir, nil, checkpoint) {
		t.Fatal("incomplete native provider result was accepted without a clean-completion checkpoint")
	}
	provider := map[string]any{
		"schema": "agent-layer-provider-checkpoint-v1", "completed_at": checkpoint.ProviderCompletedAt,
		"agent_result": map[string]any{"cost_usd": .25},
	}
	data, _ := json.Marshal(provider)
	if err := os.WriteFile(filepath.Join(agentDir, providerCheckpointFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if !replayableVerifierFailure(agentDir, nil, checkpoint) {
		t.Fatal("adapter clean-completion checkpoint was rejected")
	}
}

func TestInterruptedVerifierWithNullResultReplaysFromProviderCheckpoint(t *testing.T) {
	agentDir := t.TempDir()
	completedAt := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(map[string]any{
		"schema": "agent-layer-provider-checkpoint-v1", "completed_at": completedAt,
		"agent_result": map[string]any{"cost_usd": .25},
	})
	if err := os.WriteFile(filepath.Join(agentDir, providerCheckpointFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	result := pierTaskResult{VerifierExecution: &struct {
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
	}{StartedAt: completedAt.Add(time.Second)}}
	checkpoint := pierExecutionCheckpoint{ProviderCompletedAt: completedAt}
	if pierResultSucceeded(result) {
		t.Fatal("null verifier_result was treated as a completed Pier result")
	}
	if !replayableVerifierFailure(agentDir, &result, checkpoint) || !cleanProviderCompletion(agentDir, &result) {
		t.Fatal("interrupted verifier with durable provider output was rejected for replay")
	}
}

func retainedGrokStageFixture(t *testing.T, request ExecutionRequest) string {
	t.Helper()
	stage := writePierStage(t, request.TaskChecksum, .5, 0)
	rewriteReplayProvider(t, stage, adapterGrok)
	path := filepath.Join(stage, "jobs", "one", "agent", "grok.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := `{"type":"usage","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"reasoning_tokens":1}}` + "\n" +
		`{"type":"end","sessionId":"retained-session","stopReason":"end_turn","total_cost_usd":0.25}` + "\n"
	if err := os.WriteFile(path, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	return stage
}

func TestRetainedProviderEvidenceRequiresCompleteCostAndAcceptsEmptyPatch(t *testing.T) {
	model, effort, err := ParseModelSelection(modelGrok45 + ":low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort, Arm: ArmBaseline}
	stage := retainedGrokStageFixture(t, request)
	patch, agent, raw, err := retainedProviderEvidence(stage, request)
	if err != nil || raw == nil || filepath.Base(patch) != "model.patch" || filepath.Base(agent) != "agent" {
		t.Fatalf("retained provider evidence = patch=%q agent=%q raw=%#v err=%v", patch, agent, raw, err)
	}
	if err := os.Remove(filepath.Join(agent, "grok.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := retainedProviderEvidence(stage, request); err == nil || !strings.Contains(err.Error(), "no grok provider usage evidence") {
		t.Fatalf("incomplete retained provider evidence = %v", err)
	}
	stage = retainedGrokStageFixture(t, request)
	patch, _, _, err = retainedProviderEvidence(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// An agent that completed without committing produces an empty patch. That
	// is valid provider output, not missing evidence.
	if patch, _, _, err := retainedProviderEvidence(stage, request); err != nil || filepath.Base(patch) != "model.patch" {
		t.Fatalf("empty retained patch was rejected: %q, %v", patch, err)
	}
}

func TestSanitizationPreservesExactReplayPatchAndExcludesItFromEvidence(t *testing.T) {
	model, effort, err := ParseModelSelection(modelGrok45 + ":low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		RepoRoot: t.TempDir(), EvidenceDir: t.TempDir(), EventID: "event", Attempt: 1,
		Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort, Arm: ArmBaseline,
	}
	stage := retainedGrokStageFixture(t, request)
	evidencePatch := filepath.Join(stage, "jobs", "one", "artifacts", "model.patch")
	exact := []byte("diff --git a/notes.txt b/notes.txt\n+checkout lives at " + request.RepoRoot + "\n")
	if err := os.WriteFile(evidencePatch, exact, 0o600); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if err := sanitizePierArtifacts(request, stage); err != nil {
			t.Fatal(err)
		}
	}
	sanitized, err := os.ReadFile(evidencePatch) // #nosec G304 -- test-owned stage.
	if err != nil || bytes.Contains(sanitized, []byte(request.RepoRoot)) {
		t.Fatalf("evidence patch copy was not sanitized: %v", err)
	}
	preserved := filepath.Join(stage, replayInputDir, "model.patch")
	if data, err := os.ReadFile(preserved); err != nil || !bytes.Equal(data, exact) { // #nosec G304 -- test-owned stage.
		t.Fatalf("exact replay patch was altered or missing after repeated sanitization: %v", err)
	}
	if patch, _, _, err := retainedProviderEvidence(stage, request); err != nil || patch != preserved {
		t.Fatalf("replay input = %q, want exact copy %q: %v", patch, preserved, err)
	}
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	destination, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, replayInputDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsanitized replay input was promoted into study evidence: %v", err)
	}
}

func TestMergeRetainedProviderEvidenceKeepsOriginalAgentAndReplayVerifier(t *testing.T) {
	model, effort, err := ParseModelSelection(modelGrok45 + ":low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort, Arm: ArmBaseline}
	originalStage := retainedGrokStageFixture(t, request)
	originalResultPath, err := findPierResultPath(originalStage, request)
	if err != nil {
		t.Fatal(err)
	}
	var originalDocument map[string]json.RawMessage
	data, err := os.ReadFile(originalResultPath) // #nosec G304 -- path is beneath a test-owned temporary directory.
	if err != nil || json.Unmarshal(data, &originalDocument) != nil {
		t.Fatalf("read original result: %v", err)
	}
	originalDocument["future_pier_field"] = json.RawMessage(`{"preserved":true}`)
	data, _ = json.Marshal(originalDocument)
	if err := os.WriteFile(originalResultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	patch, agent, original, err := retainedProviderEvidence(originalStage, request)
	if err != nil {
		t.Fatal(err)
	}
	replayStage := writePierStage(t, request.TaskChecksum, .8, 0)
	checkpoint := pierExecutionCheckpoint{StartedAt: original.StartedAt, ProviderCompletedAt: original.FinishedAt}
	if err := mergeRetainedProviderEvidence(replayStage, originalStage, agent, patch, original, checkpoint, request); err != nil {
		t.Fatal(err)
	}
	merged, err := readPierTaskResult(replayStage, request)
	if err != nil {
		t.Fatal(err)
	}
	if merged.AgentInfo.ModelInfo.Provider != adapterGrok || merged.VerifierResult.Rewards.F2P != .8 ||
		!merged.StartedAt.Equal(original.StartedAt) || !merged.FinishedAt.Equal(original.FinishedAt) {
		t.Fatalf("merged replay result = %#v", merged)
	}
	mergedResultPath, err := findPierResultPath(replayStage, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(mergedResultPath) // #nosec G304 -- path is beneath a test-owned temporary directory.
	var mergedDocument map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &mergedDocument) != nil || string(mergedDocument["future_pier_field"]) != `{"preserved":true}` {
		t.Fatalf("future Pier result field was not preserved: %v, %s", err, mergedDocument["future_pier_field"])
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(patch)), "agent", "grok.jsonl")); err != nil {
		t.Fatalf("source provider evidence changed: %v", err)
	}
}

func TestMergeRetainedProviderEvidenceFillsMissingFinishTimeFromReplay(t *testing.T) {
	model, effort, err := ParseModelSelection(modelGrok45 + ":low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum", Model: model, Effort: effort, Arm: ArmBaseline}
	originalStage := retainedGrokStageFixture(t, request)
	originalResultPath, err := findPierResultPath(originalStage, request)
	if err != nil {
		t.Fatal(err)
	}
	var originalDocument map[string]json.RawMessage
	data, err := os.ReadFile(originalResultPath) // #nosec G304 -- path is beneath a test-owned temporary directory.
	if err != nil || json.Unmarshal(data, &originalDocument) != nil {
		t.Fatalf("read original result: %v", err)
	}
	delete(originalDocument, "finished_at")
	data, _ = json.Marshal(originalDocument)
	if err := os.WriteFile(originalResultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	patch, agent, original, err := retainedProviderEvidence(originalStage, request)
	if err != nil || original == nil || !original.FinishedAt.IsZero() {
		t.Fatalf("retained evidence = %#v, %v", original, err)
	}
	replayStage := writePierStage(t, request.TaskChecksum, .8, 0)
	replay, err := readPierTaskResult(replayStage, request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := pierExecutionCheckpoint{StartedAt: original.StartedAt, ProviderCompletedAt: original.StartedAt.Add(time.Second)}
	if err := mergeRetainedProviderEvidence(replayStage, originalStage, agent, patch, original, checkpoint, request); err != nil {
		t.Fatal(err)
	}
	merged, err := readPierTaskResult(replayStage, request)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.StartedAt.Equal(original.StartedAt) || !merged.FinishedAt.Equal(replay.FinishedAt) {
		t.Fatalf("merged timing = started %s finished %s; want original start %s and replay finish %s", merged.StartedAt, merged.FinishedAt, original.StartedAt, replay.FinishedAt)
	}
}

func TestFindPierResultPathIgnoresAggregateJobResult(t *testing.T) {
	request := ExecutionRequest{Task: "example-task", TaskChecksum: "task-checksum"}
	stage := writePierStage(t, request.TaskChecksum, .5, 0)
	jobRoot := filepath.Join(stage, "jobs", "one")
	trialRoot := filepath.Join(jobRoot, "example-task__Abc1234")
	if err := os.MkdirAll(trialRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	trialResult := filepath.Join(trialRoot, "result.json")
	if err := os.Rename(filepath.Join(jobRoot, "result.json"), trialResult); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobRoot, "result.json"), []byte(`{"stats":{"n_errored_trials":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := findPierResultPath(stage, request)
	if err != nil || found != trialResult {
		t.Fatalf("task result path = %q, want %q: %v", found, trialResult, err)
	}
}

func TestVerifierReplayProvenanceIsRecordedOnCanonicalReceipt(t *testing.T) {
	request := recoveryRequestFixture(t)
	request.verifierReplay = true
	if err := writePierExecutionReceipt(request, nil, nil); err != nil {
		t.Fatal(err)
	}
	destination, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt pierExecutionReceipt
	if err := readStudyJSON(filepath.Join(destination, "execution-receipt.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.VerifierReplayed {
		t.Fatalf("verifier replay provenance omitted: %#v", receipt)
	}
}
