package agentdispatch

import (
	"context"
	"fmt"
	"io"
	"os"
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

// Wait blocks until the current invocation is terminal or the bounded wait
// expires. Expiration reports running without changing the invocation.
func Wait(request WaitRequest) error {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	handle := strings.TrimSpace(request.ID)
	if handle == "" {
		return exitError(ExitUsage, "dispatch wait requires a handle")
	}
	session, err := loadSession(request.Root, handle)
	if err != nil {
		return err
	}
	record, err := currentSessionRun(request.Root, session)
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
	for !terminalDispatchState(record.State) {
		record, err = reconcileOrphan(request.Root, record)
		if err != nil {
			return err
		}
		if terminalDispatchState(record.State) {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return writePublicResult(writerOrDiscard(request.Stdout), Result{Handle: session.Name, State: dispatchStateRunning})
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
	return writeWaitResult(session.Name, record, writerOrDiscard(request.Stdout))
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

func writeWaitResult(handle string, record RunRecord, stdout io.Writer) error {
	switch record.State {
	case dispatchStateCompleted:
		path, err := completedResultPath(record)
		if err != nil {
			return err
		}
		return writePublicResult(stdout, Result{Handle: handle, State: dispatchStateCompleted, ResultPath: path})
	case dispatchStateFailed, dispatchStateInterrupted:
		reason := strings.TrimSpace(record.TerminalReason)
		if reason == "" {
			reason = "dispatch invocation failed without a recorded reason"
		}
		if err := writePublicResult(stdout, Result{Handle: handle, State: dispatchStateFailed, Error: reason}); err != nil {
			return err
		}
		code := record.TerminalExitCode
		if code == 0 {
			code = ExitTargetFailure
		}
		return exitError(code, reason)
	case dispatchStateCancelled:
		return writePublicResult(stdout, Result{Handle: handle, State: dispatchStateCancelled})
	default:
		return exitError(ExitConfig, fmt.Sprintf("dispatch invocation %s has unsupported terminal state %q", record.ID, record.State))
	}
}

func completedResultPath(record RunRecord) (string, error) {
	if strings.TrimSpace(record.AnswerPath) == "" {
		return "", exitError(ExitConfig, "completed dispatch result path is empty")
	}
	path, err := filepath.Abs(record.AnswerPath)
	if err != nil {
		return "", wrapExitError(ExitConfig, "resolve dispatch result path", err)
	}
	info, err := os.Stat(path) // #nosec G304 -- path comes from validated Agent Layer run state.
	if err != nil {
		return "", wrapExitError(ExitConfig, "stat completed dispatch result", err)
	}
	if !info.Mode().IsRegular() {
		return "", exitError(ExitConfig, "completed dispatch result is not a regular file")
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
