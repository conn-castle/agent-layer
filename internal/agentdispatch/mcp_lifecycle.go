package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
)

const (
	mcpLifecycleRead           = "read"
	mcpLifecycleRun            = "run"
	mcpLifecycleRunError       = "run_error"
	mcpLifecycleUncertain      = "uncertain"
	mcpLifecycleCancelled      = "context_cancelled"
	mcpLifecycleEventError     = "error"
	mcpLifecycleClientEOF      = "client_eof"
	mcpLifecycleStartupError   = "startup_error"
	mcpLifecycleTransportError = "transport_error"
)

// Only fixed categories and server identity belong here. In particular, error
// strings can contain private protocol input and must never be serialized.
type mcpLifecycleRecord struct {
	Schema           int       `json:"schema"`
	Event            string    `json:"event"`
	Timestamp        time.Time `json:"timestamp"`
	ConnectionID     string    `json:"connection_id"`
	Version          string    `json:"version"`
	PID              int       `json:"pid"`
	ParentPID        int       `json:"parent_pid"`
	Phase            string    `json:"phase,omitempty"`
	Condition        string    `json:"condition,omitempty"`
	Operation        string    `json:"operation,omitempty"`
	ClientEOF        bool      `json:"client_eof"`
	InputEOF         bool      `json:"input_eof"`
	ContextCondition string    `json:"context_condition,omitempty"`
	Uncertain        bool      `json:"uncertain"`
}

type mcpLifecycle struct {
	mu             sync.Mutex
	file           *os.File
	stderr         io.Writer
	identity       mcpLifecycleRecord
	closed         bool // local transport close has begun
	finished       bool // no observations after the final record
	rawEOF         bool
	framedEOF      bool
	startupError   bool
	transportError bool
	ambiguous      bool
	observed       map[string]bool
}

func newMCPLifecycle(opts MCPServerOptions, stderr io.Writer) *mcpLifecycle {
	l := &mcpLifecycle{stderr: stderr, observed: make(map[string]bool)}
	if strings.TrimSpace(opts.Root) == "" {
		return l
	}
	id, err := newUUID()
	if err != nil {
		l.warn()
		return l
	}
	l.identity = mcpLifecycleRecord{Schema: 1, ConnectionID: id, Version: opts.Version, PID: os.Getpid()}
	dir := filepath.Join(strings.TrimSpace(opts.Root), ".agent-layer", "state", "dispatch-mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.warn()
		return l
	}
	file, err := os.OpenFile(filepath.Join(dir, l.identity.ConnectionID+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // Repository-local diagnostics with a generated basename.
	if err != nil {
		l.warn()
		return l
	}
	l.file = file
	l.append("start", "startup", "", "", nil, false)
	return l
}

func (l *mcpLifecycle) warn() {
	_, _ = fmt.Fprintln(l.stderr, "Agent Dispatch MCP lifecycle diagnostics could not be persisted")
}

// append is called with mu held (or before the recorder is shared).
func (l *mcpLifecycle) append(event, phase, condition, operation string, contextErr error, uncertain bool) {
	if l.file == nil {
		return
	}
	r := l.identity
	r.Event, r.Phase, r.Condition, r.Operation = event, phase, condition, operation
	r.Timestamp, r.ParentPID = time.Now().UTC(), os.Getppid()
	r.ClientEOF = l.framedEOF
	r.InputEOF = l.rawEOF
	r.Uncertain = uncertain
	r.ContextCondition = mcpContextCondition(contextErr)
	data, err := json.Marshal(r)
	if err == nil {
		_, err = l.file.Write(append(data, '\n'))
	}
	if err == nil {
		err = l.file.Sync()
	}
	if err != nil {
		l.warn()
		_ = l.file.Close()
		l.file = nil
	}
}

func mcpContextCondition(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline"
	case errors.Is(err, context.Canceled):
		return mcpLifecycleCancelled
	default:
		return ""
	}
}

func (l *mcpLifecycle) observe(operation string, err error, ctx context.Context, locallyClosed bool) {
	if err == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.finished {
		return
	}
	if operation == mcpLifecycleRead && errors.Is(err, io.EOF) {
		if !locallyClosed && l.rawEOF {
			// Physical EOF is latched independently. Once Close overlaps,
			// the SDK may return its closed-channel EOF instead of finishing
			// decoding (including a partial frame), so clean EOF is unproven.
			if l.closed {
				l.ambiguous = true
			} else {
				l.framedEOF = true
			}
		} else if !l.closed && !locallyClosed {
			l.ambiguous = true
		}
		// EOF caused solely by local closure is expected, not competing
		// evidence against an otherwise certain context cancellation.
		return
	}
	if mcpContextCondition(err) != "" {
		return
	}
	condition := mcpLifecycleTransportError
	switch {
	case operation == "connect":
		l.startupError = true
		condition = mcpLifecycleStartupError
	case locallyClosed:
		// An operation begun after local closure supplies no remote evidence.
		return
	default:
		// Preserve failures from operations begun while open. Close may
		// itself have caused the failure, so retain uncertainty as well.
		l.transportError = true
		l.ambiguous = l.ambiguous || l.closed
	}
	if !l.observed[operation] {
		l.observed[operation] = true
		contextErr := ctx.Err()
		l.append(mcpLifecycleEventError, "transport", condition, operation, contextErr, l.ambiguous || contextErr != nil)
	}
}

func (l *mcpLifecycle) stop(ctx context.Context, phase string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	contextErr := ctx.Err()
	if phase == "startup" && err != nil {
		l.startupError = true
		l.append(mcpLifecycleEventError, phase, mcpLifecycleStartupError, "configure", contextErr, false)
	}
	unclassifiedError := err != nil && !l.startupError && !l.transportError && (contextErr == nil || !errors.Is(err, contextErr))
	if unclassifiedError {
		l.append(mcpLifecycleEventError, phase, mcpLifecycleRunError, mcpLifecycleRun, contextErr, true)
	}
	condition, uncertain := "completion_unknown", true
	switch {
	case l.startupError:
		condition, uncertain = mcpLifecycleStartupError, false
	case l.transportError:
		condition, uncertain = mcpLifecycleTransportError, l.ambiguous || contextErr != nil || l.framedEOF
	case l.rawEOF && contextErr != nil:
		condition = mcpLifecycleUncertain
	case contextErr != nil:
		condition, uncertain = mcpContextCondition(contextErr), l.ambiguous || unclassifiedError
	case unclassifiedError:
		condition = mcpLifecycleRunError
	case l.framedEOF:
		condition, uncertain = mcpLifecycleClientEOF, l.ambiguous
	}
	l.append("stop", phase, condition, mcpLifecycleRun, contextErr, uncertain)
	l.finished = true
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			l.warn()
		}
		l.file = nil
	}
}

type mcpObservedReader struct {
	io.ReadCloser
	lifecycle *mcpLifecycle
	framing   *mcpFrameObserver
}

func (r *mcpObservedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.framing.read(p[:n])
	r.lifecycle.mu.Lock()
	if !r.lifecycle.closed && errors.Is(err, io.EOF) {
		r.lifecycle.rawEOF = true
	}
	r.lifecycle.mu.Unlock()
	return n, err
}

func (r *mcpObservedReader) Close() error {
	r.lifecycle.mu.Lock()
	r.lifecycle.closed = true
	r.lifecycle.mu.Unlock()
	return r.ReadCloser.Close()
}

type mcpNopCloseWriter struct{ io.Writer }

func (mcpNopCloseWriter) Close() error { return nil }

type mcpObservedTransport struct {
	server *mcp.Server
	mcp.Transport
	lifecycle *mcpLifecycle
	framing   *mcpFrameObserver
}

func (t *mcpObservedTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	t.lifecycle.observe("connect", err, ctx, false)
	if err != nil {
		return nil, err
	}
	return &mcpObservedConnection{Connection: c, lifecycle: t.lifecycle, framing: t.framing, server: t.server}, nil
}

type mcpObservedConnection struct {
	server *mcp.Server
	mcp.Connection
	lifecycle *mcpLifecycle
	framing   *mcpFrameObserver
}

func (c *mcpObservedConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	c.lifecycle.mu.Lock()
	closed := c.lifecycle.closed
	c.lifecycle.mu.Unlock()
	msg, err := c.Connection.Read(ctx)
	if err == nil && c.framing.next() {
		if version := mcpBatchProtocolVersion(c.server); version >= mcpProtocol20250618 {
			err = fmt.Errorf("JSON-RPC batching is not supported in 2025-06-18 and later (request version: %s)", version)
			msg = nil
		}
	}
	c.lifecycle.observe(mcpLifecycleRead, err, ctx, closed)
	return msg, err
}
func (c *mcpObservedConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.lifecycle.mu.Lock()
	closed := c.lifecycle.closed
	c.lifecycle.mu.Unlock()
	err := c.Connection.Write(ctx, msg)
	c.lifecycle.observe("write", err, ctx, closed)
	return err
}
func (c *mcpObservedConnection) Close() error {
	c.lifecycle.mu.Lock()
	c.lifecycle.closed = true
	c.lifecycle.mu.Unlock()
	return c.Connection.Close()
}

func runMCPServer(ctx context.Context, opts MCPServerOptions, stdin io.ReadCloser, stdout, stderr io.Writer) (err error) {
	var cleanup agentoptions.DiscoveryCleanup
	opts.discoveryCleanup = &cleanup
	defer func() { err = errors.Join(err, cleanup.Close()) }()
	l := newMCPLifecycle(opts, stderr)
	server, err := newDispatchMCPServer(opts)
	if err != nil {
		l.stop(ctx, "startup", err)
		return err
	}
	return serveMCPServer(ctx, server, l, stdin, stdout)
}

func serveMCPServer(ctx context.Context, server *mcp.Server, l *mcpLifecycle, stdin io.ReadCloser, stdout io.Writer) error {
	framing := &mcpFrameObserver{}
	transport := &mcp.IOTransport{Reader: &mcpObservedReader{ReadCloser: stdin, lifecycle: l, framing: framing}, Writer: mcpNopCloseWriter{stdout}}
	err := server.Run(ctx, &mcpObservedTransport{Transport: transport, lifecycle: l, framing: framing, server: server})
	l.stop(ctx, "serving", err)
	return err
}
