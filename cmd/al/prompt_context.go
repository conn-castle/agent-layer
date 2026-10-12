package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// contextInput polls terminal/pipe readiness so canceled upgrade prompts do not
// need another line of stdin. It never closes or changes the caller's descriptor.
type contextInput struct {
	ctx context.Context
	in  io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return r.in.Read(p)
	}
	if file, ok := r.in.(*os.File); ok && r.ctx.Done() != nil {
		conn, err := file.SyscallConn()
		if err != nil {
			return r.in.Read(p)
		}
		for {
			if err := r.ctx.Err(); err != nil {
				return 0, err
			}
			var ready int
			var pollErr error
			err := conn.Control(func(fd uintptr) {
				fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // OS descriptors fit poll's int32 field.
				ready, pollErr = unix.Poll(fds, 100)
			})
			if err != nil {
				return r.in.Read(p)
			}
			if errors.Is(pollErr, unix.EINTR) {
				continue
			}
			if pollErr != nil {
				return 0, pollErr
			}
			if ready > 0 {
				break
			}
		}
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.in.Read(p)
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return 0, cancelErr
	}
	return n, err
}

// contextPromptReader checks cancellation even when later answers are already
// buffered, while retaining one read-ahead buffer for the whole upgrade.
type contextPromptReader struct {
	*bufio.Reader
	ctx context.Context
}

func (r *contextPromptReader) ReadString(delim byte) (string, error) {
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	line, err := r.Reader.ReadString(delim)
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return "", cancelErr
	}
	return line, err
}
