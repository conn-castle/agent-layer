package warnings

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envref"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// MockConnector implements Connector for testing. Servers missing from
// Results go to Next when set; the built-in Agent Dispatch server otherwise
// reports reachable with no tools so tests never launch al.
type MockConnector struct {
	Results map[string]DiscoveryResult
	Next    Connector
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (m *MockConnector) ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
	if res, ok := m.Results[server.ID]; ok {
		return res
	}
	if server.ID == projection.BuiltInDispatchServerID {
		return DiscoveryResult{ServerID: server.ID}
	}
	if m.Next != nil {
		return m.Next.ConnectAndDiscover(ctx, server)
	}
	return DiscoveryResult{ServerID: server.ID, Error: fmt.Errorf("mock not found")}
}

// receivingAgents enables a client so user-configured servers reach generated
// config. It also adds the built-in Agent Dispatch server, which MockConnector
// reports as reachable with no tools.
func receivingAgents() config.AgentsConfig {
	on := true
	return config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &on}}
}

// TestCheckMCPServers_NoEnabledServers verifies that a config whose servers are
// all disabled produces an *available* summary with EnabledServers == 0. The
// unavailable summary (MCPSummary{}) is reserved for genuine resolution failures
// of an enabled server; "no enabled servers" resolves cleanly. This guards the
// doctor size summary against reporting "size unavailable" when there is simply
// nothing to discover.
func TestCheckMCPServers_NoEnabledServers(t *testing.T) {
	disabled := false
	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s1", Enabled: &disabled, Transport: "stdio", Command: "echo"},
				},
			},
		},
		Env: map[string]string{},
	}

	warnings, summary, err := CheckMCPServers(context.Background(), cfg, &MockConnector{}, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.True(t, summary.Available, "no enabled servers is not a discovery failure")
	assert.Equal(t, 0, summary.EnabledServers)
	assert.Equal(t, 0, summary.TotalTools)
	assert.Equal(t, 0, summary.TotalSchemaTokens)
}

func TestCheckMCPServers(t *testing.T) {
	// Setup config
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "echo", Args: []string{"hello"}},
					{ID: "s2", Enabled: &enabled, Transport: "http", URL: "http://localhost"},
				},
			},
		},
		Env: map[string]string{},
	}

	// Setup mock results
	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {
				ServerID: "s1",
				Tools: []ToolDef{
					{Name: "tool1"},
				},
				SchemaTokens: 100,
			},
			"s2": {
				ServerID: "s2",
				Tools: []ToolDef{
					{Name: "tool2"}, // no collision
				},
				SchemaTokens: 100,
			},
		},
	}

	warnings, summary, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	assert.True(t, summary.Available)
	assert.Equal(t, 3, summary.EnabledServers, "s1, s2, and the built-in server")
	assert.Equal(t, 3, summary.ReachableServers)
	assert.Equal(t, 2, summary.TotalTools)
	assert.Equal(t, 200, summary.TotalSchemaTokens)
}

func TestCheckMCPServers_SummaryExcludesUnreachableServers(t *testing.T) {
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "echo"},
					{ID: "s2", Enabled: &enabled, Transport: "stdio", Command: "echo"},
				},
			},
		},
		Env: map[string]string{},
	}
	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {ServerID: "s1", Tools: []ToolDef{{Name: "tool1"}}, SchemaTokens: 100},
			"s2": {ServerID: "s2", Error: fmt.Errorf("connection refused")},
		},
	}

	_, summary, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	assert.True(t, summary.Available)
	assert.Equal(t, 3, summary.EnabledServers, "s1, s2, and the built-in server")
	assert.Equal(t, 2, summary.ReachableServers)
	assert.Equal(t, 1, summary.TotalTools)
	assert.Equal(t, 100, summary.TotalSchemaTokens)
}

func TestCheckMCPServers_Warnings(t *testing.T) {
	enabled := true
	serverThreshold := 6
	serverToolsThreshold := 25
	serverSchemaThreshold := 7500
	toolsTotalThreshold := 28
	schemaTotalThreshold := 8000

	// Create many servers to trigger TOO_MANY_SERVERS
	var servers []config.MCPServer
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("s%d", i)
		servers = append(servers, config.MCPServer{
			ID: id, Enabled: &enabled, Transport: "stdio", Command: "echo",
		})
	}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP:    config.MCPConfig{Servers: servers},
			Warnings: config.WarningsConfig{
				MCPServerThreshold:             &serverThreshold,
				MCPServerToolsThreshold:        &serverToolsThreshold,
				MCPSchemaTokensServerThreshold: &serverSchemaThreshold,
				MCPToolsTotalThreshold:         &toolsTotalThreshold,
				MCPSchemaTokensTotalThreshold:  &schemaTotalThreshold,
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{Results: make(map[string]DiscoveryResult)}

	// Server 0: Unreachable
	mock.Results["s0"] = DiscoveryResult{ServerID: "s0", Error: fmt.Errorf("connection refused")}

	// Server 1: Too many tools
	var toolsS1 []ToolDef
	for i := 0; i < 26; i++ {
		toolsS1 = append(toolsS1, ToolDef{Name: fmt.Sprintf("t%d", i)})
	}
	mock.Results["s1"] = DiscoveryResult{ServerID: "s1", Tools: toolsS1, SchemaTokens: 100}

	// Server 2: Schema bloat
	mock.Results["s2"] = DiscoveryResult{ServerID: "s2", Tools: []ToolDef{{Name: "t", Tokens: 7501}}, SchemaTokens: 8000}

	// Server 3 & 4: Name collision
	mock.Results["s3"] = DiscoveryResult{ServerID: "s3", Tools: []ToolDef{{Name: "collision"}}}
	mock.Results["s4"] = DiscoveryResult{ServerID: "s4", Tools: []ToolDef{{Name: "collision"}}}

	// Fill the rest
	for i := 5; i < 7; i++ {
		id := fmt.Sprintf("s%d", i)
		mock.Results[id] = DiscoveryResult{ServerID: id}
	}

	warnings, _, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)

	// We expect multiple warnings.
	// 1. TOO_MANY_SERVERS_ENABLED (7 > 6)
	// 2. SERVER_UNREACHABLE (s0)
	// 3. SERVER_TOO_MANY_TOOLS (s1)
	// 4. TOOL_SCHEMA_BLOAT_SERVER (s2)
	// 5. TOOL_NAME_COLLISION (collision)
	// 6. TOO_MANY_TOOLS_TOTAL (29 > 28)
	// 7. TOOL_SCHEMA_BLOAT_TOTAL (8100 > 8000)

	codes := make(map[string]bool)
	var bloatWarning Warning
	for _, w := range warnings {
		codes[w.Code] = true
		if w.Code == CodeMCPToolSchemaBloatServer {
			bloatWarning = w
		}
	}

	assert.True(t, codes[CodeMCPTooManyServers], "Expected TOO_MANY_SERVERS")
	assert.True(t, codes[CodeMCPServerUnreachable], "Expected SERVER_UNREACHABLE")
	assert.True(t, codes[CodeMCPServerTooManyTools], "Expected SERVER_TOO_MANY_TOOLS")
	assert.True(t, codes[CodeMCPToolSchemaBloatServer], "Expected TOOL_SCHEMA_BLOAT_SERVER")
	assert.True(t, codes[CodeMCPToolNameCollision], "Expected TOOL_NAME_COLLISION")
	assert.True(t, codes[CodeMCPTooManyToolsTotal], "Expected TOO_MANY_TOOLS_TOTAL")
	assert.True(t, codes[CodeMCPToolSchemaBloatTotal], "Expected TOOL_SCHEMA_BLOAT_TOTAL")
	for _, w := range warnings {
		if w.Code == CodeMCPServerUnreachable && w.Subject == "s0" {
			assert.Equal(t, SourceExternalDependency, w.Source)
			assert.Equal(t, SeverityCritical, w.Severity)
		}
	}

	// Verify bloat details
	assert.NotEmpty(t, bloatWarning.Details)
	assert.Contains(t, bloatWarning.Details[0], "Top contributors")
	assert.Contains(t, bloatWarning.Details[1], "t: 7501 tokens")
}

func TestCheckMCPServers_ToolNameCollisionWarningsDeterministicOrder(t *testing.T) {
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s2", Enabled: &enabled, Transport: "stdio", Command: "echo"},
					{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "echo"},
				},
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {
				ServerID: "s1",
				Tools: []ToolDef{
					{Name: "zeta"},
					{Name: "alpha"},
				},
			},
			"s2": {
				ServerID: "s2",
				Tools: []ToolDef{
					{Name: "zeta"},
					{Name: "alpha"},
				},
			},
		},
	}

	for range 25 {
		warnings, _, err := CheckMCPServers(context.Background(), cfg, mock, nil)
		require.NoError(t, err)
		require.Len(t, warnings, 2)

		assert.Equal(t, CodeMCPToolNameCollision, warnings[0].Code)
		assert.Equal(t, "alpha", warnings[0].Subject)
		assert.Contains(t, warnings[0].Message, "[s1 s2]")

		assert.Equal(t, CodeMCPToolNameCollision, warnings[1].Code)
		assert.Equal(t, "zeta", warnings[1].Subject)
		assert.Contains(t, warnings[1].Message, "[s1 s2]")
	}
}

func TestCheckMCPServers_ThresholdsDisabled(t *testing.T) {
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "echo"},
					{ID: "s2", Enabled: &enabled, Transport: "stdio", Command: "echo"},
				},
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {
				ServerID: "s1",
				Tools: []ToolDef{
					{Name: "collision"},
				},
				SchemaTokens: 9000,
			},
			"s2": {
				ServerID: "s2",
				Tools: []ToolDef{
					{Name: "collision"},
				},
				SchemaTokens: 9000,
			},
		},
	}

	warnings, _, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)

	codes := make(map[string]bool)
	for _, w := range warnings {
		codes[w.Code] = true
	}

	assert.True(t, codes[CodeMCPToolNameCollision], "Expected TOOL_NAME_COLLISION")
	assert.False(t, codes[CodeMCPTooManyServers], "Did not expect TOO_MANY_SERVERS")
	assert.False(t, codes[CodeMCPServerTooManyTools], "Did not expect SERVER_TOO_MANY_TOOLS")
	assert.False(t, codes[CodeMCPToolSchemaBloatServer], "Did not expect TOOL_SCHEMA_BLOAT_SERVER")
	assert.False(t, codes[CodeMCPTooManyToolsTotal], "Did not expect TOO_MANY_TOOLS_TOTAL")
	assert.False(t, codes[CodeMCPToolSchemaBloatTotal], "Did not expect TOOL_SCHEMA_BLOAT_TOTAL")
}

func TestCheckMCPServers_ResolveServerError(t *testing.T) {
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "bad-server", Enabled: &enabled, Transport: "http", URL: "${XYZZY_NONEXISTENT_VAR_12345}"},
				},
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{Results: map[string]DiscoveryResult{}}
	warnings, _, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, CodeMCPServerUnreachable, warnings[0].Code)
	assert.Equal(t, "bad-server", warnings[0].Subject)
	assert.Contains(t, warnings[0].Message, "Failed to resolve configuration")
	assert.Equal(t, SourceInternal, warnings[0].Source)
	assert.Equal(t, SeverityCritical, warnings[0].Severity)
}

func TestCheckMCPServers_ResolvesProcessEnv(t *testing.T) {
	t.Setenv("AL_PROCESS_TOKEN", "from-shell")
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "remote", Enabled: &enabled, Transport: "http", URL: "https://example.com/mcp?token=${AL_PROCESS_TOKEN}"},
				},
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{Results: map[string]DiscoveryResult{"remote": {ServerID: "remote"}}}
	warnings, summary, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.True(t, summary.Available)
}

func TestDiscoverTools(t *testing.T) {
	servers := []projection.ResolvedMCPServer{
		{ID: "s1"},
		{ID: "s2"},
		{ID: "s3"},
	}

	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {ServerID: "s1", Tools: []ToolDef{{Name: "tool1"}}},
			"s2": {ServerID: "s2", Tools: []ToolDef{{Name: "tool2"}}},
			"s3": {ServerID: "s3", Error: fmt.Errorf("connection failed")},
		},
	}

	results := discoverTools(context.Background(), servers, mock, nil, nil)
	require.Len(t, results, 3)

	// Results should be in order
	assert.Equal(t, "s1", results[0].ServerID)
	assert.Len(t, results[0].Tools, 1)
	assert.Equal(t, "s2", results[1].ServerID)
	assert.Len(t, results[1].Tools, 1)
	assert.Equal(t, "s3", results[2].ServerID)
	assert.Error(t, results[2].Error)
}

func TestDiscoverTools_Empty(t *testing.T) {
	mock := &MockConnector{Results: map[string]DiscoveryResult{}}
	results := discoverTools(context.Background(), nil, mock, nil, nil)
	assert.Empty(t, results)
}

func TestDiscoverTools_EmitsDiscoveryEvents(t *testing.T) {
	servers := []projection.ResolvedMCPServer{
		{ID: "s1"},
		{ID: "s2"},
	}

	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {ServerID: "s1"},
			"s2": {ServerID: "s2", Error: fmt.Errorf("boom")},
		},
	}

	var mu sync.Mutex
	events := make([]MCPDiscoveryEvent, 0, 4)
	statusFn := func(event MCPDiscoveryEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	_ = discoverTools(context.Background(), servers, mock, statusFn, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	counts := make(map[string]map[MCPDiscoveryStatus]int)
	for _, event := range events {
		if counts[event.ServerID] == nil {
			counts[event.ServerID] = make(map[MCPDiscoveryStatus]int)
		}
		counts[event.ServerID][event.Status]++

		if event.Status == MCPDiscoveryStatusError && event.Err == nil {
			t.Fatalf("expected error event to include error for server %s", event.ServerID)
		}
		if event.Status == MCPDiscoveryStatusDone && event.Err != nil {
			t.Fatalf("did not expect error on done event for server %s", event.ServerID)
		}
	}

	if counts["s1"][MCPDiscoveryStatusStart] != 1 || counts["s1"][MCPDiscoveryStatusDone] != 1 {
		t.Fatalf("expected start+done for s1, got %#v", counts["s1"])
	}
	if counts["s2"][MCPDiscoveryStatusStart] != 1 || counts["s2"][MCPDiscoveryStatusError] != 1 {
		t.Fatalf("expected start+error for s2, got %#v", counts["s2"])
	}
}

type blockingConnector struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	active  int
	max     int
}

func (b *blockingConnector) ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
	b.mu.Lock()
	b.active++
	if b.active > b.max {
		b.max = b.active
	}
	b.mu.Unlock()

	b.started <- struct{}{}
	<-b.release

	b.mu.Lock()
	b.active--
	b.mu.Unlock()

	return DiscoveryResult{ServerID: server.ID}
}

func (b *blockingConnector) MaxActive() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.max
}

func TestDiscoverTools_ConcurrencyLimit(t *testing.T) {
	original := runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(6)
	t.Cleanup(func() { runtime.GOMAXPROCS(original) })

	servers := make([]projection.ResolvedMCPServer, 10)
	for i := range servers {
		servers[i] = projection.ResolvedMCPServer{ID: fmt.Sprintf("s%d", i)}
	}

	expected := mcpDiscoveryConcurrency(len(servers))
	require.Greater(t, expected, 0, "expected positive concurrency limit")

	connector := &blockingConnector{
		started: make(chan struct{}, len(servers)),
		release: make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		_ = discoverTools(context.Background(), servers, connector, nil, nil)
		close(done)
	}()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for i := 0; i < expected; i++ {
		select {
		case <-connector.started:
		case <-timer.C:
			t.Fatalf("timed out waiting for %d workers to start, got %d", expected, i)
		}
	}

	close(connector.release)
	<-done

	assert.Equal(t, expected, connector.MaxActive(), "unexpected max concurrency")
}

func TestHeaderTransport_RoundTrip(t *testing.T) {
	// Create a test server that echoes back the received headers
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return the header values we care about
		w.Header().Set("X-Received-Auth", r.Header.Get("Authorization"))
		w.Header().Set("X-Received-Custom", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	origin, err := url.Parse(ts.URL)
	require.NoError(t, err)
	transport := &headerTransport{
		base:   http.DefaultTransport,
		origin: origin,
		headers: map[string]string{
			"Authorization":   "Bearer test-token",
			"X-Custom-Header": "custom-value",
		},
	}

	client := &http.Client{Transport: transport}
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Bearer test-token", resp.Header.Get("X-Received-Auth"))
	assert.Equal(t, "custom-value", resp.Header.Get("X-Received-Custom"))
}

func TestHeaderTransport_NilBase(t *testing.T) {
	// When base is nil, should use DefaultTransport
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Received-Auth", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	origin, err := url.Parse(ts.URL)
	require.NoError(t, err)
	transport := &headerTransport{
		base:   nil, // nil base
		origin: origin,
		headers: map[string]string{
			"Authorization": "Bearer test-token",
		},
	}

	client := &http.Client{Transport: transport}
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Bearer test-token", resp.Header.Get("X-Received-Auth"))
}

func TestHeaderTransport_DoesNotMutateOriginalRequest(t *testing.T) {
	origin, err := url.Parse("HTTPS://EXAMPLE.COM:443/mcp")
	require.NoError(t, err)
	transport := &headerTransport{
		origin: origin,
		base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "dummy-key", req.Header.Get("X-Api-Key"))
			return &http.Response{
				Body: http.NoBody,
			}, nil
		}),
		headers: map[string]string{"Authorization": "Bearer token", "X-Api-Key": "dummy-key"},
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("X-Existing", "value")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	assert.Equal(t, "", req.Header.Get("Authorization"))
	assert.Equal(t, "", req.Header.Get("X-Api-Key"))
	assert.Equal(t, "value", req.Header.Get("X-Existing"))
}

func TestSameHTTPOrigin(t *testing.T) {
	for origin, destinations := range map[string]map[string]bool{
		"HTTPS://EXAMPLE.COM:443/mcp":    {"https://example.com/messages": true, "https://other.example": false, "https://example.com:444": false, "http://example.com:443": false},
		"HTTP://EXAMPLE.COM:80/mcp":      {"http://example.com/messages": true},
		"http://[fe80::A%25eth0]:80/mcp": {"http://[fe80::a%25eth0]": true, "http://[fe80::a%25ETH0]": false, "http://[fe80::a]": false},
	} {
		a, err := url.Parse(origin)
		require.NoError(t, err)
		for endpoint, want := range destinations {
			b, err := url.Parse(endpoint)
			require.NoError(t, err)
			assert.Equal(t, want, sameHTTPOrigin(a, b), "%s -> %s", origin, endpoint)
		}
	}
}

func TestCheckMCPServers_OriginRefusal(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer destination.Close()
	refusedURL := destination.URL + "/messages?session=destination-secret"
	for _, mode := range []string{"sse-redirect", "streamable", "sse-endpoint"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "configured-secret", r.Header.Get("X-Api-Key"))
				if mode != "sse-endpoint" {
					http.Redirect(w, r, refusedURL, http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", refusedURL)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer origin.Close()
			enabled := true
			server := config.MCPServer{ID: mode, Enabled: &enabled, Transport: config.TransportHTTP, URL: origin.URL, Headers: map[string]string{"X-Api-Key": "configured-secret"}}
			if mode == "streamable" {
				server.HTTPTransport = config.HTTPTransportStreamable
			}
			cfg := &config.ProjectConfig{Config: config.Config{Agents: receivingAgents(), MCP: config.MCPConfig{Servers: []config.MCPServer{server}}}}
			warnings, _, err := CheckMCPServers(context.Background(), cfg, &MockConnector{Next: &RealConnector{}}, func(e MCPDiscoveryEvent) {
				if e.ServerID == mode && e.Status == MCPDiscoveryStatusError {
					assert.EqualError(t, e.Err, messages.WarningsMCPOriginRefused)
				}
			})
			require.NoError(t, err)
			require.Len(t, warnings, 1)
			assert.Equal(t, CodeMCPServerUnreachable, warnings[0].Code)
			assert.Equal(t, fmt.Sprintf(messages.WarningsMCPConnectFailedFmt, errMCPOriginRefused), warnings[0].Message)
			assert.NotContains(t, warnings[0].Message, refusedURL)
			assert.Zero(t, calls.Load(), "refuse before transmission")
		})
	}
}

func TestBuildMCPCommandEnv_AllowlistsBaseEnv(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"XDG_CACHE_HOME=/tmp/cache",
		"AL_TAVILY_API_KEY=abc123",
		"AL_SHIM_ACTIVE=1",
		"GITHUB_TOKEN=should-not-pass",
		"AWS_SECRET_ACCESS_KEY=should-not-pass",
	}
	serverEnv := map[string]string{
		"CUSTOM_TOKEN":   "server-value",
		"PATH":           "/custom/bin",
		"AL_SHIM_ACTIVE": "server-value",
	}

	env := buildMCPCommandEnv(base, serverEnv)
	joined := strings.Join(env, "\n")

	assert.Contains(t, joined, "PATH=/custom/bin")
	assert.Contains(t, joined, "HOME=/tmp/home")
	assert.Contains(t, joined, "XDG_CACHE_HOME=/tmp/cache")
	assert.Contains(t, joined, "AL_TAVILY_API_KEY=abc123")
	assert.NotContains(t, joined, "AL_SHIM_ACTIVE")
	assert.Contains(t, joined, "CUSTOM_TOKEN=server-value")
	assert.NotContains(t, joined, "GITHUB_TOKEN=should-not-pass")
	assert.NotContains(t, joined, "AWS_SECRET_ACCESS_KEY=should-not-pass")
}

func TestBuildMCPCommandEnv_NormalizesAllowlistedKeyCasing(t *testing.T) {
	base := []string{
		"path=/usr/bin",
	}
	serverEnv := map[string]string{
		"PATH": "/custom/bin",
	}

	env := buildMCPCommandEnv(base, serverEnv)
	joined := strings.Join(env, "\n")

	assert.Contains(t, joined, "PATH=/custom/bin")
	assert.NotContains(t, joined, "path=/usr/bin")
}

func TestRealConnector_UnsupportedTransport(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-server",
		Transport: "unsupported",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Equal(t, "test-server", result.ServerID)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "unsupported transport")
}

func TestWarningString(t *testing.T) {
	w := Warning{
		Code:    CodeMCPTooManyServers,
		Subject: "mcp.servers",
		Message: "too many servers enabled",
		Fix:     "disable some servers",
	}

	s := w.String()
	assert.Contains(t, s, "WARNING MCP_TOO_MANY_SERVERS_ENABLED")
	assert.Contains(t, s, "too many servers enabled")
	assert.Contains(t, s, "source: internal")
	assert.Contains(t, s, "severity: warning")
	assert.Contains(t, s, "subject: mcp.servers")
	assert.Contains(t, s, "fix: disable some servers")
}

func TestWarningString_WithDetails(t *testing.T) {
	w := Warning{
		Code:     CodeMCPToolNameCollision,
		Subject:  "tool1",
		Message:  "name collision",
		Fix:      "rename tools",
		Details:  []string{"server1", "server2"},
		Source:   SourceNetwork,
		Severity: SeverityCritical,
	}

	s := w.String()
	assert.Contains(t, s, "WARNING MCP_TOOL_NAME_COLLISION")
	assert.Contains(t, s, "source: network")
	assert.Contains(t, s, "severity: critical")
	assert.Contains(t, s, "details: server1")
	assert.Contains(t, s, "details: server2")
}

func TestRealConnector_StdioConnectionError(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-stdio",
		Transport: "stdio",
		Command:   "nonexistent-command-xyzzy-12345",
		Args:      []string{},
	}

	// This should fail when trying to execute the nonexistent command
	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Equal(t, "test-stdio", result.ServerID)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "connection failed")
}

func TestMCPDiscoveryTransport_Stdio(t *testing.T) {
	transport, err := mcpDiscoveryTransport(context.Background(), projection.ResolvedMCPServer{
		ID:        "test-stdio-env",
		Transport: config.TransportStdio,
		Command:   "mcp-server",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"TEST_VAR": "test_value"},
	})
	require.NoError(t, err)

	commandTransport, ok := transport.(*mcp.CommandTransport)
	require.True(t, ok, "expected CommandTransport, got %T", transport)
	assert.Equal(t, []string{"mcp-server", "arg1", "arg2"}, commandTransport.Command.Args)
	assert.Contains(t, commandTransport.Command.Env, "TEST_VAR=test_value")
	assert.NotNil(t, commandTransport.Command.Cancel, "stdio command must be bound to the discovery context")
}

func TestMCPDiscoveryTransport_HTTP(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer token"}
	tests := []struct {
		name          string
		httpTransport string
		headers       map[string]string
		streamable    bool
	}{
		{name: "default SSE"},
		{name: "SSE with headers", httpTransport: config.HTTPTransportSSE, headers: headers},
		{name: "streamable", httpTransport: config.HTTPTransportStreamable, streamable: true},
		{name: "streamable with headers", httpTransport: config.HTTPTransportStreamable, headers: headers, streamable: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transport, err := mcpDiscoveryTransport(context.Background(), projection.ResolvedMCPServer{
				ID:            "test-http",
				Transport:     config.TransportHTTP,
				HTTPTransport: tc.httpTransport,
				URL:           "http://example.com/mcp",
				Headers:       tc.headers,
			})
			require.NoError(t, err)

			var endpoint string
			var client *http.Client
			switch typed := transport.(type) {
			case *mcp.SSEClientTransport:
				require.False(t, tc.streamable, "expected StreamableClientTransport, got SSE")
				endpoint, client = typed.Endpoint, typed.HTTPClient
			case *mcp.StreamableClientTransport:
				require.True(t, tc.streamable, "expected SSEClientTransport, got streamable")
				endpoint, client = typed.Endpoint, typed.HTTPClient
			default:
				t.Fatalf("unexpected transport %T", transport)
			}
			assert.Equal(t, "http://example.com/mcp", endpoint)
			if tc.headers == nil {
				assert.Nil(t, client, "transports without headers use the SDK default client")
				return
			}
			require.NotNil(t, client)
			ht, ok := client.Transport.(*headerTransport)
			require.True(t, ok, "expected headerTransport, got %T", client.Transport)
			assert.Equal(t, tc.headers, ht.headers)
			assert.Equal(t, "http://example.com/mcp", ht.origin.String())
		})
	}
}

func TestMCPDiscovery_StdioContextCancellationKillsAndReapsChild(t *testing.T) {
	const helperEnv = "GO_TEST_MCP_HANGING_STDIO_CHILD"
	if os.Getenv(helperEnv) == "1" {
		ready := os.NewFile(3, "mcp-test-ready")
		if ready == nil {
			os.Exit(2)
		}
		if _, err := ready.Write([]byte{1}); err != nil {
			os.Exit(3)
		}
		if err := ready.Close(); err != nil {
			os.Exit(4)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}

	readyReader, readyWriter, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = readyReader.Close() }()
	defer func() { _ = readyWriter.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	transport, err := mcpDiscoveryTransport(ctx, projection.ResolvedMCPServer{
		ID:        "hanging-stdio",
		Transport: config.TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestMCPDiscovery_StdioContextCancellationKillsAndReapsChild$"},
		Env:       map[string]string{helperEnv: "1"},
	})
	require.NoError(t, err)
	commandTransport, ok := transport.(*mcp.CommandTransport)
	require.True(t, ok, "expected CommandTransport, got %T", transport)
	cmd := commandTransport.Command
	cmd.ExtraFiles = append(cmd.ExtraFiles, readyWriter)

	resultCh := make(chan DiscoveryResult, 1)
	resultDone := make(chan struct{})
	go func() {
		defer close(resultDone)
		resultCh <- discoverMCPTools(ctx, "hanging-stdio", transport)
	}()

	lifecycleTimer := time.NewTimer(5 * time.Second)
	defer lifecycleTimer.Stop()

	readyCh := make(chan error, 1)
	go func() {
		var signal [1]byte
		_, readErr := io.ReadFull(readyReader, signal[:])
		if readErr == nil && signal[0] != 1 {
			readErr = fmt.Errorf("unexpected readiness signal %d", signal[0])
		}
		readyCh <- readErr
	}()

	select {
	case readyErr := <-readyCh:
		require.NoError(t, readyErr)
	case <-lifecycleTimer.C:
		t.Fatal("timed out waiting for the hanging MCP child to signal readiness")
	}

	require.NotNil(t, cmd.Process)
	require.Nil(t, cmd.ProcessState, "hanging MCP child must still be live before cancellation")
	select {
	case earlyResult := <-resultCh:
		t.Fatalf("MCP discovery returned before cancellation: %#v", earlyResult)
	default:
	}

	cancel()

	var result DiscoveryResult
	select {
	case result = <-resultCh:
	case <-lifecycleTimer.C:
		t.Fatal("timed out waiting for MCP discovery to return after cancellation")
	}
	select {
	case <-resultDone:
	case <-lifecycleTimer.C:
		t.Fatal("timed out waiting for the MCP discovery result goroutine to exit")
	}

	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "connection failed")
	require.NotNil(t, cmd.ProcessState, "SDK command transport must reap the canceled child")
	assert.ErrorIs(t, cmd.Process.Signal(os.Kill), os.ErrProcessDone)
}

func TestRealConnector_HTTPConnectionError(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-http",
		Transport: "http",
		URL:       "http://127.0.0.1:59999/nonexistent", // Port unlikely to be listening
	}

	// Use a short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := connector.ConnectAndDiscover(ctx, server)
	assert.Equal(t, "test-http", result.ServerID)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "connection failed")
}

func TestRealConnector_UnsupportedHTTPTransport(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:            "test-http-unsupported",
		Transport:     "http",
		HTTPTransport: "grpc",
		URL:           "http://example.com",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "unsupported http transport")
}

// newToolServer returns an in-memory-capable SDK server exposing the named
// tools, paginating tools/list at pageSize (0 uses the SDK default).
func newToolServer(pageSize int, names ...string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "server", Version: "v0.0.1"}, &mcp.ServerOptions{PageSize: pageSize})
	for _, name := range names {
		server.AddTool(&mcp.Tool{
			Name:        name,
			Description: "test tool " + name,
			InputSchema: map[string]any{"type": "object"},
		}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}
	return server
}

// discoverFromServer runs discoverMCPTools against server over in-memory
// transports and requires discovery to close its client session.
func discoverFromServer(t *testing.T, server *mcp.Server) DiscoveryResult {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	result := discoverMCPTools(ctx, "test-server", clientTransport)

	closed := make(chan struct{})
	go func() {
		_ = serverSession.Wait()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		_ = serverSession.Close()
		t.Fatal("discovery did not close its MCP session")
	}
	return result
}

func TestDiscoverMCPTools_ListsToolsAndEstimatesTokens(t *testing.T) {
	result := discoverFromServer(t, newToolServer(0, "tool1", "tool2"))
	require.NoError(t, result.Error)
	assert.Equal(t, "test-server", result.ServerID)
	require.Len(t, result.Tools, 2)
	assert.Equal(t, "tool1", result.Tools[0].Name)
	assert.Equal(t, "tool2", result.Tools[1].Name)
	assert.Greater(t, result.Tools[0].Tokens, 0)
	assert.Greater(t, result.SchemaTokens, 0)
}

func TestDiscoverMCPTools_FollowsPagination(t *testing.T) {
	result := discoverFromServer(t, newToolServer(1, "tool1", "tool2", "tool3"))
	require.NoError(t, result.Error)
	require.Len(t, result.Tools, 3)
	for i, tool := range result.Tools {
		assert.Equal(t, fmt.Sprintf("tool%d", i+1), tool.Name)
	}
}

func TestDiscoverMCPTools_EmptyTools(t *testing.T) {
	result := discoverFromServer(t, newToolServer(0))
	require.NoError(t, result.Error)
	assert.Empty(t, result.Tools)
	assert.Equal(t, 0, result.SchemaTokens, "empty tools should have 0 schema tokens")
}

func TestDiscoverMCPTools_ListToolsError(t *testing.T) {
	server := newToolServer(0, "tool1")
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				return nil, fmt.Errorf("list tools error")
			}
			return next(ctx, method, req)
		}
	})

	result := discoverFromServer(t, server)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "list tools failed")
}

func TestDiscoverMCPTools_TooManyToolsGuard(t *testing.T) {
	// One page larger than the guard, with another page still pending, trips
	// the guard before discovery requests the next page.
	names := make([]string, maxToolsToDiscover+2)
	for i := range names {
		names[i] = fmt.Sprintf("tool%d", i)
	}

	result := discoverFromServer(t, newToolServer(maxToolsToDiscover+1, names...))
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "too many tools or infinite loop")
}

func TestRealConnector_OAuthNotVerifiable(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer httpServer.Close()

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:            "figma",
		Transport:     config.TransportHTTP,
		HTTPTransport: "streamable",
		URL:           httpServer.URL,
		Auth:          config.MCPAuthOAuth,
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.NoError(t, result.Error)
	assert.True(t, result.OAuthUnvalidated)
	assert.Zero(t, requests.Load(), "OAuth discovery must not connect without the client-managed credentials")
}

func TestRealConnector_StdioOAuthDoesNotSkipDiscovery(t *testing.T) {
	// Doctor's lenient-config fallback skips validation, so a malformed stdio
	// server can still carry auth = "oauth". Discovery must use the stdio path
	// rather than treating leftover OAuth as unvalidated HTTP auth.
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "malformed-stdio",
		Transport: config.TransportStdio,
		Command:   "nonexistent-command-xyzzy-oauth",
		Auth:      config.MCPAuthOAuth,
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.False(t, result.OAuthUnvalidated)
	require.Error(t, result.Error, "stdio servers with leftover auth=oauth must still be discovered")
	assert.Contains(t, result.Error.Error(), "connection failed")
}

func TestCheckMCPServers_OAuthServerNotValidated(t *testing.T) {
	trueVal := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:            "figma",
						Enabled:       &trueVal,
						Transport:     config.TransportHTTP,
						HTTPTransport: "streamable",
						URL:           "https://mcp.figma.com/mcp",
						Auth:          config.MCPAuthOAuth,
					},
				},
			},
		},
	}

	var events []MCPDiscoveryEvent
	var mu sync.Mutex
	statusFn := func(e MCPDiscoveryEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	warnings, summary, err := CheckMCPServers(context.Background(), cfg, &MockConnector{Next: &RealConnector{}}, statusFn)
	require.NoError(t, err)
	assert.Empty(t, warnings, "OAuth server with AuthStatusNotVerifiable should not produce warnings")
	assert.True(t, summary.Available)
	assert.Equal(t, 2, summary.EnabledServers, "figma and the built-in server")
	assert.Equal(t, 1, summary.ReachableServers, "only the built-in server")
	assert.Equal(t, 1, summary.OAuthUnvalidatedServers)
	assert.Equal(t, 0, summary.TotalTools)

	var hasAuthNotValidatedEvent bool
	for _, e := range events {
		if e.ServerID == "figma" && e.Status == MCPDiscoveryStatusAuthNotValidated {
			hasAuthNotValidatedEvent = true
		}
	}
	assert.True(t, hasAuthNotValidatedEvent, "expected MCPDiscoveryStatusAuthNotValidated event")
}

// echoingConnector fails every user-configured server with an error that quotes
// each resolved field, the way transport and SDK errors echo request URLs.
type echoingConnector struct{}

func (echoingConnector) ConnectAndDiscover(_ context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
	if server.ID == projection.BuiltInDispatchServerID {
		return DiscoveryResult{ServerID: server.ID}
	}
	return DiscoveryResult{ServerID: server.ID, Error: fmt.Errorf("url=%q headers=%v command=%q args=%v env=%v",
		server.URL, server.Headers, server.Command, server.Args, server.Env)}
}

// TestCheckMCPServers_RedactsResolvedSecretsFromDiscoveryErrors proves doctor
// never prints a resolved secret: both the progress event and the
// MCP_SERVER_UNREACHABLE warning show the configured placeholder instead.
func TestCheckMCPServers_RedactsResolvedSecretsFromDiscoveryErrors(t *testing.T) {
	t.Setenv("AL_SHELL_TOKEN", "shell-secret-value")
	enabled := true
	repoRoot := t.TempDir()
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID: "remote", Enabled: &enabled, Transport: config.TransportHTTP,
						URL:     "https://mcp.example.test/mcp?apiKey=${AL_URL_KEY}&shell=${AL_SHELL_TOKEN}",
						Headers: map[string]string{"Authorization": "Bearer ${AL_HEADER_TOKEN}"},
					},
					{
						ID: "local", Enabled: &enabled, Transport: config.TransportStdio,
						Command: "${AL_REPO_ROOT}/bin/server",
						Args:    []string{"--key=${AL_ARG_KEY}"},
						Env:     map[string]string{"TOKEN": "${AL_ENV_TOKEN}"}, // #nosec G101 -- placeholder reference in a redaction test.
					},
				},
			},
		},
		Env: map[string]string{
			"AL_URL_KEY":      "url-secret-value",
			"AL_HEADER_TOKEN": "header-secret-value",
			"AL_ARG_KEY":      "arg-secret",
			// Contains AL_ARG_KEY's value, so the longer value must win.
			"AL_ENV_TOKEN":               "arg-secret-and-more",
			config.BuiltinRepoRootEnvVar: repoRoot,
		},
	}

	var mu sync.Mutex
	eventErrs := map[string]string{}
	statusFn := func(event MCPDiscoveryEvent) {
		if event.Err == nil {
			return
		}
		mu.Lock()
		eventErrs[event.ServerID] = event.Err.Error()
		mu.Unlock()
	}

	warnings, _, err := CheckMCPServers(context.Background(), cfg, echoingConnector{}, statusFn)
	require.NoError(t, err)

	messages := map[string]string{}
	for _, w := range warnings {
		if w.Code == CodeMCPServerUnreachable {
			messages[w.Subject] = w.Message
		}
	}
	require.Len(t, messages, 2)

	secrets := []string{"url-secret-value", "shell-secret-value", "header-secret-value", "arg-secret", "and-more"}
	for _, id := range []string{"remote", "local"} {
		for _, text := range []string{eventErrs[id], messages[id]} {
			for _, secret := range secrets {
				assert.NotContains(t, text, secret, "server %s", id)
			}
		}
	}
	assert.Contains(t, messages["remote"], "apiKey=${AL_URL_KEY}&shell=${AL_SHELL_TOKEN}")
	assert.Contains(t, messages["remote"], "Bearer ${AL_HEADER_TOKEN}")
	assert.Contains(t, eventErrs["remote"], "apiKey=${AL_URL_KEY}")
	assert.Contains(t, messages["local"], "--key=${AL_ARG_KEY}")
	assert.Contains(t, messages["local"], "TOKEN:${AL_ENV_TOKEN}")
	// The repo root is a built-in, non-secret path and stays readable.
	assert.Contains(t, messages["local"], repoRoot+"/bin/server")
}

// TestCheckMCPServers_RedactsNormalizedSecretPaths covers the command and args
// after projection expands and cleans them, including a real exec failure.
func TestCheckMCPServers_RedactsNormalizedSecretPaths(t *testing.T) {
	for _, tc := range []struct {
		name      string
		path      string
		pathValue string
		argument  bool
		connector Connector
	}{
		{name: "repo command exec failure", path: "${AL_REPO_ROOT}/${AL_PATH_TOKEN}", pathValue: "private/../topsecret", connector: &MockConnector{Next: &RealConnector{}}},
		{name: "home command", path: "~/${AL_PATH_TOKEN}", pathValue: "private/../topsecret", connector: echoingConnector{}},
		{name: "repo argument", path: "${AL_REPO_ROOT}/${AL_PATH_TOKEN}", pathValue: "private/../topsecret", argument: true, connector: echoingConnector{}},
		{name: "home argument", path: "~/${AL_PATH_TOKEN}", pathValue: "private/../topsecret", argument: true, connector: echoingConnector{}},
		{name: "parent traversal argument", path: "${AL_REPO_ROOT}/prefix/${AL_PATH_TOKEN}", pathValue: "private/../../topsecret", argument: true, connector: echoingConnector{}},
		{name: "quoted command", path: "${AL_REPO_ROOT}/${AL_PATH_TOKEN}", pathValue: "private/../topsecret\"suffix", connector: echoingConnector{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled := true
			server := config.MCPServer{ID: "local", Enabled: &enabled, Transport: config.TransportStdio, Command: tc.path}
			if tc.argument {
				server.Command = "server"
				server.Args = []string{tc.path}
			}
			cfg := &config.ProjectConfig{
				Config: config.Config{Agents: receivingAgents(), MCP: config.MCPConfig{Servers: []config.MCPServer{server}}},
				Env: map[string]string{
					config.BuiltinRepoRootEnvVar: t.TempDir(),
					"AL_PATH_TOKEN":              tc.pathValue,
				},
			}
			var eventErr error
			statusFn := func(event MCPDiscoveryEvent) {
				if event.ServerID == server.ID && event.Status == MCPDiscoveryStatusError {
					eventErr = event.Err
				}
			}
			warnings, _, err := CheckMCPServers(context.Background(), cfg, tc.connector, statusFn)
			require.NoError(t, err)
			require.Len(t, warnings, 1)
			assert.Equal(t, CodeMCPServerUnreachable, warnings[0].Code)
			require.Error(t, eventErr)
			for _, text := range []string{eventErr.Error(), warnings[0].Message} {
				assert.NotContains(t, text, "topsecret")
				assert.Contains(t, text, "${AL_PATH_TOKEN}")
			}
		})
	}
}

// TestCheckMCPServers_KeepsErrorsWithoutResolvedValues proves redaction leaves
// an error that echoes no resolved value untouched, chain included.
func TestCheckMCPServers_KeepsErrorsWithoutResolvedValues(t *testing.T) {
	enabled := true
	cause := fmt.Errorf("dial: %w", context.DeadlineExceeded)
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "remote", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://mcp.example.test/mcp?token=${AL_TOKEN}"},
				},
			},
		},
		Env: map[string]string{"AL_TOKEN": "secret-token"},
	}

	var mu sync.Mutex
	var eventErr error
	statusFn := func(event MCPDiscoveryEvent) {
		if event.Err != nil {
			mu.Lock()
			eventErr = event.Err
			mu.Unlock()
		}
	}
	mock := &MockConnector{Results: map[string]DiscoveryResult{"remote": {ServerID: "remote", Error: cause}}}
	warnings, _, err := CheckMCPServers(context.Background(), cfg, mock, statusFn)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, fmt.Sprintf("cannot connect, initialize, or list tools: %v", cause), warnings[0].Message)
	assert.Same(t, cause, eventErr)
}

// TestCheckMCPServers_RedactsRealHTTPConnectionError drives the real SDK
// transport against a closed port: Go's HTTP client quotes the full request
// URL in its error, and the query-string secret must not survive into output.
func TestCheckMCPServers_RedactsRealHTTPConnectionError(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	baseURL := closed.URL
	closed.Close()

	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: receivingAgents(),
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "streamable", Enabled: &enabled, Transport: config.TransportHTTP, HTTPTransport: config.HTTPTransportStreamable, URL: baseURL + "/mcp/?tavilyApiKey=${AL_TAVILY_API_KEY}"},
					{ID: "sse", Enabled: &enabled, Transport: config.TransportHTTP, URL: baseURL + "/sse?tavilyApiKey=${AL_TAVILY_API_KEY}"},
				},
			},
		},
		Env: map[string]string{"AL_TAVILY_API_KEY": "tvly-SUPERSECRET123"},
	}

	warnings, _, err := CheckMCPServers(context.Background(), cfg, &MockConnector{Next: &RealConnector{}}, nil)
	require.NoError(t, err)
	var unreachable int
	for _, w := range warnings {
		if w.Code != CodeMCPServerUnreachable {
			continue
		}
		unreachable++
		assert.NotContains(t, w.Message, "tvly-SUPERSECRET123", "server %s", w.Subject)
		assert.Contains(t, w.Message, "tavilyApiKey=${AL_TAVILY_API_KEY}", "server %s", w.Subject)
	}
	assert.Equal(t, 2, unreachable)
}

// TestMCPSecretPlaceholdersCoversEncodedForms proves a secret still redacts
// when a transport error quotes it or re-encodes it inside a URL.
func TestMCPSecretPlaceholdersCoversEncodedForms(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{{
		ID: "remote", Enabled: &enabled, Transport: config.TransportHTTP,
		URL: "https://${AL_USER}@mcp.example.test/mcp?k=${AL_KEY}&empty=${AL_EMPTY}",
	}}
	placeholders := mcpSecretPlaceholders(servers, map[string]string{
		"AL_USER":  "us%40er",
		"AL_KEY":   " \t" + `a"b\c d` + " \t",
		"AL_EMPTY": " \t",
	})
	assert.NotContains(t, placeholders, "")
	for _, text := range []string{
		`Get "https://us@er@mcp.example.test/mcp?k=a\"b\\c d"`, // %q of the decoded URL
		"https://us%2540er@mcp.example.test/mcp?k=a%22b%5Cc+d", // query-escaped
		"path a%22b%5Cc%20d", // path-escaped
		`plain a"b\c d`,
	} {
		redacted := envref.Redact(text, placeholders)
		for _, leak := range []string{"us@er", "us%40er", "40er", `b\c`, `b\\c`, "b%5Cc"} {
			assert.NotContains(t, redacted, leak, "text %q", text)
		}
	}
}
