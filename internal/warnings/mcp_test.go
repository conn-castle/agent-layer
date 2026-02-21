package warnings

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// MockConnector implements Connector for testing.
type MockConnector struct {
	Results map[string]DiscoveryResult
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (m *MockConnector) ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
	if res, ok := m.Results[server.ID]; ok {
		return res
	}
	return DiscoveryResult{ServerID: server.ID, Error: fmt.Errorf("mock not found")}
}

func TestMCPDiscoveryTimeoutDefault(t *testing.T) {
	if mcpDiscoveryTimeout != 30*time.Second {
		t.Fatalf("expected mcp discovery timeout to be 30s, got %s", mcpDiscoveryTimeout)
	}
}

func TestCheckMCPServers(t *testing.T) {
	// Setup config
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
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

	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
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
			MCP: config.MCPConfig{Servers: servers},
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

	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
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
		warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
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

	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
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

func TestCheckMCPServers_NilConnector(t *testing.T) {
	// When connector is nil, a RealConnector should be created (but we can't easily test the real one)
	// This test ensures the nil check doesn't panic and the function handles disabled servers
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

	// Pass a mock to avoid actual network calls
	mock := &MockConnector{Results: map[string]DiscoveryResult{}}
	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestCheckMCPServers_ResolveServerError(t *testing.T) {
	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "bad-server", Enabled: &enabled, Transport: "http", URL: "${XYZZY_NONEXISTENT_VAR_12345}"},
				},
			},
		},
		Env: map[string]string{},
	}

	mock := &MockConnector{Results: map[string]DiscoveryResult{}}
	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, CodeMCPServerUnreachable, warnings[0].Code)
	assert.Equal(t, "bad-server", warnings[0].Subject)
	assert.Contains(t, warnings[0].Message, "Failed to resolve configuration")
	assert.Equal(t, SourceInternal, warnings[0].Source)
	assert.Equal(t, SeverityCritical, warnings[0].Severity)
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

	results := discoverTools(context.Background(), servers, mock, nil)
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
	results := discoverTools(context.Background(), nil, mock, nil)
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

	_ = discoverTools(context.Background(), servers, mock, statusFn)

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
		_ = discoverTools(context.Background(), servers, connector, nil)
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

	transport := &headerTransport{
		base: http.DefaultTransport,
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

	transport := &headerTransport{
		base: nil, // nil base
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
	transport := &headerTransport{
		base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       http.NoBody,
				Request:    req,
				Header:     make(http.Header),
			}, nil
		}),
		headers: map[string]string{"Authorization": "Bearer token"},
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
	assert.Equal(t, "value", req.Header.Get("X-Existing"))
}

func TestBuildMCPCommandEnv_AllowlistsBaseEnv(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"AL_TAVILY_API_KEY=abc123",
		"GITHUB_TOKEN=should-not-pass",
		"AWS_SECRET_ACCESS_KEY=should-not-pass",
	}
	serverEnv := map[string]string{
		"CUSTOM_TOKEN": "server-value",
		"PATH":         "/custom/bin",
	}

	env := buildMCPCommandEnv(base, serverEnv)
	joined := strings.Join(env, "\n")

	assert.Contains(t, joined, "PATH=/custom/bin")
	assert.Contains(t, joined, "HOME=/tmp/home")
	assert.Contains(t, joined, "AL_TAVILY_API_KEY=abc123")
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

func TestRealConnector_StdioWithEnv(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-stdio-env",
		Transport: "stdio",
		Command:   "nonexistent-command-xyzzy-env",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"TEST_VAR": "test_value", "ANOTHER_VAR": "another_value"},
	}

	// This exercises the env setup code path before failing at connect
	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Equal(t, "test-stdio-env", result.ServerID)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "connection failed")
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

func TestRealConnector_HTTPWithHeaders(t *testing.T) {
	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-http-headers",
		Transport: "http",
		URL:       "http://127.0.0.1:59998/nonexistent",
		Headers:   map[string]string{"Authorization": "Bearer test"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := connector.ConnectAndDiscover(ctx, server)
	assert.Equal(t, "test-http-headers", result.ServerID)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "connection failed")
}

type transportAssertingClient struct {
	t                *testing.T
	expectStreamable bool
	expectHeaders    bool
	session          mcpSessionInterface
}

func (c *transportAssertingClient) Connect(ctx context.Context, transport mcp.Transport, opts *mcp.ClientSessionOptions) (mcpSessionInterface, error) {
	c.t.Helper()
	if c.expectStreamable {
		streamable, ok := transport.(*mcp.StreamableClientTransport)
		if !ok {
			c.t.Fatalf("expected StreamableClientTransport, got %T", transport)
		}
		if streamable.Endpoint == "" {
			c.t.Fatalf("expected streamable endpoint to be set")
		}
		if c.expectHeaders {
			if streamable.HTTPClient == nil {
				c.t.Fatalf("expected HTTP client for streamable transport")
			}
			ht, ok := streamable.HTTPClient.Transport.(*headerTransport)
			if !ok {
				c.t.Fatalf("expected headerTransport, got %T", streamable.HTTPClient.Transport)
			}
			if ht.headers["Authorization"] != "Bearer token" {
				c.t.Fatalf("unexpected header value: %q", ht.headers["Authorization"])
			}
		} else if streamable.HTTPClient != nil {
			c.t.Fatalf("expected nil HTTP client for streamable transport without headers")
		}
	} else {
		sse, ok := transport.(*mcp.SSEClientTransport)
		if !ok {
			c.t.Fatalf("expected SSEClientTransport, got %T", transport)
		}
		if sse.Endpoint == "" {
			c.t.Fatalf("expected SSE endpoint to be set")
		}
		if c.expectHeaders {
			if sse.HTTPClient == nil {
				c.t.Fatalf("expected HTTP client for SSE transport")
			}
			ht, ok := sse.HTTPClient.Transport.(*headerTransport)
			if !ok {
				c.t.Fatalf("expected headerTransport, got %T", sse.HTTPClient.Transport)
			}
			if ht.headers["Authorization"] != "Bearer token" {
				c.t.Fatalf("unexpected header value: %q", ht.headers["Authorization"])
			}
		} else if sse.HTTPClient != nil {
			c.t.Fatalf("expected nil HTTP client for SSE transport without headers")
		}
	}
	return c.session, nil
}

func TestRealConnector_HTTPStreamableTransport(t *testing.T) {
	mockSession := &mockMCPSession{
		tools: []*mcp.Tool{{Name: "tool"}},
	}
	client := &transportAssertingClient{
		t:                t,
		expectStreamable: true,
		expectHeaders:    true,
		session:          mockSession,
	}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return client
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:            "test-http-streamable",
		Transport:     "http",
		HTTPTransport: "streamable",
		URL:           "http://example.com",
		Headers:       map[string]string{"Authorization": "Bearer token"},
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.NoError(t, result.Error)
	assert.Len(t, result.Tools, 1)
}

func TestRealConnector_HTTPTransportDefaultSSE(t *testing.T) {
	mockSession := &mockMCPSession{
		tools: []*mcp.Tool{{Name: "tool"}},
	}
	client := &transportAssertingClient{
		t:                t,
		expectStreamable: false,
		expectHeaders:    false,
		session:          mockSession,
	}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return client
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-http-sse",
		Transport: "http",
		URL:       "http://example.com",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.NoError(t, result.Error)
	assert.Len(t, result.Tools, 1)
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

func TestRealMCPClientAndSessionWrappers(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "server", Version: "v0.0.1"}, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v0.0.1"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	realClient := &realMCPClient{client: client}
	session, err := realClient.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NoError(t, session.Close())
}

// mockMCPClient implements mcpClientInterface for testing.
type mockMCPClient struct {
	session mcpSessionInterface
	err     error
}

func (m *mockMCPClient) Connect(ctx context.Context, transport mcp.Transport, opts *mcp.ClientSessionOptions) (mcpSessionInterface, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.session, nil
}

// mockMCPSession implements mcpSessionInterface for testing.
type mockMCPSession struct {
	tools       []*mcp.Tool
	nextCursors []string // For pagination simulation
	callCount   int
	listErr     error
	closeCalled bool
}

func (m *mockMCPSession) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	// Return tools based on call count for pagination
	result := &mcp.ListToolsResult{
		Tools: m.tools,
	}
	if m.callCount < len(m.nextCursors) {
		result.NextCursor = m.nextCursors[m.callCount]
	}
	m.callCount++
	return result, nil
}

func (m *mockMCPSession) Close() error {
	m.closeCalled = true
	return nil
}

func TestRealConnector_SuccessfulConnection(t *testing.T) {
	mockSession := &mockMCPSession{
		tools: []*mcp.Tool{
			{Name: "tool1", Description: "First tool"},
			{Name: "tool2", Description: "Second tool"},
		},
	}
	mockClient := &mockMCPClient{session: mockSession}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return mockClient
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-server",
		Transport: "stdio",
		Command:   "echo",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Equal(t, "test-server", result.ServerID)
	assert.NoError(t, result.Error)
	assert.Len(t, result.Tools, 2)
	assert.Equal(t, "tool1", result.Tools[0].Name)
	assert.Equal(t, "tool2", result.Tools[1].Name)
	assert.Greater(t, result.SchemaTokens, 0)
	assert.True(t, mockSession.closeCalled, "session.Close should be called")
}

func TestRealConnector_SuccessfulConnectionPaginated(t *testing.T) {
	// Create a paginated mock session that returns tools in multiple calls
	mockSession := &mockMCPSession{
		tools: []*mcp.Tool{
			{Name: "tool1"},
		},
		nextCursors: []string{"cursor1", ""}, // First call has cursor, second doesn't
	}
	mockClient := &mockMCPClient{session: mockSession}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return mockClient
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-paginated",
		Transport: "stdio",
		Command:   "echo",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.NoError(t, result.Error)
	assert.Equal(t, 2, mockSession.callCount, "expected 2 ListTools calls for pagination")
}

func TestRealConnector_ListToolsError(t *testing.T) {
	mockSession := &mockMCPSession{
		listErr: fmt.Errorf("list tools error"),
	}
	mockClient := &mockMCPClient{session: mockSession}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return mockClient
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-list-error",
		Transport: "stdio",
		Command:   "echo",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "list tools failed")
	assert.True(t, mockSession.closeCalled, "session.Close should still be called")
}

func TestRealConnector_EmptyTools(t *testing.T) {
	mockSession := &mockMCPSession{
		tools: []*mcp.Tool{}, // Empty tools list
	}
	mockClient := &mockMCPClient{session: mockSession}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return mockClient
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-empty",
		Transport: "stdio",
		Command:   "echo",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.NoError(t, result.Error)
	assert.Empty(t, result.Tools)
	assert.Equal(t, 0, result.SchemaTokens, "empty tools should have 0 schema tokens")
}

// infiniteLoopMockSession always returns a cursor to simulate infinite pagination.
type infiniteLoopMockSession struct {
	tools       []*mcp.Tool
	callCount   int
	closeCalled bool
}

func (m *infiniteLoopMockSession) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	m.callCount++
	return &mcp.ListToolsResult{
		Tools:      m.tools,
		NextCursor: "always-more", // Always return a cursor
	}, nil
}

func (m *infiniteLoopMockSession) Close() error {
	m.closeCalled = true
	return nil
}

func TestRealConnector_TooManyToolsGuard(t *testing.T) {
	// Create a session that returns lots of tools with infinite pagination
	tools := make([]*mcp.Tool, 5001) // Enough to exceed the 10000 guard quickly
	for i := range tools {
		tools[i] = &mcp.Tool{Name: fmt.Sprintf("tool%d", i)}
	}
	mockSession := &infiniteLoopMockSession{tools: tools}
	mockClient := &mockMCPClient{session: mockSession}

	original := NewMCPClientFunc
	NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
		return mockClient
	}
	t.Cleanup(func() { NewMCPClientFunc = original })

	connector := &RealConnector{}
	server := projection.ResolvedMCPServer{
		ID:        "test-infinite",
		Transport: "stdio",
		Command:   "echo",
	}

	result := connector.ConnectAndDiscover(context.Background(), server)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "too many tools or infinite loop")
	assert.True(t, mockSession.closeCalled, "session.Close should be called")
}
