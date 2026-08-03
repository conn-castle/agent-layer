package benchmark

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const providerCapacityRetryDelay = 60 * time.Second

type planCell struct {
	task    string
	attempt int
}

type armExecution struct {
	repoRoot     string
	stateDir     string
	arm          string
	concurrency  int
	maxNewRuns   int
	loaded       loadedBenchmarkPlan
	checksums    map[string]string
	environments map[string]string
	bundle       *TreatmentBundle
	capacityWait func(context.Context) error
}

func (execution armExecution) waitAfterProviderCapacity(ctx context.Context) error {
	if execution.capacityWait != nil {
		return execution.capacityWait(ctx)
	}
	timer := time.NewTimer(providerCapacityRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
			) || (execution.environments != nil &&
				!resultMatchesEnvironment(armResultPath(execution.stateDir, task.ID, attempt), execution.environments[task.ID])) {
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

func resultMatchesEnvironment(path, environment string) bool {
	if environment == "" {
		return false
	}
	var result AttemptResult
	return readCampaignJSON(path, &result) == nil && result.EnvironmentIdentity == environment
}

func executePlanArm(ctx context.Context, execution armExecution, executor TaskExecutor) error {
	missing := missingPlanCells(execution)
	if len(missing) == 0 {
		return nil
	}
	if execution.maxNewRuns > 0 && len(missing) > execution.maxNewRuns {
		missing = missing[:execution.maxNewRuns]
	}
	jobs := make(chan planCell)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var failures []error
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for worker := 0; worker < execution.concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
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
							RepoRoot: execution.repoRoot, EvidenceDir: execution.stateDir,
							EventID: eventID, Attempt: item.attempt, Task: item.task,
							Model: execution.loaded.Model, Effort: execution.loaded.Effort,
							Arm: execution.arm, Bundle: execution.bundle,
							TaskChecksum:        execution.checksums[item.task],
							EnvironmentIdentity: execution.environments[item.task],
						})
						if err == nil {
							if validationErr := result.Validate(); validationErr != nil {
								err = fmt.Errorf("returned invalid evidence: %w", validationErr)
							} else if result.Status != statusSuccess {
								err = fmt.Errorf("execution failed: %s", result.Error)
							}
						}
						if err == nil {
							err = writeJSON(armResultPath(execution.stateDir, item.task, item.attempt), result)
						}
					}
					if !errors.Is(err, errProviderCapacity) {
						break
					}
					if waitErr := execution.waitAfterProviderCapacity(runCtx); waitErr != nil {
						return
					}
				}
				if err != nil {
					mutex.Lock()
					failures = append(failures, fmt.Errorf("%s repetition %d: %w", item.task, item.attempt, err))
					mutex.Unlock()
					cancel()
					return
				}
			}
		}()
	}
sendJobs:
	for _, item := range missing {
		select {
		case jobs <- item:
		case <-runCtx.Done():
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return context.Cause(ctx)
}
