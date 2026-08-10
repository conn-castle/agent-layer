package benchmark

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStudySchedulerStopsAfterInfrastructureFailure(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 2}})
	executor := &schedulerExecutor{failures: map[string]error{"first-task:2": errors.New("task environment unavailable")}}
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 1, executor)
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

func TestStudySchedulerReturnsProviderCapacityWithoutRetrying(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 2}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{capacityFailures: 1}
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 1, executor)
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

func TestStudySchedulerScopesTasksAndStampsWorkerProvenance(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{}
	if err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, []string{"second-task"}, 2, executor); err != nil {
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

func TestStudySchedulerSerializesCompletionNotifications(t *testing.T) {
	arm, checksums, environments := schedulerArmFixture(t, []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}})
	executor := &schedulerExecutor{barrier: make(chan struct{}), started: make(chan struct{}, 2)}
	go func() {
		<-executor.started
		<-executor.started
		close(executor.barrier)
	}()
	var output bytes.Buffer
	err := executeMatrix(context.Background(), t.TempDir(), checksums, environments, []matrixArm{arm}, nil, 2, executor, func(result AttemptResult) {
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
