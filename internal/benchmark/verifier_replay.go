package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	providerCheckpointFile   = "provider-checkpoint.json"
	providerCheckpointSchema = "agent-layer-provider-checkpoint-v1"
	modelInfoJSONKey         = "model_info"
)

type providerCheckpoint struct {
	Schema      string    `json:"schema"`
	CompletedAt time.Time `json:"completed_at"`
	AgentResult struct {
		CostUSD *float64 `json:"cost_usd"`
	} `json:"agent_result"`
}

func (PierExecutor) replayVerifier(ctx context.Context, request ExecutionRequest, checkpoint pierExecutionCheckpoint) (AttemptResult, error) {
	request.executionCheckpointed = true
	if _, err := os.Stat(checkpoint.StagePath); err != nil {
		return AttemptResult{}, fmt.Errorf("retained Pier staging directory %s is unavailable; refusing a paid retry: %w", checkpoint.StagePath, err)
	}
	if request.Model.Adapter == adapterAntigravity {
		credentialPath := filepath.Join(checkpoint.StagePath, antigravityOAuthStageFile)
		credential, credentialErr := os.ReadFile(credentialPath) // #nosec G304 -- checkpoint stage is confined beneath the benchmark temp root.
		if errors.Is(credentialErr, os.ErrNotExist) && !checkpoint.ArtifactsSanitizedAt.IsZero() {
			credentialErr = nil
		}
		if credentialErr != nil {
			return AttemptResult{}, fmt.Errorf("retained Antigravity evidence is not marked sanitized and its original OAuth secret is unavailable: %w", credentialErr)
		}
		if len(credential) > 0 {
			request.artifactSecrets = append(request.artifactSecrets, credentialSecretValues(credential)...)
			if err := sanitizePierArtifacts(request, checkpoint.StagePath); err != nil {
				return AttemptResult{}, fmt.Errorf("sanitize retained Antigravity evidence with its original OAuth secret: %w", err)
			}
		}
		if err := os.Remove(credentialPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return AttemptResult{}, fmt.Errorf("remove retained Antigravity OAuth profile: %w", err)
		}
	}
	patch, agentDir, original, err := retainedProviderEvidence(checkpoint.StagePath, request)
	if err != nil {
		return AttemptResult{}, fmt.Errorf("validate retained provider evidence at %s: %w", checkpoint.StagePath, err)
	}
	if err := cleanupPierDockerResources(checkpoint.StagePath, request); err != nil {
		return AttemptResult{}, fmt.Errorf("clean retained Pier execution before verifier replay: %w", err)
	}
	if original != nil && pierResultSucceeded(*original) {
		if err := promoteSanitizedPierArtifacts(request, checkpoint.StagePath); err != nil {
			return AttemptResult{}, fmt.Errorf("promote completed retained Pier execution: %w; retained staging directory: %s", err, checkpoint.StagePath)
		}
		root, err := artifactDestination(request)
		if err != nil {
			return AttemptResult{}, err
		}
		result, err := normalizePier(root, request)
		if err != nil {
			return AttemptResult{}, fmt.Errorf("normalize completed retained Pier execution: %w; retained staging directory: %s", err, checkpoint.StagePath)
		}
		if err := writePierExecutionReceipt(request, nil, nil); err != nil {
			return AttemptResult{}, err
		}
		if err := removePierExecutionCheckpoint(request); err != nil {
			return AttemptResult{}, err
		}
		_ = os.RemoveAll(checkpoint.StagePath)
		return result, nil
	}
	if checkpoint.ProviderCompletedAt.IsZero() && cleanProviderCompletion(agentDir, original) {
		completedAt := time.Now().UTC()
		if provider, providerErr := readProviderCheckpoint(agentDir); providerErr == nil && !provider.CompletedAt.IsZero() {
			completedAt = provider.CompletedAt
		} else if original != nil && original.VerifierExecution != nil && !original.VerifierExecution.StartedAt.IsZero() {
			completedAt = original.VerifierExecution.StartedAt
		}
		checkpoint.ProviderCompletedAt = completedAt
		if err := markPierProviderCompleted(request, completedAt); err != nil {
			return AttemptResult{}, err
		}
	}
	if !replayableVerifierFailure(agentDir, original, checkpoint) {
		return AttemptResult{}, fmt.Errorf("retained execution does not prove a clean provider completion followed by a verifier-phase failure; refusing both replay and a paid retry; staging directory: %s", checkpoint.StagePath)
	}

	checkout, err := ensurePinnedBenchmarkCheckout(ctx, request.RepoRoot)
	if err != nil {
		return AttemptResult{}, err
	}
	stageRoot := filepath.Join(request.RepoRoot, ".agent-layer", "tmp")
	replayStage, err := os.MkdirTemp(stageRoot, "benchmark-verifier-replay-"+request.EventID+"-")
	if err != nil {
		return AttemptResult{}, fmt.Errorf("create verifier replay staging directory: %w", err)
	}
	replaySucceeded := false
	defer func() {
		if replaySucceeded {
			_ = os.RemoveAll(replayStage)
			_ = os.RemoveAll(checkpoint.StagePath)
		}
	}()

	startupArguments, err := prepareTaskStartup(checkout, request.Task, replayStage)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	timeouts, err := loadBenchmarkTaskTimeouts(checkout, request.Task, request.AgentTimeoutMultiplier)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	if request.OnProgress != nil {
		request.OnProgress(ExecutionProgress{Phase: executionPhaseVerifier, StartedAt: time.Now().UTC(), EffectiveTimeout: timeouts.Verifier})
	}
	adapterPath, err := stageEmbeddedPierAdapter(replayStage)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	dockerConfig, err := prepareBenchmarkDockerConfig(replayStage)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	arguments := make([]string, 0, 25+len(startupArguments))
	arguments = append(arguments,
		"--from", "datacurve-pier=="+PierVersion, "pier", commandRun,
		"--path", filepath.Join(checkout, "tasks"), "--model", request.Model.RuntimeIdentifier,
		"--n-attempts", "1", "--n-concurrent", "1", "--max-retries", "0",
		"--jobs-dir", filepath.Join(replayStage, "jobs"), "--job-name", request.EventID, "--yes",
		"--include-task-name", request.Task,
		"--agent-import-path", "pier_agent_layer:AgentLayerPatchReplay",
		pierAgentKwarg, "replay_patch="+patch,
	)
	arguments = append(arguments, startupArguments...)
	command := exec.CommandContext(ctx, commandUVX, arguments...) // #nosec G204 -- identities and the retained patch are validated above.
	finishCancellation := configureBenchmarkCommandCancellation(command)
	defer finishCancellation()
	command.Dir = request.RepoRoot
	command.Env = append(benchmarkProcessEnvironment(nil), "DOCKER_CONFIG="+dockerConfig, "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+adapterPath)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	commandErr := command.Run()
	finishCancellation()
	cleanupErr := cleanupPierDockerResources(replayStage, request)
	removeConfigErr := os.RemoveAll(dockerConfig)
	_ = os.WriteFile(filepath.Join(replayStage, "pier-command.log"), output.Bytes(), 0o600)
	if err := errors.Join(commandErr, cleanupErr, removeConfigErr); err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, fmt.Errorf("verifier-only Pier replay failed: %w: %s", err, strings.TrimSpace(output.String())))
	}
	if err := mergeRetainedProviderEvidence(replayStage, checkpoint.StagePath, agentDir, patch, original, checkpoint, request); err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	if err := promoteSanitizedPierArtifacts(request, replayStage); err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	request.verifierReplay = true
	root, err := artifactDestination(request)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	result, err := normalizePier(root, request)
	if err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	var verifierErr error
	if result.Status != statusSuccess {
		verifierErr = fmt.Errorf("verifier replay did not complete successfully: %s", result.Error)
	}
	if err := writePierExecutionReceipt(request, verifierErr, nil); err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	if err := removePierExecutionCheckpoint(request); err != nil {
		return AttemptResult{}, retainedReplayError(replayStage, err)
	}
	replaySucceeded = true
	if verifierErr != nil {
		return AttemptResult{}, verifierErr
	}
	return result, nil
}

func pierResultSucceeded(result pierTaskResult) bool {
	return result.VerifierResult != nil &&
		(len(bytes.TrimSpace(result.ExceptionInfo)) == 0 || bytes.Equal(bytes.TrimSpace(result.ExceptionInfo), []byte("null")))
}

func replayableVerifierFailure(agentDir string, original *pierTaskResult, checkpoint pierExecutionCheckpoint) bool {
	if checkpoint.ProviderCompletedAt.IsZero() {
		return false
	}
	if original == nil {
		return providerCheckpointCompleted(agentDir)
	}
	exceptionType := pierExceptionType(*original)
	return !pierResultSucceeded(*original) && (verifierFailureType(exceptionType) ||
		verifierStarted(*original) && providerCheckpointCompleted(agentDir))
}

func cleanProviderCompletion(agentDir string, original *pierTaskResult) bool {
	if original == nil {
		return providerCheckpointCompleted(agentDir)
	}
	if pierResultSucceeded(*original) {
		return true
	}
	if verifierFailureType(pierExceptionType(*original)) {
		return true
	}
	return verifierStarted(*original) && providerCheckpointCompleted(agentDir)
}

func verifierStarted(result pierTaskResult) bool {
	return result.VerifierExecution != nil && !result.VerifierExecution.StartedAt.IsZero()
}

func providerCheckpointCompleted(agentDir string) bool {
	checkpoint, err := readProviderCheckpoint(agentDir)
	return err == nil && !checkpoint.CompletedAt.IsZero()
}

func pierExceptionType(result pierTaskResult) string {
	var exception struct {
		Type string `json:"exception_type"`
	}
	if err := json.Unmarshal(result.ExceptionInfo, &exception); err != nil {
		return ""
	}
	return exception.Type
}

func verifierFailureType(exceptionType string) bool {
	switch exceptionType {
	case "VerifierTimeoutError", "RewardFileNotFoundError", "RewardFileEmptyError",
		"VerifierOutputParseError", "AddTestsDirError", "DownloadVerifierDirError":
		return true
	default:
		return false
	}
}

func retainedReplayError(stage string, err error) error {
	return fmt.Errorf("%w; retained verifier replay staging directory: %s", err, stage)
}

func retainedProviderEvidence(stage string, request ExecutionRequest) (patch, agentDir string, original *pierTaskResult, returnErr error) {
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == benchmarkModelPatchFile && filepath.Base(filepath.Dir(path)) == benchmarkArtifactsDir {
			if patch != "" {
				return fmt.Errorf("retained execution has multiple model.patch files")
			}
			patch = path
			agentDir = filepath.Join(filepath.Dir(filepath.Dir(path)), "agent")
		}
		return nil
	})
	if err != nil {
		return "", "", nil, err
	}
	if patch == "" {
		return "", "", nil, fmt.Errorf("retained execution has no submitted model.patch")
	}
	if info, err := os.Stat(patch); err != nil || !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("inspect retained model.patch: %w", err)
	} else if info.Size() == 0 {
		return "", "", nil, fmt.Errorf("retained execution has an empty model.patch")
	}
	if raw, err := readPierTaskResult(stage, request); err == nil {
		original = &raw
	}
	if err := validateRetainedProviderCost(stage, agentDir, request, original); err != nil {
		return "", "", nil, err
	}
	return patch, agentDir, original, nil
}

func validateRetainedProviderCost(stage, agentDir string, request ExecutionRequest, original *pierTaskResult) error {
	switch request.Model.Adapter {
	case adapterCodex:
		_, err := codexAttemptCost(stage)
		return err
	case adapterClaudeCode:
		var cost *float64
		if original != nil {
			cost = original.AgentResult.CostUSD
		}
		if cost == nil {
			var err error
			cost, err = retainedClaudeCoordinatorCost(agentDir)
			if err != nil {
				return err
			}
		}
		_, err := treatmentClaudeCost(stage, cost)
		return err
	case adapterAntigravity, adapterGrok:
		_, err := streamProviderAttemptCost(stage, request.Model.Adapter, request.Model.RuntimeIdentifier)
		return err
	default:
		return fmt.Errorf("unsupported retained provider %q", request.Model.Adapter)
	}
}

func readProviderCheckpoint(agentDir string) (providerCheckpoint, error) {
	var checkpoint providerCheckpoint
	if err := readStudyJSON(filepath.Join(agentDir, providerCheckpointFile), &checkpoint); err != nil {
		return checkpoint, err
	}
	if checkpoint.Schema != providerCheckpointSchema {
		return checkpoint, fmt.Errorf("unsupported provider checkpoint schema %q", checkpoint.Schema)
	}
	return checkpoint, nil
}

func retainedClaudeCoordinatorCost(agentDir string) (*float64, error) {
	if checkpoint, err := readProviderCheckpoint(agentDir); err == nil && checkpoint.AgentResult.CostUSD != nil {
		return checkpoint.AgentResult.CostUSD, nil
	}
	_, cost, err := parseClaudeSessionCost(filepath.Join(agentDir, "claude-code.txt"))
	if err != nil {
		return nil, err
	}
	return &cost, nil
}

func mergeRetainedProviderEvidence(replayStage, sourceStage, sourceAgent, sourcePatch string, original *pierTaskResult, checkpoint pierExecutionCheckpoint, request ExecutionRequest) error {
	resultPath, err := findPierResultPath(replayStage, request)
	if err != nil {
		return fmt.Errorf("locate verifier replay result: %w", err)
	}
	replayAgent := filepath.Join(filepath.Dir(resultPath), "agent")
	replayPatch := filepath.Join(filepath.Dir(resultPath), benchmarkArtifactsDir, benchmarkModelPatchFile)
	sourcePatchData, err := os.ReadFile(sourcePatch) // #nosec G304 -- path was discovered beneath the confined retained stage.
	if err != nil {
		return err
	}
	replayPatchData, err := os.ReadFile(replayPatch) // #nosec G304 -- path was discovered beneath the confined replay stage.
	if err != nil {
		return err
	}
	if !bytes.Equal(sourcePatchData, replayPatchData) {
		return fmt.Errorf("verifier replay submitted patch differs from retained provider patch")
	}
	replayData, err := os.ReadFile(resultPath) // #nosec G304 -- path was discovered beneath the confined replay stage.
	if err != nil {
		return err
	}
	var replayDocument map[string]json.RawMessage
	if err := json.Unmarshal(replayData, &replayDocument); err != nil {
		return err
	}
	merged := replayDocument
	if original != nil {
		originalPath, pathErr := findPierResultPath(sourceStage, request)
		if pathErr != nil {
			return pathErr
		}
		originalData, readErr := os.ReadFile(originalPath) // #nosec G304 -- path was discovered beneath the confined retained stage.
		if readErr != nil {
			return readErr
		}
		merged = make(map[string]json.RawMessage)
		if err := json.Unmarshal(originalData, &merged); err != nil {
			return err
		}
		for _, key := range []string{"verifier_result", "verifier", "exception_info"} {
			merged[key] = replayDocument[key]
		}
	} else {
		providerInfo, _ := json.Marshal(map[string]any{modelInfoJSONKey: map[string]any{executionPhaseProvider: request.Model.Adapter}})
		merged["agent_info"] = providerInfo
		if provider, providerErr := readProviderCheckpoint(sourceAgent); providerErr == nil {
			agentResult, _ := json.Marshal(provider.AgentResult)
			merged["agent_result"] = agentResult
		}
		merged["started_at"], _ = json.Marshal(checkpoint.StartedAt)
		merged["finished_at"], _ = json.Marshal(checkpoint.ProviderCompletedAt)
	}
	provenance, _ := json.Marshal(map[string]any{
		"replayed": true, "provider_completed_at": checkpoint.ProviderCompletedAt,
		"replay_started_at": replayDocument["started_at"], "replay_finished_at": replayDocument["finished_at"],
	})
	merged["agent_layer_verifier_replay"] = provenance
	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		return err
	}
	if err := os.RemoveAll(replayAgent); err != nil {
		return err
	}
	if err := copyRequiredTree(sourceAgent, replayAgent); err != nil {
		return err
	}
	return nil
}

func findPierResultPath(stage string, request ExecutionRequest) (string, error) {
	jobsPath := filepath.Join(stage, "jobs")
	root, err := os.OpenRoot(jobsPath)
	if err != nil {
		return "", fmt.Errorf("open retained Pier jobs root: %w", err)
	}
	defer func() { _ = root.Close() }()
	var paths []string
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "result.json" {
			return nil
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return err
		}
		var identity struct {
			TaskChecksum string `json:"task_checksum"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return err
		}
		if identity.TaskChecksum == request.TaskChecksum {
			paths = append(paths, filepath.Join(jobsPath, filepath.FromSlash(path)))
		}
		return nil
	})
	if err != nil || len(paths) != 1 {
		return "", fmt.Errorf("locate retained Pier result for %s: found %d: %w", request.Task, len(paths), err)
	}
	return paths[0], nil
}

func stageEmbeddedPierAdapter(stage string) (string, error) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		return "", fmt.Errorf("read embedded Pier adapter: %w", err)
	}
	path := filepath.Join(stage, "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		return "", fmt.Errorf("stage verifier replay adapter: %w", err)
	}
	return stage, nil
}
