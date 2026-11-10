package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// The view cursor and turn identity are provider-authoritative, point-in-time
// evidence, obtained without attaching or submitting a turn. A completed last
// turn cannot authorize an unlinked terminal in a new invocation.
type museRecoveryBaseline struct {
	SessionID      string    `json:"session_id"`
	RootID         string    `json:"root_id,omitempty"`
	ViewCursor     string    `json:"view_cursor"`
	ObservedAt     time.Time `json:"observed_at"`
	Source         string    `json:"source,omitempty"`
	ActiveAtRead   bool      `json:"active_at_read,omitempty"`
	PreviousRunID  string    `json:"previous_invocation_id,omitempty"`
	RootCursor     string    `json:"root_cursor,omitempty"`
	RootSequence   string    `json:"root_sequence,omitempty"`
	TerminalRootID string    `json:"terminal_root_id,omitempty"`
	TerminalState  string    `json:"terminal_state,omitempty"`

	verifyRoot func(string) (*museRecoveryBaseline, error) `json:"-"`
}

// museBaselineAttemptTimeout bounds one pre-launch `muse serve` baseline query.
// It is separate from providerVersionTimeout: Muse occasionally starts its host
// and then never answers the handshake (#526).
const museBaselineAttemptTimeout = 30 * time.Second

// museBaselineAttempts includes one retry of a hung pre-launch host. The query
// never mutates the session, so a fresh host is safe.
const museBaselineAttempts = 2

// museResumedRootTimeout bounds the single in-stream resumed-root check. That
// check runs inside the provider's stdout event handler, so it keeps the short
// version-check bound and never retries. A var so tests can shrink it.
var museResumedRootTimeout = providerVersionTimeout

// museBaselineStderrTail is how much of the host's stderr is scanned for the
// last line; museBaselineStderrLineLimit bounds the line kept in errors.
const (
	museBaselineStderrTail      = 4096
	museBaselineStderrLineLimit = 300
)

// museStderrDrainGrace bounds the wait for host stderr to reach EOF after the
// host is reaped. A descendant still holding it then gets EPIPE on its next
// write instead of unbounded output. A var so tests can pin it.
var museStderrDrainGrace = 200 * time.Millisecond

// museBaselineRead is the attempt policy for one baseline read.
type museBaselineRead struct {
	attempts int
	timeout  time.Duration
	// stop, when set, is checked before a retry; a non-nil error ends the read.
	stop func() error
	// logRetry, when set, records a retried attempt.
	logRetry func(string)
}

// museBaselineTimeoutError is a host that started but never answered a step.
// It is the only failure that is retried: early exits, closed pipes, and
// protocol answers repeat on a fresh host.
type museBaselineTimeoutError struct {
	step     string
	timeout  time.Duration
	attempts int
	stderr   string
}

func (e *museBaselineTimeoutError) Error() string {
	message := fmt.Sprintf("muse serve did not answer %s within %s", e.step, e.timeout)
	if e.attempts > 1 {
		message += fmt.Sprintf(" (%d attempts)", e.attempts)
	}
	return message + museStderrDetail(e.stderr)
}

// museBaselineStepError names the protocol step whose response never arrived.
type museBaselineStepError struct {
	step string
	err  error
}

func (e *museBaselineStepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *museBaselineStepError) Unwrap() error { return e.err }

func readMuseRecoveryBaseline(command providerCommand, root string, read museBaselineRead) (*museRecoveryBaseline, error) {
	var earlier *museBaselineTimeoutError
	for attempt := 1; ; attempt++ {
		baseline, err := readMuseRecoveryBaselineOnce(command, root, read.timeout)
		var timeout *museBaselineTimeoutError
		if err == nil {
			return baseline, nil
		}
		if !errors.As(err, &timeout) {
			if earlier != nil {
				return nil, fmt.Errorf("%w (after attempt %d timed out waiting for %s)", err, attempt-1, earlier.step)
			}
			return nil, err
		}
		if attempt >= read.attempts {
			timeout.attempts = attempt
			return nil, timeout
		}
		if read.stop != nil {
			if stopErr := read.stop(); stopErr != nil {
				return nil, stopErr
			}
		}
		if read.logRetry != nil {
			read.logRetry(fmt.Sprintf("agent-layer: Muse recovery baseline attempt %d failed, retrying with a fresh host: %v", attempt, err))
		}
		earlier = timeout
	}
}

func readMuseRecoveryBaselineOnce(command providerCommand, root string, timeout time.Duration) (*museRecoveryBaseline, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Stderr is an *os.File pipe, so exec starts no copy goroutine and a host
	// descendant holding it cannot delay Wait; this read drains it instead.
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() { _ = stderrRead.Close() }()
	defer func() { _ = stderrWrite.Close() }()
	// The deadline is enforced below rather than by the command's context, so
	// the protocol pipes are always closed before the host is killed.
	// #nosec G204 -- already resolved, version-checked provider executable.
	cmd := exec.CommandContext(context.Background(), command.Path, "serve")
	cmd.Dir = command.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = command.Env
	cmd.WaitDelay = time.Second
	cmd.Stderr = stderrWrite
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_ = stderrWrite.Close()
	stderr := &museStderrTail{}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		stderr.drain(stderrRead)
	}()
	// On the deadline, close both protocol pipes even if a host descendant
	// keeps them open. Only an I/O failed by that close is a timeout; an EOF
	// that arrived first is a host that exited on its own.
	var deadlineClosed atomic.Bool
	done, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			deadlineClosed.Store(true)
			_ = stdout.Close()
			_ = stdin.Close()
		case <-done:
		}
	}()
	baseline, err := queryMuseRecoveryBaseline(json.NewEncoder(stdin), json.NewDecoder(stdout), command.SessionID)
	close(done)
	<-watched
	timedOut := deadlineClosed.Load() && errors.Is(err, os.ErrClosed)
	elapsed := time.Since(started).Round(time.Millisecond)
	_ = cmd.Process.Kill()
	_ = stdin.Close()
	_ = stdout.Close()
	_ = cmd.Wait()
	// A descendant may still hold stderr; take what arrived within the grace.
	select {
	case <-drained:
	case <-time.After(museStderrDrainGrace):
	}
	_ = stderrRead.Close()
	<-drained
	if err == nil {
		return baseline, nil
	}
	step := "the recovery baseline query"
	var stepErr *museBaselineStepError
	if errors.As(err, &stepErr) {
		step = stepErr.step
	}
	line := sanitizeMuseStderrLine(stderr.bytes())
	if timedOut {
		return nil, &museBaselineTimeoutError{step: step, timeout: timeout, stderr: line}
	}
	if isMuseTransportError(err) {
		// Report the host's own exit code; a kill by this read's cleanup is not one.
		status := ""
		if state := cmd.ProcessState; state != nil && state.Exited() {
			status = fmt.Sprintf(" (exit status %d)", state.ExitCode())
		}
		return nil, fmt.Errorf("muse serve stopped before answering %s after %s%s: %w%s", step, elapsed, status, err, museStderrDetail(line))
	}
	return nil, err
}

// isMuseTransportError reports a host that exited or closed its pipes, as
// opposed to a protocol answer such as an RPC error or a mismatched session.
func isMuseTransportError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EPIPE)
}

func museStderrDetail(line string) string {
	if line == "" {
		return ""
	}
	return "; muse serve stderr: " + line
}

// museStderrTail keeps the last museBaselineStderrTail bytes of the host's
// stderr. It trims only past twice that size, so writes do not shift it each
// time. Only the drain goroutine writes it; it is read after that goroutine ends.
type museStderrTail struct{ data []byte }

func (t *museStderrTail) drain(r io.Reader) {
	buffer := make([]byte, museBaselineStderrTail)
	for {
		n, err := r.Read(buffer)
		t.data = append(t.data, buffer[:n]...)
		if len(t.data) > 2*museBaselineStderrTail {
			t.data = append(t.data[:0], t.data[len(t.data)-museBaselineStderrTail:]...)
		}
		if err != nil {
			return
		}
	}
}

func (t *museStderrTail) bytes() []byte {
	return t.data[max(len(t.data)-museBaselineStderrTail, 0):]
}

// sanitizeMuseStderrLine returns the last non-empty line, bounded and made
// single-line, for use in an error.
func sanitizeMuseStderrLine(tail []byte) string {
	lines := strings.Split(strings.ToValidUTF8(string(tail), "�"), "\n")
	line := ""
	for i := len(lines) - 1; i >= 0 && line == ""; i-- {
		line = singleLineMuseText(lines[i])
	}
	if len(line) <= museBaselineStderrLineLimit {
		return line
	}
	cut := museBaselineStderrLineLimit
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "..."
}

// singleLineMuseText replaces control, line/paragraph separator, and format
// (including bidi override) runes with spaces, so provider text cannot split
// or visually reorder a single-line error.
func singleLineMuseText(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf) {
			return ' '
		}
		return r
	}, text))
}

func queryMuseRecoveryBaseline(encoder *json.Encoder, decoder *json.Decoder, sessionID string) (*museRecoveryBaseline, error) {
	if err := initializeMuseRPC(encoder, decoder); err != nil {
		return nil, &museBaselineStepError{step: "initialize", err: err}
	}
	if err := encoder.Encode(map[string]any{
		jsonRPCKey: jsonRPCVersion, "id": 2, jsonMethodKey: museSessionRead,
		jsonParamsKey: map[string]any{jsonSessionIDCamelKey: sessionID, "excludeItems": true},
	}); err != nil {
		return nil, &museBaselineStepError{step: museSessionRead, err: err}
	}
	frame, err := readMuseRPCResponse(decoder, encoder, 2)
	if err != nil {
		return nil, &museBaselineStepError{step: museSessionRead, err: err}
	}
	var result struct {
		Session struct {
			ID           string `json:"sessionId"`
			ActiveTurnID string `json:"activeTurnId"`
		} `json:"session"`
		LastTurn struct {
			TurnID   string `json:"turnId"`
			Terminal string `json:"terminal"`
		} `json:"lastTurn"`
		ViewCursor string `json:"viewCursor"`
	}
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		return nil, err
	}
	if result.Session.ID != sessionID {
		return nil, errors.New("muse recovery baseline omitted the expected session")
	}
	switch result.LastTurn.Terminal {
	case dispatchStateCompleted, dispatchStateFailed, dispatchStateCancelled:
	default:
		result.LastTurn.Terminal, result.LastTurn.TurnID = "", ""
	}
	id := result.Session.ActiveTurnID
	if id == "" && (result.LastTurn.Terminal == "failed" || result.LastTurn.Terminal == "cancelled") {
		id = result.LastTurn.TurnID
	}
	return &museRecoveryBaseline{SessionID: sessionID, RootID: id, ViewCursor: result.ViewCursor, ObservedAt: time.Now().UTC(), Source: museSessionRead, ActiveAtRead: result.Session.ActiveTurnID != "", TerminalRootID: result.LastTurn.TurnID, TerminalState: result.LastTurn.Terminal}, nil
}

func initializeMuseRPC(encoder *json.Encoder, decoder *json.Decoder) error {
	if err := encoder.Encode(map[string]any{
		jsonRPCKey: jsonRPCVersion, "id": 1, jsonMethodKey: "initialize",
		jsonParamsKey: map[string]any{"clientInfo": map[string]any{"name": "agent_layer", museRPCVersionKey: "1"}, "capabilities": map[string]any{"userInputDialogs": false}},
	}); err != nil {
		return fmt.Errorf("initialize Muse session observer: %w", err)
	}
	frame, err := readMuseRPCResponse(decoder, encoder, 1)
	if err != nil {
		return err
	}
	var initialized struct {
		Schema struct {
			Version int `json:"version"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(frame.Result, &initialized); err != nil || initialized.Schema.Version != 1 {
		return errors.New("initialize Muse session observer: unsupported protocol response")
	}
	return encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, jsonMethodKey: "initialized", jsonParamsKey: map[string]any{}})
}

// The view fold can lag runtime linkage after a crash. Retained root evidence
// from this conversation's stopped, failed invocation supplies that missing
// baseline. It never authorizes an arbitrary root solely by session equality.
func resolveMuseRecoveryBaseline(root, previousID string, baseline *museRecoveryBaseline) (*museRecoveryBaseline, error) {
	if previousID != "" {
		previous, err := loadRunRecord(root, previousID)
		if err != nil {
			return nil, err
		}
		if previous.Agent != AgentMuse || previous.ProviderSessionID != baseline.SessionID {
			return nil, errors.New("muse previous invocation belongs to another provider or session")
		}
		if !previous.TerminationConfirmed {
			return nil, errors.New("muse previous invocation termination is unconfirmed")
		}
		if previous.State == dispatchStateFailed || previous.State == dispatchStateInterrupted || previous.State == dispatchStateCancelled {
			id, cursor, sequence := previous.MuseRootID, previous.MuseRootCursor, previous.MuseRootSequence
			if id == "" && previous.ProviderLaunchIntent {
				// Old records predate normalized root provenance. Replay only the owned
				// captured stream with the same lineage validation used for live stdout.
				file, err := openOwnedRegularFile(filepathForRun(root, previousID), previous.StdoutPath)
				if err != nil {
					return nil, err
				}
				replayErr := readStructuredEventsWithLineage(file, io.Discard, AgentMuse, baseline.SessionID, false, func(event providerEvent) error {
					if event.Kind == eventSession && event.RootID != "" {
						id, cursor, sequence = event.RootID, event.Cursor, event.Sequence
					}
					if event.Kind == eventFailure {
						return errors.New("retained Muse stream ended at rejection")
					}
					return nil
				}, nil)
				_ = file.Close()
				if replayErr != nil {
					return nil, fmt.Errorf("replay retained Muse stream: %w", replayErr)
				}
			}
			if id != "" {
				if baseline.ActiveAtRead && baseline.RootID != id {
					return nil, errors.New("muse active snapshot conflicts with the previous owned root")
				}
				baseline.RootID, baseline.RootCursor, baseline.RootSequence = id, cursor, sequence
				baseline.Source = "previous_invocation"
				baseline.PreviousRunID = previousID
			}
		}
	}
	if baseline.RootID == "" && baseline.ViewCursor == "" {
		return nil, errors.New("muse session has no authoritative root or view cursor baseline")
	}
	return baseline, nil
}

// A resumed exec can submit a new turn yet omit command/root announcements on
// stdout. Require its earlier task linkage AND an independent, advanced session
// view naming that exact root terminal, never just an equal session ID.
func verifyMuseResumedRoot(command providerCommand, root string, baseline *museRecoveryBaseline, id string) (*museRecoveryBaseline, error) {
	observed, err := readMuseRecoveryBaseline(command, root, museBaselineRead{attempts: 1, timeout: museResumedRootTimeout})
	if err != nil {
		return nil, err
	}
	if observed.TerminalRootID != id || observed.ViewCursor == "" || observed.ViewCursor == baseline.ViewCursor {
		return nil, errors.New("muse resumed root lacks a new authoritative terminal cursor")
	}
	switch observed.TerminalState {
	case dispatchStateCompleted, dispatchStateFailed, dispatchStateCancelled:
	default:
		return nil, errors.New("muse resumed root snapshot has no terminal state")
	}
	observed.RootID = id
	observed.Source = museRecoveredRoot
	return observed, nil
}
