package benchmark

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type planCell struct {
	task    string
	attempt int
}

type armExecution struct {
	repoRoot    string
	stateDir    string
	arm         string
	concurrency int
	loaded      loadedBenchmarkPlan
	checksums   map[string]string
	bundle      *TreatmentBundle
}

func missingPlanCells(execution armExecution) []planCell {
	var missing []planCell
	treatment := execution.arm == ArmTreatment
	for _, task := range execution.loaded.Plan.Tasks {
		for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
			if !validPlanResult(
				armResultPath(execution.stateDir, task.ID, attempt),
				task.ID, attempt, execution.checksums[task.ID],
				execution.loaded.Model, execution.loaded.Effort, treatment,
			) {
				missing = append(missing, planCell{task: task.ID, attempt: attempt})
			}
		}
	}
	return missing
}

func validPlanResult(path, task string, attempt int, checksum string, model Model, effort string, treatment bool) bool {
	var result AttemptResult
	if readCampaignJSON(path, &result) != nil || result.Validate() != nil ||
		result.Status != statusSuccess {
		return false
	}
	if result.Task != task || result.Attempt != attempt ||
		result.TaskChecksum != checksum ||
		result.RuntimeModel != model.RuntimeIdentifier ||
		result.ReasoningEffort != effort {
		return false
	}
	return !treatment || result.InvocationCount > 0
}

func executePlanArm(ctx context.Context, execution armExecution, executor TaskExecutor) error {
	missing := missingPlanCells(execution)
	if len(missing) == 0 {
		return nil
	}
	jobs := make(chan planCell)
	var failures []error
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for worker := 0; worker < execution.concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				eventID, err := NewEventID()
				if err == nil {
					var result AttemptResult
					result, err = executor.Execute(ctx, ExecutionRequest{
						RepoRoot: execution.repoRoot, EvidenceDir: execution.stateDir,
						EventID: eventID, Attempt: item.attempt, Task: item.task,
						Model: execution.loaded.Model, Effort: execution.loaded.Effort,
						Arm: execution.arm, Bundle: execution.bundle,
						TaskChecksum: execution.checksums[item.task],
					})
					if err == nil && (result.Validate() != nil || result.Status != statusSuccess) {
						err = fmt.Errorf("returned invalid or failed evidence")
					}
					if err == nil {
						err = writeJSON(armResultPath(execution.stateDir, item.task, item.attempt), result)
					}
				}
				if err != nil {
					mutex.Lock()
					failures = append(failures, fmt.Errorf("%s repetition %d: %w", item.task, item.attempt, err))
					mutex.Unlock()
				}
			}
		}()
	}
	for _, item := range missing {
		jobs <- item
	}
	close(jobs)
	workers.Wait()
	return errors.Join(failures...)
}
