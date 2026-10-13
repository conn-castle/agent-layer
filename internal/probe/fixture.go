package probe

import (
	"bytes"
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// FixtureSubcommand is the hidden al subcommand that serves the fixture.
	FixtureSubcommand = "__probe-mcp-fixture"
	// FixtureToolName is the single harmless tool the probe fixture exposes.
	FixtureToolName = "probe_ping"
	// FixtureToolReply is returned by the fixture tool. It is unique so probe
	// output cannot be confused with any other text.
	FixtureToolReply = "PROBEMCPTOOL33"
	// FixtureMarkerEnvVar names the file the fixture touches when its tool runs.
	// A marker file is stronger evidence than transcript text: it proves the
	// client actually executed the tool rather than describing it.
	FixtureMarkerEnvVar = "AL_PROBE_MCP_MARKER"
	// FixtureInvokedEvidence is the evidence recorded when FixtureInvoked
	// confirms the client executed the fixture tool.
	FixtureInvokedEvidence = "mcp fixture: " + FixtureToolName + " was invoked by the client"
)

// fixtureInput is the fixture tool's (empty) input.
type fixtureInput struct{}

// fixtureOutput is the fixture tool's structured output.
type fixtureOutput struct {
	Reply string `json:"reply"`
}

// RunMCPFixture serves a minimal, deterministic MCP server over stdio for
// capability probes. It completes protocol initialization and exposes exactly
// one harmless tool, so a failed discovery or tool call is evidence about the
// client rather than about the fixture.
func RunMCPFixture(ctx context.Context) error {
	return newFixtureServer().Run(ctx, &mcp.StdioTransport{})
}

func newFixtureServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-layer-probe", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        FixtureToolName,
		Description: "Return a fixed probe token. Takes no arguments and changes nothing.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ fixtureInput) (*mcp.CallToolResult, fixtureOutput, error) {
		if marker := os.Getenv(FixtureMarkerEnvVar); marker != "" {
			// Best effort: a marker the probe cannot write must not turn a
			// successful tool call into a client-side failure.
			_ = os.WriteFile(marker, []byte(FixtureToolReply), 0o600) // #nosec G703 -- marker path is supplied by the probe that launched this fixture.
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: FixtureToolReply}},
		}, fixtureOutput{Reply: FixtureToolReply}, nil
	})
	return server
}

// FixtureInvoked reports whether the marker at markerPath holds exactly the
// fixture reply, so an absent or unrelated file never counts as an invocation.
func FixtureInvoked(markerPath string) bool {
	data, err := os.ReadFile(markerPath) // #nosec G304 -- probe-owned path.
	if err != nil {
		return false
	}
	return string(bytes.TrimSpace(data)) == FixtureToolReply
}
