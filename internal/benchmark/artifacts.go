package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const pierExecutionReceiptSchema = "deepswe-pier-execution-v1"
const pierExecutionCheckpointSchema = "deepswe-pier-execution-checkpoint-v1"
const credentialKeyName = "key"

type pierExecutionCheckpoint struct {
	SchemaVersion        string    `json:"schema_version"`
	EventID              string    `json:"event_id"`
	Attempt              int       `json:"attempt"`
	Task                 string    `json:"task"`
	TaskChecksum         string    `json:"task_checksum"`
	EnvironmentIdentity  string    `json:"task_environment_identity,omitempty"`
	Arm                  string    `json:"arm"`
	RuntimeModel         string    `json:"runtime_model"`
	ReasoningEffort      string    `json:"reasoning_effort"`
	TreatmentHash        string    `json:"treatment_manifest_hash,omitempty"`
	StagePath            string    `json:"stage_path"`
	StartedAt            time.Time `json:"started_at"`
	ProviderCompletedAt  time.Time `json:"provider_completed_at,omitempty"`
	ArtifactsSanitizedAt time.Time `json:"artifacts_sanitized_at,omitempty"`
}

type pierExecutionReceipt struct {
	SchemaVersion       string    `json:"schema_version"`
	EventID             string    `json:"event_id"`
	Attempt             int       `json:"attempt"`
	Task                string    `json:"task"`
	TaskChecksum        string    `json:"task_checksum"`
	EnvironmentIdentity string    `json:"task_environment_identity,omitempty"`
	Arm                 string    `json:"arm"`
	RuntimeModel        string    `json:"runtime_model"`
	ReasoningEffort     string    `json:"reasoning_effort"`
	TreatmentHash       string    `json:"treatment_manifest_hash,omitempty"`
	CompletedAt         time.Time `json:"completed_at"`
	Succeeded           bool      `json:"succeeded"`
	CleanupSucceeded    bool      `json:"cleanup_succeeded"`
	// ResumedFailedEventIDs records the prior immutable infrastructure-failure
	// receipts that this distinct provider event was explicitly authorized to
	// replace on a later benchmark invocation.
	ResumedFailedEventIDs []string `json:"resumed_failed_event_ids,omitempty"`
	VerifierReplayed      bool     `json:"verifier_replayed,omitempty"`
}

func promoteSanitizedPierArtifacts(request ExecutionRequest, stage string) error {
	if err := sanitizePierArtifacts(request, stage); err != nil {
		return err
	}
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	return replacePierArtifactDestination(stage, destination)
}

func replacePierArtifactDestination(stage, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".artifact-promotion-")
	if err != nil {
		return fmt.Errorf("create artifact promotion directory: %w", err)
	}
	temporaryMoved := false
	defer func() {
		if !temporaryMoved {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := copyRequiredTree(stage, temporary); err != nil {
		return err
	}
	checkpointPath := filepath.Join(destination, "execution-checkpoint.json")
	if _, err := os.Stat(checkpointPath); err == nil {
		if err := copyRequiredFile(checkpointPath, filepath.Join(temporary, "execution-checkpoint.json")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Pier artifact checkpoint before promotion: %w", err)
	}
	backup := destination + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove stale artifact promotion backup: %w", err)
	}
	destinationExists := false
	if _, err := os.Stat(destination); err == nil {
		destinationExists = true
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("preserve prior Pier artifacts before promotion: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect prior Pier artifacts before promotion: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		if destinationExists {
			_ = os.Rename(backup, destination)
		}
		return fmt.Errorf("promote Pier artifacts: %w", err)
	}
	temporaryMoved = true
	if destinationExists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove prior Pier artifacts after promotion: %w", err)
		}
	}
	return nil
}

func sanitizePierArtifacts(request ExecutionRequest, stage string) error {
	secrets := append([][]byte(nil), request.artifactSecrets...)
	if values, err := benchmarkCredentialValues(request.RepoRoot, request.Bundle); err != nil {
		return err
	} else {
		for _, value := range values {
			secrets = append(secrets, []byte(value))
		}
	}
	if data, err := os.ReadFile(filepath.Join(stage, antigravityOAuthStageFile)); err == nil { // #nosec G304 -- fixed file beneath the private execution stage.
		secrets = append(secrets, credentialSecretValues(data)...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read staged Antigravity OAuth profile for artifact sanitization: %w", err)
	}
	for _, path := range []string{
		filepath.Join(request.RepoRoot, ".codex", "auth.json"),
		filepath.Join(request.RepoRoot, ".claude-config", ".credentials.json"),
		filepath.Join(request.RepoRoot, ".grok-config", "auth.json"),
	} {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 { // #nosec G304 -- fixed repo-local credential paths.
			secrets = append(secrets, credentialSecretValues(data)...)
		}
	}
	paths := []string{request.RepoRoot}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, home)
	}
	if err := sanitizeArtifacts(stage, secrets, paths); err != nil {
		return err
	}
	if request.executionCheckpointed {
		if err := markPierArtifactsSanitized(request, stage, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func artifactDestination(request ExecutionRequest) (string, error) {
	if request.EventID == "" || request.EventID == "." || request.EventID == ".." ||
		filepath.Base(request.EventID) != request.EventID {
		return "", fmt.Errorf("invalid Pier artifact event ID")
	}
	evidenceRoot, err := filepath.Abs(request.EvidenceDir)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark evidence directory: %w", err)
	}
	return filepath.Join(
		evidenceRoot, "attempts", fmt.Sprintf("%d", request.Attempt),
		"tasks", request.Task, benchmarkArtifactsDir, request.EventID,
	), nil
}

func writePierExecutionReceipt(request ExecutionRequest, commandErr, cleanupErr error) error {
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	receipt := pierExecutionReceipt{
		SchemaVersion: pierExecutionReceiptSchema, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, TaskChecksum: request.TaskChecksum,
		EnvironmentIdentity: request.EnvironmentIdentity, Arm: request.Arm,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		CompletedAt: time.Now().UTC(), Succeeded: commandErr == nil,
		CleanupSucceeded:      cleanupErr == nil,
		ResumedFailedEventIDs: append([]string(nil), request.resumedFailedEventIDs...),
		VerifierReplayed:      request.verifierReplay,
	}
	if request.Bundle != nil {
		receipt.TreatmentHash = request.Bundle.ManifestHash
	}
	if err := writeJSON(filepath.Join(destination, "execution-receipt.json"), receipt); err != nil {
		return fmt.Errorf("record completed Pier execution: %w", err)
	}
	return nil
}

func writePierExecutionCheckpoint(request ExecutionRequest, stage string) error {
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	absStage, err := filepath.Abs(stage)
	if err != nil {
		return fmt.Errorf("resolve Pier staging directory: %w", err)
	}
	if err := validatePierCheckpointStage(request, absStage); err != nil {
		return err
	}
	checkpoint := pierExecutionCheckpoint{
		SchemaVersion: pierExecutionCheckpointSchema, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, TaskChecksum: request.TaskChecksum,
		EnvironmentIdentity: request.EnvironmentIdentity, Arm: request.Arm,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		TreatmentHash: executionTreatmentHash(request), StagePath: absStage, StartedAt: time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(destination, "execution-checkpoint.json"), checkpoint); err != nil {
		return fmt.Errorf("record started Pier execution: %w", err)
	}
	return nil
}

func markPierProviderCompleted(request ExecutionRequest, completedAt time.Time) error {
	return updatePierExecutionCheckpoint(request, func(checkpoint *pierExecutionCheckpoint) {
		if checkpoint.ProviderCompletedAt.IsZero() {
			checkpoint.ProviderCompletedAt = completedAt.UTC()
		}
	}, "record completed provider output")
}

func markPierArtifactsSanitized(request ExecutionRequest, stage string, completedAt time.Time) error {
	return updatePierExecutionCheckpoint(request, func(checkpoint *pierExecutionCheckpoint) {
		if filepath.Clean(checkpoint.StagePath) == filepath.Clean(stage) && checkpoint.ArtifactsSanitizedAt.IsZero() {
			checkpoint.ArtifactsSanitizedAt = completedAt.UTC()
		}
	}, "record sanitized Pier artifacts")
}

func updatePierExecutionCheckpoint(request ExecutionRequest, update func(*pierExecutionCheckpoint), action string) error {
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	path := filepath.Join(destination, "execution-checkpoint.json")
	var checkpoint pierExecutionCheckpoint
	if err := readStudyJSON(path, &checkpoint); err != nil {
		return fmt.Errorf("read Pier execution checkpoint: %w", err)
	}
	if checkpoint.SchemaVersion != pierExecutionCheckpointSchema || checkpoint.EventID != request.EventID {
		return fmt.Errorf("pier execution checkpoint does not match provider completion event %s", request.EventID)
	}
	update(&checkpoint)
	if err := writeJSON(path, checkpoint); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func removePierExecutionCheckpoint(request ExecutionRequest) error {
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(destination, "execution-checkpoint.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove completed Pier execution checkpoint: %w", err)
	}
	return nil
}

type completedPierExecution struct {
	root    string
	receipt pierExecutionReceipt
}

func matchingPierExecutions(request ExecutionRequest) ([]completedPierExecution, error) {
	root := filepath.Join(
		request.EvidenceDir, "attempts", fmt.Sprintf("%d", request.Attempt),
		"tasks", request.Task, benchmarkArtifactsDir,
	)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect completed Pier executions: %w", err)
	}
	if err := recoverPierArtifactPromotionScratch(root, entries); err != nil {
		return nil, err
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("inspect recovered Pier executions: %w", err)
	}
	var candidates []completedPierExecution
	for _, entry := range entries {
		if !entry.IsDir() || isPierArtifactPromotionScratch(entry.Name()) {
			continue
		}
		var receipt pierExecutionReceipt
		path := filepath.Join(root, entry.Name(), "execution-receipt.json")
		if err := readStudyJSON(path, &receipt); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("read completed Pier execution receipt: %w", err)
		}
		if receipt.SchemaVersion != pierExecutionReceiptSchema || receipt.EventID != entry.Name() ||
			receipt.Attempt != request.Attempt || receipt.Task != request.Task || receipt.CompletedAt.IsZero() {
			return nil, fmt.Errorf("completed Pier execution receipt %s does not match its benchmark cell", path)
		}
		if receipt.TaskChecksum != request.TaskChecksum || receipt.EnvironmentIdentity != request.EnvironmentIdentity ||
			receipt.Arm != request.Arm || receipt.RuntimeModel != request.Model.RuntimeIdentifier ||
			receipt.ReasoningEffort != request.Effort || receipt.TreatmentHash != executionTreatmentHash(request) {
			continue
		}
		candidates = append(candidates, completedPierExecution{root: filepath.Dir(path), receipt: receipt})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].receipt.CompletedAt.Equal(candidates[j].receipt.CompletedAt) {
			return candidates[i].receipt.EventID < candidates[j].receipt.EventID
		}
		return candidates[i].receipt.CompletedAt.Before(candidates[j].receipt.CompletedAt)
	})
	return candidates, nil
}

func recoverCompletedPierExecution(request ExecutionRequest) (AttemptResult, bool, error) {
	if checkpoint, found, err := matchingPierExecutionCheckpoint(request); err != nil {
		return AttemptResult{}, false, err
	} else if found {
		return AttemptResult{}, false, fmt.Errorf(
			"retained incomplete Pier execution %s; refusing a paid retry; staging directory: %s",
			checkpoint.EventID, checkpoint.StagePath,
		)
	}
	candidates, err := matchingPierExecutions(request)
	if err != nil {
		return AttemptResult{}, false, err
	}
	var selected *completedPierExecution
	for index := range candidates {
		if candidates[index].receipt.Succeeded {
			selected = &candidates[index]
			break
		}
	}
	if selected == nil {
		if len(candidates) == 0 || request.ResumeFailedInfrastructure {
			return AttemptResult{}, false, nil
		}
		return AttemptResult{}, false, fmt.Errorf(
			"earliest completed Pier execution %s failed; refusing an automatic paid retry",
			candidates[0].receipt.EventID,
		)
	}
	recoveredRequest := request
	recoveredRequest.EventID = selected.receipt.EventID
	if !selected.receipt.CleanupSucceeded {
		if err := cleanupPierDockerResources(selected.root, recoveredRequest); err != nil {
			return AttemptResult{}, false, fmt.Errorf(
				"clean resources for completed Pier execution %s without a provider retry: %w",
				selected.receipt.EventID, err,
			)
		}
	}
	result, err := normalizePier(selected.root, recoveredRequest)
	if err != nil {
		return AttemptResult{}, false, fmt.Errorf(
			"normalize completed Pier execution %s without a provider retry: %w",
			selected.receipt.EventID, err,
		)
	}
	return result, true, nil
}

func matchingPierExecutionCheckpoint(request ExecutionRequest) (pierExecutionCheckpoint, bool, error) {
	root := filepath.Join(
		request.EvidenceDir, "attempts", fmt.Sprintf("%d", request.Attempt),
		"tasks", request.Task, benchmarkArtifactsDir,
	)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return pierExecutionCheckpoint{}, false, nil
	}
	if err != nil {
		return pierExecutionCheckpoint{}, false, fmt.Errorf("inspect incomplete Pier executions: %w", err)
	}
	if err := recoverPierArtifactPromotionScratch(root, entries); err != nil {
		return pierExecutionCheckpoint{}, false, err
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		return pierExecutionCheckpoint{}, false, fmt.Errorf("inspect recovered incomplete Pier executions: %w", err)
	}
	var matches []pierExecutionCheckpoint
	for _, entry := range entries {
		if !entry.IsDir() || isPierArtifactPromotionScratch(entry.Name()) {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(directory, "execution-receipt.json")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return pierExecutionCheckpoint{}, false, fmt.Errorf("inspect completed Pier execution receipt: %w", err)
		}
		var checkpoint pierExecutionCheckpoint
		path := filepath.Join(directory, "execution-checkpoint.json")
		if err := readStudyJSON(path, &checkpoint); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return pierExecutionCheckpoint{}, false, fmt.Errorf("read incomplete Pier execution checkpoint: %w", err)
		}
		if checkpoint.SchemaVersion != pierExecutionCheckpointSchema || checkpoint.EventID != entry.Name() ||
			checkpoint.Attempt != request.Attempt || checkpoint.Task != request.Task || checkpoint.StartedAt.IsZero() ||
			checkpoint.TaskChecksum != request.TaskChecksum || checkpoint.EnvironmentIdentity != request.EnvironmentIdentity ||
			checkpoint.Arm != request.Arm || checkpoint.RuntimeModel != request.Model.RuntimeIdentifier ||
			checkpoint.ReasoningEffort != request.Effort || checkpoint.TreatmentHash != executionTreatmentHash(request) ||
			checkpoint.StagePath == "" || !filepath.IsAbs(checkpoint.StagePath) {
			return pierExecutionCheckpoint{}, false, fmt.Errorf("incomplete Pier execution checkpoint %s does not match its benchmark cell", path)
		}
		checkpointRequest := request
		checkpointRequest.EventID = checkpoint.EventID
		if err := validatePierCheckpointStage(checkpointRequest, checkpoint.StagePath); err != nil {
			return pierExecutionCheckpoint{}, false, fmt.Errorf("validate incomplete Pier execution checkpoint %s: %w", path, err)
		}
		matches = append(matches, checkpoint)
	}
	if len(matches) > 1 {
		return pierExecutionCheckpoint{}, false, fmt.Errorf("benchmark cell has %d incomplete Pier executions; expected at most one", len(matches))
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return pierExecutionCheckpoint{}, false, nil
}

func isPierArtifactPromotionScratch(name string) bool {
	return strings.HasPrefix(name, ".artifact-promotion-") || strings.HasSuffix(name, ".previous")
}

func recoverPierArtifactPromotionScratch(root string, entries []os.DirEntry) error {
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".previous") {
			continue
		}
		eventID := strings.TrimSuffix(entry.Name(), ".previous")
		if eventID == "" || filepath.Base(eventID) != eventID {
			return fmt.Errorf("invalid Pier artifact promotion backup %s", entry.Name())
		}
		backup, destination := filepath.Join(root, entry.Name()), filepath.Join(root, eventID)
		if _, err := os.Stat(destination); err == nil {
			if err := os.RemoveAll(backup); err != nil {
				return fmt.Errorf("remove completed Pier artifact promotion backup: %w", err)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(backup, destination); err != nil {
				return fmt.Errorf("restore interrupted Pier artifact promotion: %w", err)
			}
		} else {
			return fmt.Errorf("inspect interrupted Pier artifact promotion: %w", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".artifact-promotion-") {
			continue
		}
		temporary := filepath.Join(root, entry.Name())
		var checkpoint pierExecutionCheckpoint
		if err := readStudyJSON(filepath.Join(temporary, "execution-checkpoint.json"), &checkpoint); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("read interrupted Pier artifact promotion checkpoint: %w", err)
		}
		if checkpoint.EventID == "" || filepath.Base(checkpoint.EventID) != checkpoint.EventID {
			return fmt.Errorf("interrupted Pier artifact promotion has invalid event ID")
		}
		destination := filepath.Join(root, checkpoint.EventID)
		if _, err := os.Stat(destination); err == nil {
			if err := os.RemoveAll(temporary); err != nil {
				return fmt.Errorf("remove superseded Pier artifact promotion: %w", err)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(temporary, destination); err != nil {
				return fmt.Errorf("complete interrupted Pier artifact promotion: %w", err)
			}
		} else {
			return fmt.Errorf("inspect interrupted Pier artifact promotion destination: %w", err)
		}
	}
	return nil
}

func validatePierCheckpointStage(request ExecutionRequest, stage string) error {
	root, err := filepath.Abs(filepath.Join(request.RepoRoot, ".agent-layer", "tmp"))
	if err != nil {
		return fmt.Errorf("resolve benchmark staging root: %w", err)
	}
	relative, err := filepath.Rel(root, stage)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) ||
		filepath.Dir(relative) != "." || !strings.HasPrefix(filepath.Base(relative), "benchmark-"+request.EventID+"-") {
		return fmt.Errorf("pier staging directory %s is outside the expected benchmark event boundary", stage)
	}
	if info, err := os.Lstat(stage); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("pier staging directory %s is not a real directory", stage)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Pier staging directory %s: %w", stage, err)
	}
	return nil
}

func failedPierExecutionIDs(request ExecutionRequest) ([]string, error) {
	candidates, err := matchingPierExecutions(request)
	if err != nil {
		return nil, err
	}
	var failed []string
	for _, candidate := range candidates {
		if !candidate.receipt.Succeeded {
			failed = append(failed, candidate.receipt.EventID)
		}
	}
	return failed, nil
}

func executionTreatmentHash(request ExecutionRequest) string {
	if request.Bundle == nil {
		return ""
	}
	return request.Bundle.ManifestHash
}

func credentialSecretValues(data []byte) [][]byte {
	values := [][]byte{data}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return values
	}
	var collect func(any, bool)
	collect = func(value any, secret bool) {
		switch typed := value.(type) {
		case string:
			if secret && typed != "" {
				values = append(values, []byte(typed))
			}
		case []any:
			for _, child := range typed {
				collect(child, secret)
			}
		case map[string]any:
			for key, child := range typed {
				collect(child, secretCredentialKey(key))
			}
		}
	}
	collect(decoded, false)
	return uniqueSecretValues(values)
}

func secretCredentialKey(key string) bool {
	var normalized strings.Builder
	for _, character := range strings.ToLower(key) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			normalized.WriteRune(character)
		}
	}
	value := normalized.String()
	return strings.HasSuffix(value, "token") ||
		strings.HasSuffix(value, "secret") ||
		strings.HasSuffix(value, "password") ||
		strings.HasSuffix(value, "apikey") ||
		value == credentialKeyName ||
		value == "email" ||
		value == "userid" ||
		value == "teamid" ||
		value == "principalid" ||
		value == "oidcclientid" ||
		value == "authorization" ||
		value == "cookie" ||
		value == "credential"
}

func uniqueSecretValues(values [][]byte) [][]byte {
	seen := make(map[string]struct{}, len(values))
	var unique [][]byte
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		key := string(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func sanitizeArtifacts(root string, secrets [][]byte, paths []string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- regular file discovered below the restricted attempt stage.
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if len(secret) > 0 {
				data = bytes.ReplaceAll(data, secret, []byte("[REDACTED]"))
			}
		}
		for _, value := range paths {
			if value != "" {
				data = bytes.ReplaceAll(data, []byte(value), []byte("[PATH]"))
			}
		}
		for _, secret := range secrets {
			if len(secret) > 0 && bytes.Contains(data, secret) {
				return fmt.Errorf("credential bytes remain in staged artifact %s", path)
			}
		}
		return os.WriteFile(path, data, 0o600) // #nosec G122,G703 -- same regular file beneath the restricted stage.
	})
}
