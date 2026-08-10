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
}

func promoteSanitizedPierArtifacts(request ExecutionRequest, stage string) error {
	var secrets [][]byte
	if values, err := benchmarkCredentialValues(request.RepoRoot, request.Bundle); err != nil {
		return err
	} else {
		for _, value := range values {
			secrets = append(secrets, []byte(value))
		}
	}
	for _, path := range []string{
		filepath.Join(request.RepoRoot, ".codex", "auth.json"),
		filepath.Join(request.RepoRoot, ".claude-config", ".credentials.json"),
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
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	return copyRequiredTree(stage, destination)
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
		"tasks", request.Task, "artifacts", request.EventID,
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
	}
	if request.Bundle != nil {
		receipt.TreatmentHash = request.Bundle.ManifestHash
	}
	if err := writeJSON(filepath.Join(destination, "execution-receipt.json"), receipt); err != nil {
		return fmt.Errorf("record completed Pier execution: %w", err)
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
		"tasks", request.Task, "artifacts",
	)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect completed Pier executions: %w", err)
	}
	var candidates []completedPierExecution
	for _, entry := range entries {
		if !entry.IsDir() {
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
