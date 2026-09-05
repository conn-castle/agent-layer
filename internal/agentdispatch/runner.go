package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type captureWriter struct {
	file *os.File
	mu   sync.Mutex
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
	SessionID  string
	Complete   bool
	AnswerSeen bool
	Answer     string
}

// After terminal evidence or process exit, only shutdown and output draining
// remain. This is not an idle timeout for an actively working provider.
const providerShutdownGrace = 5 * time.Second

// unprovenProviderTerminationError marks a provider failure whose process
// group may still be live. Failure finalization must preserve the active claim
// and nonterminal run evidence until a later cancellation or recovery proves
// that ownership is dead.
type unprovenProviderTerminationError struct{ err error }

func (e *unprovenProviderTerminationError) Error() string { return e.err.Error() }
func (e *unprovenProviderTerminationError) Unwrap() error { return e.err }

// newUnprovenProviderTerminationError preserves the primary provider exit
// classification while adding the failed group-death proof. The returned type
// tells dispatch finalization to retain the active claim and run evidence.
func newUnprovenProviderTerminationError(primary error, message string, proofErr error) *unprovenProviderTerminationError {
	return &unprovenProviderTerminationError{err: errors.Join(
		primary,
		wrapExitError(ExitTargetFailure, fmt.Sprintf("%s: %v", message, proofErr), proofErr),
	)}
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
	if run.Record.EventsPath == "" {
		run.Record.EventsPath = filepath.Join(run.Dir, "provider.events")
	}
	events, err := newCaptureWriter(run.Record.EventsPath)
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch event capture", err)
	}
	defer func() { _ = events.Close() }()
	var lineage *captureWriter
	if command.ClaudeLineage {
		if run.Record.LineagePath == "" {
			return executionResult{}, exitError(ExitConfig, "Claude lineage-capable command has no lineage artifact path")
		}
		lineage, err = newCaptureWriter(run.Record.LineagePath)
		if err != nil {
			return executionResult{}, wrapExitError(ExitConfig, "create dispatch lineage capture", err)
		}
		defer func() { _ = lineage.Close() }()
	}

	cmd := newCommand(command.Path, command.Args...)
	cmd.Dir = command.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = command.Env
	// A file avoids exec's stdin-copy goroutine, which can outlive the leader
	// when a descendant inherits an unread pipe. Unlink it while open so the
	// prompt is not left behind as another durable artifact.
	stdin, err := os.CreateTemp(run.Dir, "provider-stdin-*")
	if err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "create dispatch provider stdin", err)
	}
	defer func() { _ = stdin.Close() }()
	if err := os.Remove(stdin.Name()); err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "unlink dispatch provider stdin", err)
	}
	if _, err := stdin.Write(prompt); err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "write dispatch provider stdin", err)
	}
	if _, err := stdin.Seek(0, io.SeekStart); err != nil {
		return executionResult{}, wrapExitError(ExitConfig, "rewind dispatch provider stdin", err)
	}
	cmd.Stdin = stdin
	// Own the pipes so cmd.Wait can observe process exit independently without
	// closing output that the structured reader has not consumed yet.
	stdoutPipe, stdoutWrite, err := os.Pipe()
	if err != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, "open dispatch provider stdout", err)
	}
	defer func() { _ = stdoutPipe.Close() }()
	defer func() { _ = stdoutWrite.Close() }()
	stderrPipe, stderrWrite, err := os.Pipe()
	if err != nil {
		return executionResult{}, wrapExitError(ExitTargetFailure, "open dispatch provider stderr", err)
	}
	defer func() { _ = stderrPipe.Close() }()
	defer func() { _ = stderrWrite.Close() }()
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return executionResult{}, &preStartFailure{err: providerStartError(command.Provider, err)}
	}
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	leaderPID := run.Record.PID
	leaderGroupID := run.Record.ProcessGroupID
	leaderStart := run.Record.ProcessStartIdentity
	termination, err := newStartedProviderTermination(cmd, run.Record, providerTerminationGrace)
	if err != nil {
		// The exec.Cmd is direct proof that this leader is ours, but without a
		// durable start identity Agent Layer must not signal its process group.
		// Kill only the directly owned leader and fail before publishing running
		// state or accepting provider output.
		killErr := cmd.Process.Kill()
		waitErr := cmd.Wait()
		if !providerProcessGroupDead(run.Record.ProcessGroupID) {
			return executionResult{}, newUnprovenProviderTerminationError(
				wrapExitError(ExitTargetFailure, "verify dispatch provider process-group ownership", err),
				"verify dispatch provider process-group ownership and prove group death",
				errors.Join(killErr, waitErr),
			)
		}
		return executionResult{}, wrapExitError(ExitTargetFailure, "verify dispatch provider process-group ownership", err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	started := time.Now().UTC()
	run.Record.LastActivityAt = &started
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
		termination.request()
		_, _ = reapDuringTermination(cmd, leaderStart, termination)
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		<-stdoutDrained
		<-stderrDrained
		terminationErr := termination.err
		if terminationErr == nil {
			terminationErr = termination.providerStopped()
		}
		if terminationErr != nil {
			return executionResult{}, newUnprovenProviderTerminationError(
				err,
				"terminate dispatch provider process group after running-state publication failure",
				terminationErr,
			)
		}
		return executionResult{}, errors.Join(err, terminationErr)
	}
	caughtSignal, stopForwarder := installProviderSignalForwarder(termination.request)
	defer stopForwarder()

	var result executionResult
	var pendingAnswer string
	var resultMu sync.Mutex
	var semanticErr error
	terminal := make(chan struct{}, 1)
	setFailure := func(err error) {
		if err == nil {
			return
		}
		resultMu.Lock()
		if semanticErr == nil {
			semanticErr = err
		}
		resultMu.Unlock()
		termination.request()
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
			if command.SessionID != "" && command.SessionID != event.SessionID {
				semanticErr = errors.New("provider returned a session ID different from the requested conversation")
				return semanticErr
			}
			result.SessionID = event.SessionID
			run.Record.ProviderSessionID = event.SessionID
			if err := persist(event.SessionID); err != nil {
				semanticErr = err
				return err
			}
		case eventAnswer:
			pendingAnswer = event.Answer
			result.AnswerSeen = true
			run.Record.LastOutputAt = &now
		case eventComplete:
			result.Complete = true
			select {
			case terminal <- struct{}{}:
			default:
			}
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
		err := readStructuredEventsWithLineage(stdoutPipe, providerStdout, command.Provider, command.SessionID, command.ClaudeLineage, func(event providerEvent) error {
			encoded, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := events.Write(append(encoded, '\n')); writeErr != nil {
				return writeErr
			}
			return consume(event)
		}, func(evidence claudeLineageEvidence) error {
			encoded, marshalErr := json.Marshal(evidence)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := lineage.Write(append(encoded, '\n')); writeErr != nil {
				return writeErr
			}
			activity := "claude_lineage_invalid"
			switch evidence.Kind {
			case lineageKindToolUse:
				activity = "claude_agent_tool_use"
			case lineageKindTaskStarted:
				activity = "claude_task_started"
			case lineageKindTaskTerminal:
				activity = "claude_task_" + evidence.Status
			}
			return consume(providerEvent{Kind: eventProgress, Activity: activity})
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

	streamResult, stderrResult, waitErr, terminationErr := awaitProviderCompletion(
		cmd, termination, leaderPID, leaderGroupID, leaderStart,
		stdoutPipe, stderrPipe, streamErr, stderrErr, terminal, setFailure,
	)
	signal := caughtSignal()
	resultMu.Lock()
	currentSemanticErr := semanticErr
	resultMu.Unlock()
	var primaryErr error
	switch {
	case signal != nil:
		if signal == os.Interrupt {
			primaryErr = exitError(ExitSigint, fmt.Sprintf("%s interrupted by signal SIGINT", command.Provider))
		} else {
			primaryErr = exitError(ExitSigterm, fmt.Sprintf("%s interrupted by signal SIGTERM", command.Provider))
		}
	case currentSemanticErr != nil:
		primaryErr = exitError(ExitTargetFailure, fmt.Sprintf("%s dispatch did not complete: %v", command.Provider, currentSemanticErr))
	case streamResult != nil:
		primaryErr = wrapExitError(ExitTargetFailure, fmt.Sprintf("capture dispatch provider output: %v", streamResult), streamResult)
	case stderrResult != nil:
		primaryErr = wrapExitError(ExitTargetFailure, fmt.Sprintf("capture dispatch provider diagnostics: %v", stderrResult), stderrResult)
	case waitErr != nil:
		primaryErr = providerWaitError(command.Provider, waitErr)
	}
	if terminationErr != nil {
		return executionResult{}, newUnprovenProviderTerminationError(primaryErr, "terminate dispatch provider process group", terminationErr)
	}
	if primaryErr != nil {
		return executionResult{}, primaryErr
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
	if !result.Complete || !result.AnswerSeen || result.SessionID == "" {
		return executionResult{}, exitError(ExitTargetFailure, fmt.Sprintf("%s dispatch completed without required terminal result, session ID, and final answer", command.Provider))
	}
	resultMu.Lock()
	terminalAnswer := pendingAnswer
	resultMu.Unlock()
	result.Answer = terminalAnswer
	return result, nil
}

type providerWaitState struct {
	cmd              *exec.Cmd
	termination      *providerTermination
	leader           RunRecord
	stdoutPipe       *os.File
	stderrPipe       *os.File
	reaped           bool
	waitErr          error
	terminationErr   error
	terminationDone  <-chan struct{}
	shutdownTimer    *time.Timer
	shutdownDeadline <-chan time.Time
}

func awaitProviderCompletion(
	cmd *exec.Cmd,
	termination *providerTermination,
	leaderPID, leaderGroupID int,
	leaderStart string,
	stdoutPipe, stderrPipe *os.File,
	streamErr, stderrErr chan error,
	terminal <-chan struct{},
	setFailure func(error),
) (streamResult, stderrResult, waitErr, terminationErr error) {
	wait := &providerWaitState{
		cmd:             cmd,
		termination:     termination,
		leader:          RunRecord{PID: leaderPID, ProcessGroupID: leaderGroupID, ProcessStartIdentity: leaderStart},
		stdoutPipe:      stdoutPipe,
		stderrPipe:      stderrPipe,
		terminationDone: termination.done,
	}
	startShutdownDeadline := func() {
		if wait.shutdownTimer == nil && (streamErr != nil || stderrErr != nil || !wait.reaped) {
			wait.shutdownTimer = time.NewTimer(providerShutdownGrace)
			wait.shutdownDeadline = wait.shutdownTimer.C
		}
	}
	defer func() {
		if wait.shutdownTimer != nil {
			wait.shutdownTimer.Stop()
		}
	}()
	waitPoll := time.NewTicker(providerTerminationPollInterval)
	defer waitPoll.Stop()
	for {
		wait.observeLeader(startShutdownDeadline)
		needReap := !wait.reaped && wait.terminationErr == nil && wait.cmd.Process != nil && wait.cmd.Process.Pid > 0
		if streamErr == nil && stderrErr == nil && !needReap && wait.terminationDone == nil {
			break
		}
		if streamErr == nil && stderrErr == nil && wait.reaped && wait.shutdownTimer != nil {
			// Process exit and I/O met their deadline. Group termination has
			// its own bounded grace/proof windows; do not spend this deadline
			// a second time on that independent cleanup phase.
			wait.shutdownTimer.Stop()
			wait.shutdownDeadline = nil
		}
		select {
		case <-terminal:
			startShutdownDeadline()
		case streamResult = <-streamErr:
			streamErr = nil
			startShutdownDeadline()
		case stderrResult = <-stderrErr:
			stderrErr = nil
		case <-wait.terminationDone:
			wait.terminationDone = nil
			wait.terminationErr = wait.termination.err
			startShutdownDeadline()
			if wait.terminationErr != nil {
				// Do not hang on an unkillable provider. Retain its active claim
				// through the unproven-termination error below.
				wait.abandonUnproven()
			}
		case <-wait.shutdownDeadline:
			wait.shutdownDeadline = nil
			setFailure(fmt.Errorf("provider did not exit and close output streams within %s of terminal evidence or shutdown", providerShutdownGrace))
			_ = wait.stdoutPipe.Close()
			_ = wait.stderrPipe.Close()
		case <-waitPoll.C:
		}
	}
	if wait.terminationErr == nil {
		wait.terminationErr = wait.termination.providerStopped()
	}
	return streamResult, stderrResult, wait.waitErr, wait.terminationErr
}

func (wait *providerWaitState) observeLeader(startShutdown func()) {
	if wait.reaped || wait.cmd.Process == nil || wait.cmd.Process.Pid <= 0 {
		return
	}
	if providerProcessGroupReused(wait.leader) {
		if wait.terminationErr == nil {
			wait.terminationErr = errProviderGroupIdentityMismatch
		}
		wait.terminationDone = nil
		releaseUnreapedProvider(wait.cmd)
		return
	}
	zombie := processIsZombie(wait.cmd.Process.Pid)
	groupDead := providerProcessGroupDead(wait.leader.ProcessGroupID)
	if !wait.termination.hasRequested() {
		switch {
		case zombie && !groupDead:
			wait.termination.request()
		case zombie || groupDead:
			wait.noteReap(reapOwnedProviderLeader(wait.cmd, wait.leader.ProcessStartIdentity))
			if wait.reaped {
				startShutdown()
			}
			if providerProcessGroupDead(wait.leader.ProcessGroupID) {
				wait.terminationDone = nil
			}
			return
		default:
			return
		}
	}
	wait.noteReap(reapOwnedProviderLeader(wait.cmd, wait.leader.ProcessStartIdentity))
	if wait.reaped {
		startShutdown()
	}
}

func (wait *providerWaitState) noteReap(done bool, err error) {
	if done {
		wait.reaped = true
		wait.waitErr = err
		return
	}
	if err != nil && wait.waitErr == nil {
		wait.waitErr = err
	}
}

func (wait *providerWaitState) abandonUnproven() {
	if !wait.reaped {
		done, err := reapOwnedProviderLeader(wait.cmd, wait.leader.ProcessStartIdentity)
		if done {
			wait.reaped = true
			wait.waitErr = err
		} else {
			if err != nil && wait.waitErr == nil {
				wait.waitErr = err
			}
			releaseUnreapedProvider(wait.cmd)
		}
	}
	_ = wait.stdoutPipe.Close()
	_ = wait.stderrPipe.Close()
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
