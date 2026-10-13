package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFixtureCompletesInitializationAndExposesOneTool proves the probe fixture
// is a real protocol server. The previous `/usr/bin/true` entry exited before
// initialization, so a failed discovery said nothing about the client under
// test; only a fixture that provably works makes a negative result evidence.
func TestFixtureCompletesInitializationAndExposesOneTool(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := newFixtureServer().Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect fixture server: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "probe-test", Version: "1"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != FixtureToolName {
		t.Fatalf("fixture tools = %#v, want exactly %q", listed.Tools, FixtureToolName)
	}

	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv(FixtureMarkerEnvVar, marker)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: FixtureToolName})
	if err != nil {
		t.Fatalf("call fixture tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("fixture tool reported an error: %#v", result.Content)
	}
	data, err := os.ReadFile(marker) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatalf("fixture did not record an invocation marker: %v", err)
	}
	if strings.TrimSpace(string(data)) != FixtureToolReply {
		t.Fatalf("marker content = %q, want %q", data, FixtureToolReply)
	}
}

// TestFixtureMarkerIsIgnoredWhenAbsentOrWrong proves the probe only reports a
// tool invocation on exact fixture evidence, so an unrelated file cannot make a
// client look more capable than it is.
func TestFixtureMarkerIsIgnoredWhenAbsentOrWrong(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	if FixtureInvoked(missing) {
		t.Fatal("a missing marker must not count as an invocation")
	}
	wrong := filepath.Join(dir, "wrong.txt")
	if err := os.WriteFile(wrong, []byte("something else"), 0o600); err != nil {
		t.Fatal(err)
	}
	if FixtureInvoked(wrong) {
		t.Fatal("an unrelated marker must not count as an invocation")
	}
	valid := filepath.Join(dir, "valid.txt")
	if err := os.WriteFile(valid, []byte(FixtureToolReply+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !FixtureInvoked(valid) {
		t.Fatal("a valid marker was not recognized")
	}
}
