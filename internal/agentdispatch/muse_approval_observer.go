package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	museRPCVersionKey        = "version"
	museApprovalMinPollDelay = 250 * time.Millisecond
	museApprovalMaxPollDelay = 5 * time.Second
)

// museObserverReadyTimeout bounds the initialize handshake with `muse serve`.
// Without it a started-but-silent observer host blocks dispatch start forever.
// A var so tests can shrink the bound without waiting out the production value.
var museObserverReadyTimeout = 30 * time.Second

type museApprovalObserver struct {
	cancel context.CancelFunc
	done   chan struct{}
	ready  chan error
	mu     sync.Mutex
	err    error
}

type museRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Kind string `json:"kind"`
	} `json:"data"`
}

func (err *museRPCError) Error() string {
	return fmt.Sprintf("MSP error %d: %s", err.Code, err.Message)
}

type museRPCFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *museRPCError   `json:"error"`
}

type musePendingRequests struct {
	Approvals []struct {
		ApprovalID string `json:"approvalId"`
		ToolName   string `json:"toolName"`
	} `json:"approvals"`
	UserInputs []struct {
		UserInputID string `json:"userInputId"`
	} `json:"userInputs"`
}

type musePendingRequestIDs struct {
	approvals  map[string]struct{}
	userInputs map[string]struct{}
}

func startMuseApprovalObserver(command providerCommand, root string) (*museApprovalObserver, error) {
	if !command.ObserveMuseApprovals {
		return nil, nil
	}
	if command.Provider != AgentMuse || command.SessionID == "" {
		return nil, errors.New("muse approval observer requires a Muse command with a session ID")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// #nosec G204 -- command.Path is the already version-checked Muse target binary.
	cmd := exec.CommandContext(ctx, command.Path, "serve")
	// Bound Wait after the host exits or is killed: orphaned host children can
	// keep the observer pipes open, and without this the error-path wait below
	// hangs the same way the handshake once did. This complements the explicit
	// pipe closes on the readiness-timeout path.
	cmd.WaitDelay = providerShutdownGrace
	cmd.Dir = command.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = command.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open Muse approval observer stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open Muse approval observer stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start Muse approval observer: %w", err)
	}
	observer := &museApprovalObserver{cancel: cancel, done: make(chan struct{}), ready: make(chan error, 1)}
	go func() {
		ready := false
		reportReady := func(err error) {
			if ready {
				return
			}
			ready = true
			observer.ready <- err
		}
		protocolErr := observeMuseApprovals(ctx, stdin, stdout, command.SessionID, command.RunMode, reportReady)
		reportReady(protocolErr)
		stopped := ctx.Err() != nil
		if protocolErr != nil && !stopped {
			// The observer host is otherwise long-lived. Stop it before waiting
			// when the protocol has reached a terminal finding or failure.
			cancel()
		}
		waitErr := cmd.Wait()
		if stopped {
			observer.finish(nil)
			return
		}
		if protocolErr != nil {
			observer.finish(protocolErr)
			return
		}
		if waitErr != nil {
			observer.finish(fmt.Errorf("muse approval observer exited before dispatch completed: %w", waitErr))
		} else {
			observer.finish(errors.New("muse approval observer exited before dispatch completed"))
		}
	}()
	select {
	case err := <-observer.ready:
		if err != nil {
			cancel()
			_ = observer.wait()
			return nil, err
		}
	case <-time.After(museObserverReadyTimeout):
		cancel()
		// Close the pipes as well as killing the host: orphaned host
		// children can keep the pipe write ends open, which would leave
		// the goroutine blocked in Decode and wait() hung in turn.
		_ = stdin.Close()
		_ = stdout.Close()
		_ = observer.wait()
		return nil, fmt.Errorf("muse approval observer did not complete initialization within %s", museObserverReadyTimeout)
	}
	return observer, nil
}

func (observer *museApprovalObserver) finish(err error) {
	observer.mu.Lock()
	observer.err = err
	observer.mu.Unlock()
	close(observer.done)
}

func (observer *museApprovalObserver) wait() error {
	if observer == nil {
		return nil
	}
	<-observer.done
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return observer.err
}

func (observer *museApprovalObserver) stop() error {
	if observer == nil {
		return nil
	}
	observer.cancel()
	return observer.wait()
}

func observeMuseApprovals(ctx context.Context, stdin io.Writer, stdout io.Reader, sessionID, mode string, reportReady func(error)) error {
	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(stdout)
	if err := encoder.Encode(map[string]any{
		jsonRPCKey:    jsonRPCVersion,
		"id":          1,
		jsonMethodKey: "initialize",
		jsonParamsKey: map[string]any{
			"clientInfo":   map[string]any{"name": "agent_layer", museRPCVersionKey: "1"},
			"capabilities": map[string]any{"userInputDialogs": false},
		},
	}); err != nil {
		return fmt.Errorf("initialize Muse approval observer: %w", err)
	}
	frame, err := readMuseRPCResponse(decoder, encoder, 1)
	if err != nil {
		return fmt.Errorf("initialize Muse approval observer: %w", err)
	}
	var initialized struct {
		Schema struct {
			Version int `json:"version"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(frame.Result, &initialized); err != nil || initialized.Schema.Version != 1 {
		return errors.New("initialize Muse approval observer: unsupported protocol response")
	}
	if err := encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, jsonMethodKey: "initialized", jsonParamsKey: map[string]any{}}); err != nil {
		return fmt.Errorf("complete Muse approval observer initialization: %w", err)
	}

	requestID := 2
	sessionObserved := false
	baseline := musePendingRequestIDs{approvals: map[string]struct{}{}, userInputs: map[string]struct{}{}}
	lastLatency := time.Duration(0)
	if mode == dispatchModeResume {
		pending, latency, err := queryMusePendingRequests(encoder, decoder, sessionID, requestID)
		requestID++
		lastLatency = latency
		if err != nil {
			var rpcErr *museRPCError
			if !errors.As(err, &rpcErr) || rpcErr.Data.Kind != "sessionNotFound" {
				return fmt.Errorf("query Muse pending approvals baseline: %w", err)
			}
		} else {
			sessionObserved = true
			var baselineErr error
			baseline, baselineErr = pending.requestIDs()
			if baselineErr != nil {
				return fmt.Errorf("decode Muse pending approvals baseline: %w", baselineErr)
			}
		}
	}
	reportReady(nil)

	for {
		if !waitMuseApprovalPoll(ctx, museApprovalPollDelay(lastLatency)) {
			return nil
		}
		pending, latency, err := queryMusePendingRequests(encoder, decoder, sessionID, requestID)
		requestID++
		lastLatency = latency
		if err != nil {
			var rpcErr *museRPCError
			// The observer may initialize before exec creates the session. This
			// read-only missing-session response is expected only until it exists.
			if !sessionObserved && errors.As(err, &rpcErr) && rpcErr.Data.Kind == "sessionNotFound" {
				continue
			}
			return fmt.Errorf("query Muse pending approvals: %w", err)
		}
		sessionObserved = true
		for _, userInput := range pending.UserInputs {
			if userInput.UserInputID == "" {
				return errors.New("muse pending-request response contained user input without an ID")
			}
			if _, stale := baseline.userInputs[userInput.UserInputID]; !stale {
				return fmt.Errorf("muse dispatch is waiting for user input %s", userInput.UserInputID)
			}
		}
		for _, approval := range pending.Approvals {
			if approval.ApprovalID == "" {
				return errors.New("muse pending-request response contained approval without an ID")
			}
			if _, stale := baseline.approvals[approval.ApprovalID]; stale {
				continue
			}
			if approval.ToolName == "" {
				return fmt.Errorf("muse dispatch is waiting for human approval %s", approval.ApprovalID)
			}
			return fmt.Errorf("muse dispatch is waiting for human approval %s for tool %s", approval.ApprovalID, approval.ToolName)
		}
	}
}

func queryMusePendingRequests(encoder *json.Encoder, decoder *json.Decoder, sessionID string, requestID int) (musePendingRequests, time.Duration, error) {
	started := time.Now()
	if err := encoder.Encode(map[string]any{
		jsonRPCKey:    jsonRPCVersion,
		"id":          requestID,
		jsonMethodKey: "approval/listPending",
		jsonParamsKey: map[string]any{jsonSessionIDCamelKey: sessionID},
	}); err != nil {
		return musePendingRequests{}, time.Since(started), err
	}
	frame, err := readMuseRPCResponse(decoder, encoder, requestID)
	latency := time.Since(started)
	if err != nil {
		return musePendingRequests{}, latency, err
	}
	var pending musePendingRequests
	if err := json.Unmarshal(frame.Result, &pending); err != nil {
		return musePendingRequests{}, latency, err
	}
	if pending.Approvals == nil || pending.UserInputs == nil {
		return musePendingRequests{}, latency, errors.New("muse pending-request response omitted approvals or userInputs arrays")
	}
	return pending, latency, nil
}

func (pending musePendingRequests) requestIDs() (musePendingRequestIDs, error) {
	ids := musePendingRequestIDs{approvals: map[string]struct{}{}, userInputs: map[string]struct{}{}}
	for _, approval := range pending.Approvals {
		if approval.ApprovalID == "" {
			return musePendingRequestIDs{}, errors.New("muse pending-request response contained approval without an ID")
		}
		ids.approvals[approval.ApprovalID] = struct{}{}
	}
	for _, userInput := range pending.UserInputs {
		if userInput.UserInputID == "" {
			return musePendingRequestIDs{}, errors.New("muse pending-request response contained user input without an ID")
		}
		ids.userInputs[userInput.UserInputID] = struct{}{}
	}
	return ids, nil
}

func museApprovalPollDelay(latency time.Duration) time.Duration {
	delay := 10 * latency
	if delay < museApprovalMinPollDelay {
		return museApprovalMinPollDelay
	}
	if delay > museApprovalMaxPollDelay {
		return museApprovalMaxPollDelay
	}
	return delay
}

func waitMuseApprovalPoll(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func readMuseRPCResponse(decoder *json.Decoder, encoder *json.Encoder, requestID int) (museRPCFrame, error) {
	wantID, _ := json.Marshal(requestID)
	for {
		var frame museRPCFrame
		if err := decoder.Decode(&frame); err != nil {
			return museRPCFrame{}, err
		}
		if frame.Method == "approval/request" || frame.Method == "userInput/request" {
			if len(frame.ID) > 0 {
				if err := encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, "id": frame.ID, jsonResultKey: map[string]any{}}); err != nil {
					return museRPCFrame{}, err
				}
			}
			return museRPCFrame{}, fmt.Errorf("muse dispatch is waiting for a human: %s", frame.Method)
		}
		if string(frame.ID) != string(wantID) {
			continue
		}
		if frame.Error != nil {
			return museRPCFrame{}, frame.Error
		}
		if len(frame.Result) == 0 {
			return museRPCFrame{}, errors.New("MSP response omitted result")
		}
		return frame, nil
	}
}
