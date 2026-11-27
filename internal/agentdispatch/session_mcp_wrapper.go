package agentdispatch

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	sessionMCPProcsFile          = "session-mcp-procs.json"
	sessionMCPProcsLockFile      = ".session-mcp-procs.lock"
	sessionMCPClosedFile         = "session-mcp.closed"
	sessionMCPHandshakeDir       = "session-mcp-handshake"
	sessionMCPEnvStripPrefix     = "AL_SESSION_MCP_"
	sessionMCPMaxPendingRequests = 256
	sessionMCPMaxIDNumLen        = 128
	sessionMCPMaxIDNumExp        = 100
)

// sessionMCPExec is a narrow seam around syscall.Exec for unit testing.
var sessionMCPExec = syscall.Exec

type sessionMCPProcess struct {
	Server               string `json:"server"`
	PID                  int    `json:"pid"`
	ProcessGroupID       int    `json:"process_group_id"`
	ProcessStartIdentity string `json:"process_start_identity"`
}

type sessionMCPProcsDocument struct {
	Processes []sessionMCPProcess `json:"processes"`
}

type sessionMCPHandshakeStatus struct {
	Server               string `json:"server"`
	Nonce                string `json:"nonce,omitempty"`
	PID                  int    `json:"pid,omitempty"`
	ProcessStartIdentity string `json:"process_start_identity,omitempty"`
	ChildPID             int    `json:"child_pid,omitempty"`
	ChildStartIdentity   string `json:"child_start_identity,omitempty"`
	Ready                bool   `json:"ready"`
	Failed               bool   `json:"failed,omitempty"`
	Error                string `json:"error,omitempty"`
}

// RunSessionMCPServer is the hidden `al __dispatch-session-mcp` entry point.
func RunSessionMCPServer(runDir string, serverName string, specPath string, digest string) error {
	return RunSessionMCPServerWithOptions(runDir, serverName, specPath, digest, false)
}

// RunSessionMCPServerWithOptions runs the hidden `al __dispatch-session-mcp`
// wrapper. It verifies the out-of-workspace spec digest before isolating the
// process group, recording ownership in the private runtime directory,
// restoring caller environment and working directory, and executing or relaying
// the target stdio MCP server.
func RunSessionMCPServerWithOptions(runDir string, serverName string, specPath string, digest string, verifyHandshake bool) error {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(serverName) == "" || strings.TrimSpace(specPath) == "" || strings.TrimSpace(digest) == "" {
		return exitError(ExitUsage, "session MCP wrapper requires --run-dir, --server, --spec, and --digest")
	}
	if err := validateSessionMCPServerName(serverName); err != nil {
		return err
	}
	rtDir, err := ensureSessionMCPRuntimeDir(runDir)
	if err != nil {
		return err
	}
	var specNonce string
	recordFail := func(err error, safeReason string) error {
		if verifyHandshake && err != nil {
			if safeReason == "" {
				safeReason = "session MCP startup failed"
			}
			_ = writeSessionMCPHandshakeStatus(rtDir, sessionMCPHandshakeStatus{
				Server:               serverName,
				Nonce:                specNonce,
				PID:                  os.Getpid(),
				ProcessStartIdentity: processStartIdentity(os.Getpid()),
				Failed:               true,
				Error:                safeReason,
			})
		}
		return err
	}
	var spec sessionMCPSpec
	if err := readPrivateVerifiedJSON(specPath, digest, &spec); err != nil {
		return recordFail(wrapExitError(ExitConfig, "read session MCP server specification", err), "failed to read verified session MCP specification")
	}
	specNonce = spec.Nonce
	var target *SessionMCPServer
	for i := range spec.Servers {
		if spec.Servers[i].Name == serverName {
			target = &spec.Servers[i]
			break
		}
	}
	if target == nil {
		return recordFail(exitError(ExitConfig, fmt.Sprintf("session MCP server %q not found in run specification", serverName)), "server not found in run specification")
	}
	if err := validateSessionMCPServer(*target); err != nil {
		return recordFail(err, "invalid session MCP server definition")
	}
	if strings.TrimSpace(spec.WorkDir) == "" || !filepath.IsAbs(spec.WorkDir) {
		return recordFail(exitError(ExitConfig, fmt.Sprintf("session MCP server %q has invalid working directory %q", serverName, spec.WorkDir)), "invalid session MCP working directory")
	}

	if err := registerSessionMCPProcess(rtDir, serverName); err != nil {
		return recordFail(err, "session MCP wrapper registration rejected")
	}

	baseEnv := spec.BaseEnv
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	execEnv := buildSessionMCPProcessEnv(baseEnv, target.Env)
	if err := os.Chdir(spec.WorkDir); err != nil {
		return recordFail(wrapExitError(ExitTargetFailure, fmt.Sprintf("enter session MCP working directory %q", spec.WorkDir), err), "failed to enter working directory")
	}
	resolvedBinary, err := resolveSessionMCPCommand(target.Command, execEnv)
	if err != nil {
		return recordFail(wrapExitError(ExitTargetFailure, fmt.Sprintf("resolve session MCP server %q command %q", serverName, target.Command), err), fmt.Sprintf("command %q not executable or not found in PATH", target.Command))
	}
	if verifyHandshake {
		return runSessionMCPServerWithHandshake(rtDir, serverName, spec.Nonce, resolvedBinary, target.Args, spec.WorkDir, execEnv)
	}
	argv := append([]string{target.Command}, target.Args...)
	if err := sessionMCPExec(resolvedBinary, argv, execEnv); err != nil {
		return wrapExitError(ExitTargetFailure, fmt.Sprintf("exec session MCP server %q (%q)", serverName, resolvedBinary), err)
	}
	return nil
}

func sessionMCPHandshakePath(rtDir string, serverName string) string {
	return filepath.Join(rtDir, sessionMCPHandshakeDir, serverName+".json")
}

func writeSessionMCPHandshakeStatus(rtDir string, status sessionMCPHandshakeStatus) error {
	path := sessionMCPHandshakePath(rtDir, status.Server)
	if err := ensureOwnedPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return writeJSONAtomic(path, status)
}

func readSessionMCPHandshakeStatus(rtDir string, serverName string) (sessionMCPHandshakeStatus, bool, error) {
	path := sessionMCPHandshakePath(rtDir, serverName)
	file, err := openNoFollowRegularFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sessionMCPHandshakeStatus{}, false, nil
		}
		return sessionMCPHandshakeStatus{}, false, err
	}
	defer func() { _ = file.Close() }()
	var status sessionMCPHandshakeStatus
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&status); err != nil {
		return sessionMCPHandshakeStatus{}, false, err
	}
	return status, true, nil
}

func checkSessionMCPHandshakesReady(runDir string, servers []string, expectedNonce string) (bool, []string, error) {
	if len(servers) == 0 {
		return true, nil, nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return false, nil, err
	}
	var registered sessionMCPProcsDocument
	if _, statErr := os.Stat(filepath.Join(rtDir, sessionMCPProcsFile)); statErr == nil {
		loaded, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return false, nil, readErr
		}
		registered = loaded
	}
	var pending []string
	for _, name := range servers {
		status, ok, err := readSessionMCPHandshakeStatus(rtDir, name)
		if err != nil {
			return false, nil, wrapExitError(ExitConfig, fmt.Sprintf("read session MCP handshake status for %q", name), err)
		}
		if !ok {
			pending = append(pending, name)
			continue
		}
		if status.Failed {
			return false, nil, fmt.Errorf("session MCP server %q failed during startup: %s", name, status.Error)
		}
		if expectedNonce != "" && status.Nonce != expectedNonce {
			pending = append(pending, name)
			continue
		}
		if status.PID <= 0 || status.ProcessStartIdentity == "" {
			return false, nil, fmt.Errorf("session MCP server %q handshake status missing process identity", name)
		}
		if err := verifySessionMCPProcessLive(status.PID, status.ProcessStartIdentity, name, ""); err != nil {
			return false, nil, err
		}
		if status.ChildPID > 0 || status.Ready {
			if status.ChildPID <= 0 || status.ChildStartIdentity == "" {
				return false, nil, fmt.Errorf("session MCP server %q handshake status missing child process identity", name)
			}
			if err := verifySessionMCPProcessLive(status.ChildPID, status.ChildStartIdentity, name, "child process"); err != nil {
				return false, nil, err
			}
		}
		if !sessionMCPProcessRegistered(registered.Processes, name, status.PID, status.ProcessStartIdentity) {
			pending = append(pending, name)
			continue
		}
		if !status.Ready {
			pending = append(pending, name)
		}
	}
	return len(pending) == 0, pending, nil
}

func verifySessionMCPProcessLive(pid int, startIdentity, serverName, role string) error {
	checkOwned := func() error {
		switch ownershipForIdentity(pid, startIdentity) {
		case ownershipOwned:
			return nil
		case ownershipDead:
			if role != "" {
				return fmt.Errorf("session MCP server %q %s exited before inference started", serverName, role)
			}
			return fmt.Errorf("session MCP server %q exited before inference started", serverName)
		default:
			if role != "" {
				return fmt.Errorf("cannot verify live ownership of session MCP server %q %s before inference", serverName, role)
			}
			return fmt.Errorf("cannot verify live ownership of session MCP server %q before inference", serverName)
		}
	}
	if err := checkOwned(); err != nil {
		return err
	}
	if processIsZombie(pid) {
		if role != "" {
			return fmt.Errorf("session MCP server %q %s exited before inference started", serverName, role)
		}
		return fmt.Errorf("session MCP server %q exited before inference started", serverName)
	}
	// Recheck ownership after the zombie probe so a process that died and was
	// reaped between the two checks cannot slip past as live.
	return checkOwned()
}

func sessionMCPProcessRegistered(procs []sessionMCPProcess, server string, pid int, startIdentity string) bool {
	for _, proc := range procs {
		if proc.Server == server && proc.PID == pid && proc.ProcessGroupID == pid && proc.ProcessStartIdentity == startIdentity {
			return true
		}
	}
	return false
}

type sessionMCPResponseOutcome uint8

const (
	sessionMCPResponseIgnore sessionMCPResponseOutcome = iota
	sessionMCPResponseReady
	sessionMCPResponseFailed
)

type sessionMCPResponseDecision struct {
	outcome sessionMCPResponseOutcome
	errMsg  string
}

type sessionMCPHandshakeTracker struct {
	mu          sync.Mutex
	pending     map[[32]byte]*list.Element
	order       *list.List
	passthrough atomic.Bool
	status      sessionMCPHandshakeStatus
	publish     func(sessionMCPHandshakeStatus)
}

func newSessionMCPHandshakeTracker(initial sessionMCPHandshakeStatus, publish func(sessionMCPHandshakeStatus)) *sessionMCPHandshakeTracker {
	t := &sessionMCPHandshakeTracker{
		pending: make(map[[32]byte]*list.Element),
		order:   list.New(),
		status:  initial,
		publish: publish,
	}
	if initial.Ready {
		t.passthrough.Store(true)
	}
	return t
}

func (t *sessionMCPHandshakeTracker) isPassthrough() bool {
	return t.passthrough.Load()
}

func (t *sessionMCPHandshakeTracker) clearPendingLocked() {
	t.pending = nil
	t.order = nil
}

func (t *sessionMCPHandshakeTracker) publishLocked() {
	if t.publish != nil {
		t.publish(t.status)
	}
}

// decodeJSONRPCObject decodes a single JSON-RPC 2.0 object line using exact
// case-sensitive member names. JSON-RPC array batches are not tracked during startup.
func decodeJSONRPCObject(line []byte) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil || fields == nil {
		return nil, false
	}
	rawVersion, ok := fields["jsonrpc"]
	if !ok {
		return nil, false
	}
	var version string
	if json.Unmarshal(rawVersion, &version) != nil || version != jsonRPCVersion {
		return nil, false
	}
	return fields, true
}

// trackRequest records any startup JSON-RPC 2.0 request object carrying a
// non-empty method and valid non-null string/number id (hashed into a fixed
// 32-byte key) so the corresponding server response can be matched.
func (t *sessionMCPHandshakeTracker) trackRequest(line []byte) {
	fields, ok := decodeJSONRPCObject(line)
	if !ok {
		return
	}
	rawMethod, hasMethod := fields["method"]
	if !hasMethod {
		return
	}
	var method string
	if json.Unmarshal(rawMethod, &method) != nil || strings.TrimSpace(method) == "" {
		return
	}
	rawID, hasID := fields["id"]
	if !hasID {
		return
	}
	key, ok := canonicalJSONRPCIDKey(rawID)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.passthrough.Load() || t.status.Ready || t.status.Failed {
		return
	}
	if t.pending == nil {
		t.pending = make(map[[32]byte]*list.Element)
	}
	if t.order == nil {
		t.order = list.New()
	}
	if elem, exists := t.pending[key]; exists {
		t.order.MoveToBack(elem)
		return
	}
	for len(t.pending) >= sessionMCPMaxPendingRequests {
		front := t.order.Front()
		if front == nil {
			break
		}
		t.order.Remove(front)
		if oldestKey, ok := front.Value.([32]byte); ok {
			delete(t.pending, oldestKey)
		}
	}
	elem := t.order.PushBack(key)
	t.pending[key] = elem
}

// prepareResponse classifies a newline-terminated server stdout line BEFORE it
// is forwarded to the client. When a matching JSON-RPC 2.0 success response is
// observed, it arms raw passthrough immediately so the client cannot race
// stdin's startup frame limit upon receiving the response; Ready is only
// published after stdout.Write succeeds via commitResponse.
func (t *sessionMCPHandshakeTracker) prepareResponse(line []byte) sessionMCPResponseDecision {
	fields, ok := decodeJSONRPCObject(line)
	if !ok {
		return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
	}
	if rawMethod, hasMethod := fields["method"]; hasMethod {
		var method string
		if json.Unmarshal(rawMethod, &method) != nil || method != "" {
			return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
		}
	}
	rawID, hasID := fields["id"]
	if !hasID {
		return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
	}
	key, ok := canonicalJSONRPCIDKey(rawID)
	if !ok {
		return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.passthrough.Load() || t.status.Ready || t.status.Failed {
		return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
	}
	elem, tracked := t.pending[key]
	if !tracked {
		return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
	}
	delete(t.pending, key)
	if t.order != nil {
		t.order.Remove(elem)
	}
	rawErr := fields["error"]
	rawResult, hasResultField := fields["result"]
	trimmedErr := bytes.TrimSpace(rawErr)
	hasNonNullError := hasNonNullJSONValue(trimmedErr)
	hasNonNullResult := hasResultField && hasNonNullJSONValue(bytes.TrimSpace(rawResult))
	if hasNonNullError && hasNonNullResult {
		return sessionMCPResponseDecision{
			outcome: sessionMCPResponseFailed,
			errMsg:  "startup request returned invalid JSON-RPC response",
		}
	}
	if hasNonNullError {
		code, valid := parseJSONRPCErrorObject(trimmedErr)
		if !valid {
			return sessionMCPResponseDecision{
				outcome: sessionMCPResponseFailed,
				errMsg:  "startup request returned malformed JSON-RPC error",
			}
		}
		if code == acpRPCMethodNotFound {
			return sessionMCPResponseDecision{outcome: sessionMCPResponseIgnore}
		}
		return sessionMCPResponseDecision{
			outcome: sessionMCPResponseFailed,
			errMsg:  fmt.Sprintf("startup request returned JSON-RPC error %d", code),
		}
	}
	if !hasResultField {
		return sessionMCPResponseDecision{
			outcome: sessionMCPResponseFailed,
			errMsg:  "startup request returned invalid JSON-RPC response",
		}
	}
	// Valid JSON-RPC 2.0 success response (including explicit `"result": null`):
	// arm raw passthrough before exposing the response to the client.
	t.passthrough.Store(true)
	t.clearPendingLocked()
	return sessionMCPResponseDecision{outcome: sessionMCPResponseReady}
}

// commitResponse publishes the startup status transition after the classified
// response line has been successfully written to stdout.
func (t *sessionMCPHandshakeTracker) commitResponse(decision sessionMCPResponseDecision) {
	switch decision.outcome {
	case sessionMCPResponseReady:
		t.mu.Lock()
		if !t.status.Failed && !t.status.Ready {
			t.status.Ready = true
			t.publishLocked()
		}
		t.mu.Unlock()
	case sessionMCPResponseFailed:
		t.mu.Lock()
		if !t.status.Ready && !t.status.Failed {
			t.status.Failed = true
			t.status.Error = decision.errMsg
			t.clearPendingLocked()
			t.publishLocked()
		}
		t.mu.Unlock()
	}
}

// markFailedUnlessPassthrough records a startup failure when raw passthrough
// has not yet been armed. It returns true when passthrough is already active.
func (t *sessionMCPHandshakeTracker) markFailedUnlessPassthrough(reason string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.passthrough.Load() || t.status.Ready {
		return true
	}
	if !t.status.Failed {
		t.status.Failed = true
		t.status.Error = reason
		t.clearPendingLocked()
		t.publishLocked()
	}
	return false
}

// markFailedUnlessReady records a startup failure when readiness has not yet
// been published. It returns true when the session is already ready.
func (t *sessionMCPHandshakeTracker) markFailedUnlessReady(reason string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status.Ready {
		return true
	}
	if !t.status.Failed {
		t.status.Failed = true
		t.status.Error = reason
		t.clearPendingLocked()
		t.publishLocked()
	}
	return false
}

func hasNonNullJSONValue(trimmed []byte) bool {
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func parseJSONRPCErrorObject(raw []byte) (int, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return 0, false
	}
	rawCode, hasCode := fields["code"]
	rawMessage, hasMessage := fields["message"]
	if !hasCode || !hasMessage {
		return 0, false
	}
	var msg string
	if !hasNonNullJSONValue(bytes.TrimSpace(rawMessage)) || json.Unmarshal(rawMessage, &msg) != nil {
		return 0, false
	}
	codeTrimmed := bytes.TrimSpace(rawCode)
	if len(codeTrimmed) == 0 {
		return 0, false
	}
	code, err := strconv.Atoi(string(codeTrimmed))
	if err != nil {
		return 0, false
	}
	return code, true
}

func canonicalJSONRPCNumber(numStr string) (string, bool) {
	if len(numStr) == 0 || len(numStr) > sessionMCPMaxIDNumLen {
		return "", false
	}
	if expIdx := strings.IndexAny(numStr, "eE"); expIdx >= 0 {
		exp, err := strconv.Atoi(numStr[expIdx+1:])
		if err != nil || exp < -sessionMCPMaxIDNumExp || exp > sessionMCPMaxIDNumExp {
			return "", false
		}
	}
	rat, ok := new(big.Rat).SetString(numStr)
	if !ok {
		return "", false
	}
	return rat.RatString(), true
}

func canonicalJSONRPCIDKey(raw json.RawMessage) ([32]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return [32]byte{}, false
	}
	first := trimmed[0]
	if first != '"' && first != '-' && (first < '0' || first > '9') {
		return [32]byte{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return [32]byte{}, false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return [32]byte{}, false
	}
	h := sha256.New()
	switch v := val.(type) {
	case string:
		_, _ = h.Write([]byte("s:"))
		_, _ = h.Write([]byte(v))
	case json.Number:
		canonical, ok := canonicalJSONRPCNumber(v.String())
		if !ok {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte("n:"))
		_, _ = h.Write([]byte(canonical))
	default:
		// Reject null (nil), bool, array, and object IDs per JSON-RPC 2.0.
		return [32]byte{}, false
	}
	var sum [32]byte
	h.Sum(sum[:0])
	return sum, true
}

func lineContentLen(chunk []byte, readErr error) int {
	n := len(chunk)
	if readErr == nil && n > 0 && chunk[n-1] == '\n' {
		n--
		if n > 0 && chunk[n-1] == '\r' {
			n--
		}
	}
	return n
}

func readBoundedLine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			contentLen := lineContentLen(chunk, err)
			if errors.Is(err, bufio.ErrBufferFull) && chunk[len(chunk)-1] == '\r' {
				// Keep one trailing CR provisional in case LF starts the next chunk.
				contentLen--
			} else if err == nil && len(chunk) == 1 && chunk[0] == '\n' && buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == '\r' {
				contentLen = -1
			}
			if buf.Len()+contentLen > maxBytes {
				return nil, fmt.Errorf("MCP stdio JSON-RPC line exceeded %d byte limit", maxBytes)
			}
			buf.Write(chunk)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && buf.Len() > maxBytes {
			return nil, fmt.Errorf("MCP stdio JSON-RPC line exceeded %d byte limit", maxBytes)
		}
		return buf.Bytes(), err
	}
}

func flushStartupLineAndChunks(w io.Writer, lineBuf *bytes.Buffer, chunks ...[]byte) bool {
	if lineBuf.Len() > 0 {
		if _, err := w.Write(lineBuf.Bytes()); err != nil {
			return false
		}
		lineBuf.Reset()
	}
	for _, chunk := range chunks {
		if len(chunk) > 0 {
			if _, err := w.Write(chunk); err != nil {
				return false
			}
		}
	}
	return true
}

func relaySessionMCPStdin(
	stdin io.Reader,
	childStdin io.WriteCloser,
	maxFrameBytes int,
	tracker *sessionMCPHandshakeTracker,
	terminateChild func(),
) {
	defer func() { _ = childStdin.Close() }()
	readBuf := make([]byte, 64*1024)
	var lineBuf bytes.Buffer

	for !tracker.isPassthrough() {
		n, readErr := stdin.Read(readBuf)
		if n > 0 {
			chunk := readBuf[:n]
			if tracker.isPassthrough() {
				if !flushStartupLineAndChunks(childStdin, &lineBuf, chunk) {
					return
				}
				if readErr != nil {
					return
				}
				break
			}
			switched := false
			for len(chunk) > 0 {
				if tracker.isPassthrough() {
					if !flushStartupLineAndChunks(childStdin, &lineBuf, chunk) {
						return
					}
					switched = true
					break
				}
				nlIdx := bytes.IndexByte(chunk, '\n')
				if nlIdx < 0 {
					effectiveLen := lineBuf.Len() + len(chunk)
					if chunk[len(chunk)-1] == '\r' {
						// Keep one trailing CR provisional in case LF starts the next read.
						effectiveLen--
					}
					if effectiveLen > maxFrameBytes {
						if !tracker.markFailedUnlessPassthrough(fmt.Sprintf("MCP stdio JSON-RPC line exceeded %d byte limit", maxFrameBytes)) {
							terminateChild()
							return
						}
						if !flushStartupLineAndChunks(childStdin, &lineBuf, chunk) {
							return
						}
						switched = true
						break
					}
					lineBuf.Write(chunk)
					chunk = nil
					break
				}
				part := chunk[:nlIdx+1]
				chunk = chunk[nlIdx+1:]
				contentLen := lineContentLen(part, nil)
				if nlIdx == 0 && lineBuf.Len() > 0 && lineBuf.Bytes()[lineBuf.Len()-1] == '\r' {
					contentLen = -1
				}
				if lineBuf.Len()+contentLen > maxFrameBytes {
					if !tracker.markFailedUnlessPassthrough(fmt.Sprintf("MCP stdio JSON-RPC line exceeded %d byte limit", maxFrameBytes)) {
						terminateChild()
						return
					}
					if !flushStartupLineAndChunks(childStdin, &lineBuf, part, chunk) {
						return
					}
					switched = true
					break
				}
				lineBuf.Write(part)
				tracker.trackRequest(lineBuf.Bytes())
				if _, writeErr := childStdin.Write(lineBuf.Bytes()); writeErr != nil {
					return
				}
				lineBuf.Reset()
			}
			if switched {
				if readErr != nil {
					return
				}
				break
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if lineBuf.Len() > 0 {
					if lineBuf.Len() > maxFrameBytes && !tracker.markFailedUnlessPassthrough(fmt.Sprintf("MCP stdio JSON-RPC line exceeded %d byte limit", maxFrameBytes)) {
						terminateChild()
						return
					}
					if !tracker.isPassthrough() {
						tracker.trackRequest(lineBuf.Bytes())
					}
					_, _ = childStdin.Write(lineBuf.Bytes())
				}
				return
			}
			if !tracker.markFailedUnlessPassthrough(readErr.Error()) {
				terminateChild()
			}
			return
		}
	}

	// Flush any buffered partial startup line if passthrough flipped between
	// reads, release startup buffers, and stream the rest of stdin verbatim.
	if lineBuf.Len() > 0 {
		if _, writeErr := childStdin.Write(lineBuf.Bytes()); writeErr != nil {
			return
		}
	}
	lineBuf = bytes.Buffer{}
	readBuf = nil
	_, _ = io.Copy(childStdin, stdin)
}

func relaySessionMCPStdout(
	childStdout io.Reader,
	stdout io.Writer,
	maxFrameBytes int,
	tracker *sessionMCPHandshakeTracker,
	terminateChild func(),
) {
	reader := bufio.NewReaderSize(childStdout, 64*1024)
	for !tracker.isPassthrough() {
		line, readErr := readBoundedLine(reader, maxFrameBytes)
		if len(line) > 0 {
			var decision sessionMCPResponseDecision
			if line[len(line)-1] == '\n' {
				decision = tracker.prepareResponse(line)
			}
			if _, writeErr := stdout.Write(line); writeErr != nil {
				terminateChild()
				_, _ = io.Copy(io.Discard, reader)
				return
			}
			tracker.commitResponse(decision)
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				tracker.markFailedUnlessReady(readErr.Error())
				terminateChild()
				_, _ = io.Copy(io.Discard, reader)
			}
			return
		}
	}

	// Readiness confirmed: stream remaining output verbatim with no frame-size limit.
	if _, copyErr := io.Copy(stdout, reader); copyErr != nil {
		terminateChild()
		_, _ = io.Copy(io.Discard, reader)
	}
}

// runSessionMCPServerWithHandshake spawns the target stdio MCP server as a
// child in this wrapper's already-isolated process group and transparently
// relays stdin/stdout, recording the first successful startup JSON-RPC
// request/response exchange before switching both streams to raw passthrough.
func runSessionMCPServerWithHandshake(rtDir, serverName, nonce, resolvedBinary string, args []string, workDir string, env []string) error {
	// Ignore SIGPIPE so writing to a closed stdout pipe returns EPIPE instead
	// of killing the wrapper before it signals and reaps the child server.
	signal.Ignore(syscall.SIGPIPE)
	defer signal.Reset(syscall.SIGPIPE)

	pid := os.Getpid()
	status := sessionMCPHandshakeStatus{
		Server:               serverName,
		Nonce:                nonce,
		PID:                  pid,
		ProcessStartIdentity: processStartIdentity(pid),
	}
	// Reset any stale handshake file for this server immediately on startup.
	_ = writeSessionMCPHandshakeStatus(rtDir, status)

	failStartup := func(err error, msg, safeReason string) error {
		status.Failed = true
		status.Error = safeReason
		_ = writeSessionMCPHandshakeStatus(rtDir, status)
		return wrapExitError(ExitTargetFailure, msg, err)
	}

	cmd := exec.CommandContext(context.Background(), resolvedBinary, args...) // #nosec G204 -- caller-configured session MCP server executable.
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stderr = os.Stderr
	childStdin, err := cmd.StdinPipe()
	if err != nil {
		return failStartup(err, fmt.Sprintf("open stdin for session MCP server %q", serverName), "failed to open child stdin pipe")
	}
	childStdoutR, childStdoutW, err := os.Pipe()
	if err != nil {
		return failStartup(err, fmt.Sprintf("open stdout for session MCP server %q", serverName), "failed to open child stdout pipe")
	}
	defer func() { _ = childStdoutR.Close() }()
	cmd.Stdout = childStdoutW
	if err := cmd.Start(); err != nil {
		_ = childStdoutW.Close()
		return failStartup(err, fmt.Sprintf("start session MCP server %q (%q)", serverName, resolvedBinary), "failed to start server process")
	}
	_ = childStdoutW.Close()
	status.ChildPID = cmd.Process.Pid
	status.ChildStartIdentity = processStartIdentity(cmd.Process.Pid)
	_ = writeSessionMCPHandshakeStatus(rtDir, status)

	const childShutdownGrace = 2 * time.Second
	var shutdownOnce sync.Once
	var killTimerMu sync.Mutex
	var killTimer *time.Timer
	var killTimerStopped bool
	stopKillTimer := func() {
		killTimerMu.Lock()
		killTimerStopped = true
		if killTimer != nil {
			killTimer.Stop()
		}
		killTimerMu.Unlock()
	}
	defer stopKillTimer()
	terminateChildBounded := func() {
		shutdownOnce.Do(func() {
			_ = childStdin.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			killTimerMu.Lock()
			if !killTimerStopped {
				killTimer = time.AfterFunc(childShutdownGrace, func() {
					if cmd.Process != nil {
						_ = cmd.Process.Signal(syscall.SIGKILL)
					}
					_ = childStdoutR.Close()
				})
			}
			killTimerMu.Unlock()
		})
	}

	// Forward termination signals to the child with bounded SIGKILL escalation
	// so a SIGTERM-ignoring server or descendant cannot hang wrapper exit.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	stopSig := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(stopSig)
	}()
	go func() {
		select {
		case <-sigCh:
			terminateChildBounded()
		case <-stopSig:
		}
	}()

	tracker := newSessionMCPHandshakeTracker(status, func(s sessionMCPHandshakeStatus) {
		_ = writeSessionMCPHandshakeStatus(rtDir, s)
	})

	// Relay os.Stdin -> childStdin, tracking startup requests until readiness
	// is confirmed and then switching to raw passthrough.
	go relaySessionMCPStdin(os.Stdin, childStdin, acpMaxFrameBytes, tracker, terminateChildBounded)

	// Relay childStdoutR -> os.Stdout, recording startup readiness AFTER
	// forwarding the matching response line and then switching to raw
	// passthrough. On os.Stdout write failure (EPIPE) or pre-readiness
	// oversized frame, terminate the child with bounded SIGKILL escalation and
	// pipe closure so a SIGTERM-ignoring server or descendant can never hang Wait.
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		relaySessionMCPStdout(childStdoutR, os.Stdout, acpMaxFrameBytes, tracker, terminateChildBounded)
	}()

	<-stdoutDone
	waitErr := cmd.Wait()
	stopKillTimer()

	exitReason := "server exited before completing startup handshake"
	if waitErr != nil {
		exitReason = fmt.Sprintf("server exited before completing startup handshake: %v", waitErr)
	}
	tracker.markFailedUnlessReady(exitReason)
	return waitErr
}

func buildSessionMCPProcessEnv(base []string, overrides map[string]string) []string {
	env := withoutSessionMCPEnvironment(base)
	if len(overrides) == 0 {
		return env
	}
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		env = clients.SetEnv(env, key, overrides[key])
	}
	return env
}

func withoutSessionMCPEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, sessionMCPEnvStripPrefix) {
			result = append(result, entry)
		}
	}
	return result
}

func resolveSessionMCPCommand(command string, env []string) (string, error) {
	if strings.ContainsRune(command, filepath.Separator) || strings.ContainsRune(command, '/') {
		abs, err := filepath.Abs(command)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", err
		}
		if err := fsutil.RequireExecutable(abs, info); err != nil {
			return "", err
		}
		return abs, nil
	}
	pathEnv, _ := clients.GetEnv(env, "PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate) // #nosec G703 -- PATH lookup for caller-specified session MCP server command.
		if err != nil {
			continue
		}
		if err := fsutil.RequireExecutable(candidate, info); err != nil {
			continue
		}
		return filepath.Abs(candidate)
	}
	return "", fmt.Errorf("executable %q not found in PATH", command)
}

func withSessionMCPProcsLock(rtDir string, fn func() error) error {
	acquired, err := flockAndRun(
		filepath.Join(rtDir, sessionMCPProcsLockFile),
		unix.LOCK_EX,
		"open session MCP process lock",
		"lock session MCP process list",
		fn,
	)
	if err != nil {
		return err
	}
	if !acquired {
		return exitError(ExitConfig, "session MCP process lock was not acquired")
	}
	return nil
}

func tryWithSessionMCPProcsLock(rtDir string, fn func() error) (bool, error) {
	return flockAndRun(
		filepath.Join(rtDir, sessionMCPProcsLockFile),
		unix.LOCK_EX|unix.LOCK_NB,
		"open session MCP process lock",
		"try-lock session MCP process list",
		fn,
	)
}

func registerSessionMCPProcess(rtDir string, serverName string) error {
	return withSessionMCPProcsLock(rtDir, func() error {
		if _, err := os.Lstat(filepath.Join(rtDir, sessionMCPClosedFile)); err == nil {
			return exitError(ExitUnavailable, "dispatch invocation is terminating; refusing to start session MCP server")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return wrapExitError(ExitConfig, "check session MCP close fence", err)
		}
		pid := os.Getpid()
		if err := syscall.Setpgid(0, 0); err != nil {
			pgid, pgErr := syscall.Getpgid(0)
			if pgErr != nil || pgid != pid {
				return wrapExitError(ExitTargetFailure, "isolate session MCP process group", err)
			}
		}
		startIdentity := processStartIdentity(pid)
		if startIdentity == "" {
			return exitError(ExitTargetFailure, "capture session MCP process start identity")
		}
		doc, err := readSessionMCPProcsLocked(rtDir)
		if err != nil {
			return err
		}
		doc.Processes = append(doc.Processes, sessionMCPProcess{
			Server:               serverName,
			PID:                  pid,
			ProcessGroupID:       pid,
			ProcessStartIdentity: startIdentity,
		})
		if err := writeJSONAtomic(filepath.Join(rtDir, sessionMCPProcsFile), doc); err != nil {
			return wrapExitError(ExitConfig, "record session MCP process ownership", err)
		}
		return nil
	})
}

func readSessionMCPProcsLocked(rtDir string) (sessionMCPProcsDocument, error) {
	var doc sessionMCPProcsDocument
	path := filepath.Join(rtDir, sessionMCPProcsFile)
	if err := readJSON(path, &doc); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sessionMCPProcsDocument{}, nil
		}
		return sessionMCPProcsDocument{}, wrapExitError(ExitConfig, "read session MCP process list", err)
	}
	return doc, nil
}

// closeAndTerminateSessionMCPProcesses fences any new session MCP wrapper
// starts for runDir and terminates every tracked session MCP process group,
// returning non-nil if any tracked group could not be proven stopped.
func closeAndTerminateSessionMCPProcesses(runDir string, grace time.Duration) error {
	if runDir == "" {
		return nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(rtDir); errors.Is(statErr, fs.ErrNotExist) {
		return nil
	} else if statErr != nil {
		return wrapExitError(ExitConfig, "inspect session MCP runtime directory", statErr)
	}
	var procs []sessionMCPProcess
	err = withSessionMCPProcsLock(rtDir, func() error {
		closedPath := filepath.Join(rtDir, sessionMCPClosedFile)
		if err := publishBytes(closedPath, []byte("closed\n"), os.Rename); err != nil {
			return wrapExitError(ExitConfig, "write session MCP close fence", err)
		}
		doc, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return readErr
		}
		procs = doc.Processes
		return nil
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, proc := range procs {
		if err := terminateSessionMCPProcess(proc, grace); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func terminateSessionMCPProcess(proc sessionMCPProcess, grace time.Duration) error {
	if proc.PID <= 0 || proc.ProcessGroupID <= 0 || proc.PID != proc.ProcessGroupID || proc.ProcessStartIdentity == "" {
		return fmt.Errorf("session MCP server %q has invalid process group ownership identity", proc.Server)
	}
	record := RunRecord{
		PID:                  proc.PID,
		ProcessGroupID:       proc.ProcessGroupID,
		ProcessStartIdentity: proc.ProcessStartIdentity,
	}
	if providerProcessGroupReused(record) {
		return nil
	}
	if providerProcessGroupDead(proc.ProcessGroupID) && ownershipForIdentity(proc.PID, proc.ProcessStartIdentity) == ownershipDead {
		return nil
	}
	group := ownedProviderProcessGroup{
		pid:   proc.PID,
		pgid:  proc.ProcessGroupID,
		start: proc.ProcessStartIdentity,
	}
	if err := group.terminate(grace); err != nil {
		if errors.Is(err, errProviderGroupIdentityMismatch) && providerProcessGroupReused(record) {
			return nil
		}
		return fmt.Errorf("terminate session MCP server %q (pgid %d): %w", proc.Server, proc.ProcessGroupID, err)
	}
	if !providerProcessGroupDead(proc.ProcessGroupID) && !providerProcessGroupReused(record) {
		return fmt.Errorf("session MCP server %q (pgid %d) remained live after termination", proc.Server, proc.ProcessGroupID)
	}
	return nil
}

// sessionMCPProcessesStopped reports whether every tracked session MCP process
// group for runDir is provably dead (or its PID/PGID has been reused by an
// unrelated process).
func sessionMCPProcessesStopped(runDir string) bool {
	return trySessionMCPProcessesStoppedAndFenced(runDir, false)
}

func trySessionMCPProcessesStoppedAndFenced(runDir string, fence bool) bool {
	if runDir == "" {
		return true
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return false
	}
	if _, statErr := os.Stat(rtDir); errors.Is(statErr, fs.ErrNotExist) {
		return true
	}
	_, activeErr := os.Lstat(filepath.Join(rtDir, sessionMCPActiveFile))
	_, procsErr := os.Lstat(filepath.Join(rtDir, sessionMCPProcsFile))
	if errors.Is(activeErr, fs.ErrNotExist) && errors.Is(procsErr, fs.ErrNotExist) {
		return true
	}
	var doc sessionMCPProcsDocument
	stopped := false
	acquired, err := tryWithSessionMCPProcsLock(rtDir, func() error {
		if fence {
			closedPath := filepath.Join(rtDir, sessionMCPClosedFile)
			if _, statErr := os.Lstat(closedPath); errors.Is(statErr, fs.ErrNotExist) {
				if writeErr := publishBytes(closedPath, []byte("closed\n"), os.Rename); writeErr != nil {
					return writeErr
				}
			} else if statErr != nil {
				return statErr
			}
		}
		loaded, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return readErr
		}
		doc = loaded
		stopped = true
		return nil
	})
	if err != nil || !acquired || !stopped {
		return false
	}
	for _, proc := range doc.Processes {
		if proc.PID <= 0 || proc.ProcessGroupID <= 0 || proc.PID != proc.ProcessGroupID || proc.ProcessStartIdentity == "" {
			return false
		}
		record := RunRecord{
			PID:                  proc.PID,
			ProcessGroupID:       proc.ProcessGroupID,
			ProcessStartIdentity: proc.ProcessStartIdentity,
		}
		if providerProcessGroupReused(record) {
			continue
		}
		if !providerProcessGroupDead(proc.ProcessGroupID) {
			return false
		}
		if ownershipForIdentity(proc.PID, proc.ProcessStartIdentity) != ownershipDead {
			return false
		}
	}
	return true
}
