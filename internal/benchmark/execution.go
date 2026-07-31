package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrConfirmationRequired is returned before any paid model invocation.
var ErrConfirmationRequired = errors.New("benchmark paid execution requires confirmation")

var errProviderCapacity = errors.New("provider model is at capacity")

const providerCapacityMessage = "Selected model is at capacity. Please try a different model."

var (
	preflightBenchmark        = preflight
	preflightTreatmentRuntime = func(ctx context.Context, request ExecutionRequest) error {
		return (PierExecutor{}).Preflight(ctx, request)
	}
	validateBenchmarkAuthentication   = validateAuthentication
	ensurePinnedBenchmarkCheckout     = ensurePinnedCheckout
	preflightTaskStartups             = validatePlanTaskStartups
	certifyBenchmarkTaskEnvironments  = certifyPlanTaskEnvironments
	prepareBenchmarkTaskSet           = prepareBenchmarkTasks
	identifyBenchmarkTaskEnvironments = identifyPlanTaskEnvironments
	verifyBenchmarkPier               = verifyPinnedPier
)

type parsedSelection struct {
	model  Model
	effort string
}

// TaskExecutor is the testable boundary around one paid task execution.
type TaskExecutor interface {
	Execute(context.Context, ExecutionRequest) (AttemptResult, error)
}

// ExecutionRequest identifies one plan-selected repetition and its evidence
// destination.
type ExecutionRequest struct {
	RepoRoot      string
	EvidenceDir   string
	EventID       string
	Attempt       int
	Task          string
	Model         Model
	Effort        string
	Arm           string
	Bundle        *TreatmentBundle
	TaskChecksum  string
	PreflightOnly bool
}

func sameIntMap(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func preflight(selections []parsedSelection) error {
	for _, command := range []string{commandGit, commandDocker, commandUVX} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("benchmark prerequisite %s is unavailable: %w", command, err)
		}
	}
	for _, selection := range selections {
		client := adapterCodex
		if selection.model.Adapter == adapterClaudeCode {
			client = providerClaude
		}
		if _, err := exec.LookPath(client); err != nil {
			return fmt.Errorf("benchmark provider client %s is unavailable: %w", client, err)
		}
	}
	command := exec.CommandContext(context.Background(), commandDocker, "info", "--format", "{{.ServerVersion}}") // #nosec G204 -- fixed read-only prerequisite check.
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("benchmark Docker daemon is unavailable: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) == "" {
		return errors.New("benchmark Docker daemon did not report a server version")
	}
	return nil
}

func verifyPinnedPier(ctx context.Context) error {
	command := exec.CommandContext(ctx, commandUVX, "--from", "datacurve-pier=="+PierVersion, "pier", "--version") // #nosec G204 -- package and arguments are pinned.
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify pinned Pier %s: %w: %s", PierVersion, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validateAuthentication(repoRoot string, selections []parsedSelection) error {
	validated := make(map[string]bool)
	for _, selection := range selections {
		if validated[selection.model.Adapter] {
			continue
		}
		validated[selection.model.Adapter] = true
		var path, provider string
		switch selection.model.Adapter {
		case adapterCodex:
			path, provider = filepath.Join(repoRoot, ".codex", "auth.json"), adapterCodex
		case adapterClaudeCode:
			path, provider = filepath.Join(repoRoot, ".claude-config", ".credentials.json"), providerClaude
		default:
			return fmt.Errorf("unsupported benchmark provider adapter %q", selection.model.Adapter)
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return fmt.Errorf("%s authentication must be a non-empty JSON file at %s", provider, path)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- provider determines a fixed repo-local path.
		if err != nil {
			return fmt.Errorf("read %s authentication: %w", provider, err)
		}
		if len(bytes.TrimSpace(data)) == 0 || !json.Valid(data) {
			return fmt.Errorf("%s authentication at %s must be non-empty JSON", provider, path)
		}
	}
	return nil
}

// PierExecutor invokes the pinned official Pier adapter once.
type PierExecutor struct{}

// Preflight executes the real Pier container and treatment setup without
// invoking the provider model.
func (PierExecutor) Preflight(ctx context.Context, request ExecutionRequest) error {
	request.PreflightOnly = true
	_, err := (PierExecutor{}).Execute(ctx, request)
	return err
}

// Execute runs one task and promotes sanitized evidence before returning.
func (PierExecutor) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	if request.RepoRoot == "" || request.EvidenceDir == "" || request.EventID == "" ||
		request.Attempt < 1 || !validTaskName(request.Task) ||
		(request.Arm != ArmBaseline && request.Arm != ArmTreatment) {
		return AttemptResult{}, fmt.Errorf("invalid Pier execution request")
	}
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, request.RepoRoot)
	if err != nil {
		return AttemptResult{}, err
	}
	stageRoot := filepath.Join(request.RepoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return AttemptResult{}, fmt.Errorf("create benchmark staging root: %w", err)
	}
	stage, err := os.MkdirTemp(stageRoot, "benchmark-"+request.EventID+"-")
	if err != nil {
		return AttemptResult{}, fmt.Errorf("create restricted benchmark staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	startupArguments, err := prepareTaskStartup(checkout, request.Task, stage)
	if err != nil {
		return AttemptResult{}, err
	}

	arguments := []string{
		"--from", "datacurve-pier==" + PierVersion, "pier", "run",
		"--path", filepath.Join(checkout, "tasks"),
		"--model", request.Model.RuntimeIdentifier,
		"--n-attempts", "1", "--n-concurrent", "1", "--max-retries", "0",
		"--jobs-dir", filepath.Join(stage, "jobs"),
		"--job-name", request.EventID, "--yes",
		"--include-task-name", request.Task,
	}
	arguments = append(arguments, startupArguments...)
	if request.Arm == ArmTreatment {
		treatmentArguments, err := treatmentPierArguments(request)
		if err != nil {
			return AttemptResult{}, err
		}
		arguments = append(arguments, treatmentArguments...)
	} else {
		arguments = append(arguments, "--agent", request.Model.Adapter)
	}
	arguments = append(
		arguments,
		pierAgentKwarg, "version="+request.Model.ProviderClientVersion,
		pierAgentKwarg, "reasoning_effort="+request.Effort,
	)
	if request.Model.Adapter == adapterCodex {
		authPath := filepath.Join(request.RepoRoot, ".codex", "auth.json")
		if _, err := os.Stat(authPath); err != nil {
			return AttemptResult{}, fmt.Errorf("codex authentication is required at %s", authPath)
		}
		arguments = append(arguments, "--agent-env", "CODEX_AUTH_JSON_PATH="+authPath)
	}

	command := exec.CommandContext(ctx, commandUVX, arguments...) // #nosec G204 -- every identity is validated or pinned above.
	configureBenchmarkCommandCancellation(command)
	command.Dir = request.RepoRoot
	dockerConfig, err := prepareBenchmarkDockerConfig(stage)
	if err != nil {
		return AttemptResult{}, err
	}
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfig)
	pythonPaths := make([]string, 0, 2)
	if request.Bundle != nil {
		pythonPaths = append(pythonPaths, filepath.Dir(request.Bundle.AdapterPath))
	}
	if len(startupArguments) > 0 {
		pythonPaths = append([]string{stage}, pythonPaths...)
	}
	if len(pythonPaths) > 0 {
		command.Env = append(command.Env, "PYTHONPATH="+strings.Join(pythonPaths, string(os.PathListSeparator)))
	}
	output, commandErr := command.CombinedOutput()
	if err := os.RemoveAll(dockerConfig); err != nil {
		return AttemptResult{}, fmt.Errorf("remove transient benchmark Docker configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "pier-command.log"), output, 0o600); err != nil {
		return AttemptResult{}, err
	}
	if request.PreflightOnly {
		if commandErr != nil {
			return AttemptResult{}, fmt.Errorf("pier treatment runtime preflight failed: %w: %s", commandErr, strings.TrimSpace(string(output)))
		}
		if err := validatePierTreatmentPreflight(stage, request); err != nil {
			return AttemptResult{}, err
		}
		return AttemptResult{}, nil
	}
	result, normalizeErr := normalizePier(stage, request)
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		return AttemptResult{}, err
	}
	if commandErr != nil {
		capacity, err := hasProviderCapacityEvidence(stage)
		if err != nil {
			return AttemptResult{}, fmt.Errorf("inspect provider failure evidence: %w", err)
		}
		if capacity {
			return AttemptResult{}, fmt.Errorf("%w: %s", errProviderCapacity, providerCapacityMessage)
		}
		return AttemptResult{}, fmt.Errorf("pier task execution failed: %w", commandErr)
	}
	if normalizeErr != nil {
		return AttemptResult{}, normalizeErr
	}
	return result, nil
}

func hasProviderCapacityEvidence(root string) (bool, error) {
	capacity := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if capacity || entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		switch filepath.Base(path) {
		case "codex.txt", "claude.txt", "claude-code.txt":
		default:
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- regular provider transcript beneath private staging.
		if err != nil {
			return err
		}
		capacity = bytes.Contains(data, []byte(providerCapacityMessage))
		return nil
	})
	return capacity, err
}

// treatmentPierArguments returns the Pier flags that make the immutable
// treatment bundle and its execution policy part of the paid run.
func treatmentPierArguments(request ExecutionRequest) ([]string, error) {
	if request.Bundle == nil {
		return nil, fmt.Errorf("treatment execution requires an immutable treatment bundle")
	}
	className := "AgentLayerCodex"
	if request.Model.Adapter == adapterClaudeCode {
		className = "AgentLayerClaudeCode"
	}
	arguments := []string{
		"--agent-timeout-multiplier", strconv.FormatFloat(request.Bundle.Manifest.AgentTimeoutMultiplier, 'f', -1, 64),
		"--agent-import-path", "pier_agent_layer:" + className,
		pierAgentKwarg, "treatment_bundle=" + request.Bundle.Root,
		pierAgentKwarg, "treatment_mode=" + request.Bundle.Manifest.Mode,
		pierAgentKwarg, "required_dispatch_roles=" + strings.Join(request.Bundle.Manifest.RequiredRoles, ","),
		pierAgentKwarg, "treatment_agent=" + dispatchAgent(request.Model),
		pierAgentKwarg, "treatment_model=" + dispatchModel(request.Model),
		pierAgentKwarg, "treatment_reasoning_effort=" + request.Effort,
		"--agent-env", "PATH=/home/dev/.local/bin:/usr/local/bin:/usr/bin:/bin",
	}
	if request.Model.Adapter == adapterClaudeCode {
		credentials := filepath.Join(request.RepoRoot, ".claude-config", ".credentials.json")
		if _, err := os.Stat(credentials); err != nil {
			return nil, fmt.Errorf("claude authentication is required at %s", credentials)
		}
		arguments = append(arguments, pierAgentKwarg, "claude_credentials_path="+credentials)
	} else {
		credentials := filepath.Join(request.RepoRoot, ".codex", "auth.json")
		if _, err := os.Stat(credentials); err != nil {
			return nil, fmt.Errorf("codex authentication is required at %s", credentials)
		}
		arguments = append(arguments, pierAgentKwarg, "codex_credentials_path="+credentials)
	}
	if request.PreflightOnly {
		arguments = append(arguments, pierAgentKwarg, "preflight_only=true")
	}
	return arguments, nil
}

func prepareBenchmarkDockerConfig(stage string) (string, error) {
	target := filepath.Join(stage, "docker-config")
	if err := os.MkdirAll(filepath.Join(target, "cli-plugins"), 0o700); err != nil {
		return "", fmt.Errorf("create benchmark Docker configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(target, "config.json"), []byte("{\"auths\":{}}\n"), 0o600); err != nil {
		return "", fmt.Errorf("write benchmark Docker configuration: %w", err)
	}
	source := os.Getenv("DOCKER_CONFIG")
	if source == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate Docker CLI plugins: %w", err)
		}
		source = filepath.Join(home, ".docker")
	}
	for _, plugin := range []string{dockerBuildxPlugin, dockerComposePlugin} {
		from := filepath.Join(source, "cli-plugins", plugin)
		if _, err := os.Stat(from); err != nil { // #nosec G703 -- only fixed plugin names are accepted.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("inspect Docker CLI plugin %s: %w", plugin, err)
		}
		if err := os.Symlink(from, filepath.Join(target, "cli-plugins", plugin)); err != nil {
			return "", fmt.Errorf("link Docker CLI plugin %s: %w", plugin, err)
		}
	}
	return target, nil
}

func ensurePinnedCheckout(ctx context.Context, repoRoot string) (string, error) {
	checkout := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "checkouts", DeepSWECommit)
	if valid, err := validateExistingPinnedCheckout(ctx, checkout); err != nil {
		return "", err
	} else if valid {
		return checkout, nil
	}
	parent := filepath.Dir(checkout)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	temporary, err := os.MkdirTemp(parent, DeepSWECommit+".")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	for _, args := range [][]string{
		{"init", "--quiet", temporary},
		{"-C", temporary, "fetch", "--depth", "1", "https://github.com/datacurve-ai/deep-swe.git", DeepSWECommit},
		{"-C", temporary, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	} {
		command := exec.CommandContext(ctx, commandGit, args...) // #nosec G204 -- source and commit are pinned.
		if output, err := command.CombinedOutput(); err != nil {
			return "", fmt.Errorf("prepare pinned DeepSWE checkout: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if err := os.Rename(temporary, checkout); err != nil {
		if valid, validationErr := validateExistingPinnedCheckout(ctx, checkout); validationErr != nil {
			return "", validationErr
		} else if valid {
			return checkout, nil
		}
		return "", fmt.Errorf("promote pinned DeepSWE checkout: %w", err)
	}
	return checkout, nil
}

func validateExistingPinnedCheckout(ctx context.Context, checkout string) (bool, error) {
	info, err := os.Stat(checkout)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect pinned DeepSWE checkout: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("pinned DeepSWE checkout path is not a directory")
	}
	data, err := exec.CommandContext(ctx, commandGit, "-C", checkout, "rev-parse", "HEAD").Output() // #nosec G204 -- private fixed checkout path.
	if err != nil {
		return false, fmt.Errorf("read pinned DeepSWE checkout revision: %w", err)
	}
	if strings.TrimSpace(string(data)) != DeepSWECommit {
		return false, fmt.Errorf("pinned DeepSWE checkout revision does not match %s", DeepSWECommit)
	}
	if err := validatePinnedCheckoutClean(ctx, checkout); err != nil {
		return false, err
	}
	return true, nil
}

// validatePinnedCheckoutClean prevents a labeled benchmark from running a
// locally modified task tree under the pinned commit identity.
func validatePinnedCheckoutClean(ctx context.Context, checkout string) error {
	status, err := exec.CommandContext(ctx, commandGit, "-C", checkout, "status", "--porcelain=v1", "--untracked-files=all").Output() // #nosec G204 -- private fixed checkout path.
	if err != nil {
		return fmt.Errorf("inspect pinned DeepSWE checkout cleanliness: %w", err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return errors.New("pinned DeepSWE checkout must be clean")
	}
	return nil
}

func dispatchAgent(model Model) string {
	if model.Adapter == adapterClaudeCode {
		return providerClaude
	}
	return model.Adapter
}

func dispatchModel(model Model) string {
	return strings.TrimPrefix(model.RuntimeIdentifier, "openai/")
}
