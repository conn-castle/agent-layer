package benchmark

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	matrixSelectionSchema        = "deepswe-benchmark-selection"
	matrixSelectionSchemaVersion = 1
	matrixManifestSchema         = "deepswe-matrix-arm-v1"
	matrixReportSchema           = "deepswe-matrix-report-v1"
)

//go:embed assets/matrix-report.html.tmpl
var matrixReportAssets embed.FS

// MatrixOptions configures a descriptive cross-model benchmark matrix.
type MatrixOptions struct {
	RepoRoot           string
	SelectionPath      string
	SelectionJSON      []byte
	BaselineExecutions []string
	TreatmentExecution string
	TreatmentLabel     string
	Tasks              []string
	TaskConcurrency    int
	Confirmed          bool
}

// MatrixOutcome reports progress and, when complete, report artifacts.
type MatrixOutcome struct {
	SelectionID string
	StateDir    string
	Completed   int
	Required    int
	Missing     int
	Arms        []MatrixArmProgress
	JSONPath    string
	HTMLPath    string
	Report      *MatrixReport
}

// MatrixArmProgress reports cached execution progress for one matrix arm.
type MatrixArmProgress struct {
	Label     string
	Execution string
	Mode      string
	Completed int
	Required  int
	Missing   int
}

type matrixSelection struct {
	Schema        string `json:"schema"`
	SchemaVersion int    `json:"schemaVersion"`
	Snapshot      struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"snapshot"`
	Selector struct {
		Model             string  `json:"model"`
		Reasoning         string  `json:"reasoning"`
		BudgetUSD         float64 `json:"budgetUsd"`
		IterationsPerTask int     `json:"iterationsPerTask"`
	} `json:"selector"`
	EstimatedPublishedSpendUSD float64               `json:"estimatedPublishedSpendUsd"`
	Tasks                      []matrixSelectionTask `json:"tasks"`
}

type matrixSelectionTask struct {
	ID          string  `json:"id"`
	Repetitions int     `json:"repetitions"`
	Weight      float64 `json:"weight"`
	Calibration struct {
		Intercept float64 `json:"intercept"`
		Slope     float64 `json:"slope"`
	} `json:"calibration"`
	PublishedMeanCostUSD float64 `json:"publishedMeanCostUsd"`
}

type matrixPreparation struct {
	selection    matrixSelection
	selectionID  string
	stateDir     string
	tasks        []benchmarkPlanTask
	checksums    map[string]string
	environments map[string]string
	arms         []matrixArm
	cleanup      func()
}

type matrixArm struct {
	ID       string
	Label    string
	Mode     string
	StateDir string
	Loaded   loadedBenchmarkPlan
	Bundle   *TreatmentBundle
}

type matrixArmManifest struct {
	SchemaVersion             string            `json:"schema_version"`
	SelectionID               string            `json:"selection_id"`
	CreatedAt                 time.Time         `json:"created_at"`
	Label                     string            `json:"label"`
	Mode                      string            `json:"mode"`
	Model                     string            `json:"model"`
	Reasoning                 string            `json:"reasoning"`
	ProviderClient            string            `json:"provider_client_version"`
	TaskChecksums             map[string]string `json:"task_checksums"`
	TaskEnvironmentIdentities map[string]string `json:"task_environment_identities"`
	Repetitions               map[string]int    `json:"repetitions"`
	TreatmentHash             string            `json:"treatment_manifest_hash,omitempty"`
}

type matrixJob struct {
	arm  *matrixArm
	cell planCell
}

// MatrixReport is the canonical descriptive report shared by JSON and HTML.
type MatrixReport struct {
	SchemaVersion    string               `json:"schema_version"`
	SelectionID      string               `json:"selection_id"`
	GeneratedAt      time.Time            `json:"generated_at"`
	Selector         MatrixSelectorReport `json:"selector"`
	TaskCount        int                  `json:"task_count"`
	RunsPerArm       int                  `json:"runs_per_arm"`
	Arms             []MatrixArmReport    `json:"arms"`
	Limitations      []string             `json:"limitations"`
	PlotMaximumPrice float64              `json:"-"`
	PlotMinimumScore float64              `json:"-"`
	PlotMaximumScore float64              `json:"-"`
}

// MatrixSelectorReport records the browser selection that fixed the allocation.
type MatrixSelectorReport struct {
	Model                      string  `json:"model"`
	Reasoning                  string  `json:"reasoning"`
	BudgetUSD                  float64 `json:"budget_usd"`
	EstimatedPublishedSpendUSD float64 `json:"estimated_published_spend_usd"`
}

// MatrixArmReport contains one descriptive matrix point.
type MatrixArmReport struct {
	Label                   string                `json:"label"`
	Mode                    string                `json:"mode"`
	Model                   string                `json:"model"`
	Reasoning               string                `json:"reasoning"`
	ProviderClient          string                `json:"provider_client_version"`
	Score                   float64               `json:"score"`
	Cost                    ObservedCostRange     `json:"cost"`
	InvocationCount         int                   `json:"invocation_count"`
	DispatchConformantRuns  int                   `json:"dispatch_conformant_runs"`
	VerifierBuildFailedRuns int                   `json:"verifier_build_failed_runs"`
	Tasks                   []MatrixTaskArmReport `json:"tasks"`
	PlotX                   float64               `json:"-"`
	PlotY                   float64               `json:"-"`
	Color                   string                `json:"-"`
}

// MatrixTaskArmReport preserves the observed task score and calibrated contribution.
type MatrixTaskArmReport struct {
	Task                 string            `json:"task"`
	F2PScore             float64           `json:"f2p_score"`
	CalibratedScore      float64           `json:"calibrated_score"`
	Weight               float64           `json:"weight"`
	WeightedContribution float64           `json:"weighted_contribution"`
	Cost                 ObservedCostRange `json:"cost"`
	VerifierBuildFailed  bool              `json:"verifier_build_failed"`
}

// CheckMatrix validates a matrix without making paid model calls.
func CheckMatrix(ctx context.Context, options MatrixOptions) (MatrixOutcome, error) {
	preparation, err := prepareMatrix(ctx, options)
	if err != nil {
		return MatrixOutcome{}, err
	}
	defer preparation.cleanup()
	return matrixProgress(preparation), nil
}

// RunMatrix executes missing matrix cells and renders the report when complete.
func RunMatrix(ctx context.Context, options MatrixOptions, executor TaskExecutor) (MatrixOutcome, error) {
	preparation, err := prepareMatrix(ctx, options)
	if err != nil {
		return MatrixOutcome{}, err
	}
	defer preparation.cleanup()
	outcome := matrixProgress(preparation)
	if outcome.Missing == 0 {
		return buildMatrixReport(preparation)
	}
	if !options.Confirmed {
		return outcome, ErrConfirmationRequired
	}
	for index := range preparation.arms {
		if err := ensureMatrixManifest(preparation.selectionID, preparation.tasks, preparation.checksums, preparation.environments, &preparation.arms[index]); err != nil {
			return outcome, err
		}
	}
	if executor == nil {
		executor = PierExecutor{}
	}
	if err := executeMatrix(
		ctx, options.RepoRoot, preparation.checksums,
		preparation.arms, options.Tasks, options.TaskConcurrency, executor,
	); err != nil {
		return matrixProgress(preparation), err
	}
	outcome = matrixProgress(preparation)
	if outcome.Missing > 0 {
		if len(options.Tasks) > 0 {
			return outcome, nil
		}
		return outcome, fmt.Errorf("matrix execution completed with %d missing runs", outcome.Missing)
	}
	return buildMatrixReport(preparation)
}

func prepareMatrix(ctx context.Context, options MatrixOptions) (matrixPreparation, error) {
	if options.RepoRoot == "" || options.SelectionPath == "" ||
		len(options.BaselineExecutions) == 0 {
		return matrixPreparation{}, fmt.Errorf("benchmark matrix requires a repository root, --selection, and baseline executions")
	}
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	if options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return matrixPreparation{}, fmt.Errorf("benchmark matrix task concurrency must be from 1 to 8")
	}
	selection, selectionID, err := loadMatrixSelection(options.SelectionPath, options.SelectionJSON)
	if err != nil {
		return matrixPreparation{}, err
	}
	if err := validateMatrixTaskFilter(selection, options.Tasks); err != nil {
		return matrixPreparation{}, err
	}
	tasks := make([]benchmarkPlanTask, len(selection.Tasks))
	for index, task := range selection.Tasks {
		tasks[index] = benchmarkPlanTask{ID: task.ID, RepetitionsPerArm: task.Repetitions}
	}
	stateDir := filepath.Join(
		options.RepoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID,
	)
	type armInput struct {
		execution string
		mode      string
		label     string
	}
	inputs := make([]armInput, 0, len(options.BaselineExecutions)+1)
	seen := make(map[string]bool)
	for _, execution := range options.BaselineExecutions {
		key := ArmBaseline + "\x00" + execution
		if seen[key] {
			return matrixPreparation{}, fmt.Errorf("duplicate baseline execution %q", execution)
		}
		seen[key] = true
		inputs = append(inputs, armInput{
			execution: execution, mode: ArmBaseline,
			label: "Bare " + strings.ReplaceAll(execution, ":", " "),
		})
	}
	if options.TreatmentExecution != "" {
		treatmentLabel := strings.TrimSpace(options.TreatmentLabel)
		if treatmentLabel == "" {
			treatmentLabel = "Agent Layer " + strings.ReplaceAll(options.TreatmentExecution, ":", " ")
		}
		inputs = append(inputs, armInput{
			execution: options.TreatmentExecution, mode: ArmTreatment, label: treatmentLabel,
		})
	}
	selections := make([]parsedSelection, 0, len(inputs))
	for _, input := range inputs {
		model, effort, parseErr := ParseModelSelection(input.execution)
		if parseErr != nil {
			return matrixPreparation{}, parseErr
		}
		selections = append(selections, parsedSelection{model: model, effort: effort})
	}
	if err := preflightBenchmark(selections); err != nil {
		return matrixPreparation{}, err
	}
	if err := verifyBenchmarkPier(ctx); err != nil {
		return matrixPreparation{}, err
	}
	if err := validateBenchmarkAuthentication(options.RepoRoot, selections); err != nil {
		return matrixPreparation{}, err
	}
	checksums, environments, err := prepareBenchmarkTaskSet(ctx, options.RepoRoot, tasks)
	if err != nil {
		return matrixPreparation{}, err
	}
	var bundle *TreatmentBundle
	cleanup := func() {}
	if options.TreatmentExecution != "" {
		treatmentModel, treatmentEffort, _ := ParseModelSelection(options.TreatmentExecution)
		bundle, err = buildCampaignTreatmentBundle(
			options.RepoRoot, runtime.GOARCH, TreatmentInstructionsAndSkills,
			treatmentModel, treatmentEffort,
		)
		if err != nil {
			return matrixPreparation{}, fmt.Errorf("build Agent Layer matrix treatment: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(bundle.Root) }
	}
	preparation := matrixPreparation{
		selection: selection, selectionID: selectionID, stateDir: stateDir,
		tasks: tasks, checksums: checksums, environments: environments, cleanup: cleanup,
	}
	for _, input := range inputs {
		model, effort, _ := ParseModelSelection(input.execution)
		identity := struct {
			Schema                    string            `json:"schema"`
			SelectionID               string            `json:"selection_id"`
			Mode                      string            `json:"mode"`
			Model                     string            `json:"model"`
			Reasoning                 string            `json:"reasoning"`
			TreatmentHash             string            `json:"treatment_hash,omitempty"`
			TaskEnvironmentIdentities map[string]string `json:"task_environment_identities"`
		}{
			Schema: "deepswe-matrix-arm-identity-v1", SelectionID: selectionID,
			Mode: input.mode, Model: model.PublishedIdentifier, Reasoning: effort,
			TaskEnvironmentIdentities: copyStringMap(environments),
		}
		var armBundle *TreatmentBundle
		if input.mode == ArmTreatment {
			identity.TreatmentHash = bundle.ManifestHash
			armBundle = bundle
		}
		armID, hashErr := hashCanonical(identity)
		if hashErr != nil {
			cleanup()
			return matrixPreparation{}, fmt.Errorf("identify matrix arm: %w", hashErr)
		}
		runCount := 0
		for _, task := range tasks {
			runCount += task.RepetitionsPerArm
		}
		loaded := loadedBenchmarkPlan{
			ID: selectionID, CampaignID: selectionID, Model: model, Effort: effort,
			RunCount: runCount,
		}
		loaded.Plan.Tasks = append([]benchmarkPlanTask(nil), tasks...)
		arm := matrixArm{
			ID: armID, Label: input.label, Mode: input.mode,
			StateDir: filepath.Join(stateDir, "arms", armID),
			Loaded:   loaded, Bundle: armBundle,
		}
		if err := validateMatrixManifest(selectionID, tasks, checksums, environments, &arm); err != nil {
			cleanup()
			return matrixPreparation{}, err
		}
		preparation.arms = append(preparation.arms, arm)
	}
	if bundle != nil {
		treatmentArm := preparation.arms[len(preparation.arms)-1]
		eventID, eventErr := NewEventID()
		if eventErr != nil {
			cleanup()
			return matrixPreparation{}, eventErr
		}
		if err := preflightTreatmentRuntime(ctx, ExecutionRequest{
			RepoRoot: options.RepoRoot, EvidenceDir: treatmentArm.StateDir,
			EventID: eventID, Attempt: 1, Task: tasks[0].ID,
			Model: treatmentArm.Loaded.Model, Effort: treatmentArm.Loaded.Effort,
			Arm: ArmTreatment, Bundle: bundle, TaskChecksum: checksums[tasks[0].ID],
		}); err != nil {
			cleanup()
			return matrixPreparation{}, fmt.Errorf("preflight Agent Layer matrix treatment runtime: %w", err)
		}
	}
	return preparation, nil
}

func loadMatrixSelection(path string, data []byte) (matrixSelection, string, error) {
	if len(data) == 0 {
		info, err := os.Stat(path)
		if err != nil {
			return matrixSelection{}, "", fmt.Errorf("inspect benchmark selection: %w", err)
		}
		if info.IsDir() || info.Size() <= 0 || info.Size() > maxBenchmarkPlanBytes {
			return matrixSelection{}, "", fmt.Errorf("benchmark selection must be a non-empty JSON file no larger than %d bytes", maxBenchmarkPlanBytes)
		}
		data, err = os.ReadFile(path) // #nosec G304 -- the user explicitly selects the artifact.
		if err != nil {
			return matrixSelection{}, "", fmt.Errorf("read benchmark selection: %w", err)
		}
	}
	if len(data) == 0 || len(data) > maxBenchmarkPlanBytes {
		return matrixSelection{}, "", fmt.Errorf("benchmark selection must be non-empty and no larger than %d bytes", maxBenchmarkPlanBytes)
	}
	var selection matrixSelection
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		return matrixSelection{}, "", fmt.Errorf("decode benchmark selection: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return matrixSelection{}, "", fmt.Errorf("benchmark selection contains trailing JSON")
	}
	if err := validateMatrixSelection(selection); err != nil {
		return matrixSelection{}, "", err
	}
	selectionID, err := hashCanonical(selection)
	if err != nil {
		return matrixSelection{}, "", fmt.Errorf("identify benchmark selection: %w", err)
	}
	return selection, selectionID, nil
}

func validateMatrixSelection(selection matrixSelection) error {
	if selection.Schema != matrixSelectionSchema ||
		selection.SchemaVersion != matrixSelectionSchemaVersion ||
		selection.Snapshot.URL != DeepSWETrialsSourceURL ||
		len(selection.Snapshot.SHA256) != 64 ||
		selection.Selector.BudgetUSD <= 0 ||
		selection.Selector.IterationsPerTask < 1 ||
		selection.Selector.IterationsPerTask > 4 ||
		selection.EstimatedPublishedSpendUSD <= 0 ||
		selection.EstimatedPublishedSpendUSD-selection.Selector.BudgetUSD > 1e-9 ||
		len(selection.Tasks) == 0 {
		return fmt.Errorf("unsupported or invalid DeepSWE benchmark selection")
	}
	selectorModel, selectorEffort, err := ParseModelSelection(
		modelNameForPublished(selection.Selector.Model) + ":" + selection.Selector.Reasoning,
	)
	if err != nil || selectorModel.PublishedIdentifier != selection.Selector.Model ||
		selectorEffort != selection.Selector.Reasoning {
		return fmt.Errorf("benchmark selection has an invalid selector configuration")
	}
	seen := make(map[string]bool, len(selection.Tasks))
	var totalWeight, totalCost float64
	for _, task := range selection.Tasks {
		if !validTaskName(task.ID) || seen[task.ID] ||
			task.Repetitions != selection.Selector.IterationsPerTask ||
			task.Weight <= 0 || !finite(task.Weight) ||
			!finite(task.Calibration.Intercept) ||
			!finite(task.Calibration.Slope) ||
			task.PublishedMeanCostUSD <= 0 ||
			!finite(task.PublishedMeanCostUSD) {
			return fmt.Errorf("benchmark selection contains an invalid task allocation")
		}
		seen[task.ID] = true
		totalWeight += task.Weight
		totalCost += task.PublishedMeanCostUSD * float64(task.Repetitions)
	}
	if math.Abs(totalWeight-1) > 1e-9 ||
		math.Abs(totalCost-selection.EstimatedPublishedSpendUSD) > 1e-9 {
		return fmt.Errorf("benchmark selection weights or costs do not reconcile")
	}
	return nil
}

func validateMatrixTaskFilter(selection matrixSelection, tasks []string) error {
	selected := make(map[string]bool, len(selection.Tasks))
	for _, task := range selection.Tasks {
		selected[task.ID] = true
	}
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if !selected[task] {
			return fmt.Errorf("benchmark matrix task %q is not in the selection", task)
		}
		if seen[task] {
			return fmt.Errorf("duplicate benchmark matrix task filter %q", task)
		}
		seen[task] = true
	}
	return nil
}

func matrixProgress(preparation matrixPreparation) MatrixOutcome {
	outcome := MatrixOutcome{
		SelectionID: preparation.selectionID,
		StateDir:    preparation.stateDir,
	}
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		execution := armExecution{
			stateDir: arm.StateDir, arm: arm.Mode, loaded: arm.Loaded,
			checksums: preparation.checksums,
		}
		missing := len(missingPlanCells(execution))
		progress := MatrixArmProgress{
			Label: arm.Label, Execution: executionLabel(arm.Loaded.Model, arm.Loaded.Effort),
			Mode: arm.Mode, Required: arm.Loaded.RunCount,
			Completed: arm.Loaded.RunCount - missing, Missing: missing,
		}
		outcome.Arms = append(outcome.Arms, progress)
		outcome.Required += progress.Required
		outcome.Completed += progress.Completed
		outcome.Missing += progress.Missing
	}
	return outcome
}

func expectedMatrixManifest(
	selectionID string,
	tasks []benchmarkPlanTask,
	checksums map[string]string,
	environments map[string]string,
	arm *matrixArm,
) matrixArmManifest {
	treatmentHash := ""
	if arm.Bundle != nil {
		treatmentHash = arm.Bundle.ManifestHash
	}
	return matrixArmManifest{
		SchemaVersion: matrixManifestSchema, SelectionID: selectionID,
		Label: arm.Label, Mode: arm.Mode,
		Model: arm.Loaded.Model.PublishedIdentifier, Reasoning: arm.Loaded.Effort,
		ProviderClient: arm.Loaded.Model.ProviderClientVersion,
		TaskChecksums:  copyStringMap(checksums), Repetitions: repetitionsForTasks(tasks),
		TaskEnvironmentIdentities: copyStringMap(environments),
		TreatmentHash:             treatmentHash,
	}
}

func repetitionsForTasks(tasks []benchmarkPlanTask) map[string]int {
	repetitions := make(map[string]int, len(tasks))
	for _, task := range tasks {
		repetitions[task.ID] = task.RepetitionsPerArm
	}
	return repetitions
}

func validateMatrixManifest(
	selectionID string,
	tasks []benchmarkPlanTask,
	checksums map[string]string,
	environments map[string]string,
	arm *matrixArm,
) error {
	path := filepath.Join(arm.StateDir, "manifest.json")
	var existing matrixArmManifest
	if err := readCampaignJSON(path, &existing); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read matrix arm manifest: %w", err)
	}
	expected := expectedMatrixManifest(selectionID, tasks, checksums, environments, arm)
	expected.CreatedAt = existing.CreatedAt
	expectedJSON, _ := json.Marshal(expected)
	existingJSON, _ := json.Marshal(existing)
	if string(expectedJSON) != string(existingJSON) {
		return fmt.Errorf("matrix arm %q conflicts with its immutable manifest", arm.Label)
	}
	return nil
}

func ensureMatrixManifest(
	selectionID string,
	tasks []benchmarkPlanTask,
	checksums map[string]string,
	environments map[string]string,
	arm *matrixArm,
) error {
	path := filepath.Join(arm.StateDir, "manifest.json")
	if _, err := os.Stat(path); err == nil {
		return validateMatrixManifest(selectionID, tasks, checksums, environments, arm)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect matrix arm manifest: %w", err)
	}
	manifest := expectedMatrixManifest(selectionID, tasks, checksums, environments, arm)
	manifest.CreatedAt = time.Now().UTC()
	return writeJSON(path, manifest)
}

func executeMatrix(
	ctx context.Context,
	repoRoot string,
	checksums map[string]string,
	arms []matrixArm,
	tasks []string,
	concurrency int,
	executor TaskExecutor,
) error {
	selected := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		selected[task] = true
	}
	var jobs []matrixJob
	maxTasks := len(arms[0].Loaded.Plan.Tasks)
	for taskIndex := 0; taskIndex < maxTasks; taskIndex++ {
		for armIndex := range arms {
			arm := &arms[armIndex]
			task := arm.Loaded.Plan.Tasks[taskIndex]
			if len(selected) > 0 && !selected[task.ID] {
				continue
			}
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				if !validPlanResult(
					armResultPath(arm.StateDir, task.ID, attempt),
					task.ID, attempt, checksums[task.ID],
					arm.Loaded.Model, arm.Loaded.Effort,
					arm.Mode == ArmTreatment,
				) {
					jobs = append(jobs, matrixJob{
						arm: arm, cell: planCell{task: task.ID, attempt: attempt},
					})
				}
			}
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobChannel := make(chan matrixJob)
	var failures []error
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobChannel {
				if runCtx.Err() != nil {
					return
				}
				var err error
				for {
					eventID, eventErr := NewEventID()
					err = eventErr
					if err == nil {
						var result AttemptResult
						result, err = executor.Execute(runCtx, ExecutionRequest{
							RepoRoot:    repoRoot,
							EvidenceDir: job.arm.StateDir, EventID: eventID,
							Attempt: job.cell.attempt, Task: job.cell.task,
							Model: job.arm.Loaded.Model, Effort: job.arm.Loaded.Effort,
							Arm: job.arm.Mode, Bundle: job.arm.Bundle,
							TaskChecksum: checksums[job.cell.task],
						})
						if err == nil {
							if validationErr := result.Validate(); validationErr != nil {
								err = fmt.Errorf("returned invalid evidence: %w", validationErr)
							} else if result.Status != statusSuccess {
								err = fmt.Errorf("execution failed: %s", result.Error)
							}
						}
						if err == nil {
							err = writeJSON(
								armResultPath(job.arm.StateDir, job.cell.task, job.cell.attempt),
								result,
							)
						}
					}
					if !errors.Is(err, errProviderCapacity) {
						break
					}
					timer := time.NewTimer(providerCapacityRetryDelay)
					select {
					case <-timer.C:
					case <-runCtx.Done():
						timer.Stop()
						return
					}
				}
				if err != nil {
					mutex.Lock()
					failures = append(failures, fmt.Errorf(
						"%s: %s repetition %d: %w",
						job.arm.Label, job.cell.task, job.cell.attempt, err,
					))
					mutex.Unlock()
					cancel()
					return
				}
			}
		}()
	}
sendJobs:
	for _, job := range jobs {
		select {
		case jobChannel <- job:
		case <-runCtx.Done():
			break sendJobs
		}
	}
	close(jobChannel)
	workers.Wait()
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return context.Cause(ctx)
}

func buildMatrixReport(preparation matrixPreparation) (MatrixOutcome, error) {
	report := MatrixReport{
		SchemaVersion: matrixReportSchema,
		SelectionID:   preparation.selectionID,
		GeneratedAt:   time.Now().UTC(),
		Selector: MatrixSelectorReport{
			Model:                      preparation.selection.Selector.Model,
			Reasoning:                  preparation.selection.Selector.Reasoning,
			BudgetUSD:                  preparation.selection.Selector.BudgetUSD,
			EstimatedPublishedSpendUSD: preparation.selection.EstimatedPublishedSpendUSD,
		},
		TaskCount: len(preparation.selection.Tasks),
		Limitations: []string{
			"Each task was executed once per arm, so this descriptive comparison has no local run-to-run confidence interval or significance verdict.",
			"Published mini-swe-agent calibrations and fixed precision weights transform local F2P observations onto the full DeepSWE score scale; transfer to the local Codex harness is an assumption.",
			"Results apply only to this exact task allocation, provider-client version, and immutable Agent Layer bundle.",
		},
	}
	for _, task := range preparation.selection.Tasks {
		report.RunsPerArm += task.Repetitions
	}
	colors := []string{"#176b46", "#2878b8", "#7045a1", "#b35f0b"}
	for armIndex := range preparation.arms {
		arm := &preparation.arms[armIndex]
		execution := armExecution{
			stateDir: arm.StateDir, arm: arm.Mode, loaded: arm.Loaded,
			checksums: preparation.checksums,
		}
		if missing := missingPlanCells(execution); len(missing) > 0 {
			return MatrixOutcome{}, fmt.Errorf(
				"matrix arm %q has %d missing runs", arm.Label, len(missing),
			)
		}
		results, err := readArmResults(
			arm.StateDir, arm.Loaded.Plan.Tasks, preparation.checksums,
		)
		if err != nil {
			return MatrixOutcome{}, err
		}
		armReport := MatrixArmReport{
			Label: arm.Label, Mode: arm.Mode,
			Model:     arm.Loaded.Model.PublishedIdentifier,
			Reasoning: arm.Loaded.Effort,
			Color:     colors[armIndex%len(colors)],
		}
		for _, plannedTask := range preparation.selection.Tasks {
			observed := results[plannedTask.ID]
			if len(observed) != plannedTask.Repetitions {
				return MatrixOutcome{}, fmt.Errorf(
					"matrix task %s has %d results; expected %d",
					plannedTask.ID, len(observed), plannedTask.Repetitions,
				)
			}
			taskReport := MatrixTaskArmReport{
				Task: plannedTask.ID, Weight: plannedTask.Weight,
			}
			for _, result := range observed {
				if result.RuntimeModel != arm.Loaded.Model.RuntimeIdentifier ||
					result.ReasoningEffort != arm.Loaded.Effort {
					return MatrixOutcome{}, fmt.Errorf(
						"matrix arm %q contains mismatched execution evidence",
						arm.Label,
					)
				}
				if armReport.ProviderClient == "" {
					armReport.ProviderClient = result.ProviderClientVersion
				} else if armReport.ProviderClient != result.ProviderClientVersion {
					return MatrixOutcome{}, fmt.Errorf(
						"matrix arm %q mixes provider client versions", arm.Label,
					)
				}
				taskReport.F2PScore += result.F2PScore / float64(len(observed))
				minimum, maximum, costErr := result.CostBounds()
				if costErr != nil {
					return MatrixOutcome{}, fmt.Errorf(
						"matrix task %s has invalid cost: %w",
						plannedTask.ID, costErr,
					)
				}
				taskReport.Cost.Midpoint += *result.CostUSD
				taskReport.Cost.Minimum += minimum
				taskReport.Cost.Maximum += maximum
				taskReport.VerifierBuildFailed =
					taskReport.VerifierBuildFailed || result.VerifierBuildFailed
				armReport.InvocationCount += result.InvocationCount
				if result.DispatchConformant {
					armReport.DispatchConformantRuns++
				}
				if result.VerifierBuildFailed {
					armReport.VerifierBuildFailedRuns++
				}
			}
			taskReport.CalibratedScore =
				plannedTask.Calibration.Intercept +
					plannedTask.Calibration.Slope*taskReport.F2PScore
			taskReport.WeightedContribution =
				plannedTask.Weight * taskReport.CalibratedScore
			armReport.Score += taskReport.WeightedContribution
			armReport.Cost = addObservedCost(armReport.Cost, taskReport.Cost)
			armReport.Tasks = append(armReport.Tasks, taskReport)
		}
		report.Arms = append(report.Arms, armReport)
	}
	prepareMatrixPlot(&report)
	reportDir := filepath.Join(preparation.stateDir, "report")
	jsonPath := filepath.Join(reportDir, "report.json")
	htmlPath := filepath.Join(reportDir, "report.html")
	if err := writeJSON(jsonPath, report); err != nil {
		return MatrixOutcome{}, err
	}
	html, err := renderMatrixReportHTML(report)
	if err != nil {
		return MatrixOutcome{}, err
	}
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		return MatrixOutcome{}, fmt.Errorf("create matrix report directory: %w", err)
	}
	if err := fsutil.WriteFileAtomic(htmlPath, html, 0o600); err != nil {
		return MatrixOutcome{}, fmt.Errorf("write matrix HTML report: %w", err)
	}
	outcome := matrixProgress(preparation)
	outcome.JSONPath = jsonPath
	outcome.HTMLPath = htmlPath
	outcome.Report = &report
	return outcome, nil
}

func prepareMatrixPlot(report *MatrixReport) {
	report.PlotMaximumPrice = 0.01
	report.PlotMinimumScore = 0
	report.PlotMaximumScore = 1
	for _, arm := range report.Arms {
		report.PlotMaximumPrice = math.Max(report.PlotMaximumPrice, arm.Cost.Midpoint)
		report.PlotMinimumScore = math.Min(report.PlotMinimumScore, arm.Score)
		report.PlotMaximumScore = math.Max(report.PlotMaximumScore, arm.Score)
	}
	report.PlotMaximumPrice *= 1.1
	scoreRange := report.PlotMaximumScore - report.PlotMinimumScore
	if report.PlotMinimumScore < 0 {
		report.PlotMinimumScore -= scoreRange * 0.05
	}
	if report.PlotMaximumScore > 1 {
		report.PlotMaximumScore += scoreRange * 0.05
	}
	const (
		left   = 70.0
		right  = 730.0
		top    = 20.0
		bottom = 330.0
	)
	for index := range report.Arms {
		arm := &report.Arms[index]
		arm.PlotX = left + arm.Cost.Midpoint/report.PlotMaximumPrice*(right-left)
		arm.PlotY = bottom -
			(arm.Score-report.PlotMinimumScore)/
				(report.PlotMaximumScore-report.PlotMinimumScore)*(bottom-top)
	}
}

func renderMatrixReportHTML(report MatrixReport) ([]byte, error) {
	templateData, err := matrixReportAssets.ReadFile("assets/matrix-report.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("read matrix report template: %w", err)
	}
	tmpl, err := template.New("matrix-report").Funcs(template.FuncMap{
		"percent": func(value float64) string {
			return fmt.Sprintf("%.1f%%", value*100)
		},
		"money": func(value float64) string {
			return fmt.Sprintf("$%.2f", value)
		},
		"costRange": func(cost ObservedCostRange) string {
			if math.Abs(cost.Maximum-cost.Minimum) < 0.0000005 {
				return fmt.Sprintf("$%.2f", cost.Midpoint)
			}
			return fmt.Sprintf(
				"$%.2f ($%.2f–$%.2f)",
				cost.Midpoint, cost.Minimum, cost.Maximum,
			)
		},
	}).Parse(string(templateData))
	if err != nil {
		return nil, fmt.Errorf("parse matrix report template: %w", err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, report); err != nil {
		return nil, fmt.Errorf("render matrix report: %w", err)
	}
	return output.Bytes(), nil
}
