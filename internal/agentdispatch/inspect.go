package agentdispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxOutputReadBytes = 64 * 1024

// InspectResult is a prompt observation of one invocation. Process identities
// and proof details remain private implementation evidence.
type InspectResult struct {
	Handle                 string     `json:"handle"`
	InvocationID           string     `json:"invocation_id"`
	State                  string     `json:"state"`
	Error                  string     `json:"error,omitempty"`
	LastActivityAt         *time.Time `json:"last_activity_at,omitempty"`
	LastOutputAt           *time.Time `json:"last_output_at,omitempty"`
	TerminationConfirmed   bool       `json:"termination_confirmed"`
	TerminationConfirmedAt *time.Time `json:"termination_confirmed_at,omitempty"`
}

// OutputResult contains bounded text from one supported invocation output.
type OutputResult struct {
	Handle       string `json:"handle"`
	InvocationID string `json:"invocation_id"`
	Artifact     string `json:"artifact"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated,omitempty"`
}

// Inspect returns a non-blocking observation of one invocation. It never
// signals processes or waits for a busy run lock.
func Inspect(request InspectRequest) error {
	record, err := resolveInvocationSelector(request.Root, request.ID, request.Handle, request.InvocationID)
	if err != nil {
		return err
	}
	dir := filepathForRun(request.Root, record.ID)
	acquired, lockErr := tryWithRunLock(dir, func() error {
		updated, persistErr := applyRunEvidenceLocked(dir, func(current *RunRecord) error {
			if !terminalDispatchState(current.State) || current.TerminationConfirmed {
				return nil
			}
			applyTerminationEvidence(current, nil, true, time.Now().UTC())
			return nil
		})
		if persistErr != nil {
			return persistErr
		}
		record = updated
		return nil
	})
	if lockErr != nil {
		return lockErr
	}
	if !acquired {
		current, loadErr := loadRunRecord(request.Root, record.ID)
		if loadErr != nil {
			return loadErr
		}
		record = current
	}
	if record.TerminationConfirmed {
		if releaseErr := tryReleaseIfConfirmed(request.Root, record); releaseErr != nil {
			return releaseErr
		}
	}
	return writeJSONResult(writerOrDiscard(request.Stdout), inspectResultFromRecord(record))
}

func inspectResultFromRecord(record RunRecord) InspectResult {
	state := record.State
	if state == dispatchStateInterrupted {
		state = dispatchStateFailed
	}
	reason := strings.TrimSpace(record.TerminalReason)
	if state == dispatchStateCancelled {
		reason = ""
	}
	return InspectResult{
		Handle:                 record.Name,
		InvocationID:           record.ID,
		State:                  state,
		Error:                  reason,
		LastActivityAt:         record.LastActivityAt,
		LastOutputAt:           record.LastOutputAt,
		TerminationConfirmed:   record.TerminationConfirmed,
		TerminationConfirmedAt: record.TerminationConfirmedAt,
	}
}

// Output returns bounded text for a completed final answer or the captured
// event stream that may contain partial output from any invocation state.
func Output(request OutputRequest) error {
	artifact := strings.TrimSpace(request.Artifact)
	if artifact != artifactFinalAnswer && artifact != artifactEvents {
		return exitError(ExitUsage, fmt.Sprintf("dispatch output %q is invalid; use final_answer or events", artifact))
	}
	record, err := resolveInvocationSelector(request.Root, request.ID, request.Handle, request.InvocationID)
	if err != nil {
		return err
	}
	var path string
	if artifact == artifactFinalAnswer {
		if record.State != dispatchStateCompleted {
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch invocation %s did not produce a final answer", record.ID))
		}
		path = record.AnswerPath
	} else {
		path = record.EventsPath
		if !record.ProviderLaunchIntent && !readableOwnedOutput(filepathForRun(request.Root, record.ID), path) {
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch invocation %s has not produced partial output", record.ID))
		}
	}
	content, truncated, err := readBoundedOutput(filepathForRun(request.Root, record.ID), path)
	if err != nil {
		return wrapExitError(ExitConfig, "read dispatch output", err)
	}
	return writeJSONResult(writerOrDiscard(request.Stdout), OutputResult{
		Handle: record.Name, InvocationID: record.ID, Artifact: artifact,
		Content: content, Truncated: truncated,
	})
}

func readBoundedOutput(runDir string, path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, errors.New("dispatch output path is empty")
	}
	file, err := openOwnedRegularFile(runDir, path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxOutputReadBytes+1))
	if err != nil {
		return "", false, err
	}
	truncated := len(data) > maxOutputReadBytes
	if truncated {
		data = data[:maxOutputReadBytes]
		// A valid UTF-8 rune may straddle the fixed read boundary. Remove only
		// that incomplete suffix; malformed captured text still fails below.
		for removed := 0; removed < utf8.UTFMax-1 && !utf8.Valid(data); removed++ {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return "", false, errors.New("dispatch output is not valid UTF-8 text")
	}
	return string(data), truncated, nil
}

func openOwnedRegularFile(runDir string, path string) (*os.File, error) {
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(absRun, path)
	}
	if !ownedRunPath(runDir, path) {
		return nil, errors.New("output path is not inside the invocation directory")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0) // #nosec G304 -- path is a validated owned run output; nonblocking rejects FIFOs without waiting for a writer.
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, errors.New("output is a symlink")
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("output is not a regular file")
	}
	return file, nil
}

func readableOwnedOutput(runDir string, path string) bool {
	file, err := openOwnedRegularFile(runDir, path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func ownedRunPath(runDir string, path string) bool {
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return false
	}
	if resolvedRun, err := filepath.EvalSymlinks(absRun); err == nil {
		absRun = resolvedRun
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(absRun, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolvedPath
	} else {
		parent, base := filepath.Split(absPath)
		if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
			absPath = filepath.Join(resolvedParent, base)
		}
	}
	rel, err := filepath.Rel(absRun, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func writeJSONResult(stdout io.Writer, value any) error {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		return wrapExitError(ExitTargetFailure, "write dispatch response", err)
	}
	return nil
}
