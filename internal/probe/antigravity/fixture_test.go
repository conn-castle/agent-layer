package antigravity

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

// TestProbeSeedsTheValidMCPFixture proves the seeded MCP configuration points
// at the fixture rather than a program that cannot speak the protocol.
func TestProbeSeedsTheValidMCPFixture(t *testing.T) {
	probeDir := t.TempDir()
	fixture, err := probeMCPFixtureCommand(probeDir)
	if err != nil {
		t.Fatalf("resolve fixture command: %v", err)
	}
	if _, _, _, err := seedProbeWorkspace(probeDir, fixture); err != nil {
		t.Fatalf("seed probe workspace: %v", err)
	}
	for _, rel := range []string{
		filepath.Join("workspace", ".agents", "mcp_config.json"),
		filepath.Join("agycfg", "antigravity-cli", "mcp_config.json"),
	} {
		data, err := os.ReadFile(filepath.Join(probeDir, rel)) // #nosec G304 -- test-owned path.
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(data), "/usr/bin/true") {
			t.Fatalf("%s still seeds a non-protocol MCP command:\n%s", rel, data)
		}
		if !strings.Contains(string(data), "__probe-mcp-fixture") {
			t.Fatalf("%s does not seed the probe MCP fixture:\n%s", rel, data)
		}
	}
}

// TestFixtureMarkerIsIgnoredWhenAbsentOrWrong proves the probe only reports a
// tool invocation on exact fixture evidence, so an unrelated file cannot make
// Antigravity look more capable than it is.
func TestFixtureMarkerIsIgnoredWhenAbsentOrWrong(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	if invoked, _ := inspectFixtureMarker(missing); invoked {
		t.Fatal("a missing marker must not count as an invocation")
	}
	wrong := filepath.Join(dir, "wrong.txt")
	if err := os.WriteFile(wrong, []byte("something else"), 0o600); err != nil {
		t.Fatal(err)
	}
	if invoked, _ := inspectFixtureMarker(wrong); invoked {
		t.Fatal("an unrelated marker must not count as an invocation")
	}
}
