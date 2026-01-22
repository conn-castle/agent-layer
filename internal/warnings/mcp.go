package warnings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

// mcpSessionInterface wraps the MCP session for testing.
type mcpSessionInterface interface {
	ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	Close() error
}

// mcpClientInterface wraps the MCP client for testing.
type mcpClientInterface interface {
	Connect(ctx context.Context, transport mcp.Transport, opts *mcp.ClientSessionOptions) (mcpSessionInterface, error)
}

// realMCPClient wraps the real mcp.Client.
type realMCPClient struct {
	client *mcp.Client
}

func (r *realMCPClient) Connect(ctx context.Context, transport mcp.Transport, opts *mcp.ClientSessionOptions) (mcpSessionInterface, error) {
	session, err := r.client.Connect(ctx, transport, opts)
	if err != nil {
		return nil, err
	}
	return &realMCPSession{session: session}, nil
}

// realMCPSession wraps the real mcp.ClientSession.
type realMCPSession struct {
	session *mcp.ClientSession
}

func (r *realMCPSession) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	return r.session.ListTools(ctx, params)
}

func (r *realMCPSession) Close() error {
	return r.session.Close()
}

// NewMCPClientFunc is a mockable function for creating MCP clients.
var NewMCPClientFunc = func(impl *mcp.Implementation, opts *mcp.ClientOptions) mcpClientInterface {
	return &realMCPClient{client: mcp.NewClient(impl, opts)}
}

// CheckMCPServers performs discovery on enabled MCP servers and checks against warning thresholds.
// cfg supplies the configured thresholds; nil thresholds disable the corresponding warnings.
func CheckMCPServers(ctx context.Context, cfg *config.ProjectConfig, connector Connector) ([]Warning, error) {
	if connector == nil {
		connector = &RealConnector{}
	}

	// 1. Identify enabled servers
	var enabledServers []ResolvedMCPServer
	for _, s := range cfg.Config.MCP.Servers {
		if s.Enabled != nil && *s.Enabled {
			resolved, err := resolveServer(s, cfg.Env)
			if err != nil {
				return []Warning{{
					Code:    CodeMCPServerUnreachable,
					Subject: s.ID,
					Message: fmt.Sprintf("Failed to resolve configuration: %v", err),
					Fix:     "Correct URL/command/auth or environment variables.",
				}}, nil
			}
			enabledServers = append(enabledServers, resolved)
		}
	}

	var warnings []Warning

	thresholds := cfg.Config.Warnings

	// Check: MCP_TOO_MANY_SERVERS_ENABLED
	if thresholds.MCPServerThreshold != nil && len(enabledServers) > *thresholds.MCPServerThreshold {
		warnings = append(warnings, Warning{
			Code:    CodeMCPTooManyServers,
			Subject: "mcp.servers",
			Message: fmt.Sprintf("enabled server count > %d (%d > %d)", *thresholds.MCPServerThreshold, len(enabledServers), *thresholds.MCPServerThreshold),
			Fix:     "disable rarely used servers; consolidate.",
		})
	}

	// 2. Discovery (Parallel)
	results := discoverTools(ctx, enabledServers, connector)

	// 3. Process results
	var totalTools int
	var totalSchemaTokens int
	toolNames := make(map[string][]string) // name -> serverIDs

	for _, res := range results {
		if res.Error != nil {
			warnings = append(warnings, Warning{
				Code:    CodeMCPServerUnreachable,
				Subject: res.ServerID,
				Message: fmt.Sprintf("cannot connect, initialize, or list tools: %v", res.Error),
				Fix:     "correct URL/command/auth; or disable the server.",
			})
			continue
		}

		// Check: MCP_SERVER_TOO_MANY_TOOLS
		if thresholds.MCPServerToolsThreshold != nil && len(res.Tools) > *thresholds.MCPServerToolsThreshold {
			warnings = append(warnings, Warning{
				Code:    CodeMCPServerTooManyTools,
				Subject: res.ServerID,
				Message: fmt.Sprintf("server has > %d tools (%d > %d)", *thresholds.MCPServerToolsThreshold, len(res.Tools), *thresholds.MCPServerToolsThreshold),
				Fix:     "split the server by domain or reduce exported tools.",
			})
		}

		// Check: MCP_TOOL_SCHEMA_BLOAT_SERVER
		if thresholds.MCPSchemaTokensServerThreshold != nil && res.SchemaTokens > *thresholds.MCPSchemaTokensServerThreshold {
			warnings = append(warnings, Warning{
				Code:    CodeMCPToolSchemaBloatServer,
				Subject: res.ServerID,
				Message: fmt.Sprintf("estimated tokens for tool definitions > %d (%d > %d)", *thresholds.MCPSchemaTokensServerThreshold, res.SchemaTokens, *thresholds.MCPSchemaTokensServerThreshold),
				Fix:     "reduce schema verbosity; shorten descriptions; remove huge enums/oneOf; reduce tools.",
			})
		}

		totalTools += len(res.Tools)
		totalSchemaTokens += res.SchemaTokens

		for _, t := range res.Tools {
			toolNames[t.Name] = append(toolNames[t.Name], res.ServerID)
		}
	}

	// Check: MCP_TOO_MANY_TOOLS_TOTAL
	if thresholds.MCPToolsTotalThreshold != nil && totalTools > *thresholds.MCPToolsTotalThreshold {
		warnings = append(warnings, Warning{
			Code:    CodeMCPTooManyToolsTotal,
			Subject: "mcp.tools.total",
			Message: fmt.Sprintf("total discovered tools > %d (%d > %d)", *thresholds.MCPToolsTotalThreshold, totalTools, *thresholds.MCPToolsTotalThreshold),
			Fix:     "disable servers; reduce tool surface.",
		})
	}

	// Check: MCP_TOOL_SCHEMA_BLOAT_TOTAL
	if thresholds.MCPSchemaTokensTotalThreshold != nil && totalSchemaTokens > *thresholds.MCPSchemaTokensTotalThreshold {
		warnings = append(warnings, Warning{
			Code:    CodeMCPToolSchemaBloatTotal,
			Subject: "mcp.tools.schema.total",
			Message: fmt.Sprintf("estimated tokens for all tool definitions > %d (%d > %d)", *thresholds.MCPSchemaTokensTotalThreshold, totalSchemaTokens, *thresholds.MCPSchemaTokensTotalThreshold),
			Fix:     "reduce schema verbosity; shorten descriptions; remove huge enums/oneOf; reduce tools.",
		})
	}

	// Check: MCP_TOOL_NAME_COLLISION
	for name, servers := range toolNames {
		if len(servers) > 1 {
			warnings = append(warnings, Warning{
				Code:    CodeMCPToolNameCollision,
				Subject: name,
				Message: fmt.Sprintf("same tool name appears in more than one server: %v", servers),
				Fix:     "namespace tool names per server (recommended pattern: <server>__<action>).",
			})
		}
	}

	return warnings, nil
}

// ResolvedMCPServer holds configuration for an MCP server with environment variables resolved.
type ResolvedMCPServer struct {
	ID        string
	Transport string
	URL       string // for http
	Headers   map[string]string
	Command   string // for stdio
	Args      []string
	Env       map[string]string
}

// ToolDef represents a discovered tool from an MCP server.
type ToolDef struct {
	Name string
}

// DiscoveryResult contains the results of discovering tools from an MCP server.
type DiscoveryResult struct {
	ServerID     string
	Tools        []ToolDef
	SchemaTokens int
	Error        error
}

// Connector interface for mocking.
type Connector interface {
	ConnectAndDiscover(ctx context.Context, server ResolvedMCPServer) DiscoveryResult
}

// RealConnector implements Connector using the SDK.
type RealConnector struct{}

// ConnectAndDiscover connects to an MCP server and discovers its tools.
func (r *RealConnector) ConnectAndDiscover(ctx context.Context, server ResolvedMCPServer) DiscoveryResult {
	res := DiscoveryResult{ServerID: server.ID}

	// Create context with timeout for this server
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Create client
	mcpClient := NewMCPClientFunc(&mcp.Implementation{
		Name:    "agent-layer-doctor",
		Version: "1.0.0",
	}, nil)

	var transport mcp.Transport

	switch server.Transport {
	case "stdio":
		cmd := exec.Command(server.Command, server.Args...)
		cmd.Env = os.Environ() // Start with current env
		for k, v := range server.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}

		transport = &mcp.CommandTransport{
			Command: cmd,
		}
	case "http":
		t := &mcp.SSEClientTransport{
			Endpoint: server.URL,
		}
		if len(server.Headers) > 0 {
			t.HTTPClient = &http.Client{
				Transport: &headerTransport{
					base:    http.DefaultTransport,
					headers: server.Headers,
				},
			}
		}
		transport = t
	default:
		res.Error = fmt.Errorf("unsupported transport: %s", server.Transport)
		return res
	}

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		res.Error = fmt.Errorf("connection failed: %w", err)
		return res
	}
	defer func() { _ = session.Close() }()

	// List tools (paginated)
	var allTools []*mcp.Tool
	var cursor string

	for {
		listRes, err := session.ListTools(ctx, &mcp.ListToolsParams{
			Cursor: cursor,
		})

		if err != nil {
			res.Error = fmt.Errorf("list tools failed: %w", err)
			return res
		}

		allTools = append(allTools, listRes.Tools...)

		if listRes.NextCursor == "" {
			break
		}
		cursor = listRes.NextCursor

		// Guard against infinite loops
		if len(allTools) > 10000 { // heuristic guard
			res.Error = fmt.Errorf("too many tools or infinite loop")
			return res
		}
	}

	// Process tools
	var toolsJSON []any
	for _, t := range allTools {
		res.Tools = append(res.Tools, ToolDef{Name: t.Name})
		toolsJSON = append(toolsJSON, t)
	}

	if len(toolsJSON) > 0 {
		bytes, err := json.Marshal(toolsJSON)
		if err == nil {
			res.SchemaTokens = EstimateTokens(string(bytes))
		}
	}

	return res
}

// headerTransport adds headers to HTTP requests.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.base == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.base.RoundTrip(req)
}

// Helpers

func resolveServer(s config.MCPServer, env map[string]string) (ResolvedMCPServer, error) {
	// Simplified resolver relying on config.SubstituteEnvVarsWith
	replacer := func(key string, val string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	}

	res := ResolvedMCPServer{ID: s.ID, Transport: s.Transport}
	var err error

	if s.Transport == "http" {
		res.URL, err = config.SubstituteEnvVarsWith(s.URL, env, replacer)
		if err != nil {
			return res, err
		}
		if len(s.Headers) > 0 {
			res.Headers = make(map[string]string)
			for k, v := range s.Headers {
				res.Headers[k], err = config.SubstituteEnvVarsWith(v, env, replacer)
				if err != nil {
					return res, err
				}
			}
		}
	} else if s.Transport == "stdio" {
		res.Command, err = config.SubstituteEnvVarsWith(s.Command, env, replacer)
		if err != nil {
			return res, err
		}
		for _, arg := range s.Args {
			resolvedArg, err := config.SubstituteEnvVarsWith(arg, env, replacer)
			if err != nil {
				return res, err
			}
			res.Args = append(res.Args, resolvedArg)
		}
		if len(s.Env) > 0 {
			res.Env = make(map[string]string)
			for k, v := range s.Env {
				res.Env[k], err = config.SubstituteEnvVarsWith(v, env, replacer)
				if err != nil {
					return res, err
				}
			}
		}
	}

	return res, nil
}

func discoverTools(ctx context.Context, servers []ResolvedMCPServer, connector Connector) []DiscoveryResult {
	results := make([]DiscoveryResult, len(servers))

	// Semaphore for concurrency
	sem := make(chan struct{}, 4) // Max 4 concurrent
	var wg sync.WaitGroup

	for i, server := range servers {
		wg.Add(1)
		go func(i int, s ResolvedMCPServer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[i] = connector.ConnectAndDiscover(ctx, s)
		}(i, server)
	}

	wg.Wait()
	return results
}
