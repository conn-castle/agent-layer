package warnings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
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

// maxToolsToDiscover is the maximum number of tools to discover before aborting.
// This guards against infinite pagination loops.
const maxToolsToDiscover = 1000

// RealConnector implements Connector using the SDK.
type RealConnector struct{}

// ConnectAndDiscover connects to an MCP server and discovers its tools.
func (r *RealConnector) ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
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
		switch server.HTTPTransport {
		case "", "sse":
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
		case "streamable":
			t := &mcp.StreamableClientTransport{
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
			res.Error = fmt.Errorf(messages.WarningsUnsupportedHTTPTransportFmt, server.HTTPTransport)
			return res
		}
	default:
		res.Error = fmt.Errorf(messages.WarningsUnsupportedTransportFmt, server.Transport)
		return res
	}

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		res.Error = fmt.Errorf(messages.WarningsConnectionFailedFmt, err)
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
			res.Error = fmt.Errorf(messages.WarningsListToolsFailedFmt, err)
			return res
		}

		allTools = append(allTools, listRes.Tools...)

		if listRes.NextCursor == "" {
			break
		}
		cursor = listRes.NextCursor

		// Guard against infinite loops
		if len(allTools) > maxToolsToDiscover {
			res.Error = fmt.Errorf(messages.WarningsTooManyTools)
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
