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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitenv"
)

var errProviderCapacity = errors.New("provider model is at capacity")

const providerCapacityMessage = "Selected model is at capacity. Please try a different model."

var (
	preflightBenchmark        = preflight
	preflightTreatmentRuntime = func(ctx context.Context, request ExecutionRequest) error {
		return (PierExecutor{}).Preflight(ctx, request)
	}
	validateBenchmarkAuthentication  = validateAuthentication
	ensurePinnedBenchmarkCheckout    = ensurePinnedCheckout
	preflightTaskStartups            = validatePlanTaskStartups
	certifyBenchmarkTaskEnvironments = certifyPlanTaskEnvironments
	prepareBenchmarkTaskSet          = prepareBenchmarkTasks
	verifyBenchmarkPier              = verifyPinnedPier
	runBenchmarkDockerCommand        = runDockerCommand
)

const (
	benchmarkDockerCleanupTimeout  = 30 * time.Second
	authenticationPreflightTimeout = 15 * time.Second
	codexLoginStatusCheck          = "codex login status"
	codexAuthMethodChatGPT         = "chatgpt"
	codexAuthMethodAPIKey          = "api_key"
	dockerFormatFlag               = "--format"
	dockerImageResource            = "image"
	dockerNetworkResource          = "network"
	dockerVolumeResource           = "volume"
	commandRun                     = "run"
	// These formats print one resource identity and its Compose project label
	// per line. Docker expands the literal \t escape itself. Containers and
	// networks are addressed by ID; volumes are addressed by name.
	dockerComposeOwnershipIDFormat   = `{{.ID}}\t{{.Label "com.docker.compose.project"}}`
	dockerComposeOwnershipNameFormat = `{{.Name}}\t{{.Label "com.docker.compose.project"}}`
)

var (
	dockerResourceIDPattern = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	dockerVolumeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]+$`)
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
	RepoRoot               string
	EvidenceDir            string
	EventID                string
	Attempt                int
	Task                   string
	Model                  Model
	Effort                 string
	Arm                    string
	Bundle                 *TreatmentBundle
	TaskChecksum           string
	EnvironmentIdentity    string
	AgentTimeoutMultiplier float64
	PreflightOnly          bool
	// ResumeFailedInfrastructure is set only by the study scheduler for a new
	// user-authorized benchmark invocation. It permits a fresh event after an
	// earlier immutable infrastructure-failure receipt; it never retries a cell
	// within the failed invocation.
	ResumeFailedInfrastructure bool
	resumedFailedEventIDs      []string
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

func validateAuthentication(ctx context.Context, repoRoot string, selections []parsedSelection) (map[string]AuthenticationPreflight, error) {
	validated := make(map[string]bool)
	evidence := make(map[string]AuthenticationPreflight)
	for _, selection := range selections {
		adapter := selection.model.Adapter
		if validated[adapter] {
			continue
		}
		validated[adapter] = true
		item, err := validateProviderAuthentication(ctx, repoRoot, adapter)
		if err != nil {
			return nil, err
		}
		evidence[adapter] = item
	}
	return evidence, nil
}

func validateProviderAuthentication(ctx context.Context, repoRoot, adapter string) (AuthenticationPreflight, error) {
	var path, provider string
	switch adapter {
	case adapterCodex:
		path, provider = filepath.Join(repoRoot, ".codex", "auth.json"), adapterCodex
	case adapterClaudeCode:
		path, provider = filepath.Join(repoRoot, ".claude-config", ".credentials.json"), providerClaude
	default:
		return AuthenticationPreflight{}, fmt.Errorf("unsupported benchmark provider adapter %q", adapter)
	}
	if err := requireJSONCredentialFile(path, provider); err != nil {
		return AuthenticationPreflight{}, err
	}
	if adapter == adapterClaudeCode {
		return AuthenticationPreflight{}, fmt.Errorf("%s authentication cannot be validated before task setup because no available non-billing command verifies the copied OAuth token", provider)
	}
	return validateCodexLoginStatus(ctx, repoRoot)
}

func requireJSONCredentialFile(path, provider string) error {
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
	return nil
}

func validateCodexLoginStatus(ctx context.Context, repoRoot string) (AuthenticationPreflight, error) {
	ctx, cancel := context.WithTimeout(ctx, authenticationPreflightTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, adapterCodex, "login", "status") // #nosec G204 -- provider binary name and arguments are fixed.
	configureBenchmarkCommandCancellation(command)
	command.WaitDelay = 2 * time.Second
	command.Env = replaceEnvValue(os.Environ(), "CODEX_HOME", filepath.Join(repoRoot, ".codex"))
	output, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return AuthenticationPreflight{}, errors.New("codex authentication status timed out")
		}
		if ctx.Err() != nil {
			return AuthenticationPreflight{}, errors.New("codex authentication status canceled")
		}
		return AuthenticationPreflight{}, errors.New("codex authentication status command failed")
	}
	method, ok := parseCodexLoginStatus(output)
	if !ok {
		return AuthenticationPreflight{}, errors.New("codex authentication status was unrecognized")
	}
	return AuthenticationPreflight{
		Provider:             adapterCodex,
		Check:                codexLoginStatusCheck,
		AuthenticationMethod: method,
		VerifiedAt:           time.Now().UTC(),
	}, nil
}

type codexLoginStatusMethod struct {
	match      string
	prefix     bool
	normalized string
}

var codexLoginStatusAllowlist = []codexLoginStatusMethod{
	{match: "Logged in using ChatGPT", normalized: codexAuthMethodChatGPT},
	{match: "Logged in using an API key", prefix: true, normalized: codexAuthMethodAPIKey},
	{match: "Logged in using workload identity", normalized: "workload_identity"},
	{match: "Logged in using access token", normalized: "access_token"},
	{match: "Logged in using personal access token", normalized: "personal_access_token"},
	{match: "Logged in using Amazon Bedrock API key", normalized: "amazon_bedrock_api_key"},
}

func parseCodexLoginStatus(output []byte) (string, bool) {
	method := ""
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		normalized, ok := matchCodexLoginStatusLine(line)
		if !ok {
			return "", false
		}
		if found {
			return "", false
		}
		found, method = true, normalized
	}
	return method, found
}

func matchCodexLoginStatusLine(line string) (string, bool) {
	for _, item := range codexLoginStatusAllowlist {
		if item.prefix {
			if line == item.match || strings.HasPrefix(line, item.match+" - ") {
				return item.normalized, true
			}
			continue
		}
		if line == item.match {
			return item.normalized, true
		}
	}
	return "", false
}

func replaceEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	replaced := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			replaced = append(replaced, entry)
		}
	}
	return append(replaced, prefix+value)
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
	if !request.PreflightOnly {
		if recovered, found, err := recoverCompletedPierExecution(request); err != nil {
			return AttemptResult{}, err
		} else if found {
			return recovered, nil
		}
		if request.ResumeFailedInfrastructure {
			resumed, err := failedPierExecutionIDs(request)
			if err != nil {
				return AttemptResult{}, err
			}
			request.resumedFailedEventIDs = resumed
		}
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
		"--from", "datacurve-pier==" + PierVersion, "pier", commandRun,
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
		if request.AgentTimeoutMultiplier > 0 {
			arguments = append(arguments, "--agent-timeout-multiplier", strconv.FormatFloat(request.AgentTimeoutMultiplier, 'f', -1, 64))
		}
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
	credentialValues, err := benchmarkCredentialValues(request.RepoRoot, request.Bundle)
	if err != nil {
		return AttemptResult{}, err
	}
	command.Env = append(
		benchmarkProcessEnvironment(credentialValues),
		"DOCKER_CONFIG="+dockerConfig,
		"PYTHONDONTWRITEBYTECODE=1",
	)
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
	cleanupErr := cleanupPierDockerResources(stage, request)
	if err := os.RemoveAll(dockerConfig); err != nil {
		return AttemptResult{}, fmt.Errorf("remove transient benchmark Docker configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "pier-command.log"), output, 0o600); err != nil {
		return AttemptResult{}, err
	}
	if request.PreflightOnly {
		if commandErr != nil {
			return AttemptResult{}, fmt.Errorf("pier treatment runtime preflight failed: %w: %s", errors.Join(commandErr, cleanupErr), strings.TrimSpace(string(output)))
		}
		if cleanupErr != nil {
			return AttemptResult{}, fmt.Errorf("clean Pier treatment runtime preflight: %w", cleanupErr)
		}
		if err := validatePierTreatmentPreflight(stage, request); err != nil {
			return AttemptResult{}, err
		}
		return AttemptResult{}, nil
	}
	if err := promoteSanitizedPierArtifacts(request, stage); err != nil {
		return AttemptResult{}, err
	}
	artifactRoot, err := artifactDestination(request)
	if err != nil {
		return AttemptResult{}, err
	}
	if commandErr != nil {
		capacity, err := hasProviderCapacityEvidence(artifactRoot)
		if err != nil {
			return AttemptResult{}, fmt.Errorf("inspect provider failure evidence: %w", err)
		}
		if capacity {
			// The provider was invoked, so its failed event must remain in the
			// immutable receipt chain. A later benchmark invocation can resume
			// it explicitly; this invocation must not silently retry it.
			if err := writePierExecutionReceipt(request, commandErr, cleanupErr); err != nil {
				return AttemptResult{}, err
			}
			if cleanupErr != nil {
				return AttemptResult{}, fmt.Errorf("%w: %s; cleanup: %v", errProviderCapacity, providerCapacityMessage, cleanupErr)
			}
			return AttemptResult{}, fmt.Errorf("%w: %s", errProviderCapacity, providerCapacityMessage)
		}
	}
	if err := writePierExecutionReceipt(request, commandErr, cleanupErr); err != nil {
		return AttemptResult{}, err
	}
	result, normalizeErr := normalizePier(artifactRoot, request)
	if commandErr != nil {
		return AttemptResult{}, fmt.Errorf("pier task execution failed: %w", errors.Join(commandErr, cleanupErr))
	}
	if cleanupErr != nil {
		return AttemptResult{}, fmt.Errorf("clean Pier task environment: %w", cleanupErr)
	}
	if normalizeErr != nil {
		return AttemptResult{}, normalizeErr
	}
	return result, nil
}

// benchmarkProcessEnvironment preserves non-secret host infrastructure while
// adding only MCP credential names referenced by the effective config. Secret
// values travel in the child environment, never Pier's command line.
func benchmarkProcessEnvironment(credentials map[string]string) []string {
	keys := []string{
		"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "TERM",
		"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_STATE_HOME",
	}
	values := make([]string, 0, len(keys)+len(credentials))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			values = append(values, key+"="+value)
		}
	}
	names := make([]string, 0, len(credentials))
	for name := range credentials {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values = append(values, name+"="+credentials[name])
	}
	return values
}

func benchmarkCredentialValues(repoRoot string, bundle *TreatmentBundle) (map[string]string, error) {
	values := map[string]string{}
	if bundle == nil || len(bundle.CredentialNames) == 0 {
		return values, nil
	}
	fromFile := map[string]string{}
	envPath := filepath.Join(repoRoot, ".agent-layer", ".env")
	if loaded, err := config.LoadEnv(envPath); err == nil {
		fromFile = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read benchmark credential boundary: %w", err)
	}
	for _, name := range bundle.CredentialNames {
		value := fromFile[name]
		if value == "" {
			value = os.Getenv(name)
		}
		if value == "" {
			return nil, fmt.Errorf("configured MCP credential %s is unavailable; set it in .agent-layer/.env or the process environment", name)
		}
		values[name] = value
	}
	return values, nil
}

func runDockerCommand(ctx context.Context, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, commandDocker, arguments...) // #nosec G204 -- callers validate Docker-owned resource IDs.
	return command.CombinedOutput()
}

// cleanupPierDockerResources removes only the Compose resources owned by the
// exact Pier trial that just exited. Pier 0.3.0 can abandon its shielded
// teardown when cancellation closes the Python event loop.
func cleanupPierDockerResources(stage string, request ExecutionRequest) error {
	project, err := identifyPierComposeProject(stage, request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), benchmarkDockerCleanupTimeout)
	defer cancel()
	for _, resource := range []struct {
		name       string
		list       []string
		removeBase []string
		validName  *regexp.Regexp
	}{
		{name: "container", list: []string{"ps", "--all", dockerFormatFlag, dockerComposeOwnershipIDFormat}, removeBase: []string{"rm", "--force"}, validName: dockerResourceIDPattern},
		{name: dockerNetworkResource, list: []string{dockerNetworkResource, "ls", dockerFormatFlag, dockerComposeOwnershipIDFormat}, removeBase: []string{dockerNetworkResource, "rm"}, validName: dockerResourceIDPattern},
		{name: dockerVolumeResource, list: []string{dockerVolumeResource, "ls", dockerFormatFlag, dockerComposeOwnershipNameFormat}, removeBase: []string{dockerVolumeResource, "rm", "--force"}, validName: dockerVolumeNamePattern},
	} {
		output, listErr := runBenchmarkDockerCommand(ctx, resource.list...)
		if listErr != nil {
			return fmt.Errorf("list Pier %ss for project %s: %w: %s", resource.name, project, listErr, strings.TrimSpace(string(output)))
		}
		var ids []string
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				continue
			}
			fields := strings.SplitN(line, "\t", 2)
			if len(fields) != 2 {
				return fmt.Errorf("inspect Pier %s ownership: Docker returned malformed record %q", resource.name, line)
			}
			owner := strings.ToLower(fields[1])
			if owner != project && !strings.HasPrefix(owner, project+"__verifier__") {
				continue
			}
			if !resource.validName.MatchString(fields[0]) {
				return fmt.Errorf("inspect Pier %s ownership: Docker returned invalid ID %q for project %s", resource.name, fields[0], owner)
			}
			ids = append(ids, fields[0])
		}
		if len(ids) == 0 {
			continue
		}
		arguments := append(append([]string{}, resource.removeBase...), ids...)
		removeOutput, removeErr := runBenchmarkDockerCommand(ctx, arguments...)
		if removeErr != nil {
			return fmt.Errorf("remove Pier %ss for project %s: %w: %s", resource.name, project, removeErr, strings.TrimSpace(string(removeOutput)))
		}
	}
	// Pier asks Compose to build a uniquely named main image for every trial.
	// Compose down does not remove that image, so leaving it behind makes image
	// storage grow linearly even though the trial's containers are gone.
	var imageIDs []string
	for _, imageReference := range []string{
		project + "-main:latest",
		project + "__verifier__*-main:latest",
	} {
		output, listErr := runBenchmarkDockerCommand(
			ctx,
			dockerImageResource, "ls", "--filter", "reference="+imageReference,
			dockerFormatFlag, "{{.ID}}",
		)
		if listErr != nil {
			return fmt.Errorf("list Pier images for project %s: %w: %s", project, listErr, strings.TrimSpace(string(output)))
		}
		for _, line := range strings.Split(string(output), "\n") {
			id := strings.TrimSpace(line)
			if id == "" {
				continue
			}
			if !dockerResourceIDPattern.MatchString(id) {
				return fmt.Errorf("inspect Pier image ownership: Docker returned invalid ID %q for project %s", id, project)
			}
			imageIDs = append(imageIDs, id)
		}
	}
	if len(imageIDs) > 0 {
		arguments := append([]string{dockerImageResource, "rm"}, imageIDs...)
		removeOutput, removeErr := runBenchmarkDockerCommand(ctx, arguments...)
		if removeErr != nil {
			return fmt.Errorf("remove Pier images for project %s: %w: %s", project, removeErr, strings.TrimSpace(string(removeOutput)))
		}
	}
	return nil
}

func identifyPierComposeProject(stage string, request ExecutionRequest) (string, error) {
	raw, resultErr := readPierTaskResult(stage, request)
	if resultErr == nil {
		project, err := validatePierComposeProject(raw.TrialName, request.Task)
		if err != nil {
			return "", err
		}
		return project, nil
	}
	projects := make(map[string]struct{})
	walkErr := filepath.WalkDir(filepath.Join(stage, "jobs"), func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if project, err := validatePierComposeProject(entry.Name(), request.Task); err == nil {
			projects[project] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("identify Pier Compose project after incomplete result: %w", errors.Join(resultErr, walkErr))
	}
	if len(projects) != 1 {
		return "", fmt.Errorf(
			"identify Pier Compose project after incomplete result: %w; found %d matching trial directories",
			resultErr, len(projects),
		)
	}
	for project := range projects {
		return project, nil
	}
	return "", fmt.Errorf("identify Pier Compose project after incomplete result: no project remained after validation")
}

func validatePierComposeProject(trialName, task string) (string, error) {
	prefix := task
	if len(prefix) > 32 {
		prefix = prefix[:32]
	}
	expected := prefix + "__"
	if !strings.HasPrefix(trialName, expected) || len(trialName) != len(expected)+7 {
		return "", fmt.Errorf("pier trial name %q does not match benchmark task %s", trialName, task)
	}
	for _, character := range trialName[len(expected):] {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return "", fmt.Errorf("pier trial name %q has an invalid random suffix", trialName)
		}
	}
	return strings.ToLower(trialName), nil
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
	timeout := request.AgentTimeoutMultiplier
	if timeout == 0 {
		timeout = request.Bundle.Manifest.AgentTimeoutMultiplier
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("treatment execution requires a positive agent timeout multiplier")
	}
	arguments := []string{
		"--agent-timeout-multiplier", strconv.FormatFloat(timeout, 'f', -1, 64),
		"--agent-import-path", "pier_agent_layer:" + className,
		pierAgentKwarg, "treatment_bundle=" + request.Bundle.Root,
		pierAgentKwarg, "treatment_mode=" + request.Bundle.Manifest.Mode,
		pierAgentKwarg, "required_dispatch_roles=" + strings.Join(request.Bundle.Manifest.RequiredRoles, ","),
		pierAgentKwarg, "treatment_agent=" + dispatchAgent(request.Model),
		pierAgentKwarg, "treatment_model=" + dispatchModel(request.Model),
		pierAgentKwarg, "treatment_reasoning_effort=" + request.Effort,
		pierAgentKwarg, "credential_names=" + strings.Join(request.Bundle.CredentialNames, ","),
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
		command.Env = gitenv.WithoutDiscovery()
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
	revisionCommand := exec.CommandContext(ctx, commandGit, "-C", checkout, "rev-parse", "HEAD") // #nosec G204 -- private fixed checkout path.
	revisionCommand.Env = gitenv.WithoutDiscovery()
	data, err := revisionCommand.Output()
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
	statusCommand := exec.CommandContext(ctx, commandGit, "-C", checkout, "status", "--porcelain=v1", "--untracked-files=all") // #nosec G204 -- private fixed checkout path.
	statusCommand.Env = gitenv.WithoutDiscovery()
	status, err := statusCommand.Output()
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
