package benchmark

import (
	"errors"
	"fmt"
	"os"
)

// validPlanAttemptResult is retained for the study report reader. It validates
// immutable normalized evidence without treating a corrupt record as missing.
func validPlanAttemptResult(result AttemptResult, task string, attempt int, checksum string, model Model, effort string, treatment bool) bool {
	if result.Validate() != nil || result.Status != statusSuccess {
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

type studyCellState int

type studyPreparedProgress struct {
	Completed int
	Missing   int
	Arms      []studyArmProgress
}

type studyArmProgress struct {
	Completed int
	Missing   int
}

const (
	studyCellMissing studyCellState = iota
	studyCellValid
)

// inspectStudyCell is the sole study evidence boundary. Missing means that the
// result path does not exist. Any existing malformed/conflicting result or
// incomplete receipt is immutable evidence corruption, never new paid work.
func inspectStudyCell(arm matrixArm, task string, attempt int, checksum, environment string) (studyCellState, AttemptResult, error) {
	path := armResultPath(arm.StateDir, task, attempt)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return studyCellMissing, AttemptResult{}, nil
		}
		return studyCellMissing, AttemptResult{}, fmt.Errorf("inspect study result %s: %w", path, err)
	}
	result, err := readStudyResult(path, task, attempt, checksum, environment, arm)
	if err != nil {
		return studyCellMissing, AttemptResult{}, fmt.Errorf("inspect immutable study cell %s repetition %d: %w", task, attempt, err)
	}
	return studyCellValid, result, nil
}

func studyProgressChecked(preparation matrixPreparation) (studyPreparedProgress, error) {
	outcome := studyPreparedProgress{}
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		progress := studyArmProgress{}
		for _, task := range arm.Loaded.Plan.Tasks {
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, _, err := inspectStudyCell(*arm, task.ID, attempt, preparation.checksums[task.ID], preparation.environments[task.ID])
				if err != nil {
					return studyPreparedProgress{}, err
				}
				if state == studyCellValid {
					progress.Completed++
				} else {
					progress.Missing++
				}
			}
		}
		outcome.Arms = append(outcome.Arms, progress)
		outcome.Completed += progress.Completed
		outcome.Missing += progress.Missing
	}
	return outcome, nil
}

func studyStoredCostChecked(preparation matrixPreparation) (ObservedCostRange, error) {
	var total ObservedCostRange
	for _, arm := range preparation.arms {
		for _, task := range arm.Loaded.Plan.Tasks {
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, result, err := inspectStudyCell(arm, task.ID, attempt, preparation.checksums[task.ID], preparation.environments[task.ID])
				if err != nil {
					return ObservedCostRange{}, err
				}
				if state == studyCellMissing {
					continue
				}
				minimum, maximum, err := result.CostBounds()
				if err != nil {
					return ObservedCostRange{}, err
				}
				total.Midpoint += *result.CostUSD
				total.Minimum += minimum
				total.Maximum += maximum
			}
		}
	}
	return total, nil
}
