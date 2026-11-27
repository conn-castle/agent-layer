package agentdispatch

import (
	"bufio"
	"bytes"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/conn-castle/agent-layer/internal/clients/grok"
)

const (
	acpProtocolVersion        = 1
	acpHandshakeTimeout       = 30 * time.Second
	acpMCPVerifyTimeout       = 30 * time.Second
	acpConfigRequestTimeout   = 15 * time.Second
	acpPromptTimeout          = 24 * time.Hour
	acpWriteDeadline          = 30 * time.Second
	acpMaxFrameBytes          = 16 * 1024 * 1024
	acpMaxCachedToolCalls     = 256
	acpCancelPollInterval     = 200 * time.Millisecond
	acpClientName             = "agent-layer"
	acpClientVersion          = "1.0"
	acpStopReasonEndTurn      = "end_turn"
	acpMethodInitialize       = "initialize"
	acpMethodAuthenticate     = "authenticate"
	acpRPCAuthRequired        = -32000
	acpMethodSessionNew       = "session/new"
	acpMethodSessionResume    = "session/resume"
	acpMethodSessionSetModel  = "session/set_model"
	acpMethodSessionSetConfig = "session/set_config_option"
	acpMethodSessionPrompt    = "session/prompt"
	acpMethodSessionCancel    = "session/cancel"
	acpMethodSessionUpdate    = "session/update"
	acpMethodRequestPerm      = "session/request_permission"
	acpGrokMCPInitMethod      = "_x.ai/mcp_initialized"
	acpGrokMCPStatusMethod    = "_x.ai/mcp/server_status"
	jsonMCPServersKey         = "mcpServers"
	jsonPromptKey             = "prompt"
	jsonCommandKey            = "command"
	jsonOutcomeKey            = "outcome"
)

type acpMCPServerSpec struct {
	Name    string      `json:"name"`
	Command string      `json:"command"`
	Args    []string    `json:"args"`
	Env     []acpEnvVar `json:"env"`
}

type acpEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type acpJSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *acpRPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != 0 {
		return fmt.Sprintf("ACP error %d: %s", e.Code, e.Message)
	}
	return e.Message
}

// acpConfigError identifies configuration failures detected by the ACP adapter.
// Its wrapped ExitError also exposes the exit code to callers of the adapter.
type acpConfigError struct{ *ExitError }

func (e *acpConfigError) Unwrap() error { return e.ExitError }

type acpIncomingFrame struct {
	msg acpJSONRPCMessage
	err error
}

type acpClient struct {
	stdin            io.WriteCloser
	writeMu          sync.Mutex
	frames           <-chan acpIncomingFrame
	nextID           int64
	provider         string
	cfg              acpLaunchConfig
	root             string
	runID            string
	runDir           string
	consume          func(providerEvent) error
	answer           strings.Builder
	answerTruncated  bool
	sawToolSinceText bool
	deniedTools      []string
	toolCalls        map[string]*list.Element
	toolCallOrder    *list.List
	grokMCPReady     map[string]acpGrokMCPStatus
	grokMCPDone      bool
	grokMCPDoneAt    time.Time
	sessionID        string
	promptActive     bool
	cancelled        bool
}

type acpCachedToolEntry struct {
	id   string
	tool acpPermissionTool
}

type acpGrokMCPStatus struct {
	Status string
	Reason string
}

func buildACPMCPServerSpecs(runDir string, staged stagedSessionMCP, servers []SessionMCPServer) []acpMCPServerSpec {
	normalized := normalizeSessionMCPServers(servers)
	if len(normalized) == 0 {
		return []acpMCPServerSpec{}
	}
	specs := make([]acpMCPServerSpec, 0, len(normalized))
	for _, srv := range normalized {
		specs = append(specs, acpMCPServerSpec{
			Name:    srv.Name,
			Command: staged.Executable,
			Args:    sessionMCPWrapperArgsWithSpec(runDir, srv.Name, staged.SpecPath, staged.Digest, true),
			Env:     []acpEnvVar{},
		})
	}
	return specs
}

// runACPDispatch executes one full ACP v1 dispatch invocation over stdin/stdout,
// translating ACP lifecycle, streaming updates, permission callbacks, and token
// usage into Agent Dispatch's providerEvent contract.
func runACPDispatch(
	stdin io.WriteCloser,
	stdout io.Reader,
	rawWriter io.Writer,
	command providerCommand,
	prompt []byte,
	root string,
	run *dispatchRun,
	consume func(providerEvent) error,
) error {
	if command.ACP == nil {
		return &acpConfigError{exitError(ExitConfig, "ACP dispatch command missing ACP configuration")}
	}
	if command.RunMode == dispatchModeResume && command.SessionID == "" {
		return &acpConfigError{exitError(ExitConfig, fmt.Sprintf("%s ACP resume requires a provider session ID", command.Provider))}
	}
	frames := make(chan acpIncomingFrame, 64)
	stopReader := make(chan struct{})
	readerDone := make(chan struct{})
	go readACPFrames(stdout, rawWriter, frames, stopReader, readerDone)

	client := &acpClient{
		stdin:         stdin,
		frames:        frames,
		provider:      command.Provider,
		cfg:           *command.ACP,
		root:          root,
		runID:         run.Record.ID,
		runDir:        run.Dir,
		consume:       consume,
		toolCalls:     make(map[string]*list.Element),
		toolCallOrder: list.New(),
		grokMCPReady:  make(map[string]acpGrokMCPStatus),
	}

	defer func() {
		_ = stdin.Close()
		close(stopReader)
	}()

	initResp, err := client.call(acpMethodInitialize, map[string]any{
		"protocolVersion":    acpProtocolVersion,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    acpClientName,
			"version": acpClientVersion,
		},
	}, acpHandshakeTimeout)
	if err != nil {
		return fmt.Errorf("%s ACP initialize failed: %w", command.Provider, err)
	}
	var initParsed struct {
		ProtocolVersion *int `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initResp, &initParsed); err != nil {
		return fmt.Errorf("%s ACP initialize response malformed: %w", command.Provider, err)
	}
	if initParsed.ProtocolVersion == nil {
		return fmt.Errorf("%s ACP initialize response omitted protocolVersion (want %d)", command.Provider, acpProtocolVersion)
	}
	if *initParsed.ProtocolVersion != acpProtocolVersion {
		return fmt.Errorf("%s ACP initialize negotiated unsupported protocolVersion %d (want %d)", command.Provider, *initParsed.ProtocolVersion, acpProtocolVersion)
	}

	if client.cfg.Authenticate {
		if err := client.authenticate(initResp); err != nil {
			return fmt.Errorf("%s ACP authenticate failed: %w", command.Provider, err)
		}
	}

	sessionID, err := client.openSession(command)
	if err != nil {
		return err
	}
	client.sessionID = sessionID
	if err := client.emit(providerEvent{Kind: eventSession, SessionID: sessionID}); err != nil {
		return err
	}

	if err := client.configureSession(command, sessionID); err != nil {
		return err
	}

	if len(client.cfg.SessionMCPServers) > 0 {
		if err := client.verifySessionMCPReady(sessionID); err != nil {
			_ = client.emit(providerEvent{Kind: eventFailure, Reason: err.Error()})
			return err
		}
	}

	promptResp, err := client.callPrompt(sessionID, string(prompt))
	if err != nil {
		if !client.cancelled {
			_ = client.emit(providerEvent{Kind: eventFailure, Reason: err.Error()})
		}
		return err
	}

	if err := client.finishPrompt(promptResp); err != nil {
		return err
	}
	// Close stdin to signal clean ACP shutdown and drain frames until EOF so
	// trailing read/decode errors are surfaced and readerDone finishes cleanly.
	_ = stdin.Close()
	drainTimer := time.NewTimer(providerShutdownGrace)
	defer drainTimer.Stop()
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				<-readerDone
				return nil
			}
			if frame.err != nil {
				<-readerDone
				return frame.err
			}
		case <-drainTimer.C:
			return fmt.Errorf("%s ACP stdout did not close within %s after prompt completion", command.Provider, providerShutdownGrace)
		}
	}
}

func readACPFrames(stdout io.Reader, rawWriter io.Writer, out chan<- acpIncomingFrame, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	defer close(out)
	send := func(frame acpIncomingFrame) bool {
		select {
		case out <- frame:
			return true
		case <-stop:
			return false
		}
	}
	reader := bufio.NewReaderSize(io.TeeReader(stdout, rawWriter), structuredJSONBufferBytes)
	var lineBuf bytes.Buffer
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if len(chunk) > 0 {
			if lineBuf.Len()+len(chunk) > acpMaxFrameBytes {
				send(acpIncomingFrame{err: fmt.Errorf("ACP JSON-RPC frame exceeded %d byte limit", acpMaxFrameBytes)})
				return
			}
			lineBuf.Write(chunk)
		}
		if isPrefix {
			if err != nil {
				if !errors.Is(err, io.EOF) {
					send(acpIncomingFrame{err: err})
				}
				return
			}
			continue
		}
		if lineBuf.Len() > 0 {
			trimmed := bytes.TrimSpace(lineBuf.Bytes())
			if len(trimmed) > 0 {
				var msg acpJSONRPCMessage
				if unmarshalErr := json.Unmarshal(trimmed, &msg); unmarshalErr != nil {
					send(acpIncomingFrame{err: fmt.Errorf("decode ACP JSON-RPC frame: %w", unmarshalErr)})
					return
				}
				if !send(acpIncomingFrame{msg: msg}) {
					return
				}
			}
			lineBuf.Reset()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				send(acpIncomingFrame{err: err})
			}
			return
		}
	}
}

func (c *acpClient) emit(event providerEvent) error {
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	return c.consume(event)
}

func (c *acpClient) sendMessage(payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if deadliner, ok := c.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = deadliner.SetWriteDeadline(time.Now().Add(acpWriteDeadline))
		defer func() { _ = deadliner.SetWriteDeadline(time.Time{}) }()
	}
	_, err = c.stdin.Write(encoded)
	return err
}

func (c *acpClient) sendRequest(method string, params any) (int64, error) {
	c.nextID++
	id := c.nextID
	err := c.sendMessage(map[string]any{
		jsonRPCKey:    jsonRPCVersion,
		"id":          id,
		jsonMethodKey: method,
		jsonParamsKey: params,
	})
	return id, err
}

func (c *acpClient) sendNotification(method string, params any) error {
	return c.sendMessage(map[string]any{
		jsonRPCKey:    jsonRPCVersion,
		jsonMethodKey: method,
		jsonParamsKey: params,
	})
}

func (c *acpClient) sendResponse(id json.RawMessage, result any) error {
	return c.sendMessage(map[string]any{
		jsonRPCKey: jsonRPCVersion,
		"id":       id,
		"result":   result,
	})
}

func (c *acpClient) sendError(id json.RawMessage, code int, message string) error {
	return c.sendMessage(map[string]any{
		jsonRPCKey: jsonRPCVersion,
		"id":       id,
		jsonErrorKey: map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *acpClient) checkCancelled() bool {
	if c.cancelled {
		return true
	}
	current, err := loadRunRecord(c.root, c.runID)
	if err == nil && current.State == dispatchStateCancelled {
		c.cancelled = true
		if c.sessionID != "" {
			_ = c.sendNotification(acpMethodSessionCancel, map[string]any{jsonSessionIDCamelKey: c.sessionID})
		}
		return true
	}
	return false
}

func (c *acpClient) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id, err := c.sendRequest(method, params)
	if err != nil {
		return nil, err
	}
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	ticker := time.NewTicker(acpCancelPollInterval)
	defer ticker.Stop()

	for {
		select {
		case frame, ok := <-c.frames:
			if !ok {
				return nil, fmt.Errorf("ACP stdout closed while awaiting %s response", method)
			}
			if frame.err != nil {
				return nil, frame.err
			}
			if matched, result, err := c.dispatchFrame(frame.msg, id); matched {
				return result, err
			} else if err != nil {
				return nil, err
			}
		case <-ticker.C:
			if c.checkCancelled() {
				return nil, errors.New("dispatch was cancelled")
			}
		case <-deadline:
			return nil, fmt.Errorf("timed out waiting for %s response after %s", method, timeout)
		}
	}
}

func (c *acpClient) callPrompt(sessionID string, prompt string) (json.RawMessage, error) {
	if c.checkCancelled() {
		return nil, errors.New("dispatch was cancelled")
	}
	c.promptActive = true
	defer func() { c.promptActive = false }()
	return c.call(acpMethodSessionPrompt, map[string]any{
		jsonSessionIDCamelKey: sessionID,
		jsonPromptKey: []map[string]any{
			{"type": jsonTextKey, jsonTextKey: prompt},
		},
	}, acpPromptTimeout)
}

func (c *acpClient) dispatchFrame(msg acpJSONRPCMessage, pendingID int64) (bool, json.RawMessage, error) {
	if msg.JSONRPC != jsonRPCVersion {
		return false, nil, fmt.Errorf("unexpected ACP JSON-RPC version %q (want %q)", msg.JSONRPC, jsonRPCVersion)
	}
	trimmedID := bytes.TrimSpace(msg.ID)
	hasID := len(trimmedID) > 0
	isNullID := bytes.Equal(trimmedID, []byte("null"))
	if msg.Method != "" && hasID && isNullID {
		return false, nil, fmt.Errorf("malformed ACP JSON-RPC frame: method %q must not have null id", msg.Method)
	}
	// Incoming agent-to-client request (has both method and non-null id).
	if msg.Method != "" && hasID {
		if err := c.handleAgentRequest(msg); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	}
	// Incoming notification (has method, omitted id).
	if msg.Method != "" {
		if err := c.handleNotification(msg); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	}
	// Response matching our pending client request.
	if matchRPCID(msg.ID, pendingID) {
		if msg.Error != nil {
			return true, nil, msg.Error
		}
		return true, msg.Result, nil
	}
	return false, nil, nil
}

func matchRPCID(raw json.RawMessage, want int64) bool {
	if len(raw) == 0 {
		return false
	}
	var numeric int64
	return json.Unmarshal(raw, &numeric) == nil && numeric == want
}

var acpNonInteractiveAuthMethods = []string{"cached_token", "api_key", "xai.api_key"}

func (c *acpClient) authConfigError(cause error) error {
	hint := "sign in to the provider CLI or configure non-interactive credentials"
	if c.provider == AgentGrok {
		hint = "sign in using `al grok` or configure non-interactive credentials"
	}
	message := fmt.Sprintf("%s. Before dispatching, %s", cause, hint)
	return &acpConfigError{wrapExitError(ExitConfig, message, cause)}
}

func (c *acpClient) authenticate(initResult json.RawMessage) error {
	var parsed struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if err := json.Unmarshal(initResult, &parsed); err != nil {
		return fmt.Errorf("parse initialize authMethods: %w", err)
	}
	if len(parsed.AuthMethods) == 0 {
		return nil
	}
	methodID := ""
	for _, preferred := range acpNonInteractiveAuthMethods {
		for _, candidate := range parsed.AuthMethods {
			if candidate.ID == preferred {
				methodID = preferred
				break
			}
		}
		if methodID != "" {
			break
		}
	}
	if methodID == "" {
		offered := make([]string, 0, len(parsed.AuthMethods))
		seen := make(map[string]struct{}, len(parsed.AuthMethods))
		for _, candidate := range parsed.AuthMethods {
			id := candidate.ID
			if strings.TrimSpace(id) == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			offered = append(offered, strconv.Quote(id))
		}
		offeredDesc := "none with an id"
		if len(offered) > 0 {
			offeredDesc = strings.Join(offered, ", ")
		}
		return c.authConfigError(fmt.Errorf(
			"not authenticated: no supported non-interactive auth method offered (offered: %s, supported: %s)",
			offeredDesc,
			strings.Join(acpNonInteractiveAuthMethods, ", "),
		))
	}
	_, err := c.call(acpMethodAuthenticate, map[string]any{
		"methodId": methodID,
		"_meta":    map[string]any{"headless": true},
	}, acpHandshakeTimeout)
	var rpcErr *acpRPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == acpRPCAuthRequired {
		return c.authConfigError(fmt.Errorf("authentication method %q rejected: %w", methodID, err))
	}
	return err
}

func (c *acpClient) openSession(command providerCommand) (string, error) {
	mcpSpecs := c.cfg.MCPServers
	if mcpSpecs == nil {
		mcpSpecs = []acpMCPServerSpec{}
	}
	if command.RunMode == dispatchModeResume {
		raw, err := c.call(acpMethodSessionResume, map[string]any{
			jsonSessionIDCamelKey: command.SessionID,
			"cwd":                 c.cfg.WorkDir,
			jsonMCPServersKey:     mcpSpecs,
		}, acpHandshakeTimeout)
		if err != nil {
			return "", fmt.Errorf("%s ACP session/resume failed: %w", command.Provider, err)
		}
		var resp struct {
			SessionID string `json:"sessionId"`
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &resp); err != nil {
				return "", fmt.Errorf("parse %s ACP session/resume response: %w", command.Provider, err)
			}
		}
		if resp.SessionID != "" && resp.SessionID != command.SessionID {
			return "", exitError(ExitTargetFailure, fmt.Sprintf("%s ACP resume returned a different session ID (%q vs %q)", command.Provider, resp.SessionID, command.SessionID))
		}
		return command.SessionID, nil
	}
	raw, err := c.call(acpMethodSessionNew, map[string]any{
		"cwd":             c.cfg.WorkDir,
		jsonMCPServersKey: mcpSpecs,
	}, acpHandshakeTimeout)
	if err != nil {
		return "", fmt.Errorf("%s ACP session/new failed: %w", command.Provider, err)
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("parse %s ACP session/new response: %w", command.Provider, err)
	}
	if strings.TrimSpace(resp.SessionID) == "" {
		return "", exitError(ExitTargetFailure, fmt.Sprintf("%s ACP session/new returned an empty sessionId", command.Provider))
	}
	return strings.TrimSpace(resp.SessionID), nil
}

func (c *acpClient) configureSession(command providerCommand, sessionID string) error {
	switch command.Provider {
	case AgentAntigravity:
		if command.Model != "" {
			if _, err := c.call(acpMethodSessionSetModel, map[string]any{
				jsonSessionIDCamelKey: sessionID,
				"modelId":             command.Model,
			}, acpConfigRequestTimeout); err != nil {
				return fmt.Errorf("antigravity ACP set model %q failed: %w", command.Model, err)
			}
		}
	case AgentGrok:
		if command.Model != "" {
			if _, err := c.call(acpMethodSessionSetConfig, map[string]any{
				jsonSessionIDCamelKey: sessionID,
				"configId":            "model",
				"value":               command.Model,
			}, acpConfigRequestTimeout); err != nil {
				return fmt.Errorf("grok ACP set model %q failed: %w", command.Model, err)
			}
		}
		if command.Effort != "" {
			if _, err := c.call(acpMethodSessionSetConfig, map[string]any{
				jsonSessionIDCamelKey: sessionID,
				"configId":            "reasoning_effort",
				"value":               command.Effort,
			}, acpConfigRequestTimeout); err != nil {
				return fmt.Errorf("grok ACP set reasoning_effort %q failed: %w", command.Effort, err)
			}
		}
	}
	return nil
}

func (c *acpClient) verifySessionMCPReady(sessionID string) error {
	deadline := time.Now().Add(acpMCPVerifyTimeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	const grokMCPDoneGrace = 2 * time.Second

	for {
		if c.provider == AgentGrok {
			if err := c.checkGrokMCPStatusError(); err != nil {
				return err
			}
			// Grok 1.0.25 can emit _x.ai/mcp_initialized immediately before
			// trailing _x.ai/mcp/server_status notifications. Check any
			// already-reported non-ready status after the brief grace window so
			// trailing ready statuses are consumed while dropped servers still fail fast.
			if c.grokMCPDone && time.Since(c.grokMCPDoneAt) >= grokMCPDoneGrace {
				for _, name := range c.cfg.SessionMCPServers {
					status, ok := c.grokMCPReady[name]
					if !ok {
						return fmt.Errorf("grok ACP finished MCP initialization without reporting session MCP server %q ready", name)
					}
					if !strings.EqualFold(status.Status, "ready") {
						return fmt.Errorf("grok session MCP server %q failed to initialize (status=%q reason=%q)", name, status.Status, status.Reason)
					}
				}
			}
		}
		handshakesReady, pending, err := checkSessionMCPHandshakesReady(c.runDir, c.cfg.SessionMCPServers, c.cfg.HandshakeNonce)
		if err != nil {
			return err
		}
		grokReady := c.provider != AgentGrok || c.grokSessionMCPAllReady()
		if grokReady && handshakesReady {
			drainedAll, err := c.drainQueuedFrames()
			if err != nil {
				return err
			}
			if c.provider == AgentGrok {
				if err := c.checkGrokMCPStatusError(); err != nil {
					return err
				}
				if !c.grokSessionMCPAllReady() {
					continue
				}
			}
			if !drainedAll {
				if time.Now().After(deadline) {
					return fmt.Errorf("timed out draining queued ACP frames while verifying session MCP readiness for session %s", sessionID)
				}
				if c.checkCancelled() {
					return errors.New("dispatch was cancelled")
				}
				continue
			}
			// Re-verify handshake status and live process ownership after draining
			// queued ACP frames so a server that crashed during drain cannot slip through.
			recheckReady, _, err := checkSessionMCPHandshakesReady(c.runDir, c.cfg.SessionMCPServers, c.cfg.HandshakeNonce)
			if err != nil {
				return err
			}
			if !recheckReady {
				continue
			}
			return nil
		}
		if time.Now().After(deadline) {
			if c.provider == AgentGrok && !grokReady {
				return fmt.Errorf("timed out waiting for Grok ACP session %s to initialize session MCP servers (%s)", sessionID, strings.Join(c.cfg.SessionMCPServers, ", "))
			}
			return fmt.Errorf("session MCP servers did not complete startup handshake before inference: %s", strings.Join(pending, ", "))
		}
		select {
		case frame, ok := <-c.frames:
			if !ok {
				return fmt.Errorf("%s ACP stdout closed while initializing session MCP servers", c.provider)
			}
			if frame.err != nil {
				return frame.err
			}
			if _, _, err := c.dispatchFrame(frame.msg, 0); err != nil {
				return err
			}
		case <-ticker.C:
			if c.checkCancelled() {
				return errors.New("dispatch was cancelled")
			}
		}
	}
}

func (c *acpClient) grokSessionMCPAllReady() bool {
	for _, name := range c.cfg.SessionMCPServers {
		status, ok := c.grokMCPReady[name]
		if !ok || !strings.EqualFold(status.Status, "ready") {
			return false
		}
	}
	return len(c.cfg.SessionMCPServers) > 0
}

func (c *acpClient) checkGrokMCPStatusError() error {
	for _, name := range c.cfg.SessionMCPServers {
		status, ok := c.grokMCPReady[name]
		if !ok {
			continue
		}
		switch strings.ToLower(status.Status) {
		case "failed", "error", "disconnected":
			return fmt.Errorf("grok session MCP server %q failed to start (status=%q reason=%q)", name, status.Status, status.Reason)
		}
	}
	return nil
}

func (c *acpClient) drainQueuedFrames() (bool, error) {
	budget := cap(c.frames)
	if budget <= 0 {
		budget = 64
	}
	for range budget {
		select {
		case frame, ok := <-c.frames:
			if !ok {
				return false, fmt.Errorf("%s ACP stdout closed while initializing session MCP servers", c.provider)
			}
			if frame.err != nil {
				return false, frame.err
			}
			if _, _, err := c.dispatchFrame(frame.msg, 0); err != nil {
				return false, err
			}
		default:
			return true, nil
		}
	}
	return len(c.frames) == 0, nil
}

func (c *acpClient) handleNotification(msg acpJSONRPCMessage) error {
	switch msg.Method {
	case acpGrokMCPInitMethod:
		c.grokMCPDone = true
		c.grokMCPDoneAt = time.Now()
		return c.emit(providerEvent{Kind: eventProgress, Activity: "mcp_initialized"})
	case acpGrokMCPStatusMethod:
		var status struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(msg.Params, &status); err == nil && status.Name != "" {
			c.grokMCPReady[status.Name] = acpGrokMCPStatus{Status: status.Status, Reason: status.Reason}
		}
		if err := c.emit(providerEvent{Kind: eventProgress, Activity: "mcp_server_status"}); err != nil {
			return err
		}
		return c.checkGrokMCPStatusError()
	case acpMethodSessionUpdate:
		return c.handleSessionUpdate(msg.Params)
	default:
		return nil
	}
}

func (c *acpClient) handleSessionUpdate(raw json.RawMessage) error {
	var envelope struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string          `json:"sessionUpdate"`
			ToolCallID    string          `json:"toolCallId"`
			Status        string          `json:"status"`
			Name          string          `json:"name"`
			Kind          string          `json:"kind"`
			Title         string          `json:"title"`
			Args          map[string]any  `json:"args"`
			RawInput      map[string]any  `json:"rawInput"`
			Content       json.RawMessage `json:"content"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return c.emit(providerEvent{Kind: eventProgress, Activity: invalidStructuredEvent, Reason: err.Error()})
	}
	if envelope.SessionID != "" && c.sessionID != "" && envelope.SessionID != c.sessionID {
		return nil
	}
	switch envelope.Update.SessionUpdate {
	case "agent_message_chunk":
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if len(envelope.Update.Content) > 0 && json.Unmarshal(envelope.Update.Content, &content) != nil {
			return c.emit(providerEvent{Kind: eventProgress, Activity: invalidStructuredEvent, Reason: "malformed agent_message_chunk content"})
		}
		if c.promptActive && content.Type == jsonTextKey && content.Text != "" {
			if !c.answerTruncated {
				if c.sawToolSinceText && c.answer.Len() > 0 {
					if appendRetainedText(&c.answer, "\n\n") {
						c.answerTruncated = true
					}
					c.sawToolSinceText = false
				}
				if !c.answerTruncated && appendRetainedText(&c.answer, content.Text) {
					c.answerTruncated = true
				}
			}
			return c.emit(providerEvent{Kind: eventProgress, Activity: textDeltaActivity})
		}
		return nil
	case "agent_thought_chunk":
		return c.emit(providerEvent{Kind: eventProgress, Activity: thoughtActivity})
	case "tool_call", "tool_call_update":
		if id := envelope.Update.ToolCallID; id != "" {
			switch strings.ToLower(strings.TrimSpace(envelope.Update.Status)) {
			case "completed", "failed", "cancelled", "canceled":
				c.evictToolCall(id)
			default:
				c.cacheToolCall(id, trimACPPermissionToolForCache(acpPermissionTool{
					ToolCallID: id,
					Name:       envelope.Update.Name,
					Kind:       envelope.Update.Kind,
					Title:      envelope.Update.Title,
					Args:       envelope.Update.Args,
					RawInput:   envelope.Update.RawInput,
				}))
			}
		}
		if c.promptActive && c.answer.Len() > 0 {
			c.sawToolSinceText = true
		}
		return c.emit(providerEvent{Kind: eventProgress, Activity: envelope.Update.SessionUpdate})
	case "":
		return nil
	default:
		return c.emit(providerEvent{Kind: eventProgress, Activity: envelope.Update.SessionUpdate})
	}
}

func (c *acpClient) lookupCachedToolCall(id string) (acpPermissionTool, bool) {
	if c.toolCalls == nil {
		return acpPermissionTool{}, false
	}
	elem, ok := c.toolCalls[id]
	if !ok {
		return acpPermissionTool{}, false
	}
	entry, ok := elem.Value.(*acpCachedToolEntry)
	if !ok || entry == nil {
		return acpPermissionTool{}, false
	}
	return entry.tool, true
}

func (c *acpClient) evictToolCall(id string) {
	if c.toolCalls == nil {
		return
	}
	if elem, ok := c.toolCalls[id]; ok {
		delete(c.toolCalls, id)
		if c.toolCallOrder != nil {
			c.toolCallOrder.Remove(elem)
		}
	}
}

func (c *acpClient) cacheToolCall(id string, incoming acpPermissionTool) {
	if c.toolCalls == nil {
		c.toolCalls = make(map[string]*list.Element)
	}
	if c.toolCallOrder == nil {
		c.toolCallOrder = list.New()
	}
	if elem, exists := c.toolCalls[id]; exists {
		if entry, ok := elem.Value.(*acpCachedToolEntry); ok && entry != nil {
			entry.tool = mergeACPToolCallUpdate(entry.tool, incoming)
			c.toolCallOrder.MoveToBack(elem)
			return
		}
	}
	for len(c.toolCalls) >= acpMaxCachedToolCalls {
		front := c.toolCallOrder.Front()
		if front == nil {
			break
		}
		c.toolCallOrder.Remove(front)
		if entry, ok := front.Value.(*acpCachedToolEntry); ok && entry != nil {
			delete(c.toolCalls, entry.id)
		}
	}
	elem := c.toolCallOrder.PushBack(&acpCachedToolEntry{id: id, tool: incoming})
	c.toolCalls[id] = elem
}

func appendRetainedText(builder *strings.Builder, chunk string) bool {
	return appendRetainedTextLimit(builder, chunk, maxRetainedAnswerBytes)
}

func appendRetainedTextLimit(builder *strings.Builder, chunk string, limit int) bool {
	if builder == nil || chunk == "" {
		return false
	}
	remaining := limit - builder.Len()
	if remaining <= 0 {
		return true
	}
	truncated := false
	if len(chunk) > remaining {
		truncated = true
		cut := remaining
		for cut > 0 && !utf8.RuneStart(chunk[cut]) {
			cut--
		}
		chunk = chunk[:cut]
	}
	builder.WriteString(chunk)
	return truncated
}

func (c *acpClient) finishPrompt(raw json.RawMessage) error {
	var resp struct {
		StopReason string         `json:"stopReason"`
		Usage      map[string]any `json:"usage"`
		Meta       struct {
			Usage map[string]any `json:"usage"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse %s ACP session/prompt response: %w", c.provider, err)
	}
	if resp.StopReason == "" {
		return fmt.Errorf("%s ACP session/prompt response omitted stopReason", c.provider)
	}
	if resp.StopReason != acpStopReasonEndTurn {
		if c.provider != AgentGrok || !grok.IsSuccessfulStopReason(resp.StopReason) {
			return fmt.Errorf("%s terminated with abnormal stop reason %q", c.provider, resp.StopReason)
		}
	}
	usage := resp.Usage
	if len(usage) == 0 {
		usage = resp.Meta.Usage
	}
	if len(usage) > 0 {
		if err := c.emit(providerEvent{Kind: eventProgress, Activity: "usage", Usage: usage}); err != nil {
			return err
		}
	}
	if len(c.deniedTools) > 0 {
		return fmt.Errorf("%s denied tool call(s) (%s), so the dispatch could not do the requested work; check approvals.mode and .agent-layer/commands.allow before dispatching again", c.provider, strings.Join(c.deniedTools, ", "))
	}
	answer := c.answer.String()
	if c.answerTruncated {
		answer += truncatedAnswerNotice
	}
	if answer == "" {
		return fmt.Errorf("%s ACP terminal result did not contain a final answer", c.provider)
	}
	if err := c.emit(providerEvent{Kind: eventAnswer, Answer: answer}); err != nil {
		return err
	}
	return c.emit(providerEvent{Kind: eventComplete})
}
