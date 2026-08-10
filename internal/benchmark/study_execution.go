package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	matrixSelectionSchema        = "deepswe-benchmark-selection"
	matrixSelectionSchemaVersion = 2
	// matrixManifestSchema is retained only to read immutable selection-arm
	// evidence produced by the predecessor runner.
	matrixManifestSchema   = "deepswe-matrix-arm-v2"
	studyArmManifestSchema = "deepswe-study-arm-v1"
)

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
	ManualExclusions           []string              `json:"manualExclusions,omitempty"`
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
	selection       matrixSelection
	taskConcurrency int
	selectionID     string
	stateDir        string
	tasks           []benchmarkPlanTask
	checksums       map[string]string
	environments    map[string]string
	arms            []matrixArm
}

type matrixArm struct {
	ID                             string
	Label                          string
	Mode                           string
	StateDir                       string
	Loaded                         loadedBenchmarkPlan
	Bundle                         *TreatmentBundle
	AgentTimeoutMultiplier         float64
	IgnoreProviderClientInManifest bool
}

type matrixArmManifest struct {
	SchemaVersion          string            `json:"schema_version"`
	SelectionID            string            `json:"selection_id"`
	CreatedAt              time.Time         `json:"created_at"`
	Label                  string            `json:"label"`
	Mode                   string            `json:"mode"`
	Model                  string            `json:"model"`
	Reasoning              string            `json:"reasoning"`
	ProviderClient         string            `json:"provider_client_version"`
	TaskChecksums          map[string]string `json:"task_checksums"`
	Repetitions            map[string]int    `json:"repetitions"`
	AgentTimeoutMultiplier float64           `json:"agent_timeout_multiplier,omitempty"`
	TreatmentHash          string            `json:"treatment_manifest_hash,omitempty"`
}

// studyArmManifest is the immutable manifest written by the public study
// runner. matrixArmManifest above is intentionally legacy-reader-only.
type studyArmManifest struct {
	SchemaVersion          string            `json:"schema_version"`
	SelectionID            string            `json:"selection_id"`
	CreatedAt              time.Time         `json:"created_at"`
	Label                  string            `json:"label"`
	Mode                   string            `json:"mode"`
	Model                  string            `json:"model"`
	Reasoning              string            `json:"reasoning"`
	ProviderClient         string            `json:"provider_client_version"`
	TaskChecksums          map[string]string `json:"task_checksums"`
	Repetitions            map[string]int    `json:"repetitions"`
	AgentTimeoutMultiplier float64           `json:"agent_timeout_multiplier"`
	TreatmentHash          string            `json:"treatment_manifest_hash,omitempty"`
}

type matrixJob struct {
	arm  *matrixArm
	cell planCell
}

const studyExecutionLockFile = ".execution.lock"

type studyExecutionLock struct {
	file *os.File
}

func acquireStudyExecutionLock(arms []matrixArm) (*studyExecutionLock, error) {
	root := filepath.Dir(arms[0].StateDir)
	for _, arm := range arms[1:] {
		if filepath.Dir(arm.StateDir) != root {
			return nil, fmt.Errorf("study experiments do not share one execution root")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create study execution root: %w", err)
	}
	path := filepath.Join(root, studyExecutionLockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path is under the content-addressed private study state root.
	if err != nil {
		return nil, fmt.Errorf("open study execution lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // supported Unix file descriptors are small non-negative ints.
		closeErr := file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			if closeErr != nil {
				return nil, fmt.Errorf("study execution is already in progress; close lock: %w", closeErr)
			}
			return nil, fmt.Errorf("study execution is already in progress")
		}
		return nil, fmt.Errorf("lock study execution: %w", errors.Join(err, closeErr))
	}
	return &studyExecutionLock{file: file}, nil
}

func (lock *studyExecutionLock) release() error {
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN) //nolint:gosec // supported Unix file descriptors are small non-negative ints.
	closeErr := lock.file.Close()
	if err := errors.Join(unlockErr, closeErr); err != nil {
		return fmt.Errorf("release study execution lock: %w", err)
	}
	return nil
}

type planCell struct {
	task    string
	attempt int
}

func loadMatrixSelection(path string, data []byte) (matrixSelection, string, error) {
	if len(data) == 0 {
		info, err := os.Lstat(path)
		if err != nil {
			return matrixSelection{}, "", fmt.Errorf("inspect benchmark selection: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStudySelectionBytes {
			return matrixSelection{}, "", fmt.Errorf("benchmark selection must be a non-empty JSON file no larger than %d bytes", maxStudySelectionBytes)
		}
		data, err = os.ReadFile(path) // #nosec G304 -- explicit study input.
		if err != nil {
			return matrixSelection{}, "", fmt.Errorf("read benchmark selection: %w", err)
		}
	}
	if len(data) == 0 || len(data) > maxStudySelectionBytes {
		return matrixSelection{}, "", fmt.Errorf("benchmark selection must be non-empty and no larger than %d bytes", maxStudySelectionBytes)
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
		(selection.SchemaVersion != 1 && selection.SchemaVersion != matrixSelectionSchemaVersion) ||
		selection.Snapshot.URL != DeepSWETrialsSourceURL || len(selection.Snapshot.SHA256) != 64 ||
		selection.Selector.BudgetUSD <= 0 || selection.Selector.IterationsPerTask < 1 ||
		selection.Selector.IterationsPerTask > 4 || selection.EstimatedPublishedSpendUSD <= 0 ||
		selection.EstimatedPublishedSpendUSD-selection.Selector.BudgetUSD > 1e-9 || len(selection.Tasks) == 0 {
		return fmt.Errorf("unsupported or invalid DeepSWE benchmark selection")
	}
	// Schema v1 is accepted solely for immutable historical selections. The
	// manual-exclusion extension was introduced with v2, so treating it as v1
	// would create a new, non-historical selection under the old contract.
	if selection.SchemaVersion == 1 && len(selection.ManualExclusions) > 0 {
		return fmt.Errorf("benchmark selection schema v1 does not support manual exclusions")
	}
	model, effort, err := ParseModelSelection(modelNameForPublished(selection.Selector.Model) + ":" + selection.Selector.Reasoning)
	if err != nil || model.PublishedIdentifier != selection.Selector.Model || effort != selection.Selector.Reasoning {
		return fmt.Errorf("benchmark selection has an invalid selector configuration")
	}
	seen, excluded := map[string]bool{}, map[string]bool{}
	for _, task := range selection.ManualExclusions {
		if !validTaskName(task) || excluded[task] {
			return fmt.Errorf("benchmark selection contains invalid manual exclusions")
		}
		excluded[task] = true
	}
	var weight, cost float64
	for _, task := range selection.Tasks {
		if !validTaskName(task.ID) || seen[task.ID] || excluded[task.ID] || task.Repetitions != selection.Selector.IterationsPerTask || task.Weight <= 0 || !finite(task.Weight) || !finite(task.Calibration.Intercept) || !finite(task.Calibration.Slope) || task.PublishedMeanCostUSD <= 0 || !finite(task.PublishedMeanCostUSD) {
			return fmt.Errorf("benchmark selection contains an invalid task allocation")
		}
		seen[task.ID] = true
		weight += task.Weight
		cost += task.PublishedMeanCostUSD * float64(task.Repetitions)
	}
	if math.Abs(weight-1) > 1e-9 || math.Abs(cost-selection.EstimatedPublishedSpendUSD) > 1e-9 {
		return fmt.Errorf("benchmark selection weights or costs do not reconcile")
	}
	return nil
}

func validateMatrixTaskFilter(selection matrixSelection, tasks []string) error {
	selected, seen := map[string]bool{}, map[string]bool{}
	for _, task := range selection.Tasks {
		selected[task.ID] = true
	}
	for _, task := range tasks {
		if !selected[task] {
			return fmt.Errorf("benchmark study task %q is not in the selection", task)
		}
		if seen[task] {
			return fmt.Errorf("duplicate benchmark study task filter %q", task)
		}
		seen[task] = true
	}
	return nil
}

func expectedStudyArmManifest(selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, arm *matrixArm) studyArmManifest {
	treatmentHash := ""
	if arm.Bundle != nil {
		treatmentHash = arm.Bundle.ManifestHash
	}
	manifest := studyArmManifest{SchemaVersion: studyArmManifestSchema, SelectionID: selectionID, Label: arm.Label, Mode: arm.Mode, Model: arm.Loaded.Model.PublishedIdentifier, Reasoning: arm.Loaded.Effort, ProviderClient: arm.Loaded.Model.ProviderClientVersion, TaskChecksums: copyStringMap(checksums), Repetitions: repetitionsForTasks(tasks), TreatmentHash: treatmentHash, AgentTimeoutMultiplier: arm.AgentTimeoutMultiplier}
	if arm.IgnoreProviderClientInManifest {
		manifest.ProviderClient = ""
	}
	return manifest
}

func repetitionsForTasks(tasks []benchmarkPlanTask) map[string]int {
	repetitions := make(map[string]int, len(tasks))
	for _, task := range tasks {
		repetitions[task.ID] = task.RepetitionsPerArm
	}
	return repetitions
}

func validateStudyArmManifest(selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, arm *matrixArm) error {
	path := filepath.Join(arm.StateDir, "manifest.json")
	var existing studyArmManifest
	if err := readStudyJSON(path, &existing); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read study arm manifest: %w", err)
	}
	expected := expectedStudyArmManifest(selectionID, tasks, checksums, arm)
	expected.CreatedAt = existing.CreatedAt
	expectedJSON, _ := json.Marshal(expected)
	existingJSON, _ := json.Marshal(existing)
	if string(expectedJSON) != string(existingJSON) {
		return fmt.Errorf("study arm %q conflicts with its immutable manifest", arm.Label)
	}
	return nil
}

func ensureStudyArmManifest(selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, arm *matrixArm) error {
	path := filepath.Join(arm.StateDir, "manifest.json")
	if _, err := os.Stat(path); err == nil {
		return validateStudyArmManifest(selectionID, tasks, checksums, arm)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect study arm manifest: %w", err)
	}
	manifest := expectedStudyArmManifest(selectionID, tasks, checksums, arm)
	manifest.CreatedAt = time.Now().UTC()
	return writeJSON(path, manifest)
}

func executeMatrix(ctx context.Context, repoRoot string, checksums, environments map[string]string, arms []matrixArm, tasks []string, concurrency int, executor TaskExecutor, onCellComplete ...func(AttemptResult)) (returnErr error) {
	if concurrency < 1 {
		return fmt.Errorf("study execution requires at least one task worker, got %d", concurrency)
	}
	if len(arms) == 0 {
		return fmt.Errorf("study execution requires an experiment")
	}
	executionLock, err := acquireStudyExecutionLock(arms)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, executionLock.release())
	}()
	var notify func(AttemptResult)
	if len(onCellComplete) > 0 {
		notify = onCellComplete[0]
	}
	selected := map[string]bool{}
	for _, task := range tasks {
		selected[task] = true
	}
	var jobs []matrixJob
	for taskIndex := range arms[0].Loaded.Plan.Tasks {
		for index := range arms {
			arm := &arms[index]
			task := arm.Loaded.Plan.Tasks[taskIndex]
			if len(selected) > 0 && !selected[task.ID] {
				continue
			}
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, _, err := inspectStudyCell(*arm, task.ID, attempt, checksums[task.ID], environments[task.ID])
				if err != nil {
					return err
				}
				if state == studyCellMissing {
					jobs = append(jobs, matrixJob{arm: arm, cell: planCell{task: task.ID, attempt: attempt}})
				}
			}
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan matrixJob)
	var failures []error
	var lock sync.Mutex
	var notifyLock sync.Mutex
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range queue {
				if runCtx.Err() != nil {
					return
				}
				eventID, eventErr := NewEventID()
				err := eventErr
				if err == nil {
					var result AttemptResult
					// A failed paid provider event is immutable evidence.  A fresh
					// `benchmark run` may explicitly resume it, but this invocation
					// must return the infrastructure failure rather than retrying it.
					result, err = executor.Execute(runCtx, ExecutionRequest{RepoRoot: repoRoot, EvidenceDir: job.arm.StateDir, EventID: eventID, Attempt: job.cell.attempt, Task: job.cell.task, Model: job.arm.Loaded.Model, Effort: job.arm.Loaded.Effort, Arm: job.arm.Mode, Bundle: job.arm.Bundle, AgentTimeoutMultiplier: job.arm.AgentTimeoutMultiplier, TaskChecksum: checksums[job.cell.task], EnvironmentIdentity: environments[job.cell.task], ResumeFailedInfrastructure: true})
					if err == nil && result.Validate() != nil {
						err = fmt.Errorf("returned invalid evidence")
					}
					if err == nil && result.Status != statusSuccess {
						err = fmt.Errorf("execution failed: %s", result.Error)
					}
					if err == nil {
						result.InvocationWorkers = concurrency
						err = writeJSON(armResultPath(job.arm.StateDir, job.cell.task, job.cell.attempt), result)
						if err == nil && notify != nil {
							// Callers commonly stream to a CLI writer or update an
							// invocation total. Serialize those completion events so
							// worker parallelism cannot race accounting or output.
							notifyLock.Lock()
							notify(result)
							notifyLock.Unlock()
						}
					}
				}
				if err != nil {
					lock.Lock()
					failures = append(failures, fmt.Errorf("%s: %s repetition %d: %w", job.arm.Label, job.cell.task, job.cell.attempt, err))
					lock.Unlock()
					cancel()
					return
				}
			}
		}()
	}
send:
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-runCtx.Done():
			break send
		}
	}
	close(queue)
	workers.Wait()
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return context.Cause(ctx)
}
