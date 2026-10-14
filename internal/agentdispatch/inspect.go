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
	ReservationExpiresAt   *time.Time `json:"reservation_expires_at,omitempty"`
}

// OutputResult contains bounded text from one supported invocation output.
type OutputResult struct {
	Handle               string `json:"handle"`
	InvocationID         string `json:"invocation_id"`
	Artifact             string `json:"artifact"`
	Content              string `json:"content"`
	Truncated            bool   `json:"truncated,omitempty"`
	Offset               int64  `json:"offset"`
	NextOffset           int64  `json:"next_offset"`
	SizeBytes            int64  `json:"size_bytes"`
	State                string `json:"state"`
	TerminationConfirmed bool   `json:"termination_confirmed"`
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
			expireReservation(current, time.Now().UTC())
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
		reason = strings.TrimSpace(strings.TrimPrefix(reason, terminalReasonCancelledByCaller))
	}
	result := InspectResult{
		Handle:                 record.Name,
		InvocationID:           record.ID,
		State:                  state,
		Error:                  reason,
		LastActivityAt:         record.LastActivityAt,
		LastOutputAt:           record.LastOutputAt,
		TerminationConfirmed:   record.TerminationConfirmed,
		TerminationConfirmedAt: record.TerminationConfirmedAt,
	}
	if record.State == dispatchStateReserved {
		result.ReservationExpiresAt = record.ReservationExpiresAt
	}
	return result
}

// Output returns bounded text for a completed final answer or the captured
// event stream that may contain partial output from any invocation state.
func Output(request OutputRequest) error {
	if request.Offset < 0 || (request.Tail && request.Offset != 0) {
		return exitError(ExitUsage, "output offset must be nonnegative and cannot be combined with tail")
	}
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
	page, err := readOutputPage(filepathForRun(request.Root, record.ID), path, request.Offset, request.Tail)
	if err != nil {
		return wrapExitError(ExitConfig, "read dispatch output", err)
	}
	return writeJSONResult(writerOrDiscard(request.Stdout), OutputResult{
		Handle: record.Name, InvocationID: record.ID, Artifact: artifact,
		Content: page.Content, Truncated: page.Truncated, Offset: page.Offset, NextOffset: page.NextOffset, SizeBytes: page.SizeBytes,
		State: inspectResultFromRecord(record).State, TerminationConfirmed: record.TerminationConfirmed,
	})
}

// Pages are byte slices of captured text; JSONL records may span pages. The
// returned cursor never splits a UTF-8 rune and lets callers assemble records.
func readOutputPage(runDir, path string, offset int64, tail bool) (OutputResult, error) {
	if strings.TrimSpace(path) == "" {
		return OutputResult{}, errors.New("dispatch output path is empty")
	}
	file, err := openOwnedRegularFile(runDir, path)
	if err != nil {
		return OutputResult{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return OutputResult{}, err
	}
	size := info.Size()
	if tail && size > maxOutputReadBytes {
		offset = size - maxOutputReadBytes
	}
	if offset > size {
		return OutputResult{}, errors.New("output offset exceeds artifact size")
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return OutputResult{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, min(int64(maxOutputReadBytes), size-offset)))
	if err != nil {
		return OutputResult{}, err
	}
	if tail && offset > 0 {
		// Tail can start inside a rune. Only discard UTF-8 continuation bytes.
		for removed := 0; removed < utf8.UTFMax-1 && len(data) > 0 && data[0]&0xc0 == 0x80; removed++ {
			data = data[1:]
			offset++
		}
	}
	if offset+int64(len(data)) < size {
		for removed := 0; removed < utf8.UTFMax-1 && !utf8.Valid(data) && len(data) > 0; removed++ {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return OutputResult{}, errors.New("dispatch output is not valid UTF-8 text or offset splits a rune")
	}
	next := offset + int64(len(data))
	return OutputResult{Content: string(data), Offset: offset, NextOffset: next, SizeBytes: size, Truncated: (tail && offset > 0) || next < size}, nil
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
