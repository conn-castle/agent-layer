package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
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

func readMuseRecoveryBaseline(command providerCommand, root string) (*museRecoveryBaseline, error) {
	ctx, cancel := context.WithTimeout(context.Background(), providerVersionTimeout)
	defer cancel()
	// #nosec G204 -- already resolved, version-checked provider executable.
	cmd := exec.CommandContext(ctx, command.Path, "serve")
	cmd.Dir = command.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = command.Env
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { cancel(); _ = stdin.Close(); _ = stdout.Close(); _ = cmd.Wait() }()
	// Close the decoder pipe on timeout even if a host descendant keeps it open.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = stdout.Close()
		case <-done:
		}
	}()
	return queryMuseRecoveryBaseline(json.NewEncoder(stdin), json.NewDecoder(stdout), command.SessionID)
}

func queryMuseRecoveryBaseline(encoder *json.Encoder, decoder *json.Decoder, sessionID string) (*museRecoveryBaseline, error) {
	if err := initializeMuseRPC(encoder, decoder); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{
		jsonRPCKey: jsonRPCVersion, "id": 2, jsonMethodKey: museSessionRead,
		jsonParamsKey: map[string]any{jsonSessionIDCamelKey: sessionID, "excludeItems": true},
	}); err != nil {
		return nil, err
	}
	frame, err := readMuseRPCResponse(decoder, encoder, 2)
	if err != nil {
		return nil, err
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
				_ = readStructuredEventsWithLineage(file, io.Discard, AgentMuse, baseline.SessionID, false, func(event providerEvent) error {
					if event.Kind == eventSession && event.RootID != "" {
						id, cursor, sequence = event.RootID, event.Cursor, event.Sequence
					}
					if event.Kind == eventFailure {
						return errors.New("retained Muse stream ended at rejection")
					}
					return nil
				}, nil)
				_ = file.Close()
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
	observed, err := readMuseRecoveryBaseline(command, root)
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
