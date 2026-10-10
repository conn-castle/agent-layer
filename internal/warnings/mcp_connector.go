package warnings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// maxToolsToDiscover is the maximum number of tools to discover before aborting.
// This guards against infinite pagination loops.
const maxToolsToDiscover = 1000

// mcpDiscoveryTimeout is the per-server timeout for doctor MCP discovery checks.
const mcpDiscoveryTimeout = 30 * time.Second

var errMCPOriginRefused = errors.New(messages.WarningsMCPOriginRefused)

var mcpAllowedEnvKeys = map[string]struct{}{
	"HOME":           {},
	"LANG":           {},
	"LC_ALL":         {},
	"LC_CTYPE":       {},
	pathEnvKey:       {},
	"SHELL":          {},
	"SYSTEMROOT":     {},
	"TERM":           {},
	"TMP":            {},
	"TMPDIR":         {},
	"TEMP":           {},
	"USER":           {},
	"WINDIR":         {},
	"XDG_CACHE_HOME": {},
}

// RealConnector implements Connector using the SDK.
type RealConnector struct{}

// ConnectAndDiscover connects to an MCP server and discovers its tools.
func (r *RealConnector) ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult {
	if server.Transport == config.TransportHTTP && server.Auth == config.MCPAuthOAuth {
		return DiscoveryResult{ServerID: server.ID, OAuthUnvalidated: true}
	}

	// Create context with timeout for this server
	ctx, cancel := context.WithTimeout(ctx, mcpDiscoveryTimeout)
	defer cancel()

	transport, err := mcpDiscoveryTransport(ctx, server)
	if err != nil {
		return DiscoveryResult{ServerID: server.ID, Error: err}
	}
	return discoverMCPTools(ctx, server.ID, transport)
}

// mcpDiscoveryTransport builds the client transport for one configured server.
func mcpDiscoveryTransport(ctx context.Context, server projection.ResolvedMCPServer) (mcp.Transport, error) {
	switch server.Transport {
	case config.TransportStdio:
		// Bind the spawned MCP server process to the discovery context (which
		// carries mcpDiscoveryTimeout) so a hung or misbehaving server is killed
		// directly on cancel/timeout rather than only torn down indirectly via
		// session Close().
		cmd := exec.CommandContext(ctx, server.Command, server.Args...)
		cmd.Env = buildMCPCommandEnv(os.Environ(), server.Env)
		return &mcp.CommandTransport{Command: cmd}, nil
	case config.TransportHTTP:
		client, err := headerHTTPClient(server.URL, server.Headers)
		if err != nil {
			return nil, err
		}
		switch server.HTTPTransport {
		case "", config.HTTPTransportSSE:
			return &mcp.SSEClientTransport{Endpoint: server.URL, HTTPClient: client}, nil
		case config.HTTPTransportStreamable:
			return &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client}, nil
		default:
			return nil, fmt.Errorf(messages.WarningsUnsupportedHTTPTransportFmt, server.HTTPTransport)
		}
	default:
		return nil, fmt.Errorf(messages.WarningsUnsupportedTransportFmt, server.Transport)
	}
}

// headerHTTPClient scopes configured headers to the endpoint's origin, or returns nil
// so the SDK uses its default client when there are no headers.
func headerHTTPClient(endpoint string, headers map[string]string) (*http.Client, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	origin, err := url.Parse(endpoint)
	if err != nil || origin.Hostname() == "" ||
		(!strings.EqualFold(origin.Scheme, "http") && !strings.EqualFold(origin.Scheme, "https")) {
		return nil, errors.New(messages.WarningsMCPInvalidHTTPEndpoint)
	}
	return &http.Client{
		Transport: &headerTransport{
			base:    http.DefaultTransport,
			headers: headers,
			origin:  origin,
		},
	}, nil
}

// discoverMCPTools connects over transport and lists every tool the server
// exposes, estimating per-tool and total schema tokens.
func discoverMCPTools(ctx context.Context, serverID string, transport mcp.Transport) (res DiscoveryResult) {
	res.ServerID = serverID
	defer func() {
		// HTTP clients and SDK errors can wrap the refusal with a credential-bearing URL.
		// Drop the entire wrapper before returning a discovery result.
		if res.Error != nil && (errors.Is(res.Error, errMCPOriginRefused) || mcpTransportRefusedOrigin(transport)) {
			res.Error = errMCPOriginRefused
		}
	}()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "agent-layer-doctor",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(ctx, transport, nil)
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
		toolDef := ToolDef{Name: t.Name}

		// Estimate tokens per tool
		toolBytes, err := json.Marshal(t)
		if err == nil {
			toolDef.Tokens = EstimateTokens(string(toolBytes))
		}

		res.Tools = append(res.Tools, toolDef)
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

// headerTransport rejects requests outside the configured origin before sending headers.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
	origin  *url.URL
	refused atomic.Bool
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !sameHTTPOrigin(t.origin, req.URL) {
		t.refused.Store(true)
		return nil, errMCPOriginRefused
	}
	cloned := req.Clone(req.Context())
	for k, v := range t.headers {
		cloned.Header.Set(k, v)
	}
	if t.base == nil {
		return http.DefaultTransport.RoundTrip(cloned)
	}
	return t.base.RoundTrip(cloned)
}

// mcpTransportRefusedOrigin also detects refusals when the SDK replaces an
// error chain with text. Each HTTP client is scoped to one discovery attempt.
func mcpTransportRefusedOrigin(transport mcp.Transport) bool {
	var client *http.Client
	switch t := transport.(type) {
	case *mcp.SSEClientTransport:
		client = t.HTTPClient
	case *mcp.StreamableClientTransport:
		client = t.HTTPClient
	}
	if client == nil {
		return false
	}
	guard, ok := client.Transport.(*headerTransport)
	return ok && guard.refused.Load()
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	// Hostname includes an IPv6 zone after '%'. Interface names are case-sensitive
	// even though DNS names and hexadecimal address digits are not.
	aHost, aZone, _ := strings.Cut(a.Hostname(), "%")
	bHost, bZone, _ := strings.Cut(b.Hostname(), "%")
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(aHost, bHost) &&
		aZone == bZone && effectiveHTTPPort(a) == effectiveHTTPPort(b)
}

func effectiveHTTPPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func buildMCPCommandEnv(baseEnv []string, serverEnv map[string]string) []string {
	values := make(map[string]string)
	for _, pair := range baseEnv {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		normalized, allowed := normalizeAllowedMCPEnvKey(key)
		if !allowed {
			continue
		}
		values[normalized] = value
	}
	for key, value := range serverEnv {
		if strings.EqualFold(key, versiondispatch.EnvShimActive) {
			continue
		}
		if normalized, allowed := normalizeAllowedMCPEnvKey(key); allowed {
			values[normalized] = value
			continue
		}
		values[key] = value
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, fmt.Sprintf("%s=%s", key, values[key]))
	}
	return env
}

func normalizeAllowedMCPEnvKey(key string) (string, bool) {
	upper := strings.ToUpper(key)
	// AL_SHIM_ACTIVE is scoped to one version-dispatch handoff. Passing it to
	// the built-in MCP child prevents that child from selecting the repository
	// pin when the global CLI and pin differ.
	if upper == versiondispatch.EnvShimActive {
		return "", false
	}
	if _, ok := mcpAllowedEnvKeys[upper]; ok {
		return upper, true
	}
	if strings.HasPrefix(upper, "AL_") {
		return upper, true
	}
	return "", false
}
