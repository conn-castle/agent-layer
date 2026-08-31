package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// StudyOptions configures the single public DeepSWE benchmark workflow.
// TaskConcurrency and Tasks scope one invocation only; neither affects identity.
type StudyOptions struct {
	RepoRoot          string
	StudyPath         string
	TaskConcurrency   int
	Tasks             []string
	DryRun            bool
	RecoveryOnly      bool
	ResourcePreflight bool
	ReclaimTaskImages bool
	// OnProgress receives stage and cell updates. Cell phase updates arrive
	// from per-cell watcher goroutines, so with TaskConcurrency above one the
	// hook may be invoked concurrently and consumers must synchronize.
	OnProgress func(StudyProgress)
	// OnPrepared receives validated cached/missing progress after the full
	// runtime preflight and before any inference call.
	OnPrepared func(StudyOutcome) error
	// OnCellComplete is a serial CLI-facing progress hook. Library code never
	// writes stdout, and completed cells remain observable even if a later cell
	// fails.
	OnCellComplete func(ObservedCostRange)
}

// StudyProgress is an operator-facing stage or cell update.
type StudyProgress struct {
	Phase, Message, Task, Experiment string
	Attempt                          int
	Completed, Required              int
	StartedAt                        time.Time
	EffectiveTimeout                 time.Duration
	MaximumAttempts                  int
}

// StudyOutcome is the user-facing progress summary for a study invocation.
type StudyOutcome struct {
	StudyID                  string
	SelectionID              string
	Required                 int
	Completed                int
	Missing                  int
	ExecutionTimeoutSum      time.Duration
	RecoveredCells           int
	HasBareExperiment        bool
	BarePublishedEstimateUSD *float64
	ObservedInvocationCost   ObservedCostRange
	JSONPath                 string
	HTMLPath                 string
	Experiments              []StudyExperimentProgress
}

// StudyExperimentProgress reports cached and missing cells for one experiment.
type StudyExperimentProgress struct {
	Name                  string
	Identity              string
	AgentLayer            bool
	Skills                bool
	RequiredDispatchRoles []string
	DispatchTargets       []StudyDispatchTargetProgress
	Completed             int
	Required              int
	Missing               int
}

// StudyDispatchTargetProgress exposes one effective required workflow target.
type StudyDispatchTargetProgress struct {
	Role, Agent, Model, ReasoningEffort string
}

type studyDocument struct {
	Selection   string            `toml:"selection"`
	Experiments []studyExperiment `toml:"experiments"`
}

type studyExperiment struct {
	Name                  string   `toml:"name"`
	Model                 string   `toml:"model"`
	Reasoning             string   `toml:"reasoning"`
	Config                string   `toml:"config"`
	Instructions          string   `toml:"instructions"`
	Skills                string   `toml:"skills"`
	EntryPrompt           string   `toml:"entry_prompt"`
	RequiredDispatchRoles []string `toml:"required_dispatch_roles"`
}

type preparedStudyExperiment struct {
	studyExperiment
	model       Model
	effort      string
	identity    string
	inputHashes map[string]string
	inputs      studyExperimentInputs
}

type preparedStudy struct {
	document    studyDocument
	selection   matrixSelection
	selectionID string
	studyID     string
	experiments []preparedStudyExperiment
	inputRoot   string
}

// studyExperimentInputs names bytes copied into a private immutable staging
// directory during preparation. No execution path reads the mutable files
// declared by study.toml after this point.
type studyExperimentInputs struct {
	Config       string
	Instructions string
	Skills       string
	EntryPrompt  string
}

func (study preparedStudy) cleanupInputs() {
	if study.inputRoot != "" {
		_ = os.RemoveAll(study.inputRoot)
	}
}

type immutableStudyManifest struct {
	SchemaVersion string             `json:"schema_version"`
	StudyID       string             `json:"study_id"`
	SelectionID   string             `json:"selection_id"`
	Membership    []string           `json:"ordered_display_membership"`
	Checksums     map[string]string  `json:"task_checksums"`
	Environments  map[string]string  `json:"task_environment_identities"`
	Resources     map[string]any     `json:"resource_contract"`
	Arms          []studyArmContract `json:"arms"`
}

type studyArmContract struct {
	Name           string `json:"name"`
	ID             string `json:"id"`
	Target         string `json:"target"`
	Bundle         string `json:"bundle_manifest_hash,omitempty"`
	Adapter        string `json:"adapter_sha256,omitempty"`
	Runtime        string `json:"runtime_sha256,omitempty"`
	RuntimeSource  string `json:"runtime_source_kind,omitempty"`
	RuntimeVersion string `json:"runtime_version,omitempty"`
}

func writeStudyManifest(study *preparedStudy, preparation matrixPreparation) error {
	manifest := immutableStudyManifest{SchemaVersion: immutableStudyManifestSchema, StudyID: study.studyID, SelectionID: study.selectionID, Checksums: copyStringMap(preparation.checksums), Environments: copyStringMap(preparation.environments), Resources: studyResourceContract()}
	for _, arm := range preparation.arms {
		manifest.Membership = append(manifest.Membership, arm.Label)
		contract := studyArmContract{Name: arm.Label, ID: arm.ID, Target: arm.Loaded.Model.RuntimeIdentifier + ":" + arm.Loaded.Effort, Adapter: arm.AdapterSHA256}
		if arm.Bundle != nil {
			contract.Bundle, contract.Runtime, contract.RuntimeSource, contract.RuntimeVersion = arm.Bundle.ManifestHash, arm.Bundle.LinuxBinarySHA256, arm.Bundle.RuntimeSourceKind, arm.Bundle.RuntimeVersion
		}
		manifest.Arms = append(manifest.Arms, contract)
	}
	path := filepath.Join(preparation.stateDir, "study-manifest.json")
	if _, err := os.Stat(path); err == nil {
		var existing immutableStudyManifest
		if err := readStudyJSON(path, &existing); err != nil {
			return fmt.Errorf("read immutable study manifest: %w", err)
		}
		left, leftErr := hashCanonical(existing)
		right, rightErr := hashCanonical(manifest)
		if leftErr != nil || rightErr != nil {
			return fmt.Errorf("hash immutable study manifest: %w", errors.Join(leftErr, rightErr))
		}
		if left != right {
			return fmt.Errorf("study %s conflicts with its immutable manifest", study.studyID)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect immutable study manifest: %w", err)
	}
	return writeJSON(path, manifest)
}

// RunStudy is intentionally the only paid public entry point. The command itself
// is authorization unless DryRun or RecoveryOnly selects a no-inference path.
func RunStudy(ctx context.Context, options StudyOptions, executor TaskExecutor) (StudyOutcome, error) {
	if options.DryRun && options.RecoveryOnly {
		return StudyOutcome{}, fmt.Errorf("benchmark study cannot combine dry-run and recovery-only modes")
	}
	emitStudyProgress(options, StudyProgress{Phase: "study", Message: "Validating and snapshotting study inputs"})
	prepared, err := prepareStudy(options)
	if err != nil {
		return StudyOutcome{}, err
	}
	defer prepared.cleanupInputs()
	emitStudyProgress(options, StudyProgress{Phase: "preflight", Message: "Checking tools, authentication, treatment, and task readiness"})
	preparation, err := prepareStudyExecution(ctx, options, &prepared)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome := studyProgress(prepared, options)
	progress, err := studyProgressChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome.Completed, outcome.Missing = progress.Completed, progress.Missing
	for i := range outcome.Experiments {
		outcome.Experiments[i].Completed = progress.Arms[i].Completed
		outcome.Experiments[i].Missing = progress.Arms[i].Missing
	}
	outcome.ExecutionTimeoutSum, err = missingStudyCellsTimeoutSum(options.RepoRoot, preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	if options.OnPrepared != nil {
		if err := options.OnPrepared(outcome); err != nil {
			return StudyOutcome{}, err
		}
	}
	if options.RecoveryOnly {
		recovered, err := recoverTerminalVerifierTimeoutCells(ctx, options.RepoRoot, preparation, options.Tasks)
		if err != nil {
			return StudyOutcome{}, err
		}
		outcome.RecoveredCells = recovered
		progress, err = studyProgressChecked(preparation)
		if err != nil {
			return StudyOutcome{}, err
		}
		outcome.Completed, outcome.Missing = progress.Completed, progress.Missing
		for i := range outcome.Experiments {
			outcome.Experiments[i].Completed = progress.Arms[i].Completed
			outcome.Experiments[i].Missing = progress.Arms[i].Missing
		}
		outcome.ExecutionTimeoutSum, err = missingStudyCellsTimeoutSum(options.RepoRoot, preparation)
		if err != nil {
			return StudyOutcome{}, err
		}
		if outcome.Missing == 0 {
			_, outcome.JSONPath, outcome.HTMLPath, err = buildStudyReport(prepared, preparation)
			if err != nil {
				return StudyOutcome{}, err
			}
		}
		return outcome, nil
	}
	if options.DryRun {
		return outcome, nil
	}
	if executor == nil {
		executor = PierExecutor{}
	}
	executor = &studyProgressExecutor{delegate: executor, options: options}
	priorCost, err := studyStoredCostChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	invocationCost := ObservedCostRange{}
	completedCells := outcome.Completed
	readinessByTask := map[string]loadedTaskReadiness{}
	if options.ReclaimTaskImages && preparation.taskConcurrency == 1 {
		checkout, checkoutErr := ensurePinnedBenchmarkCheckout(ctx, options.RepoRoot)
		if checkoutErr != nil {
			return StudyOutcome{}, checkoutErr
		}
		for _, task := range preparation.tasks {
			readiness, loadErr := loadStudyTaskReadiness(checkout, task.ID)
			if loadErr != nil {
				return StudyOutcome{}, loadErr
			}
			readinessByTask[task.ID] = readiness
		}
	}
	var reclaimingExecutor *serialTaskReclaimingExecutor
	if len(readinessByTask) > 0 {
		reclaimingExecutor = &serialTaskReclaimingExecutor{delegate: executor, readiness: readinessByTask}
		executor = reclaimingExecutor
	}
	executionErr := executeMatrix(ctx, options.RepoRoot, preparation.checksums, preparation.environments, preparation.arms, options.Tasks, preparation.taskConcurrency, executor, func(job matrixJob) {
		emitStudyProgress(options, StudyProgress{Phase: "run", Message: "Running benchmark cell", Task: job.cell.task, Experiment: job.arm.Label, Attempt: job.cell.attempt, Completed: completedCells, Required: outcome.Required})
	}, func(job matrixJob, result AttemptResult) {
		minimum, maximum, boundsErr := result.CostBounds()
		if boundsErr != nil {
			return
		}
		invocationCost.Midpoint += *result.CostUSD
		invocationCost.Minimum += minimum
		invocationCost.Maximum += maximum
		if options.OnCellComplete != nil {
			options.OnCellComplete(addObservedCost(priorCost, invocationCost))
		}
		completedCells++
		emitStudyProgress(options, StudyProgress{Phase: "run", Message: "Completed benchmark cell", Task: result.Task, Experiment: job.arm.Label, Attempt: job.cell.attempt, Completed: completedCells, Required: outcome.Required})
	})
	if reclaimingExecutor != nil {
		cleanupCtx, cancelCleanup := detachedBenchmarkCleanupContext(ctx)
		executionErr = errors.Join(executionErr, reclaimingExecutor.close(cleanupCtx))
		cancelCleanup()
	}
	if executionErr != nil {
		return StudyOutcome{}, executionErr
	}
	report, jsonPath, htmlPath, err := buildStudyReport(prepared, preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome = studyProgress(prepared, options)
	progress, err = studyProgressChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome.Completed, outcome.Missing = progress.Completed, progress.Missing
	outcome.JSONPath, outcome.HTMLPath = jsonPath, htmlPath
	outcome.ObservedInvocationCost = subtractObservedCost(studyReportCost(report), priorCost)
	for i := range outcome.Experiments {
		outcome.Experiments[i].Completed = progress.Arms[i].Completed
		outcome.Experiments[i].Missing = progress.Arms[i].Missing
	}
	return outcome, nil
}

func recoverTerminalVerifierTimeoutCells(ctx context.Context, repoRoot string, preparation matrixPreparation, tasks []string) (recovered int, returnErr error) {
	lock, err := acquireStudyExecutionLock(preparation.arms)
	if err != nil {
		return 0, err
	}
	defer func() { returnErr = errors.Join(returnErr, lock.release()) }()
	selected := map[string]bool{}
	for _, task := range tasks {
		selected[task] = true
	}
	for armIndex := range preparation.arms {
		arm := &preparation.arms[armIndex]
		for _, task := range arm.Loaded.Plan.Tasks {
			if len(selected) > 0 && !selected[task.ID] {
				continue
			}
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, _, err := inspectStudyCell(*arm, task.ID, attempt, preparation.checksums[task.ID], preparation.environments[task.ID])
				if err != nil {
					return recovered, err
				}
				if state != studyCellMissing {
					continue
				}
				request := ExecutionRequest{
					RepoRoot: repoRoot, EvidenceDir: arm.StateDir, Attempt: attempt, Task: task.ID,
					Experiment: arm.Label, Model: arm.Loaded.Model, Effort: arm.Loaded.Effort, Arm: arm.Mode,
					Bundle: arm.Bundle, AgentTimeoutMultiplier: arm.AgentTimeoutMultiplier,
					TaskChecksum: preparation.checksums[task.ID], EnvironmentIdentity: preparation.environments[task.ID],
					ResumeFailedInfrastructure: true,
				}
				checkpoint, found, err := matchingPierExecutionCheckpoint(request)
				if err != nil {
					return recovered, err
				}
				if !found {
					continue
				}
				raw, err := readPierTaskResult(checkpoint.StagePath, request)
				if err != nil {
					return recovered, fmt.Errorf("inspect retained checkpoint %s at %s: %w", checkpoint.EventID, checkpoint.StagePath, err)
				}
				terminal, err := terminalVerifierTestTimeout(checkpoint.StagePath, raw)
				if err != nil {
					return recovered, err
				}
				if !terminal {
					continue
				}
				request.EventID = checkpoint.EventID
				request.executionCheckpointed = true
				request.recoveryOnly = true
				result, err := (PierExecutor{}).replayVerifier(ctx, request, checkpoint)
				if err != nil {
					return recovered, fmt.Errorf("recover terminal verifier timeout for %s repetition %d: %w", task.ID, attempt, err)
				}
				if result.VerifierOutcome != verifierOutcomeTestTimeout || result.Validate() != nil {
					return recovered, fmt.Errorf("recovery for %s repetition %d returned invalid terminal timeout evidence", task.ID, attempt)
				}
				if err := writeJSON(armResultPath(arm.StateDir, task.ID, attempt), result); err != nil {
					return recovered, err
				}
				recovered++
			}
		}
	}
	return recovered, nil
}

func emitStudyProgress(options StudyOptions, progress StudyProgress) {
	if options.OnProgress != nil {
		options.OnProgress(progress)
	}
}

var stageBenchmarkExperimentBundles = stageStudyExperimentBundles

const runtimePreflightProgressMessage = "Preflighting benchmark runtime"

const runtimePreflightReceiptSchema = "deepswe-runtime-preflight-v1"

type runtimePreflightReceipt struct {
	Schema                 string  `json:"schema"`
	DeepSWECommit          string  `json:"deep_swe_commit"`
	PierVersion            string  `json:"pier_version"`
	TaskContainerPlatform  string  `json:"task_container_platform"`
	HostOS                 string  `json:"host_os"`
	HostArchitecture       string  `json:"host_architecture"`
	Task                   string  `json:"task"`
	TaskChecksum           string  `json:"task_checksum"`
	EnvironmentIdentity    string  `json:"environment_identity"`
	Arm                    string  `json:"arm"`
	ProviderAdapter        string  `json:"provider_adapter"`
	RuntimeModel           string  `json:"runtime_model"`
	ReasoningEffort        string  `json:"reasoning_effort"`
	ProviderClientVersion  string  `json:"provider_client_version"`
	AdapterSHA256          string  `json:"adapter_sha256,omitempty"`
	TreatmentHash          string  `json:"treatment_manifest_hash,omitempty"`
	AgentTimeoutMultiplier float64 `json:"agent_timeout_multiplier"`
}

var (
	loadStudyTaskReadiness = loadTaskReadiness
	reclaimStudyTaskImages = removeTaskReadinessImages
)

// serialTaskReclaimingExecutor retains a task image across all of that task's
// adjacent cells, then reclaims it before the serial scheduler starts the next
// task. executeMatrix deliberately orders serial jobs by task.
type serialTaskReclaimingExecutor struct {
	delegate   TaskExecutor
	readiness  map[string]loadedTaskReadiness
	activeTask string
}

type studyProgressExecutor struct {
	delegate TaskExecutor
	options  StudyOptions
}

func (executor *studyProgressExecutor) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	request.OnProgress = func(progress ExecutionProgress) {
		message := map[string]string{
			executionPhaseEnvironment: "Preparing benchmark cell environment",
			executionPhaseProvider:    "Running provider inference",
			executionPhaseVerifier:    "Running verifier",
		}[progress.Phase]
		if message == "" {
			message = "Running benchmark cell"
		}
		emitStudyProgress(executor.options, StudyProgress{
			Phase: progress.Phase, Message: message, Task: request.Task, Experiment: request.Experiment,
			Attempt:   request.Attempt,
			StartedAt: progress.StartedAt, EffectiveTimeout: progress.EffectiveTimeout, MaximumAttempts: progress.MaximumAttempts,
		})
	}
	return executor.delegate.Execute(ctx, request)
}

func (executor *serialTaskReclaimingExecutor) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	if executor.activeTask != "" && executor.activeTask != request.Task {
		if err := executor.reclaim(ctx); err != nil {
			return AttemptResult{}, err
		}
	}
	executor.activeTask = request.Task
	return executor.delegate.Execute(ctx, request)
}

func (executor *serialTaskReclaimingExecutor) close(ctx context.Context) error {
	return executor.reclaim(ctx)
}

func (executor *serialTaskReclaimingExecutor) reclaim(ctx context.Context) error {
	task := executor.activeTask
	if task == "" {
		return nil
	}
	readiness, ok := executor.readiness[task]
	if !ok {
		return fmt.Errorf("reclaim task image after %s: readiness is unavailable", task)
	}
	if err := reclaimStudyTaskImages(ctx, readiness); err != nil {
		return fmt.Errorf("reclaim task image after %s: %w", task, err)
	}
	executor.activeTask = ""
	return nil
}

func detachedBenchmarkCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), benchmarkDockerCleanupTimeout)
}

type studyRuntimePreflight struct {
	experiment string
	request    ExecutionRequest
}

func prepareStudyExecution(ctx context.Context, options StudyOptions, prepared *preparedStudy) (matrixPreparation, error) {
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	tasks := make([]benchmarkPlanTask, len(prepared.selection.Tasks))
	for i, task := range prepared.selection.Tasks {
		tasks[i] = benchmarkPlanTask{ID: task.ID, RepetitionsPerArm: task.Repetitions}
	}
	selections := make([]parsedSelection, len(prepared.experiments))
	for i, experiment := range prepared.experiments {
		selections[i] = parsedSelection{model: experiment.model, effort: experiment.effort}
	}
	candidates, err := listPlausibleCompletedCachedStudies(options.RepoRoot, prepared)
	if err != nil {
		return matrixPreparation{}, err
	}
	if options.RecoveryOnly {
		recoverable, found, err := loadRecoverableCachedStudy(options.RepoRoot, prepared, candidates, tasks, options.TaskConcurrency)
		if err != nil {
			return matrixPreparation{}, err
		}
		if found {
			return recoverable, nil
		}
	}
	cached, bundles, found, err := loadCompleteCachedStudy(options.RepoRoot, prepared, candidates, tasks, options.TaskConcurrency)
	if err != nil {
		return matrixPreparation{}, err
	}
	if found {
		return cached, nil
	}
	if err := preflightBenchmark(selections); err != nil {
		return matrixPreparation{}, err
	}
	if err := verifyBenchmarkPier(ctx); err != nil {
		return matrixPreparation{}, err
	}
	authentication := map[string]AuthenticationPreflight{}
	if !options.RecoveryOnly {
		authentication, err = validateBenchmarkAuthentication(ctx, options.RepoRoot, selections)
		if err != nil {
			return matrixPreparation{}, err
		}
	}
	if bundles == nil {
		bundles, err = stageStudyBundlesIfNeeded(options.RepoRoot, prepared)
		if err != nil {
			return matrixPreparation{}, err
		}
	}
	if options.ResourcePreflight && !options.RecoveryOnly {
		emitStudyProgress(options, StudyProgress{Phase: "resources", Message: "Checking Docker disk capacity before image pulls"})
		if err := preflightStudyDisk(ctx, tasks, options.ReclaimTaskImages && options.TaskConcurrency == 1); err != nil {
			return matrixPreparation{}, err
		}
	}
	emitStudyProgress(options, StudyProgress{Phase: "tasks", Message: "Preparing and certifying task environments"})
	checksums, environments, err := prepareBenchmarkTaskSet(ctx, options.RepoRoot, tasks)
	if err != nil {
		return matrixPreparation{}, err
	}
	preparation, err := bindStudyPreparation(options.RepoRoot, prepared, tasks, checksums, environments, bundles, authentication, options.TaskConcurrency)
	if err != nil {
		return matrixPreparation{}, err
	}
	reclaimTaskImages := options.ReclaimTaskImages && preparation.taskConcurrency == 1
	checkout := ""
	if reclaimTaskImages {
		checkout, err = ensurePinnedBenchmarkCheckout(ctx, options.RepoRoot)
		if err != nil {
			return matrixPreparation{}, err
		}
	}
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		if err := ensureStudyArmManifest(prepared.selectionID, tasks, checksums, arm); err != nil {
			return matrixPreparation{}, err
		}
		if err := reuseMatchingEvidence(options.RepoRoot, prepared.selectionID, tasks, checksums, environments, arm); err != nil {
			return matrixPreparation{}, fmt.Errorf("reuse immutable historical evidence for experiment %q: %w", arm.Label, err)
		}
	}
	if !options.RecoveryOnly {
		work := scheduleStudyRuntimePreflights(options.RepoRoot, tasks, checksums, environments, preparation.arms)
		if err := preflightStudyRuntimes(ctx, options, tasks, work, reclaimTaskImages, checkout); err != nil {
			return matrixPreparation{}, err
		}
	}
	if err := writeStudyManifest(prepared, preparation); err != nil {
		return matrixPreparation{}, err
	}
	return preparation, nil
}

func preflightStudyDisk(ctx context.Context, tasks []benchmarkPlanTask, reclaimTaskImages bool) error {
	return preflightReadinessDisk(ctx, tasks, !reclaimTaskImages)
}

// scheduleStudyRuntimePreflights preserves per-arm environment deduplication,
// then arranges the resulting work by selected task so serial reclamation can
// retain each task image for every applicable arm.
func scheduleStudyRuntimePreflights(repoRoot string, tasks []benchmarkPlanTask, checksums, environments map[string]string, arms []matrixArm) [][]studyRuntimePreflight {
	work := make([][]studyRuntimePreflight, len(tasks))
	for armIndex := range arms {
		arm := &arms[armIndex]
		request := ExecutionRequest{RepoRoot: repoRoot, EvidenceDir: arm.StateDir, Attempt: 1, Model: arm.Loaded.Model, Effort: arm.Loaded.Effort, Arm: arm.Mode, Bundle: arm.Bundle, AgentTimeoutMultiplier: arm.AgentTimeoutMultiplier, AdapterSHA256: arm.AdapterSHA256}
		if !usesAgentLayerPierAdapter(request) {
			continue
		}
		seen := map[string]bool{}
		for taskIndex, task := range tasks {
			environment := environments[task.ID]
			if seen[environment] {
				continue
			}
			seen[environment] = true
			request.Task = task.ID
			request.TaskChecksum, request.EnvironmentIdentity = checksums[task.ID], environment
			work[taskIndex] = append(work[taskIndex], studyRuntimePreflight{experiment: arm.Label, request: request})
		}
	}
	return work
}

func preflightStudyRuntimes(ctx context.Context, options StudyOptions, tasks []benchmarkPlanTask, work [][]studyRuntimePreflight, reclaimTaskImages bool, checkout string) error {
	required := 0
	for _, taskWork := range work {
		required += len(taskWork)
	}
	if required == 0 {
		return nil
	}
	hostArchitecture, err := dockerHostArchitecture(ctx)
	if err != nil {
		return err
	}
	completed := 0
	var last StudyProgress
	for taskIndex, taskWork := range work {
		if len(taskWork) == 0 {
			continue
		}
		if err := preflightStudyTaskRuntimes(ctx, tasks[taskIndex].ID, taskWork, reclaimTaskImages, checkout, &completed, required, options, hostArchitecture); err != nil {
			return err
		}
		last = StudyProgress{Phase: "runtime-preflight", Message: runtimePreflightProgressMessage, Task: tasks[taskIndex].ID, Experiment: taskWork[len(taskWork)-1].experiment, Completed: completed, Required: required}
	}
	emitStudyProgress(options, last)
	return nil
}

type pendingRuntimePreflightReceipt struct {
	experiment string
	path       string
	receipt    runtimePreflightReceipt
}

func commitRuntimePreflightReceipts(task string, pending []pendingRuntimePreflightReceipt) error {
	for _, item := range pending {
		if err := writeJSON(item.path, item.receipt); err != nil {
			return fmt.Errorf("record successful runtime preflight for experiment %q task %q: %w", item.experiment, task, err)
		}
	}
	return nil
}

func preflightStudyTaskRuntimes(ctx context.Context, task string, work []studyRuntimePreflight, reclaimTaskImages bool, checkout string, completed *int, required int, options StudyOptions, hostArchitecture string) (err error) {
	var cleanupReadiness *loadedTaskReadiness
	var pending []pendingRuntimePreflightReceipt
	defer func() {
		if cleanupReadiness != nil {
			cleanupCtx, cancelCleanup := detachedBenchmarkCleanupContext(ctx)
			cleanupErr := reclaimStudyTaskImages(cleanupCtx, *cleanupReadiness)
			cancelCleanup()
			if cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("reclaim task images after runtime preflights for %q: %w", task, cleanupErr))
				return
			}
		}
		err = errors.Join(err, commitRuntimePreflightReceipts(task, pending))
	}()
	installCleanup := func() error {
		if !reclaimTaskImages || cleanupReadiness != nil {
			return nil
		}
		readiness, loadErr := loadStudyTaskReadiness(checkout, task)
		if loadErr != nil {
			return loadErr
		}
		cleanupReadiness = &readiness
		return nil
	}
	for _, item := range work {
		cached, receipt, receiptPath, receiptErr := completedRuntimePreflight(item.request, hostArchitecture)
		if receiptErr != nil {
			return receiptErr
		}
		if cached {
			(*completed)++
			continue
		}
		if cleanupErr := installCleanup(); cleanupErr != nil {
			return cleanupErr
		}
		eventID, eventErr := NewEventID()
		if eventErr != nil {
			return fmt.Errorf("create runtime preflight event for experiment %q task %q: %w", item.experiment, task, eventErr)
		}
		item.request.EventID = eventID
		emitStudyProgress(options, StudyProgress{Phase: "runtime-preflight", Message: runtimePreflightProgressMessage, Task: task, Experiment: item.experiment, Completed: *completed, Required: required})
		if preflightErr := preflightTreatmentRuntime(ctx, item.request); preflightErr != nil {
			return fmt.Errorf("preflight experiment %q task %q runtime: %w", item.experiment, task, preflightErr)
		}
		pending = append(pending, pendingRuntimePreflightReceipt{experiment: item.experiment, path: receiptPath, receipt: receipt})
		(*completed)++
	}
	return nil
}

func completedRuntimePreflight(request ExecutionRequest, hostArchitecture string) (bool, runtimePreflightReceipt, string, error) {
	if request.EvidenceDir == "" {
		return false, runtimePreflightReceipt{}, "", errors.New("benchmark runtime preflight requires an evidence directory")
	}
	if hostArchitecture == "" {
		return false, runtimePreflightReceipt{}, "", errors.New("benchmark runtime preflight requires a Docker host architecture")
	}
	receipt := runtimePreflightReceipt{
		Schema: runtimePreflightReceiptSchema, DeepSWECommit: DeepSWECommit, PierVersion: PierVersion,
		TaskContainerPlatform: benchmarkTaskContainerPlatform, HostOS: runtime.GOOS, HostArchitecture: hostArchitecture,
		Task:         request.Task,
		TaskChecksum: request.TaskChecksum, EnvironmentIdentity: request.EnvironmentIdentity,
		Arm: request.Arm, ProviderAdapter: request.Model.Adapter,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		ProviderClientVersion: request.Model.ProviderClientVersion, AdapterSHA256: request.AdapterSHA256,
		TreatmentHash: executionTreatmentHash(request), AgentTimeoutMultiplier: request.AgentTimeoutMultiplier,
	}
	identity, err := hashCanonical(receipt)
	if err != nil {
		return false, runtimePreflightReceipt{}, "", fmt.Errorf("identify benchmark runtime preflight: %w", err)
	}
	path := filepath.Join(request.EvidenceDir, "runtime-preflights", identity+".json")
	var existing runtimePreflightReceipt
	if err := readStudyJSON(path, &existing); errors.Is(err, os.ErrNotExist) {
		return false, receipt, path, nil
	} else if err != nil {
		return false, runtimePreflightReceipt{}, "", fmt.Errorf("read benchmark runtime preflight receipt: %w", err)
	}
	if existing != receipt {
		return false, runtimePreflightReceipt{}, "", fmt.Errorf("benchmark runtime preflight receipt %s does not match its content-addressed identity", path)
	}
	return true, receipt, path, nil
}

func stageStudyExperimentBundles(repoRoot string, prepared *preparedStudy) ([]*TreatmentBundle, error) {
	bundles := make([]*TreatmentBundle, len(prepared.experiments))
	for index, experiment := range prepared.experiments {
		if experiment.Config == "" && experiment.Instructions == "" && experiment.Skills == "" {
			continue
		}
		bundle, err := BuildStudyTreatmentBundle(repoRoot, experiment)
		if err != nil {
			return nil, fmt.Errorf("stage experiment %q: %w", experiment.Name, err)
		}
		bundle, err = pinStudyTreatmentBundle(repoRoot, bundle)
		if err != nil {
			return nil, fmt.Errorf("pin experiment %q effective bundle: %w", experiment.Name, err)
		}
		bundles[index] = bundle
	}
	return bundles, nil
}

func studyDeclaresTreatment(prepared *preparedStudy) bool {
	for _, experiment := range prepared.experiments {
		if experiment.Config != "" || experiment.Instructions != "" || experiment.Skills != "" {
			return true
		}
	}
	return false
}

func stageStudyBundlesIfNeeded(repoRoot string, prepared *preparedStudy) ([]*TreatmentBundle, error) {
	if !studyDeclaresTreatment(prepared) {
		return make([]*TreatmentBundle, len(prepared.experiments)), nil
	}
	return stageBenchmarkExperimentBundles(repoRoot, prepared)
}

const (
	maxHistoricalStudyDirectories = 1024
	immutableStudyManifestSchema  = "deepswe-study-manifest-v1"
)

type cachedStudyCandidate struct {
	stateDir              string
	reportClaimedComplete bool
}

type cachedStudyReportDeclaration struct {
	SchemaVersion string                             `json:"schema_version"`
	StudyID       string                             `json:"study_id"`
	SelectionID   string                             `json:"selection_id"`
	Experiments   []cachedStudyExperimentDeclaration `json:"experiments"`
}

type cachedStudyExperimentDeclaration struct {
	Name                    string                   `json:"name"`
	Identity                string                   `json:"identity"`
	Model                   string                   `json:"model"`
	Reasoning               string                   `json:"reasoning"`
	AuthenticationPreflight *AuthenticationPreflight `json:"authentication_preflight,omitempty"`
	CompletedCells          int                      `json:"completed_cells"`
	RequiredCells           int                      `json:"required_cells"`
}

func listPlausibleCompletedCachedStudies(repoRoot string, prepared *preparedStudy) ([]cachedStudyCandidate, error) {
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect completed study state: %w", err)
	}
	var directories int
	var candidates []cachedStudyCandidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directories++
		if directories > maxHistoricalStudyDirectories {
			return nil, fmt.Errorf("benchmark study cache inspection exceeded %d historical study directories under %s; remove unused study state before retrying", maxHistoricalStudyDirectories, root)
		}
		stateDir := filepath.Join(root, entry.Name())
		var report cachedStudyReportDeclaration
		if err := readStudyJSON(filepath.Join(stateDir, "report", "report.json"), &report); err == nil && cachedStudyReportMatchesDeclaration(report, prepared, entry.Name()) {
			candidates = append(candidates, cachedStudyCandidate{stateDir: stateDir, reportClaimedComplete: true})
			continue
		}
		var manifest immutableStudyManifest
		if err := readStudyJSON(filepath.Join(stateDir, "study-manifest.json"), &manifest); err != nil {
			continue
		}
		if !studyManifestDeclarationCompatible(manifest, prepared) {
			continue
		}
		candidates = append(candidates, cachedStudyCandidate{stateDir: stateDir})
	}
	return candidates, nil
}

func cachedStudyReportMatchesDeclaration(report cachedStudyReportDeclaration, prepared *preparedStudy, studyID string) bool {
	if !cachedStudyReportMatchesIdentity(report, prepared, studyID) {
		return false
	}
	for _, item := range report.Experiments {
		if item.CompletedCells != item.RequiredCells {
			return false
		}
	}
	return true
}

func cachedStudyReportMatchesIdentity(report cachedStudyReportDeclaration, prepared *preparedStudy, studyID string) bool {
	if report.SchemaVersion != studyReportSchema || report.StudyID != studyID || report.SelectionID != prepared.selectionID ||
		len(report.Experiments) != len(prepared.experiments) {
		return false
	}
	required := 0
	for _, task := range prepared.selection.Tasks {
		required += task.Repetitions
	}
	if required < 1 {
		return false
	}
	for index, experiment := range prepared.experiments {
		item := report.Experiments[index]
		if item.Name != experiment.Name || item.Identity != experiment.identity ||
			item.Model != experiment.model.PublishedIdentifier || item.Reasoning != experiment.effort {
			return false
		}
		if item.RequiredCells != required {
			return false
		}
	}
	return true
}

func loadCompleteCachedStudy(repoRoot string, prepared *preparedStudy, candidates []cachedStudyCandidate, tasks []benchmarkPlanTask, concurrency int) (matrixPreparation, []*TreatmentBundle, bool, error) {
	var reportClaimed, manifestOnly []cachedStudyCandidate
	for _, candidate := range candidates {
		if candidate.reportClaimedComplete {
			reportClaimed = append(reportClaimed, candidate)
			continue
		}
		manifestOnly = append(manifestOnly, candidate)
	}
	var bundles []*TreatmentBundle
	var matches []matrixPreparation
	if len(reportClaimed) > 0 {
		var err error
		bundles, err = stageStudyBundlesIfNeeded(repoRoot, prepared)
		if err != nil {
			return matrixPreparation{}, nil, false, err
		}
		for _, candidate := range reportClaimed {
			preparation, skip, err := loadReportClaimedCachedStudy(repoRoot, prepared, candidate.stateDir, bundles, tasks, concurrency)
			if err != nil {
				return matrixPreparation{}, nil, false, err
			}
			if skip {
				continue
			}
			matches = append(matches, preparation)
		}
	}
	var completeManifestOnly []cachedStudyCandidate
	for _, candidate := range manifestOnly {
		complete, err := manifestOnlyCachedStudyComplete(repoRoot, prepared, candidate.stateDir, tasks, concurrency)
		if err != nil {
			return matrixPreparation{}, nil, false, err
		}
		if complete {
			completeManifestOnly = append(completeManifestOnly, candidate)
		}
	}
	if len(completeManifestOnly) > 0 {
		if bundles == nil {
			var err error
			bundles, err = stageStudyBundlesIfNeeded(repoRoot, prepared)
			if err != nil {
				return matrixPreparation{}, nil, false, err
			}
		}
		for _, candidate := range completeManifestOnly {
			var manifest immutableStudyManifest
			if err := readStudyJSON(filepath.Join(candidate.stateDir, "study-manifest.json"), &manifest); err != nil {
				return matrixPreparation{}, nil, false, fmt.Errorf("read immutable study manifest %s: %w", filepath.Base(candidate.stateDir), err)
			}
			if !studyManifestBundleMatches(manifest, prepared, bundles) {
				continue
			}
			preparation, err := preparationFromCachedStudy(repoRoot, prepared, manifest, candidate.stateDir, bundles, tasks, concurrency)
			if err != nil {
				return matrixPreparation{}, nil, false, err
			}
			matches = append(matches, preparation)
		}
	}
	switch len(matches) {
	case 0:
		return matrixPreparation{}, bundles, false, nil
	case 1:
		prepared.studyID = filepath.Base(matches[0].stateDir)
		return matches[0], bundles, true, nil
	default:
		return matrixPreparation{}, nil, false, fmt.Errorf("completed benchmark study matches more than one historical state directory")
	}
}

// loadRecoverableCachedStudy binds recovery to immutable historical runtime
// identity. A candidate binary may carry a newer embedded adapter, which must
// not redirect recovery into a newly identified study and strand the retained
// checkpoint it was invoked to canonicalize.
func loadRecoverableCachedStudy(repoRoot string, prepared *preparedStudy, candidates []cachedStudyCandidate, tasks []benchmarkPlanTask, concurrency int) (matrixPreparation, bool, error) {
	var matches []matrixPreparation
	for _, candidate := range candidates {
		var manifest immutableStudyManifest
		if err := readStudyJSON(filepath.Join(candidate.stateDir, "study-manifest.json"), &manifest); err != nil {
			return matrixPreparation{}, false, fmt.Errorf("read recoverable immutable study manifest %s: %w", filepath.Base(candidate.stateDir), err)
		}
		if !studyManifestDeclarationCompatible(manifest, prepared) {
			continue
		}
		if filepath.Base(candidate.stateDir) != manifest.StudyID {
			continue
		}
		preparation := recoveryPreparationFromManifest(prepared, manifest, candidate.stateDir, tasks, concurrency)
		compatible := true
		for index := range preparation.arms {
			arm := &preparation.arms[index]
			if arm.ID != manifest.Arms[index].ID || arm.Label != manifest.Arms[index].Name {
				compatible = false
				break
			}
			if err := requireStudyArmManifest(prepared.selectionID, tasks, preparation.checksums, arm); err != nil {
				return matrixPreparation{}, false, err
			}
		}
		if !compatible {
			continue
		}
		count, err := terminalVerifierTimeoutCheckpointCount(repoRoot, preparation)
		if err != nil {
			return matrixPreparation{}, false, err
		}
		if count > 0 {
			matches = append(matches, preparation)
		}
	}
	switch len(matches) {
	case 0:
		return matrixPreparation{}, false, nil
	case 1:
		prepared.studyID = filepath.Base(matches[0].stateDir)
		return matches[0], true, nil
	default:
		return matrixPreparation{}, false, fmt.Errorf("terminal verifier timeout recovery matches more than one historical study; narrow or repair retained study state before retrying")
	}
}

func recoveryPreparationFromManifest(prepared *preparedStudy, manifest immutableStudyManifest, stateDir string, tasks []benchmarkPlanTask, concurrency int) matrixPreparation {
	preparation := matrixPreparation{
		selection: prepared.selection, selectionID: prepared.selectionID, tasks: tasks,
		checksums: copyStringMap(manifest.Checksums), environments: copyStringMap(manifest.Environments),
		taskConcurrency: concurrency, stateDir: stateDir,
	}
	bundles := historicalBundlesFromManifest(manifest)
	for index, experiment := range prepared.experiments {
		mode := ArmBaseline
		if bundles[index] != nil {
			mode = ArmTreatment
		}
		loaded := loadedBenchmarkPlan{Model: experiment.model, Effort: experiment.effort}
		loaded.Plan.Tasks = tasks
		contract := manifest.Arms[index]
		preparation.arms = append(preparation.arms, matrixArm{
			ID: contract.ID, Label: contract.Name, Mode: mode, Loaded: loaded, Bundle: bundles[index],
			AdapterSHA256: contract.Adapter, AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
			IgnoreProviderClientInManifest: true, StateDir: filepath.Join(stateDir, "arms", contract.ID),
		})
	}
	return preparation
}

func terminalVerifierTimeoutCheckpointCount(repoRoot string, preparation matrixPreparation) (int, error) {
	count := 0
	for armIndex := range preparation.arms {
		arm := &preparation.arms[armIndex]
		for _, task := range arm.Loaded.Plan.Tasks {
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, _, err := inspectStudyCell(*arm, task.ID, attempt, preparation.checksums[task.ID], preparation.environments[task.ID])
				if err != nil {
					return 0, err
				}
				if state != studyCellMissing {
					continue
				}
				request := ExecutionRequest{
					RepoRoot: repoRoot, EvidenceDir: arm.StateDir, Attempt: attempt, Task: task.ID,
					Model: arm.Loaded.Model, Effort: arm.Loaded.Effort, Arm: arm.Mode, Bundle: arm.Bundle,
					AgentTimeoutMultiplier: arm.AgentTimeoutMultiplier, TaskChecksum: preparation.checksums[task.ID],
					EnvironmentIdentity: preparation.environments[task.ID],
				}
				checkpoint, found, err := matchingPierExecutionCheckpoint(request)
				if err != nil {
					return 0, err
				}
				if !found {
					continue
				}
				raw, err := readPierTaskResult(checkpoint.StagePath, request)
				if err != nil {
					return 0, fmt.Errorf("inspect retained checkpoint %s at %s: %w", checkpoint.EventID, checkpoint.StagePath, err)
				}
				terminal, err := terminalVerifierTestTimeout(checkpoint.StagePath, raw)
				if err != nil {
					return 0, err
				}
				if terminal {
					count++
				}
			}
		}
	}
	return count, nil
}

func loadReportClaimedCachedStudy(repoRoot string, prepared *preparedStudy, stateDir string, bundles []*TreatmentBundle, tasks []benchmarkPlanTask, concurrency int) (matrixPreparation, bool, error) {
	var manifest immutableStudyManifest
	if err := readStudyJSON(filepath.Join(stateDir, "study-manifest.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return matrixPreparation{}, false, fmt.Errorf("completed cached study %s is missing its immutable manifest", filepath.Base(stateDir))
		}
		return matrixPreparation{}, false, fmt.Errorf("read immutable study manifest %s: %w", filepath.Base(stateDir), err)
	}
	if !studyManifestDeclarationCompatible(manifest, prepared) {
		return matrixPreparation{}, false, fmt.Errorf("completed cached study %s conflicts with its immutable manifest declaration", filepath.Base(stateDir))
	}
	if !studyManifestBundleMatches(manifest, prepared, bundles) {
		return matrixPreparation{}, true, nil
	}
	preparation, err := preparationFromCachedStudy(repoRoot, prepared, manifest, stateDir, bundles, tasks, concurrency)
	if err != nil {
		return matrixPreparation{}, false, err
	}
	return preparation, false, nil
}

func manifestOnlyCachedStudyComplete(repoRoot string, prepared *preparedStudy, stateDir string, tasks []benchmarkPlanTask, concurrency int) (bool, error) {
	var manifest immutableStudyManifest
	if err := readStudyJSON(filepath.Join(stateDir, "study-manifest.json"), &manifest); err != nil {
		return false, fmt.Errorf("reread immutable study manifest %s: %w", filepath.Base(stateDir), err)
	}
	if !studyManifestDeclarationCompatible(manifest, prepared) {
		return false, nil
	}
	probe := *prepared
	preparation, err := bindStudyPreparation(repoRoot, &probe, tasks, copyStringMap(manifest.Checksums), copyStringMap(manifest.Environments), historicalBundlesFromManifest(manifest), nil, concurrency)
	if err != nil {
		return false, err
	}
	if filepath.Base(stateDir) != manifest.StudyID || preparation.stateDir != stateDir {
		return false, nil
	}
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		if arm.ID != manifest.Arms[index].ID || arm.Label != manifest.Arms[index].Name {
			return false, nil
		}
	}
	for index := range preparation.arms {
		if err := requireStudyArmManifest(prepared.selectionID, tasks, preparation.checksums, &preparation.arms[index]); err != nil {
			return false, err
		}
	}
	progress, err := studyProgressChecked(preparation)
	if err != nil {
		return false, err
	}
	return progress.Missing == 0, nil
}

func historicalBundlesFromManifest(manifest immutableStudyManifest) []*TreatmentBundle {
	bundles := make([]*TreatmentBundle, len(manifest.Arms))
	for index, arm := range manifest.Arms {
		bundles[index] = historicalTreatmentBundle(arm)
	}
	return bundles
}

// historicalTreatmentBundle reconstructs the hashes needed to prove study/arm
// identity and validate receipts. It is not a staged bundle and must not be
// used for report generation.
func historicalTreatmentBundle(contract studyArmContract) *TreatmentBundle {
	if contract.Bundle == "" && contract.Runtime == "" && contract.RuntimeSource == "" && contract.RuntimeVersion == "" {
		return nil
	}
	return &TreatmentBundle{
		ManifestHash:      contract.Bundle,
		AdapterSHA256:     contract.Adapter,
		LinuxBinarySHA256: contract.Runtime,
		RuntimeSourceKind: contract.RuntimeSource,
		RuntimeVersion:    contract.RuntimeVersion,
	}
}

func studyManifestDeclarationCompatible(manifest immutableStudyManifest, prepared *preparedStudy) bool {
	if manifest.SchemaVersion != immutableStudyManifestSchema || manifest.SelectionID != prepared.selectionID {
		return false
	}
	if len(manifest.Arms) != len(prepared.experiments) || len(manifest.Membership) != len(prepared.experiments) {
		return false
	}
	left, leftErr := hashCanonical(manifest.Resources)
	right, rightErr := hashCanonical(studyResourceContract())
	if leftErr != nil || rightErr != nil || left != right {
		return false
	}
	for index, experiment := range prepared.experiments {
		arm := manifest.Arms[index]
		if manifest.Membership[index] != experiment.Name || arm.Name != experiment.Name {
			return false
		}
		if arm.Target != experiment.model.RuntimeIdentifier+":"+experiment.effort {
			return false
		}
	}
	return true
}

func studyManifestBundleMatches(manifest immutableStudyManifest, prepared *preparedStudy, bundles []*TreatmentBundle) bool {
	if len(manifest.Arms) != len(bundles) || len(prepared.experiments) != len(bundles) {
		return false
	}
	for index, arm := range manifest.Arms {
		bundle := bundles[index]                                                                // #nosec G602 -- all three parallel slices have equal lengths by the guard above.
		adapterSHA256, err := executionAdapterSHA256(prepared.experiments[index].model, bundle) // #nosec G602 -- equal lengths are validated above.
		if err != nil || arm.Adapter != adapterSHA256 {
			return false
		}
		if bundle == nil {
			if arm.Bundle != "" || arm.Runtime != "" || arm.RuntimeSource != "" || arm.RuntimeVersion != "" {
				return false
			}
			continue
		}
		if arm.Bundle != bundle.ManifestHash || arm.Runtime != bundle.LinuxBinarySHA256 ||
			arm.RuntimeSource != bundle.RuntimeSourceKind || arm.RuntimeVersion != bundle.RuntimeVersion {
			return false
		}
	}
	return true
}

func preparationFromCachedStudy(repoRoot string, prepared *preparedStudy, manifest immutableStudyManifest, stateDir string, bundles []*TreatmentBundle, tasks []benchmarkPlanTask, concurrency int) (matrixPreparation, error) {
	preparation, err := bindStudyPreparation(repoRoot, prepared, tasks, copyStringMap(manifest.Checksums), copyStringMap(manifest.Environments), bundles, nil, concurrency)
	if err != nil {
		return matrixPreparation{}, err
	}
	if filepath.Base(stateDir) != manifest.StudyID {
		return matrixPreparation{}, fmt.Errorf("completed cached study %s conflicts with its immutable manifest identity", filepath.Base(stateDir))
	}
	if preparation.stateDir != stateDir {
		return matrixPreparation{}, fmt.Errorf("completed cached study %s identity does not match its state directory", filepath.Base(stateDir))
	}
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		if arm.ID != manifest.Arms[index].ID || arm.Label != manifest.Arms[index].Name {
			return matrixPreparation{}, fmt.Errorf("completed cached study %s experiment %q identity does not match its immutable manifest", filepath.Base(stateDir), arm.Label)
		}
		if err := requireStudyArmManifest(prepared.selectionID, tasks, preparation.checksums, arm); err != nil {
			return matrixPreparation{}, err
		}
	}
	progress, err := studyProgressChecked(preparation)
	if err != nil {
		return matrixPreparation{}, err
	}
	if progress.Missing > 0 {
		return matrixPreparation{}, fmt.Errorf("completed cached study %s is missing required cell evidence", filepath.Base(stateDir))
	}
	authentication, err := cachedAuthenticationPreflight(stateDir, prepared)
	if err != nil {
		return matrixPreparation{}, err
	}
	preparation.authentication = authentication
	return preparation, nil
}

func cachedAuthenticationPreflight(stateDir string, prepared *preparedStudy) (map[string]AuthenticationPreflight, error) {
	var report cachedStudyReportDeclaration
	if err := readStudyJSON(filepath.Join(stateDir, "report", "report.json"), &report); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]AuthenticationPreflight{}, nil
		}
		return nil, fmt.Errorf("read completed cached study authentication provenance: %w", err)
	}
	if !cachedStudyReportMatchesIdentity(report, prepared, filepath.Base(stateDir)) {
		return nil, fmt.Errorf("completed cached study %s report declaration does not match its immutable study", filepath.Base(stateDir))
	}
	authentication := make(map[string]AuthenticationPreflight)
	for index, item := range report.Experiments {
		if item.AuthenticationPreflight == nil {
			continue
		}
		adapter := prepared.experiments[index].model.Adapter
		evidence := *item.AuthenticationPreflight
		if !validCachedAuthenticationPreflight(adapter, evidence) {
			return nil, fmt.Errorf("completed cached study %s has invalid authentication provenance for experiment %q", filepath.Base(stateDir), item.Name)
		}
		if existing, ok := authentication[adapter]; ok && !sameAuthenticationPreflight(existing, evidence) {
			return nil, fmt.Errorf("completed cached study %s has conflicting authentication provenance for provider %q", filepath.Base(stateDir), evidence.Provider)
		}
		authentication[adapter] = evidence
	}
	return authentication, nil
}

func validCachedAuthenticationPreflight(adapter string, evidence AuthenticationPreflight) bool {
	if evidence.VerifiedAt.IsZero() {
		return false
	}
	switch adapter {
	case adapterCodex:
		if evidence.Provider != adapterCodex || evidence.Check != codexLoginStatusCheck {
			return false
		}
		for _, method := range codexLoginStatusAllowlist {
			if evidence.AuthenticationMethod == method.normalized {
				return true
			}
		}
	case adapterAntigravity:
		return evidence.Provider == adapterAntigravity && evidence.Check == authCheckOAuthProfilePresence && evidence.AuthenticationMethod == authMethodGoogleOAuth
	case adapterGrok:
		return evidence.Provider == adapterGrok && evidence.Check == authCheckJSONFilePresence && evidence.AuthenticationMethod == authMethodJSONFile
	}
	return false
}

func sameAuthenticationPreflight(left, right AuthenticationPreflight) bool {
	return left.Provider == right.Provider && left.Check == right.Check &&
		left.AuthenticationMethod == right.AuthenticationMethod && left.VerifiedAt.Equal(right.VerifiedAt)
}

func bindStudyPreparation(repoRoot string, prepared *preparedStudy, tasks []benchmarkPlanTask, checksums, environments map[string]string, bundles []*TreatmentBundle, authentication map[string]AuthenticationPreflight, concurrency int) (matrixPreparation, error) {
	preparation := matrixPreparation{selection: prepared.selection, selectionID: prepared.selectionID, tasks: tasks, checksums: checksums, environments: environments, authentication: authentication, taskConcurrency: concurrency}
	for index, experiment := range prepared.experiments {
		adapterSHA256, err := executionAdapterSHA256(experiment.model, bundles[index])
		if err != nil {
			return matrixPreparation{}, err
		}
		identity, err := studyArmIdentity(prepared.selectionID, experiment.identity, checksums, environments, bundles[index], adapterSHA256)
		if err != nil {
			return matrixPreparation{}, err
		}
		mode := ArmBaseline
		if bundles[index] != nil {
			mode = ArmTreatment
		}
		loaded := loadedBenchmarkPlan{Model: experiment.model, Effort: experiment.effort}
		loaded.Plan.Tasks = tasks
		preparation.arms = append(preparation.arms, matrixArm{ID: identity, Label: experiment.Name, Mode: mode, Loaded: loaded, Bundle: bundles[index], AdapterSHA256: adapterSHA256, AgentTimeoutMultiplier: skillsAgentTimeoutFactor, IgnoreProviderClientInManifest: true})
	}
	membership := make([]struct{ Name, Arm string }, len(preparation.arms))
	for i, arm := range preparation.arms {
		membership[i] = struct{ Name, Arm string }{arm.Label, arm.ID}
	}
	studyID, err := identifyStudy(prepared.selectionID, membership, checksums, environments)
	if err != nil {
		return matrixPreparation{}, err
	}
	prepared.studyID = studyID
	preparation.stateDir = filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies", studyID)
	for index := range preparation.arms {
		preparation.arms[index].StateDir = filepath.Join(preparation.stateDir, "arms", preparation.arms[index].ID)
	}
	return preparation, nil
}

func executionAdapterSHA256(model Model, bundle *TreatmentBundle) (string, error) {
	if bundle != nil {
		if bundle.AdapterSHA256 == "" {
			return "", fmt.Errorf("benchmark treatment bundle is missing its Pier adapter hash")
		}
		return bundle.AdapterSHA256, nil
	}
	if model.Adapter == adapterGrok || model.Adapter == adapterAntigravity {
		return embeddedPierAdapterSHA256()
	}
	return "", nil
}

func studyArmIdentity(selectionID, experimentIdentity string, checksums, environments map[string]string, bundle *TreatmentBundle, adapterSHA256 string) (string, error) {
	identityInput := struct {
		Schema, Selection, Experiment          string
		TaskChecksums, Environments            map[string]string
		Resource                               map[string]any
		ManifestHash, AdapterHash, RuntimeHash string
	}{Schema: "deepswe-study-arm-v2", Selection: selectionID, Experiment: experimentIdentity, AdapterHash: adapterSHA256,
		TaskChecksums: checksums, Environments: environments, Resource: studyResourceContract()}
	if bundle != nil {
		identityInput.ManifestHash, identityInput.RuntimeHash = bundle.ManifestHash, bundle.LinuxBinarySHA256
	}
	return hashCanonical(identityInput)
}

func studyResourceContract() map[string]any {
	return map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}
}

// reuseMatchingEvidence reuses only arms requested by this study. It checks
// sibling study compositions first, then compatible public matrix history;
// private campaign state is never traversed.
func reuseMatchingEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	if err := reuseMatchingStudyEvidence(repoRoot, selectionID, tasks, checksums, environments, destination); err != nil {
		return err
	}
	return reuseMatchingMatrixEvidence(repoRoot, selectionID, tasks, checksums, environments, destination)
}

func reuseMatchingStudyEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect compatible study evidence: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sourceDir := filepath.Join(root, entry.Name(), "arms", destination.ID)
		if sourceDir == destination.StateDir {
			continue
		}
		manifest, found, err := reusableStudyArmManifest(sourceDir)
		if err != nil {
			return err
		}
		if !found || !compatibleStudyArmManifest(manifest, selectionID, tasks, checksums, destination) {
			continue
		}
		source := *destination
		source.StateDir = sourceDir
		if err := reuseArmCells(source, destination, tasks, checksums, environments); err != nil {
			return err
		}
	}
	return nil
}

func reuseMatchingMatrixEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID, "arms")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect historical matrix evidence: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sourceDir := filepath.Join(root, entry.Name())
		manifest, found, err := reusableLegacyMatrixArmManifest(sourceDir)
		if err != nil {
			return err
		}
		if !found || !compatibleLegacyMatrixArmManifest(manifest, selectionID, tasks, checksums, destination, sourceDir) {
			continue
		}
		source := *destination
		source.StateDir = sourceDir
		if err := reuseArmCells(source, destination, tasks, checksums, environments); err != nil {
			return err
		}
	}
	return nil
}

func reusableStudyArmManifest(root string) (studyArmManifest, bool, error) {
	var manifest studyArmManifest
	if err := readStudyJSON(filepath.Join(root, "manifest.json"), &manifest); errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	} else if err != nil {
		return manifest, false, fmt.Errorf("read reusable arm manifest %s: %w", root, err)
	}
	return manifest, true, nil
}

func compatibleStudyArmManifest(manifest studyArmManifest, selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, destination *matrixArm) bool {
	return manifest.SchemaVersion == studyArmManifestSchema && manifest.SelectionID == selectionID &&
		manifest.Mode == destination.Mode && manifest.Model == destination.Loaded.Model.PublishedIdentifier &&
		manifest.Reasoning == destination.Loaded.Effort && sameStringMap(manifest.TaskChecksums, checksums) &&
		sameIntMap(manifest.Repetitions, repetitionsForTasks(tasks)) && manifest.AgentTimeoutMultiplier == destination.AgentTimeoutMultiplier &&
		manifest.TreatmentHash == bundleManifestHash(destination.Bundle) && manifest.AdapterSHA256 == destination.AdapterSHA256
}

func reusableLegacyMatrixArmManifest(root string) (matrixArmManifest, bool, error) {
	var manifest matrixArmManifest
	if err := readStudyJSON(filepath.Join(root, "manifest.json"), &manifest); errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	} else if err != nil {
		return manifest, false, fmt.Errorf("read reusable legacy matrix arm manifest %s: %w", root, err)
	}
	return manifest, true, nil
}

func compatibleLegacyMatrixArmManifest(manifest matrixArmManifest, selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, destination *matrixArm, sourceDir string) bool {
	multiplier := manifest.AgentTimeoutMultiplier
	if multiplier == 0 && manifest.Mode == ArmTreatment {
		multiplier = historicalTreatmentMultiplier(sourceDir, manifest.TreatmentHash)
	}
	return manifest.SchemaVersion == matrixManifestSchema && manifest.SelectionID == selectionID &&
		manifest.Mode == destination.Mode && manifest.Model == destination.Loaded.Model.PublishedIdentifier &&
		manifest.Reasoning == destination.Loaded.Effort && sameStringMap(manifest.TaskChecksums, checksums) &&
		sameIntMap(manifest.Repetitions, repetitionsForTasks(tasks)) && multiplier == destination.AgentTimeoutMultiplier &&
		manifest.TreatmentHash == bundleManifestHash(destination.Bundle) && (destination.Bundle != nil || destination.AdapterSHA256 == "")
}

func historicalTreatmentMultiplier(sourceDir, treatmentHash string) float64 {
	if treatmentHash == "" {
		return 0
	}
	matrixRoot := filepath.Dir(filepath.Dir(sourceDir))
	entries, err := os.ReadDir(filepath.Join(matrixRoot, "treatment-pins"))
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		var pin historicalMatrixTreatmentPin
		if entry.IsDir() && readStudyJSON(filepath.Join(matrixRoot, "treatment-pins", entry.Name(), "pin.json"), &pin) == nil && pin.ManifestHash == treatmentHash {
			return pin.Manifest.AgentTimeoutMultiplier
		}
	}
	return 0
}

// historicalMatrixTreatmentPin is intentionally a narrow reader for the
// predecessor runner's immutable pin evidence. New studies write
// studyTreatmentPin instead and never extend this retired schema.
type historicalMatrixTreatmentPin struct {
	ManifestHash string            `json:"manifest_hash"`
	Manifest     TreatmentManifest `json:"manifest"`
}

func reuseArmCells(source matrixArm, destination *matrixArm, tasks []benchmarkPlanTask, checksums, environments map[string]string) error {
	for _, task := range tasks {
		for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
			state, _, err := inspectStudyCell(*destination, task.ID, attempt, checksums[task.ID], environments[task.ID])
			if err != nil {
				return err
			}
			if state == studyCellValid {
				continue
			}
			sourcePath := armResultPath(source.StateDir, task.ID, attempt)
			result, err := readStudyResult(sourcePath, task.ID, attempt, checksums[task.ID], environments[task.ID], source)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("validate reusable %s repetition %d: %w", task.ID, attempt, err)
			}
			if err := promoteReusableCell(sourcePath, result, destination, task.ID, attempt, checksums[task.ID], environments[task.ID]); err != nil {
				return err
			}
		}
	}
	return nil
}

func promoteReusableCell(sourcePath string, result AttemptResult, destination *matrixArm, task string, attempt int, checksum, environment string) error {
	// readStudyResult has already validated this value, including any narrowly
	// authorized correction to predecessor matrix evidence. A study deliberately
	// does not consume legacy canonical-result sidecars, so promote the returned
	// canonical result itself rather than copying the obsolete source bytes.
	if err := result.Validate(); err != nil {
		return fmt.Errorf("validate canonical reusable result: %w", err)
	}
	destinationPath := armResultPath(destination.StateDir, task, attempt)
	stage, err := os.MkdirTemp(filepath.Dir(destination.StateDir), ".reuse-cell-")
	if err != nil {
		return fmt.Errorf("stage reusable cell: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	stagedResult := armResultPath(stage, task, attempt)
	if err := copyRequiredTree(filepath.Join(filepath.Dir(sourcePath), benchmarkArtifactsDir, result.EventID), filepath.Join(filepath.Dir(stagedResult), benchmarkArtifactsDir, result.EventID)); err != nil {
		return fmt.Errorf("copy reusable artifacts: %w", err)
	}
	if err := writeJSON(stagedResult, result); err != nil {
		return fmt.Errorf("stage canonical reusable result: %w", err)
	}
	stagedArm := *destination
	stagedArm.StateDir = stage
	if _, _, err := inspectStudyCell(stagedArm, task, attempt, checksum, environment); err != nil {
		return fmt.Errorf("validate staged reusable cell: %w", err)
	}
	if err := copyRequiredTree(filepath.Join(filepath.Dir(stagedResult), benchmarkArtifactsDir), filepath.Join(filepath.Dir(destinationPath), benchmarkArtifactsDir)); err != nil {
		return err
	}
	// Publish the result last and atomically. Until this succeeds the copied
	// artifacts are not a valid study cell, so a failed promotion is retriable
	// without allowing a raw legacy score or a sidecar to enter study analysis.
	if err := writeJSON(destinationPath, result); err != nil {
		return fmt.Errorf("publish canonical reusable result: %w", err)
	}
	return nil
}

func studyProgress(prepared preparedStudy, _ StudyOptions) StudyOutcome {
	required := 0
	for _, task := range prepared.selection.Tasks {
		required += task.Repetitions
	}
	outcome := StudyOutcome{StudyID: prepared.studyID, SelectionID: prepared.selectionID}
	for _, experiment := range prepared.experiments {
		agentLayer := experiment.Config != "" || experiment.Instructions != "" || experiment.Skills != ""
		if !agentLayer {
			outcome.HasBareExperiment = true
			if experiment.model.PublishedIdentifier == prepared.selection.Selector.Model && experiment.effort == prepared.selection.Selector.Reasoning {
				estimate := prepared.selection.EstimatedPublishedSpendUSD
				outcome.BarePublishedEstimateUSD = &estimate
			}
		}
		workflow := StudyExperimentProgress{
			Name: experiment.Name, Identity: experiment.identity, AgentLayer: agentLayer, Skills: experiment.Skills != "",
			RequiredDispatchRoles: append([]string(nil), experiment.RequiredDispatchRoles...), Required: required, Missing: required,
		}
		if workflow.Skills && len(workflow.RequiredDispatchRoles) > 0 {
			config := defaultTreatmentDispatchConfig(experiment.model, experiment.effort)
			for _, role := range workflow.RequiredDispatchRoles {
				switch role {
				case requiredRolePlanReviewer:
					for _, target := range config.PlanReviewers {
						workflow.DispatchTargets = append(workflow.DispatchTargets, StudyDispatchTargetProgress{Role: role, Agent: target.Agent, Model: target.Model, ReasoningEffort: target.ReasoningEffort})
					}
				case requiredRoleImplementer:
					target := config.Implementer
					workflow.DispatchTargets = append(workflow.DispatchTargets, StudyDispatchTargetProgress{Role: role, Agent: target.Agent, Model: target.Model, ReasoningEffort: target.ReasoningEffort})
				case requiredRoleCodeReviewer:
					target := config.CodeReviewer
					workflow.DispatchTargets = append(workflow.DispatchTargets, StudyDispatchTargetProgress{Role: role, Agent: target.Agent, Model: target.Model, ReasoningEffort: target.ReasoningEffort})
				}
			}
		}
		outcome.Experiments = append(outcome.Experiments, workflow)
		outcome.Required += required
		outcome.Missing += required
	}
	return outcome
}

func prepareStudy(options StudyOptions) (preparedStudy, error) {
	if options.RepoRoot == "" || options.StudyPath == "" {
		return preparedStudy{}, fmt.Errorf("benchmark run requires one study.toml path")
	}
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	if options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return preparedStudy{}, fmt.Errorf("benchmark run task concurrency must be from 1 to 8")
	}
	studyPath, err := filepath.Abs(options.StudyPath)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("resolve benchmark study: %w", err)
	}
	data, err := readRegularNonempty(studyPath)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("read benchmark study: %w", err)
	}
	var document studyDocument
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return preparedStudy{}, fmt.Errorf("decode benchmark study: %w", err)
	}
	if strings.TrimSpace(document.Selection) == "" || len(document.Experiments) == 0 {
		return preparedStudy{}, fmt.Errorf("benchmark study requires selection and one or more experiments")
	}
	stageParent := filepath.Join(options.RepoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return preparedStudy{}, fmt.Errorf("create study input snapshot root: %w", err)
	}
	inputRoot, err := os.MkdirTemp(stageParent, "benchmark-study-input-")
	if err != nil {
		return preparedStudy{}, fmt.Errorf("create study input snapshot: %w", err)
	}
	prepared := preparedStudy{document: document, inputRoot: inputRoot}
	keepInputs := false
	defer func() {
		if !keepInputs {
			prepared.cleanupInputs()
		}
	}()
	if err := writeSnapshotFile(filepath.Join(inputRoot, "study.toml"), data); err != nil {
		return preparedStudy{}, fmt.Errorf("snapshot benchmark study: %w", err)
	}
	base := filepath.Dir(studyPath)
	selectionPath, err := resolveStudyPath(base, document.Selection)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("benchmark study selection: %w", err)
	}
	selectionSnapshot := filepath.Join(inputRoot, "selection.json")
	selectionData, err := snapshotStudySelection(selectionPath, selectionSnapshot)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("benchmark study selection: %w", err)
	}
	selection, selectionID, err := loadMatrixSelection(selectionSnapshot, selectionData)
	if err != nil {
		return preparedStudy{}, err
	}
	if err := validateMatrixTaskFilter(selection, options.Tasks); err != nil {
		return preparedStudy{}, err
	}
	prepared.selection, prepared.selectionID = selection, selectionID
	seenNames := map[string]bool{}
	seenIdentities := map[string]bool{}
	for index, experiment := range document.Experiments {
		if strings.TrimSpace(experiment.Name) == "" || seenNames[experiment.Name] {
			return preparedStudy{}, fmt.Errorf("benchmark study experiment names must be unique and non-empty")
		}
		seenNames[experiment.Name] = true
		model, effort, err := ParseModelSelection(experiment.Model + ":" + experiment.Reasoning)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		if experiment.Model != modelNameForPublished(model.PublishedIdentifier) || experiment.Reasoning != effort {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q has an invalid explicit model or reasoning", experiment.Name)
		}
		roles, err := canonicalizeRequiredDispatchRoles(experiment.RequiredDispatchRoles, experiment.Skills != "")
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		experiment.RequiredDispatchRoles = roles
		inputs, err := snapshotStudyExperimentInputs(inputRoot, index, base, experiment)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		hashes, err := validateExperimentInputs(inputs)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		identity, err := hashCanonical(struct {
			Schema    string            `json:"schema"`
			Model     string            `json:"model"`
			Reasoning string            `json:"reasoning"`
			Resources map[string]any    `json:"resources"`
			Inputs    map[string]string `json:"inputs"`
			Roles     []string          `json:"required_dispatch_roles,omitempty"`
		}{
			Schema: "deepswe-benchmark-experiment-v1", Model: model.PublishedIdentifier, Reasoning: effort,
			Resources: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}, Inputs: hashes,
			Roles: roles,
		})
		if err != nil {
			return preparedStudy{}, fmt.Errorf("identify benchmark experiment %q: %w", experiment.Name, err)
		}
		if seenIdentities[identity] {
			return preparedStudy{}, fmt.Errorf("benchmark study has duplicate content-addressed experiment identities")
		}
		seenIdentities[identity] = true
		prepared.experiments = append(prepared.experiments, preparedStudyExperiment{
			studyExperiment: experiment, model: model, effort: effort, identity: identity, inputHashes: hashes, inputs: inputs,
		})
	}
	keepInputs = true
	return prepared, nil
}

// identifyStudy defines the production v3 state-directory identity after the
// task environment and immutable arm identities are known.
func identifyStudy(selectionID string, membership []struct{ Name, Arm string }, checksums, environments map[string]string) (string, error) {
	return hashCanonical(struct {
		Schema, Selection       string
		Membership              []struct{ Name, Arm string }
		Checksums, Environments map[string]string
		Resources               map[string]any
	}{Schema: "deepswe-study-v3", Selection: selectionID, Membership: membership, Checksums: checksums, Environments: environments, Resources: studyResourceContract()})
}

func canonicalizeRequiredDispatchRoles(roles []string, hasSkills bool) ([]string, error) {
	// pelletier/go-toml/v2 decodes an explicit required_dispatch_roles = [] as a
	// non-nil empty slice, so nil means the key was omitted.
	if roles == nil {
		if hasSkills {
			return nil, fmt.Errorf("skills require required_dispatch_roles")
		}
		return nil, nil
	}
	if !hasSkills {
		return nil, fmt.Errorf("required_dispatch_roles is only valid with skills")
	}
	seen := make(map[string]bool, len(roles))
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			return nil, fmt.Errorf("required_dispatch_roles must not contain blank values")
		}
		if !validRequiredDispatchRole(role) {
			return nil, fmt.Errorf("unsupported required_dispatch_roles value %q", role)
		}
		if seen[role] {
			return nil, fmt.Errorf("required_dispatch_roles must not contain duplicate values")
		}
		seen[role] = true
		normalized = append(normalized, role)
	}
	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

func snapshotStudyExperimentInputs(snapshotRoot string, index int, base string, experiment studyExperiment) (studyExperimentInputs, error) {
	usesLayer := experiment.Config != "" || experiment.Instructions != "" || experiment.Skills != ""
	if usesLayer && experiment.Config == "" {
		return studyExperimentInputs{}, fmt.Errorf("agent layer inputs require config")
	}
	if experiment.Skills != "" && experiment.EntryPrompt == "" {
		return studyExperimentInputs{}, fmt.Errorf("skills require entry_prompt")
	}
	if experiment.Skills == "" && experiment.EntryPrompt != "" {
		return studyExperimentInputs{}, fmt.Errorf("entry_prompt is only valid with skills")
	}
	root := filepath.Join(snapshotRoot, "experiments", fmt.Sprintf("%03d", index))
	inputs := studyExperimentInputs{}
	for _, input := range []struct {
		name, value string
		directory   bool
		set         func(string)
	}{
		{"config", experiment.Config, false, func(path string) { inputs.Config = path }},
		{studyInputInstructions, experiment.Instructions, true, func(path string) { inputs.Instructions = path }},
		{studyInputSkills, experiment.Skills, true, func(path string) { inputs.Skills = path }},
		{studyInputEntryPrompt, experiment.EntryPrompt, false, func(path string) { inputs.EntryPrompt = path }},
	} {
		if input.value == "" {
			continue
		}
		source, err := resolveStudyPath(base, input.value)
		if err != nil {
			return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
		}
		destination := filepath.Join(root, input.name)
		if input.directory {
			info, statErr := os.Lstat(source)
			if statErr != nil {
				return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, statErr)
			}
			if !info.IsDir() {
				return studyExperimentInputs{}, fmt.Errorf("%s: must be a directory", input.name)
			}
			if err := copyRequiredTree(source, destination); err != nil {
				return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
			}
		} else if err := snapshotStudyFile(source, destination); err != nil {
			return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
		}
		input.set(destination)
	}
	return inputs, nil
}

func snapshotStudyFile(source, destination string) error {
	data, err := readRegularNonempty(source)
	if err != nil {
		return err
	}
	return writeSnapshotFile(destination, data)
}

func snapshotStudySelection(source, destination string) ([]byte, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStudySelectionBytes {
		return nil, fmt.Errorf("must be a non-empty JSON file no larger than %d bytes", maxStudySelectionBytes)
	}
	data, err := os.ReadFile(source) // #nosec G304 -- source is a validated declared study input.
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxStudySelectionBytes {
		return nil, fmt.Errorf("must be non-empty and no larger than %d bytes", maxStudySelectionBytes)
	}
	if err := writeSnapshotFile(destination, data); err != nil {
		return nil, err
	}
	return data, nil
}

func writeSnapshotFile(destination string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600) // #nosec G703 -- destination is beneath the private snapshot root.
}

func validateExperimentInputs(inputs studyExperimentInputs) (map[string]string, error) {
	hashes := map[string]string{}
	for _, input := range []struct {
		name, path string
		directory  bool
	}{
		{"config", inputs.Config, false}, {studyInputInstructions, inputs.Instructions, true},
		{studyInputSkills, inputs.Skills, true}, {studyInputEntryPrompt, inputs.EntryPrompt, false},
	} {
		if input.path == "" {
			continue
		}
		var digest string
		var contents []byte
		var err error
		if input.directory {
			digest, err = treeDigest(input.path)
		} else {
			contents, err = readRegularNonempty(input.path)
			if err == nil {
				digest = digestBytes(contents)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", input.name, err)
		}
		hashes[input.name] = digest
		if input.name == studyInputEntryPrompt {
			if count := bytes.Count(contents, []byte("{{task}}")); count != 1 {
				return nil, fmt.Errorf("entry_prompt must contain exactly one literal {{task}} placeholder")
			}
		}
	}
	return hashes, nil
}

func resolveStudyPath(base, value string) (string, error) {
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("paths must be relative to study.toml")
	}
	path := filepath.Clean(filepath.Join(base, value))
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes study directory")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve study directory: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolvedRelative, err := filepath.Rel(resolvedBase, resolvedPath)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes study directory through a symlink")
	}
	return path, nil
}

func readRegularNonempty(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, fmt.Errorf("must be a non-empty regular file")
	}
	return os.ReadFile(path) // #nosec G304 -- validated explicit study input.
}

func treeDigest(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("must be a directory")
	}
	hash := sha256.New()
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("contains non-regular file %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("must not be empty")
	}
	sort.Strings(paths)
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(root, relative)) // #nosec G304 -- relative was collected from WalkDir below the validated root.
		if err != nil {
			return "", err
		}
		hash.Write([]byte(filepath.ToSlash(relative)))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
