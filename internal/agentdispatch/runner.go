package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

type captureWriter struct {
	file *os.File
	mu   sync.Mutex
}

// answerPrefixBuffer retains a bounded answer prefix while reporting complete
// writes so the provider's full stdout continues streaming to raw evidence.
type answerPrefixBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *answerPrefixBuffer) Write(data []byte) (int, error) {
	received := len(data)
	limit := w.limit
	if limit <= 0 {
		limit = maxRetainedAnswerBytes
	}
	remaining := limit - w.buffer.Len()
	if remaining > 0 {
		_, _ = w.buffer.Write(data[:min(remaining, received)])
	}
	if received > remaining {
		w.truncated = true
	}
	return received, nil
}

func (w *answerPrefixBuffer) String() string {
	retained := w.buffer.Bytes()
	if w.truncated {
		retained = retained[:validUTF8PrefixLength(retained)]
	}
	answer := string(retained)
	if w.truncated {
		answer += truncatedAnswerNotice
	}
	return answer
}

func newCaptureWriter(path string) (*captureWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- path is in an isolated dispatch run directory.
	if err != nil {
		return nil, err
	}
	return &captureWriter{file: file}, nil
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.file.Write(data)
	if err != nil {
		return n, err
	}
	if n != len(data) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (w *captureWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

type executionResult struct {
	SessionID    string
	Complete     bool
	AnswerSeen   bool
	NotResumable bool
	Answer       string
}

func executeProvider(
	command providerCommand,
	prompt []byte,
	run *dispatchRun,
	root string,
	newCommand CommandFactory,
	persist func(string) error,
) (executionResult, error) {
	if newCommand == nil {
		newCommand = defaultProviderCommandFactory
	}
	providerStdout, err := newCaptureWriter(run.Record.StdoutPath)
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch stdout capture", err)
	}
	defer func() { _ = providerStdout.Close() }()
	providerStderr, err := newCaptureWriter(run.Record.StderrPath)
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch stderr capture", err)
	}
	defer func() { _ = providerStderr.Close() }()
	var events *captureWriter
	if command.Structured {
		events, err = newCaptureWriter(run.Record.EventsPath)
		if err != nil {
			return executionResult{}, wrapExitError(ExitConfig, "create dispatch event capture", err)
		}
		defer func() { _ = events.Close() }()
	}

	cmd := newCommand(command.Path, command.Args...)
	cmd.Dir = command.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = command.Env
	if command.Plain {
		cmd.Stdin = nil
	} else {
		cmd.Stdin = bytes.NewReader(prompt)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, "open dispatch provider stdout", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, "open dispatch provider stderr", err)
	}
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return executionResult{}, &preStartFailure{err: providerStartError(command.Provider, err)}
	}
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	started := time.Now().UTC()
	run.Record.LastActivityAt = &started
	run.Record.LastActivityKind = "provider_started"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		stdoutDrained := make(chan struct{})
		stderrDrained := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, stdoutPipe)
			close(stdoutDrained)
		}()
		go func() {
			_, _ = io.Copy(io.Discard, stderrPipe)
			close(stderrDrained)
		}()
		signalProviderProcess(cmd, syscall.SIGTERM)
		_ = cmd.Wait()
		<-stdoutDrained
		<-stderrDrained
		return executionResult{}, err
	}
	caughtSignal, stopForwarder := installProviderSignalForwarder(cmd)
	defer stopForwarder()

	var result executionResult
	var answerCandidate string
	var resultMu sync.Mutex
	var semanticErr error
	setFailure := func(err error) {
		if err == nil {
			return
		}
		resultMu.Lock()
		if semanticErr == nil {
			semanticErr = err
		}
		resultMu.Unlock()
		signalProviderProcess(cmd, syscall.SIGTERM)
	}
	consume := func(event providerEvent) error {
		resultMu.Lock()
		defer resultMu.Unlock()
		if current, loadErr := loadRunRecord(root, run.Record.ID); loadErr == nil && current.State == dispatchStateCancelled {
			semanticErr = errors.New("dispatch was cancelled")
			return semanticErr
		}
		now := time.Now().UTC()
		run.Record.LastActivityAt = &now
		switch event.Kind {
		case eventSession:
			if event.SessionID == "" {
				semanticErr = errors.New("provider returned an empty session ID")
				return semanticErr
			}
			if result.SessionID != "" && result.SessionID != event.SessionID {
				semanticErr = errors.New("provider returned conflicting session IDs")
				return semanticErr
			}
			result.SessionID = event.SessionID
			run.Record.ProviderSessionID = event.SessionID
			if err := persist(event.SessionID); err != nil {
				semanticErr = err
				return err
			}
		case eventAnswer:
			answerCandidate = event.Answer
			result.AnswerSeen = true
			run.Record.LastOutputAt = &now
			run.Record.LastActivityKind = "answer_candidate"
		case eventProgress:
			run.Record.LastActivityKind = event.Activity
		case eventComplete:
			result.Complete = true
			run.Record.LastActivityKind = "provider_completed"
		case eventFailure:
			semanticErr = errors.New(event.Reason)
			return semanticErr
		}
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			semanticErr = err
			return err
		}
		return nil
	}

	streamErr := make(chan error, 1)
	stderrErr := make(chan error, 1)
	go func() {
		if command.Plain {
			var candidate answerPrefixBuffer
			_, err := io.Copy(io.MultiWriter(providerStdout, &candidate), stdoutPipe)
			if err == nil {
				resultMu.Lock()
				answerCandidate = candidate.String()
				result.AnswerSeen = candidate.buffer.Len() > 0
				now := time.Now().UTC()
				run.Record.LastOutputAt = &now
				run.Record.LastActivityAt = &now
				run.Record.LastActivityKind = "answer_candidate"
				resultMu.Unlock()
			}
			if err != nil {
				setFailure(fmt.Errorf("capture provider stdout: %w", err))
			}
			streamErr <- err
			return
		}
		err := readStructuredEvents(stdoutPipe, providerStdout, command.Provider, command.SessionID, func(event providerEvent) error {
			encoded, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := events.Write(append(encoded, '\n')); writeErr != nil {
				return writeErr
			}
			return consume(event)
		})
		if err != nil {
			setFailure(err)
		}
		streamErr <- err
	}()
	go func() {
		_, err := io.Copy(providerStderr, stderrPipe)
		if err != nil {
			setFailure(fmt.Errorf("capture provider stderr: %w", err))
		}
		stderrErr <- err
	}()

	streamResult := <-streamErr
	stderrResult := <-stderrErr
	waitErr := cmd.Wait()
	if signal := caughtSignal(); signal != nil {
		if signal == os.Interrupt {
			return executionResult{}, exitError(ExitSigint, fmt.Sprintf("%s interrupted by signal SIGINT", command.Provider))
		}
		return executionResult{}, exitError(ExitSigterm, fmt.Sprintf("%s interrupted by signal SIGTERM", command.Provider))
	}
	if streamResult != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, fmt.Sprintf("capture dispatch provider output: %v", streamResult), streamResult)
	}
	if stderrResult != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, fmt.Sprintf("capture dispatch provider diagnostics: %v", stderrResult), stderrResult)
	}
	resultMu.Lock()
	currentSemanticErr := semanticErr
	resultMu.Unlock()
	if currentSemanticErr != nil {
		return executionResult{}, exitError(ExitTargetFailure, fmt.Sprintf("%s dispatch did not complete: %v", command.Provider, currentSemanticErr))
	}
	if waitErr != nil {
		return executionResult{}, providerWaitError(command.Provider, waitErr)
	}
	if command.Provider == AgentAntigravity {
		timedOut, err := antigravityTimeoutReported(run.Record.StderrPath, command.LogPath)
		if err != nil {
			return executionResult{}, wrapExitError(ExitTargetFailure, "inspect Antigravity terminal diagnostics", err)
		}
		if timedOut {
			return executionResult{}, exitError(ExitTargetFailure, "antigravity reported terminal failure: Error: timeout waiting for response")
		}
	}
	if command.Structured && (!result.Complete || !result.AnswerSeen || result.SessionID == "") {
		return executionResult{}, exitError(ExitTargetFailure, fmt.Sprintf("%s dispatch completed without required terminal result, session ID, and final answer", command.Provider))
	}
	if command.Plain && !result.AnswerSeen {
		return executionResult{}, exitError(ExitTargetFailure, "antigravity dispatch completed without a final answer")
	}
	resultMu.Lock()
	terminalAnswer := answerCandidate
	resultMu.Unlock()
	result.Answer = terminalAnswer
	return result, nil
}

func antigravityTimeoutReported(stderrPath string, logPath string) (bool, error) {
	timedOut := false
	marker := []byte("Error: timeout waiting for response")
	for _, path := range []string{stderrPath, logPath} {
		found, err := fileContains(path, marker)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		if found {
			timedOut = true
		}
	}
	return timedOut, nil
}

func fileContains(path string, marker []byte) (bool, error) {
	if len(marker) == 0 {
		return true, nil
	}
	file, err := os.Open(path) // #nosec G304 -- paths are in the active isolated run.
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	const chunkBytes = 64 * 1024
	buffer := make([]byte, chunkBytes+len(marker)-1)
	retained := 0
	for {
		read, readErr := file.Read(buffer[retained:])
		available := retained + read
		if bytes.Contains(buffer[:available], marker) {
			return true, nil
		}
		if readErr != nil {
			if readErr == io.EOF {
				return false, nil
			}
			return false, readErr
		}
		retained = min(available, len(marker)-1)
		copy(buffer[:retained], buffer[available-retained:available])
	}
}

func replayAnswer(path string, stdout io.Writer) error {
	file, err := os.Open(path) // #nosec G304 -- path belongs to the completed dispatch run.
	if err != nil {
		return wrapExitError(ExitTargetFailure, "open captured final answer", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(stdout, file); err != nil {
		return wrapExitError(ExitTargetFailure, "write captured final answer to stdout", err)
	}
	return nil
}
