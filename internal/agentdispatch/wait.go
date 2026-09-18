package agentdispatch

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const (
	dispatchWaitInterval = 100 * time.Millisecond
	dispatchWaitTimeout  = 8 * time.Minute
	// mcpWaitPollInterval is the coarser cadence used by the long MCP wait.
	mcpWaitPollInterval = time.Second
)

// Wait blocks until the selected condition is met or the bounded wait
// expires. Expiration reports the current observation and whether the
// requested condition was met. A handle is resolved once; an invocation ID
// never follows a later continuation.
func Wait(request WaitRequest) error {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	condition, err := waitConditionName(request.Condition)
	if err != nil {
		return err
	}
	record, err := resolveInvocationSelector(request.Root, request.ID, request.Handle, request.InvocationID)
	if err != nil {
		return err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = dispatchWaitTimeout
	}
	interval := request.PollInterval
	if interval <= 0 {
		interval = dispatchWaitInterval
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			result := publicResult(record)
			if !terminalDispatchState(record.State) {
				result.State = dispatchStateRunning
				result.Error = ""
			}
			result.ConditionMet = boolPtr(false)
			return writePublicResult(writerOrDiscard(request.Stdout), result)
		}
		updated, reconErr := tryReconcileOrphan(request.Root, record)
		if reconErr != nil {
			return reconErr
		}
		record = updated
		if waitConditionSatisfied(record, condition) {
			return writeWaitResult(request.Root, record, condition, writerOrDiscard(request.Stdout))
		}
		remaining = time.Until(deadline)
		if remaining <= 0 {
			continue
		}
		pollDelay := min(interval, remaining)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollDelay):
		}
		record, err = loadRunRecord(request.Root, record.ID)
		if err != nil {
			return err
		}
	}
}

func currentSessionRun(root string, session Session) (RunRecord, error) {
	runID := session.ActiveRunID
	if runID == "" {
		runID = session.RunID
	}
	if runID == "" {
		return RunRecord{}, exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has no invocation", session.Name))
	}
	return loadRunRecord(root, runID)
}

func writeWaitResult(root string, record RunRecord, condition string, stdout io.Writer) error {
	result := publicResult(record)
	result.ConditionMet = boolPtr(true)
	switch record.State {
	case dispatchStateCompleted:
		if condition == waitConditionTerminal {
			path, err := completedResultPath(root, record)
			if err != nil {
				return err
			}
			result.ResultPath = path
			result.Error = ""
			return writePublicResult(stdout, result)
		}
		if path, err := completedResultPath(root, record); err == nil {
			result.ResultPath = path
			result.Error = ""
		}
		return writePublicResult(stdout, result)
	case dispatchStateFailed, dispatchStateInterrupted:
		reason := strings.TrimSpace(record.TerminalReason)
		if reason == "" {
			reason = "dispatch invocation failed without a recorded reason"
		}
		result.State = dispatchStateFailed
		result.Error = reason
		if err := writePublicResult(stdout, result); err != nil {
			return err
		}
		if condition == waitConditionTerminationConfirmed {
			return nil
		}
		code := record.TerminalExitCode
		if code == 0 {
			code = ExitTargetFailure
		}
		return exitError(code, reason)
	case dispatchStateCancelled:
		result.Error = ""
		return writePublicResult(stdout, result)
	default:
		if condition == waitConditionTerminationConfirmed && record.TerminationConfirmed {
			return writePublicResult(stdout, result)
		}
		return exitError(ExitConfig, fmt.Sprintf("dispatch invocation %s has unsupported terminal state %q", record.ID, record.State))
	}
}

func completedResultPath(root string, record RunRecord) (string, error) {
	if strings.TrimSpace(record.AnswerPath) == "" {
		return "", exitError(ExitConfig, "completed dispatch result path is empty")
	}
	runDir := filepathForRun(root, record.ID)
	answerPath := record.AnswerPath
	if !filepath.IsAbs(answerPath) {
		answerPath = filepath.Join(runDir, answerPath)
	}
	file, err := openOwnedRegularFile(runDir, answerPath)
	if err != nil {
		return "", wrapExitError(ExitConfig, "open completed dispatch result", err)
	}
	_ = file.Close()
	path, err := filepath.Abs(answerPath)
	if err != nil {
		return "", wrapExitError(ExitConfig, "resolve dispatch result path", err)
	}
	return path, nil
}

// resolveWaitRun remains the single internal resolver used while preparing a
// continuation. Public callers address conversations only by handle.
func resolveWaitRun(root string, handle string) (RunRecord, error) {
	session, err := loadSession(root, handle)
	if err != nil {
		return RunRecord{}, err
	}
	return currentSessionRun(root, session)
}
