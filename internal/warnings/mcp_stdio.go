package warnings

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/ownedprocess"
)

// mcpStdioShutdownGrace matches the SDK command transport's wait for a server
// to exit after end of input, and again after SIGTERM.
var mcpStdioShutdownGrace = 5 * time.Second

var startOwnedProcess = ownedprocess.Start

// ownedStdioTransport runs a stdio MCP server in an owned process group. The
// SDK's CommandTransport waits only for the server leader, so descendants could
// outlive discovery. Close stops and joins the whole group.
type ownedStdioTransport struct {
	cmd *exec.Cmd
	// ctx bounds the server's lifetime; cancelling it stops the whole group.
	ctx context.Context

	stop      context.CancelFunc
	stdin     *os.File
	stdout    *os.File
	exited    chan error
	closeOnce sync.Once
	closeErr  error
}

// Connect starts the server over caller-owned pipes and connects to it.
func (t *ownedStdioTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	stdout, serverOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	serverIn, stdin, err := os.Pipe()
	if err != nil {
		_ = stdout.Close()
		_ = serverOut.Close()
		return nil, err
	}
	runCtx, stop := context.WithCancel(t.ctx)
	t.cmd.Stdin, t.cmd.Stdout = serverIn, serverOut
	wait, err := startOwnedProcess(runCtx, t.cmd)
	// The server holds its own copies of these ends.
	_ = serverIn.Close()
	_ = serverOut.Close()
	if err != nil {
		stop()
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	t.stop, t.stdin, t.stdout = stop, stdin, stdout
	t.exited = make(chan error, 1)
	go func() { t.exited <- wait() }()
	// Closing the connection closes server input; output stays readable until
	// the group has stopped.
	transport := &mcp.IOTransport{Reader: io.NopCloser(stdout), Writer: ownedStdioInput{stdin, t}}
	return transport.Connect(ctx)
}

// Close ends server input and, like the SDK command transport, sends SIGTERM
// to a server still running after the grace. A server still running after a
// second grace has its process group stopped. Cleanup is joined in every case;
// cancellation of ctx skips the graces. Later calls return the first result.
func (t *ownedStdioTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.stop == nil {
			return
		}
		defer t.stop()
		inputErr := t.stdin.Close()
		grace := time.NewTimer(mcpStdioShutdownGrace)
		defer grace.Stop()
		var waitErr error
		select {
		case waitErr = <-t.exited:
		case <-grace.C:
			// The owned runner reaps the leader only after its own stop, so this
			// signal cannot reach a reused process ID.
			_ = t.cmd.Process.Signal(syscall.SIGTERM)
			grace.Reset(mcpStdioShutdownGrace)
			select {
			case waitErr = <-t.exited:
			case <-grace.C:
				t.stop()
				waitErr = <-t.exited
			}
		}
		t.closeErr = errors.Join(inputErr, waitErr, t.stdout.Close())
	})
	return t.closeErr
}

// ownedStdioInput routes the SDK's connection close through the transport.
type ownedStdioInput struct {
	*os.File
	owner *ownedStdioTransport
}

func (w ownedStdioInput) Close() error { return w.owner.Close() }
