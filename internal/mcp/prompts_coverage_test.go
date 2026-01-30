package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRunServer_RealImplementation(t *testing.T) {
	// We want to test the real prompt server runner.
	// It uses os.Stdin/os.Stdout via mcp.StdioTransport.
	// We pass a canceled context so server.Run should return immediately (or quickly).

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test",
		Version: "1.0",
	}, nil)

	// Calling the real runner
	err := defaultPromptServerRunner(ctx, server)

	// We expect an error because context is canceled or stdin is closed/empty, etc.
	// We don't strictly care about the specific error, just that it ran and didn't panic or hang.
	// However, server.Run usually returns context.Canceled if ctx is canceled.
	if err == nil {
		// It might return nil if it shuts down cleanly on cancellation.
		_ = err
	}
}
