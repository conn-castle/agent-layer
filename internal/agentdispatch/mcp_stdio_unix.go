//go:build unix

package agentdispatch

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Coordinate process-owned streams: changing stdout's open-file description
// can also make stdin nonblocking when a launcher attaches both to one socket.
func newMCPStdio(stdin io.ReadCloser) (*mcpStdio, error) {
	originals := []*os.File{os.Stdout}
	if stdin == os.Stdin {
		originals = append(originals, os.Stdin)
	}
	s := &mcpStdio{}
	// Capture both original flag sets before changing either shared mode.
	for _, original := range originals {
		fd := original.Fd()
		flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if err != nil {
			return nil, err
		}
		s.streams = append(s.streams, mcpStdioStream{fd: fd, flags: flags})
	}
	for i := range s.streams {
		stream := &s.streams[i]
		dup, err := unix.FcntlInt(stream.fd, unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return nil, errors.Join(err, s.Close())
		}
		if err := unix.SetNonblock(dup, true); err != nil {
			return nil, errors.Join(err, unix.Close(dup), s.Close())
		}
		// NewFile registers an already nonblocking descriptor with Go's poller.
		stream.file = os.NewFile(uintptr(dup), "mcp-stdio")
	}
	s.File = s.streams[0].file
	if len(s.streams) == 2 {
		s.input = s.streams[1].file
	}
	// Regular files do not support deadlines and cannot block on a pipe reader.
	if err := s.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, os.ErrNoDeadline) {
		return nil, errors.Join(err, s.Close())
	}
	return s, nil
}

type mcpStdioStream struct {
	fd    uintptr
	flags int
	file  *os.File
}

type mcpStdio struct {
	*os.File
	input        *os.File
	streams      []mcpStdioStream
	closeOnce    sync.Once
	closeErr     error
	shutdownOnce sync.Once
}

func (s *mcpStdio) shutdown() {
	s.shutdownOnce.Do(func() {
		// Permit replies already being written to drain on EOF. An unread pipe
		// must not keep the SDK waiting for in-flight writes indefinitely.
		_ = s.SetWriteDeadline(time.Now().Add(time.Second))
	})
}

func (s *mcpStdio) Close() error {
	s.closeOnce.Do(func() {
		// Release both poller registrations before restoring shared flags.
		for _, stream := range s.streams {
			if stream.file != nil {
				s.closeErr = errors.Join(s.closeErr, stream.file.Close())
			}
		}
		for _, stream := range s.streams {
			_, err := unix.FcntlInt(stream.fd, unix.F_SETFL, stream.flags)
			s.closeErr = errors.Join(s.closeErr, err)
		}
	})
	return s.closeErr
}
