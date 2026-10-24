package agentdispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// Timeouts and interrupted waits must not trigger a read: an unconnected FIFO
// would return zero bytes, indistinguishable from a connected writer closing.
var errPromptNotReadable = errors.New("dispatch prompt is not readable yet")

func promptPreparationCancelled(ctx context.Context) error {
	return wrapExitError(ExitUnavailable, "dispatch prompt preparation was cancelled; nothing was sent to the provider", ctx.Err())
}

func promptFileError(path, op string, err error) error {
	return wrapExitError(ExitUsage, "read dispatch prompt file", &os.PathError{Op: op, Path: path, Err: err})
}

func checkPromptFileSize(size int64) error {
	if size > MaxStdinPromptBytes {
		return exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", size, MaxStdinPromptBytes))
	}
	return nil
}

// readPromptBytes keeps both read sizes and buffer capacity within the cap plus
// one byte, so streams can be rejected without draining them or waiting for EOF.
func readPromptBytes(ctx context.Context, path string, size int64, read func([]byte) (int, error)) ([]byte, error) {
	const chunk = 64 * 1024
	const limit = MaxStdinPromptBytes + 1
	capacity := chunk
	if size > 0 {
		capacity = int(min(size, int64(MaxStdinPromptBytes))) + 1
	}
	data := make([]byte, 0, capacity)
	for {
		if ctx.Err() != nil {
			return nil, promptPreparationCancelled(ctx)
		}
		if len(data) == cap(data) {
			grown := make([]byte, len(data), min(max(2*cap(data), chunk), limit))
			copy(grown, data)
			data = grown
		}
		n, err := read(data[len(data):cap(data)])
		if n > 0 {
			data = data[:len(data)+n]
		}
		if len(data) > MaxStdinPromptBytes {
			return nil, exitError(ExitUsage, fmt.Sprintf("dispatch prompt file exceeds the maximum of %d bytes", MaxStdinPromptBytes))
		}
		if err != nil && err != io.EOF {
			if ctx.Err() != nil {
				return nil, promptPreparationCancelled(ctx)
			}
			return nil, promptFileError(path, "read", err)
		}
		if n == 0 || err == io.EOF {
			if ctx.Err() != nil {
				return nil, promptPreparationCancelled(ctx)
			}
			return data, nil
		}
	}
}
