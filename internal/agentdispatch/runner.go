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

type captureBudget struct {
	mu   sync.Mutex
	used int64
	max  int64
}

func (b *captureBudget) reserve(size int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(size) > b.max-b.used {
		return fmt.Errorf("dispatch provider capture exceeded %d byte limit", b.max)
	}
	b.used += int64(size)
	return nil
}

type limitedWriter struct {
	file    *os.File
	limit   int64
	written int64
	budget  *captureBudget
	mu      sync.Mutex
}

func newLimitedWriter(path string, limit int64, budget *captureBudget) (*limitedWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- path is in an isolated dispatch run directory.
	if err != nil {
		return nil, err
	}
	return &limitedWriter{file: file, limit: limit, budget: budget}, nil
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(len(data)) > w.limit-w.written {
		return 0, fmt.Errorf("dispatch output exceeded %d byte limit", w.limit)
	}
	if w.budget != nil {
		if err := w.budget.reserve(len(data)); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.written += int64(n)
	if err != nil {
		return n, err
	}
	if n != len(data) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (w *limitedWriter) Close() error {
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
	budget := &captureBudget{max: maxCaptureBytes}
	providerStdout, err := newLimitedWriter(run.Record.StdoutPath, maxCaptureBytes, budget)
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch stdout capture", err)
	}
	defer func() { _ = providerStdout.Close() }()
	providerStderr, err := newLimitedWriter(run.Record.StderrPath, maxCaptureBytes, budget)
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch stderr capture", err)
	}
	defer func() { _ = providerStderr.Close() }()
	var events *limitedWriter
	if command.Structured {
		events, err = newLimitedWriter(run.Record.EventsPath, maxCaptureBytes, budget)
		if err != nil {
			return executionResult{}, wrapExitError(ExitConfig, "create dispatch event capture", err)
		}
		defer func() { _ = events.Close() }()
	}

	cmd := newCommand(command.Path, command.Args...)
	cmd.Dir = root
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
			if len(event.Answer) > maxAnswerBytes {
				semanticErr = fmt.Errorf("dispatch final answer exceeded %d byte limit", maxAnswerBytes)
				return semanticErr
			}
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
			var candidate bytes.Buffer
			_, err := io.Copy(io.MultiWriter(providerStdout, &candidate), stdoutPipe)
			if err == nil {
				resultMu.Lock()
				if candidate.Len() > maxAnswerBytes {
					err = fmt.Errorf("dispatch final answer exceeded %d byte limit", maxAnswerBytes)
				} else {
					answerCandidate = candidate.String()
					result.AnswerSeen = candidate.Len() > 0
					now := time.Now().UTC()
					run.Record.LastOutputAt = &now
					run.Record.LastActivityAt = &now
					run.Record.LastActivityKind = "answer_candidate"
				}
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
	if command.LogPath != "" {
		info, err := os.Stat(command.LogPath)
		if err != nil {
			return executionResult{}, wrapExitError(ExitTargetFailure, "stat Antigravity provider log", err)
		}
		budget.mu.Lock()
		remaining := budget.max - budget.used
		budget.mu.Unlock()
		if info.Size() > remaining {
			return executionResult{}, exitError(ExitTargetFailure, fmt.Sprintf("Antigravity provider log exceeded the remaining %d byte dispatch capture budget", remaining))
		}
	}
	if command.Provider == AgentAntigravity && antigravityTimeoutReported(run.Record.StderrPath, command.LogPath) {
		return executionResult{}, exitError(ExitTargetFailure, "antigravity reported terminal failure: Error: timeout waiting for response")
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
	if err := writeBytesAtomic(run.Record.AnswerPath, []byte(terminalAnswer), 0o600); err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "publish dispatch terminal answer", err)
	}
	return result, nil
}

func antigravityTimeoutReported(stderrPath string, logPath string) bool {
	for _, path := range []string{stderrPath, logPath} {
		data, err := os.ReadFile(path) // #nosec G304 -- paths are in the active isolated run.
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte("Error: timeout waiting for response")) {
			return true
		}
	}
	return false
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
