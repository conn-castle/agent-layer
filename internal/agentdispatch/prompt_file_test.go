package agentdispatch

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestPromptFileCancellationRacingEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := readPromptBytes(ctx, "prompt", 0, func(buf []byte) (int, error) {
		copy(buf, "prompt")
		cancel()
		return len("prompt"), io.EOF
	})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitUnavailable || !errors.Is(err, context.Canceled) {
		t.Fatalf("EOF racing cancellation = %v, want cancellation with exit 69", err)
	}
}
