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

// contextInput checks terminal/pipe readability so canceled upgrade prompts do not
// need another line of stdin. It never closes or changes the caller's descriptor.
type contextInput struct {
	ctx context.Context
	in  io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
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
			var selectErr error
			err := conn.Control(func(fd uintptr) {
				// Darwin poll does not support terminal devices.
				if fd >= unix.FD_SETSIZE {
					ready = 1 // Fall back to Read for descriptors outside the set.
					return
				}
				var readable unix.FdSet
				readable.Set(int(fd))
				timeout := unix.Timeval{Usec: 100000}
				ready, selectErr = unix.Select(int(fd)+1, &readable, nil, nil, &timeout)
			})
			if err != nil {
				return r.in.Read(p)
			}
			if errors.Is(selectErr, unix.EINTR) {
				continue
			}
			if selectErr != nil {
				return 0, selectErr
			}
			if ready > 0 {
				break
			}
		}
	}
	return r.in.Read(p)
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
