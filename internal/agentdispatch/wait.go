package agentdispatch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dispatchWaitInterval = 100 * time.Millisecond

// Wait blocks until the current invocation for one conversation is terminal.
func Wait(request WaitRequest) error {
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
	for !terminalDispatchState(record.State) {
		record, err = reconcileOrphan(request.Root, record)
		if err != nil {
			return err
		}
		if terminalDispatchState(record.State) {
			break
		}
		time.Sleep(dispatchWaitInterval)
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
		return writePublicResult(stdout, publicResult{Handle: handle, State: dispatchStateCompleted, ResultPath: path})
	case dispatchStateFailed, dispatchStateInterrupted:
		reason := strings.TrimSpace(record.TerminalReason)
		if reason == "" {
			reason = "dispatch invocation failed without a recorded reason"
		}
		if err := writePublicResult(stdout, publicResult{Handle: handle, State: dispatchStateFailed, Error: reason}); err != nil {
			return err
		}
		code := record.TerminalExitCode
		if code == 0 {
			code = ExitTargetFailure
		}
		return exitError(code, reason)
	case dispatchStateCancelled:
		return writePublicResult(stdout, publicResult{Handle: handle, State: dispatchStateCancelled})
	default:
		return exitError(ExitConfig, fmt.Sprintf("dispatch invocation %s has unsupported terminal state %q", record.ID, record.State))
	}
}

func completedResultPath(record RunRecord) (string, error) {
	path, err := filepath.Abs(record.AnswerPath)
	if err != nil {
		return "", wrapExitError(ExitConfig, "resolve dispatch result path", err)
	}
	file, err := os.Open(path) // #nosec G304 -- path comes from validated Agent Layer run state.
	if err != nil {
		return "", wrapExitError(ExitConfig, "open completed dispatch result", err)
	}
	if err := file.Close(); err != nil {
		return "", wrapExitError(ExitConfig, "close completed dispatch result", err)
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
