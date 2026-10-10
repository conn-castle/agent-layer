//go:build unix

package agentdispatch

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Duplicate only process-owned stdout. A nonblocking descriptor registered with
// Go's poller can interrupt a pending pipe write on Linux and Darwin; closing a
// descriptor in a blocking kernel write cannot reliably do that.
func newMCPStdioWriter() (*mcpStdioWriter, error) {
	fd := int(os.Stdout.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return nil, err
	}
	dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(dup, true); err != nil {
		_ = unix.Close(dup)
		return nil, err
	}
	w := &mcpStdioWriter{File: os.NewFile(uintptr(dup), "mcp-stdout"), fd: fd, nonblocking: flags&unix.O_NONBLOCK != 0}
	// Regular files do not support deadlines and cannot block on a pipe reader.
	if err := w.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, os.ErrNoDeadline) {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

type mcpStdioWriter struct {
	*os.File
	fd           int
	nonblocking  bool
	closeOnce    sync.Once
	closeErr     error
	shutdownOnce sync.Once
}

func (w *mcpStdioWriter) shutdown() {
	w.shutdownOnce.Do(func() {
		// Permit already accepted replies to drain on EOF. An unread pipe must
		// not keep the SDK waiting for in-flight response writes indefinitely.
		_ = w.SetWriteDeadline(time.Now().Add(time.Second))
	})
}

func (w *mcpStdioWriter) Close() error {
	w.closeOnce.Do(func() {
		w.closeErr = errors.Join(w.File.Close(), unix.SetNonblock(w.fd, w.nonblocking))
	})
	return w.closeErr
}
