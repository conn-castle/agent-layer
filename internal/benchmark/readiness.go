package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	readinessContractSchema = "deepswe-task-readiness-v1"
	readinessReceiptSchema  = "deepswe-task-readiness-certification-v1"
)

//go:embed readiness/*/*/*
var embeddedReadinessContracts embed.FS

var readinessContracts fs.FS = embeddedReadinessContracts

type taskReadinessContract struct {
	Schema      string `json:"schema"`
	Task        string `json:"task"`
	Image       string `json:"image"`
	ImageDigest string `json:"image_digest"`
	Check       string `json:"check"`
}

type taskReadinessCertification struct {
	Schema        string `json:"schema"`
	DeepSWECommit string `json:"deep_swe_commit"`
	Task          string `json:"task"`
	TaskChecksum  string `json:"task_checksum"`
	ContractHash  string `json:"contract_hash"`
	PinnedImage   string `json:"pinned_image"`
}

type loadedTaskReadiness struct {
	contract     taskReadinessContract
	contractHash string
	check        []byte
	pinnedImage  string
}

var runTaskReadinessCommand = func(ctx context.Context, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, commandDocker, arguments...) // #nosec G204 -- inputs come from validated embedded contracts.
	return command.CombinedOutput()
}

func loadTaskReadiness(checkout, task string) (loadedTaskReadiness, error) {
	if !validTaskName(task) {
		return loadedTaskReadiness{}, fmt.Errorf("invalid benchmark task %q", task)
	}
	root := filepath.ToSlash(filepath.Join("readiness", DeepSWECommit, task))
	contractData, err := fs.ReadFile(readinessContracts, root+"/contract.json")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return loadedTaskReadiness{}, fmt.Errorf("benchmark task %s has no mandatory environment readiness contract for DeepSWE %s", task, DeepSWECommit)
		}
		return loadedTaskReadiness{}, fmt.Errorf("read benchmark task %s readiness contract: %w", task, err)
	}
	var contract taskReadinessContract
	decoder := json.NewDecoder(bytes.NewReader(contractData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contract); err != nil {
		return loadedTaskReadiness{}, fmt.Errorf("decode benchmark task %s readiness contract: %w", task, err)
	}
	if contract.Schema != readinessContractSchema || contract.Task != task || contract.Image == "" ||
		len(contract.ImageDigest) != 71 || !strings.HasPrefix(contract.ImageDigest, "sha256:") ||
		filepath.Base(contract.Check) != contract.Check || contract.Check == "." || contract.Check == "" {
		return loadedTaskReadiness{}, fmt.Errorf("benchmark task %s has an invalid environment readiness contract", task)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(contract.ImageDigest, "sha256:")); err != nil {
		return loadedTaskReadiness{}, fmt.Errorf("benchmark task %s has an invalid image digest: %w", task, err)
	}
	check, err := fs.ReadFile(readinessContracts, root+"/"+contract.Check)
	if err != nil || len(bytes.TrimSpace(check)) == 0 {
		return loadedTaskReadiness{}, fmt.Errorf("read benchmark task %s readiness program: %w", task, err)
	}
	taskImage, err := readTaskDockerImage(filepath.Join(checkout, "tasks", task, taskTOMLFile))
	if err != nil {
		return loadedTaskReadiness{}, err
	}
	if taskImage != contract.Image {
		return loadedTaskReadiness{}, fmt.Errorf("benchmark task %s readiness image %q does not match task.toml image %q", task, contract.Image, taskImage)
	}
	hash := sha256.New()
	hash.Write(contractData)
	hash.Write([]byte{0})
	hash.Write(check)
	return loadedTaskReadiness{
		contract: contract, contractHash: hex.EncodeToString(hash.Sum(nil)), check: check,
		pinnedImage: contract.Image + "@" + contract.ImageDigest,
	}, nil
}

func readTaskDockerImage(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path belongs to the validated pinned checkout.
	if err != nil {
		return "", fmt.Errorf("read benchmark task environment identity: %w", err)
	}
	var taskDefinition struct {
		Environment struct {
			DockerImage string `toml:"docker_image"`
		} `toml:"environment"`
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&taskDefinition); err != nil {
		return "", fmt.Errorf("decode benchmark task environment identity: %w", err)
	}
	if taskDefinition.Environment.DockerImage == "" {
		return "", fmt.Errorf("benchmark task environment must declare one Docker image")
	}
	return taskDefinition.Environment.DockerImage, nil
}

func certifyPlanTaskEnvironments(ctx context.Context, repoRoot, checkout string, tasks []benchmarkPlanTask, checksums map[string]string) (map[string]string, error) {
	identities := make(map[string]string, len(tasks))
	for _, task := range tasks {
		identity, err := certifyTaskEnvironment(ctx, repoRoot, checkout, task.ID, checksums[task.ID])
		if err != nil {
			return nil, err
		}
		identities[task.ID] = identity
	}
	return identities, nil
}

func identifyPlanTaskEnvironments(ctx context.Context, repoRoot string, tasks []benchmarkPlanTask) (map[string]string, error) {
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	identities := make(map[string]string, len(tasks))
	for _, task := range tasks {
		root := filepath.Join(checkout, "tasks", task.ID)
		checksum, err := TaskTreeChecksum(root)
		if err != nil {
			return nil, fmt.Errorf("checksum benchmark task %s for report identity: %w", task.ID, err)
		}
		readiness, err := loadTaskReadiness(checkout, task.ID)
		if err != nil {
			return nil, err
		}
		_, identity, err := taskEnvironmentCertificationIdentity(readiness, task.ID, checksum)
		if err != nil {
			return nil, err
		}
		identities[task.ID] = identity
	}
	return identities, nil
}

func validateTaskEnvironmentParity(tasks []benchmarkPlanTask, baseline, treatment map[string]string) error {
	if len(baseline) != len(tasks) || len(treatment) != len(tasks) || !sameStringMap(baseline, treatment) {
		return fmt.Errorf("baseline task environments do not match the current certified readiness contracts; run a fresh baseline")
	}
	for _, task := range tasks {
		if baseline[task.ID] == "" {
			return fmt.Errorf("baseline task environments do not match the current certified readiness contracts; run a fresh baseline")
		}
	}
	return nil
}

func certifyTaskEnvironment(ctx context.Context, repoRoot, checkout, task, taskChecksum string) (string, error) {
	if len(taskChecksum) != 64 {
		return "", fmt.Errorf("benchmark task %s has no valid task checksum for environment certification", task)
	}
	readiness, err := loadTaskReadiness(checkout, task)
	if err != nil {
		return "", err
	}
	receipt, identity, err := taskEnvironmentCertificationIdentity(readiness, task, taskChecksum)
	if err != nil {
		return "", err
	}
	receiptPath := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "environment-certifications", identity+".json")
	if data, readErr := os.ReadFile(receiptPath); readErr == nil { // #nosec G304 -- content-addressed private benchmark state.
		var existing taskReadinessCertification
		if json.Unmarshal(data, &existing) == nil && existing == receipt {
			return identity, nil
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", fmt.Errorf("read benchmark task %s environment certification: %w", task, readErr)
	}
	stageRoot := filepath.Join(repoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return "", fmt.Errorf("create task readiness staging root: %w", err)
	}
	stage, err := os.MkdirTemp(stageRoot, "task-readiness-")
	if err != nil {
		return "", fmt.Errorf("create task readiness staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	checkPath := filepath.Join(stage, "check.sh")
	if err := os.WriteFile(checkPath, readiness.check, 0o600); err != nil {
		return "", fmt.Errorf("materialize benchmark task %s readiness program: %w", task, err)
	}
	output, runErr := runTaskReadinessCommand(ctx,
		"run", "--rm", "--network", "none",
		"--mount", "type=bind,source="+checkPath+",target=/opt/agent-layer/readiness.sh,readonly",
		"--entrypoint", "/bin/bash", readiness.pinnedImage, "/opt/agent-layer/readiness.sh",
	)
	if runErr != nil {
		return "", fmt.Errorf("benchmark task %s environment readiness failed before provider execution: %w: %s", task, runErr, strings.TrimSpace(string(output)))
	}
	if err := writeJSON(receiptPath, receipt); err != nil {
		return "", fmt.Errorf("record benchmark task %s environment certification: %w", task, err)
	}
	return identity, nil
}

func taskEnvironmentCertificationIdentity(readiness loadedTaskReadiness, task, taskChecksum string) (taskReadinessCertification, string, error) {
	receipt := taskReadinessCertification{
		Schema: readinessReceiptSchema, DeepSWECommit: DeepSWECommit, Task: task,
		TaskChecksum: taskChecksum, ContractHash: readiness.contractHash, PinnedImage: readiness.pinnedImage,
	}
	identity, err := hashCanonical(receipt)
	if err != nil {
		return taskReadinessCertification{}, "", fmt.Errorf("identify benchmark task %s environment certification: %w", task, err)
	}
	return receipt, identity, nil
}
