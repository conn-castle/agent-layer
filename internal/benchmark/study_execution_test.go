package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestStudySchedulerStopsAfterInfrastructureFailure(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 2}})
	executor := &schedulerExecutor{failures: map[string]error{"first-task:2": errors.New("task environment unavailable")}}
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 1, executor, nil)
	if err == nil || !strings.Contains(err.Error(), "first-task repetition 2") {
		t.Fatalf("infrastructure failure = %v", err)
	}
	calls := executor.requests()
	if len(calls) != 2 || calls[0].Task != "first-task" || calls[1].Attempt != 2 {
		t.Fatalf("scheduler ran after infrastructure failure: %#v", calls)
	}
	if !calls[0].ResumeFailedInfrastructure || !calls[1].ResumeFailedInfrastructure {
		t.Fatalf("scheduler did not mark its fresh invocation as resumable: %#v", calls)
	}
}

func TestStudySchedulerLetsInFlightPaidCellFinishAfterSiblingFailure(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 3}})
	executor := &inFlightFailureExecutor{started: make(chan struct{}, 2), release: make(chan struct{})}
	go func() {
		<-executor.started
		<-executor.started
		close(executor.release)
	}()
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 2, executor, nil)
	if err == nil || !strings.Contains(err.Error(), "first-task repetition 1") || strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("in-flight failure = %v", err)
	}
	var result AttemptResult
	if err := readStudyJSON(armResultPath(arm.StateDir, "first-task", 2), &result); err != nil {
		t.Fatalf("healthy in-flight cell was not persisted: %v", err)
	}
	if result.Attempt != 2 || result.InvocationWorkers != 2 {
		t.Fatalf("healthy in-flight result = %#v", result)
	}
	if _, err := os.Stat(armResultPath(arm.StateDir, "first-task", 3)); !os.IsNotExist(err) {
		t.Fatalf("scheduler started a new cell after failure: %v", err)
	}
}

func TestStudySchedulerReturnsProviderCapacityWithoutRetrying(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{capacityFailures: 1}
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 1, executor, nil)
	if !errors.Is(err, errProviderCapacity) {
		t.Fatalf("provider capacity error = %v", err)
	}
	calls := executor.requests()
	if len(calls) != 1 {
		t.Fatalf("provider capacity retry calls=%#v", calls)
	}
	if !calls[0].ResumeFailedInfrastructure {
		t.Fatalf("provider capacity request did not authorize a later invocation resume: %#v", calls[0])
	}
}

func TestStudySelectionRejectsManualExclusionsInSchemaV1(t *testing.T) {
	historical := matrixSelectionFixture()
	historical.SchemaVersion = 1
	if err := validateMatrixSelection(historical); err != nil {
		t.Fatalf("genuine historical v1 selection rejected: %v", err)
	}
	historical.ManualExclusions = []string{"excluded-historical-task"}
	if err := validateMatrixSelection(historical); err == nil || !strings.Contains(err.Error(), "schema v1 does not support manual exclusions") {
		t.Fatalf("v1 manual exclusions error = %v", err)
	}
}

func TestStudySelectionRejectsPublishedVarianceOutsideSchemaV3(t *testing.T) {
	historical := matrixSelectionFixture()
	if err := validateMatrixSelection(historical); err != nil {
		t.Fatalf("schema v2 without published evidence rejected: %v", err)
	}
	historical.Tasks[0].PublishedVariance = lunaPublishedVariance(.04)
	if err := validateMatrixSelection(historical); err == nil || !strings.Contains(err.Error(), "schema v2 does not support published variance evidence") {
		t.Fatalf("v2 published variance error = %v", err)
	}
	v1 := matrixSelectionFixture()
	v1.SchemaVersion = 1
	v1.Tasks[0].PublishedVariance = lunaPublishedVariance(.04)
	if err := validateMatrixSelection(v1); err == nil || !strings.Contains(err.Error(), "schema v1 does not support published variance evidence") {
		t.Fatalf("v1 published variance error = %v", err)
	}
}

func TestStudySelectionSchemaV3RequiresValidPublishedVariance(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	if err := validateMatrixSelection(selection); err != nil {
		t.Fatalf("valid schema v3 rejected: %v", err)
	}
	selection.Tasks[0].PublishedVariance = nil
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "requires pinned published variance evidence") {
		t.Fatalf("missing published evidence error = %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.SampleVariance = math.NaN()
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "finite and non-negative") {
		t.Fatalf("non-finite variance error = %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.SampleVariance = -0.1
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "finite and non-negative") {
		t.Fatalf("negative variance error = %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.SampleSize = 1
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("sample size error = %v", err)
	}

	selection = matrixSelectionFixtureV3(0)
	if err := validateMatrixSelection(selection); err != nil {
		t.Fatalf("zero published variance rejected: %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.ConfigurationID = "other::low"
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "incoherent") {
		t.Fatalf("incoherent configuration error = %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Tasks[0].PublishedVariance.PublishedModel = "gpt-5-6-sol"
	selection.Tasks[0].PublishedVariance.ConfigurationID = "gpt-5-6-sol::low"
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "does not correspond to selector") {
		t.Fatalf("selector mismatch error = %v", err)
	}

	selection = matrixSelectionFixtureV3(.04)
	selection.Selector.Model, selection.Selector.Reasoning = modelGrok46, effortHigh
	for index := range selection.Tasks {
		selection.Tasks[index].PublishedVariance.PublishedModel = deepSWEPublishedGrok46
		selection.Tasks[index].PublishedVariance.PublishedReasoning = effortHigh
		selection.Tasks[index].PublishedVariance.ConfigurationID = deepSWEPublishedGrok46 + "::" + effortHigh
	}
	selection.Tasks[1].PublishedVariance.PublishedModel = modelGrok46
	selection.Tasks[1].PublishedVariance.ConfigurationID = modelGrok46 + "::" + effortHigh
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "mixes configuration identities") {
		t.Fatalf("mixed configuration error = %v", err)
	}
}

func TestStudySelectionSchemaV3AcceptsPublishedGrokConfigurationMapping(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	selection.Selector.Model, selection.Selector.Reasoning = modelGrok46, effortHigh
	for index := range selection.Tasks {
		selection.Tasks[index].PublishedVariance.PublishedModel = deepSWEPublishedGrok46
		selection.Tasks[index].PublishedVariance.PublishedReasoning = effortHigh
		selection.Tasks[index].PublishedVariance.ConfigurationID = deepSWEPublishedGrok46 + "::" + effortHigh
	}
	if err := validateMatrixSelection(selection); err != nil {
		t.Fatalf("grok published mapping rejected: %v", err)
	}
	selection.Selector.Model = modelGrok45
	if err := validateMatrixSelection(selection); err == nil || !strings.Contains(err.Error(), "does not correspond to selector") {
		t.Fatalf("grok selector mismatch error = %v", err)
	}
}

func TestStudySelectionLoadsSchemaV3PublishedVariance(t *testing.T) {
	selection := matrixSelectionFixtureV3(.04)
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, identity, err := loadMatrixSelection(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity) != 64 || loaded.SchemaVersion != matrixSelectionSchemaVersion || loaded.Tasks[0].PublishedVariance == nil || loaded.Tasks[0].PublishedVariance.SampleSize != 4 {
		t.Fatalf("loaded v3 selection identity=%q selection=%#v", identity, loaded)
	}
}

func TestStudySelectionNamesInvalidSelectorAndSupportedModels(t *testing.T) {
	selection := matrixSelectionFixture()
	selection.Selector.Model = "grok-4-5"
	err := validateMatrixSelection(selection)
	if err == nil ||
		!strings.Contains(err.Error(), `model "grok-4-5" with reasoning "low"`) ||
		!strings.Contains(err.Error(), "grok-4.5") ||
		!strings.Contains(err.Error(), "supported models:") {
		t.Fatalf("invalid selector error = %v", err)
	}
}

func TestStudySelectionBoundaryRejectsMalformedDocumentsAndTaskScopes(t *testing.T) {
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(path, selectionData, 0o600); err != nil {
		t.Fatal(err)
	}
	selection, identity, err := loadMatrixSelection(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity) != 64 || len(selection.Tasks) != 2 {
		t.Fatalf("loaded selection identity=%q selection=%#v", identity, selection)
	}
	if _, _, err := loadMatrixSelection(path, append(append([]byte(nil), selectionData...), []byte("\n{}")...)); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing document error=%v", err)
	}
	if _, _, err := loadMatrixSelection(path, []byte(`{"unknown":true}`)); err == nil || !strings.Contains(err.Error(), "decode benchmark selection") {
		t.Fatalf("unknown selection field error=%v", err)
	}
	if _, _, err := loadMatrixSelection(path, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "unsupported or invalid") {
		t.Fatalf("invalid selection error=%v", err)
	}
	if _, _, err := loadMatrixSelection(path, make([]byte, maxStudySelectionBytes+1)); err == nil || !strings.Contains(err.Error(), "no larger than") {
		t.Fatalf("oversized selection error=%v", err)
	}
	if _, _, err := loadMatrixSelection(filepath.Join(t.TempDir(), "missing.json"), nil); err == nil || !strings.Contains(err.Error(), "inspect benchmark selection") {
		t.Fatalf("missing selection error=%v", err)
	}
	if err := validateMatrixTaskFilter(selection, []string{"first-task"}); err != nil {
		t.Fatal(err)
	}
	if err := validateMatrixTaskFilter(selection, []string{"unknown-task"}); err == nil || !strings.Contains(err.Error(), "is not in the selection") {
		t.Fatalf("unknown task filter error=%v", err)
	}
	if err := validateMatrixTaskFilter(selection, []string{"first-task", "first-task"}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate task filter error=%v", err)
	}
}

func TestStudySchedulerScopesTasksAndStampsWorkerProvenance(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{}
	if err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, []string{"second-task"}, 2, executor, nil); err != nil {
		t.Fatal(err)
	}
	calls := executor.requests()
	if len(calls) != 1 || calls[0].Task != "second-task" {
		t.Fatalf("task filter scheduled %#v", calls)
	}
	var result AttemptResult
	if err := readStudyJSON(armResultPath(arm.StateDir, "second-task", 1), &result); err != nil {
		t.Fatal(err)
	}
	if result.InvocationWorkers != 2 {
		t.Fatalf("worker provenance = %d, want 2", result.InvocationWorkers)
	}
}

func TestStudySchedulerOrdersSerialJobsByTaskAcrossArmsAndRepetitions(t *testing.T) {
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 2}}
	first, checksums, environments := schedulerArmFixture(t, tasks)
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	second := matrixArmFixture(filepath.Dir(first.StateDir), "second", ArmTreatment, model, effort, tasks)
	var starts []string
	err = executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{first, second}, nil, 1, &schedulerExecutor{}, func(job matrixJob) {
		starts = append(starts, fmt.Sprintf("%s:%s:%d", job.arm.Label, job.cell.task, job.cell.attempt))
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "scheduler:first-task:1,scheduler:first-task:2,second:first-task:1,second:first-task:2," +
		"scheduler:second-task:1,scheduler:second-task:2,second:second-task:1,second:second-task:2"
	if got := strings.Join(starts, ","); got != want {
		t.Fatalf("serial job order = %q, want %q", got, want)
	}
}

func TestStudySchedulerSerializesCompletionNotifications(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{barrier: make(chan struct{}), started: make(chan struct{}, 2)}
	go func() {
		<-executor.started
		<-executor.started
		close(executor.barrier)
	}()
	var output bytes.Buffer
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 2, executor, nil, func(_ matrixJob, result AttemptResult) {
		// bytes.Buffer is deliberately not synchronized. This mirrors a CLI
		// writer and makes `go test -race` prove completion callbacks are serial.
		_, _ = fmt.Fprintf(&output, "%s:%d\n", result.Task, result.Attempt)
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "first-task:1\nsecond-task:1\n" && output.String() != "second-task:1\nfirst-task:1\n" {
		t.Fatalf("completion stream = %q", output.String())
	}
}

func TestStudyExecutionClaimsStudyBeforeSchedulingPaidWork(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}})
	repoRoot := t.TempDir()
	firstExecutor := &schedulerExecutor{barrier: make(chan struct{}), started: make(chan struct{}, 1)}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- executeMatrix(context.Background(), repoRoot, checksums, environments, []matrixArm{arm}, nil, 1, firstExecutor, nil)
	}()
	select {
	case <-firstExecutor.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first study execution did not start")
	}
	released := false
	defer func() {
		if !released {
			close(firstExecutor.barrier)
		}
	}()

	lockPath := filepath.Join(filepath.Dir(arm.StateDir), studyExecutionLockFile)
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0o600) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	probeErr := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // test-owned descriptor.
	if !errors.Is(probeErr, unix.EWOULDBLOCK) && !errors.Is(probeErr, unix.EAGAIN) {
		_ = probe.Close()
		t.Fatalf("independent process lock probe = %v, want contention", probeErr)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	secondExecutor := &schedulerExecutor{}
	err = executeMatrix(context.Background(), repoRoot, checksums, environments, []matrixArm{arm}, nil, 1, secondExecutor, nil)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("concurrent execution error = %v", err)
	}
	if calls := secondExecutor.requests(); len(calls) != 0 {
		t.Fatalf("concurrent execution scheduled paid work: %#v", calls)
	}

	close(firstExecutor.barrier)
	released = true
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func schedulerArmFixture(t *testing.T, tasks []benchmarkPlanTask) (matrixArm, map[string]string, map[string]string) {
	t.Helper()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	arm := matrixArmFixture(t.TempDir(), "scheduler", ArmBaseline, model, effort, tasks)
	checksums, environments := make(map[string]string, len(tasks)), make(map[string]string, len(tasks))
	for _, task := range tasks {
		checksums[task.ID] = task.ID + "-checksum"
		environments[task.ID] = task.ID + "-environment"
	}
	return arm, checksums, environments
}

type schedulerExecutor struct {
	mu               sync.Mutex
	calls            []ExecutionRequest
	failures         map[string]error
	capacityFailures int
	barrier          chan struct{}
	started          chan struct{}
}

type inFlightFailureExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (executor *inFlightFailureExecutor) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	executor.started <- struct{}{}
	select {
	case <-executor.release:
	case <-ctx.Done():
		return AttemptResult{}, ctx.Err()
	}
	if request.Attempt == 1 {
		return AttemptResult{}, errors.New("simulated provider infrastructure failure")
	}
	select {
	case <-time.After(100 * time.Millisecond):
		return schedulerResult(request, request.Attempt), nil
	case <-ctx.Done():
		return AttemptResult{}, fmt.Errorf("healthy sibling was canceled: %w", ctx.Err())
	}
}

func (executor *schedulerExecutor) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	executor.mu.Lock()
	executor.calls = append(executor.calls, request)
	callNumber := len(executor.calls)
	failure := executor.failures[fmt.Sprintf("%s:%d", request.Task, request.Attempt)]
	capacity := executor.capacityFailures > 0
	if capacity {
		executor.capacityFailures--
	}
	executor.mu.Unlock()
	if failure != nil {
		return AttemptResult{}, failure
	}
	if capacity {
		return AttemptResult{}, fmt.Errorf("%w: %s", errProviderCapacity, providerCapacityMessage)
	}
	if executor.started != nil {
		executor.started <- struct{}{}
	}
	if executor.barrier != nil {
		select {
		case <-executor.barrier:
		case <-ctx.Done():
			return AttemptResult{}, ctx.Err()
		}
	}
	return schedulerResult(request, callNumber), nil
}

func (executor *schedulerExecutor) requests() []ExecutionRequest {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return append([]ExecutionRequest(nil), executor.calls...)
}

func schedulerResult(request ExecutionRequest, callNumber int) AttemptResult {
	cost, duration := .1, 1.0
	now := time.Now().UTC()
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, Status: statusSuccess,
		F2PPassed: callNumber, F2PTotal: 8, F2PScore: float64(callNumber) / 8,
		CostUSD: &cost, CostKind: costKindProviderReported,
		DurationSeconds: &duration, TaskChecksum: request.TaskChecksum,
		EnvironmentIdentity: request.EnvironmentIdentity, StartedAt: now, FinishedAt: now,
		Provider: request.Model.Adapter, PublishedModel: request.Model.PublishedIdentifier,
		RuntimeModel: request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		ProviderClientVersion: request.Model.ProviderClientVersion,
		DispatchConformant:    true, InvocationCount: 1,
	}
}
