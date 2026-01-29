package warnings

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// CheckMCPServers performs discovery on enabled MCP servers and checks against warning thresholds.
// cfg supplies the configured thresholds; nil thresholds disable the corresponding warnings.
func CheckMCPServers(ctx context.Context, cfg *config.ProjectConfig, connector Connector) ([]Warning, error) {
	if connector == nil {
		connector = &RealConnector{}
	}

	// 1. Identify enabled servers
	enabledServers, err := projection.ResolveEnabledMCPServers(cfg.Config.MCP.Servers, cfg.Env)
	if err != nil {
		subject := "mcp.servers"
		var resolveErr *projection.MCPServerResolveError
		if errors.As(err, &resolveErr) && resolveErr.ServerID != "" {
			subject = resolveErr.ServerID
		}
		return []Warning{{
			Code:    CodeMCPServerUnreachable,
			Subject: subject,
			Message: fmt.Sprintf(messages.WarningsResolveConfigFailedFmt, err),
			Fix:     messages.WarningsResolveConfigFix,
		}}, nil
	}

	var warnings []Warning

	thresholds := cfg.Config.Warnings

	// Check: MCP_TOO_MANY_SERVERS_ENABLED
	if thresholds.MCPServerThreshold != nil && len(enabledServers) > *thresholds.MCPServerThreshold {
		warnings = append(warnings, Warning{
			Code:    CodeMCPTooManyServers,
			Subject: "mcp.servers",
			Message: fmt.Sprintf(messages.WarningsTooManyServersFmt, *thresholds.MCPServerThreshold, len(enabledServers), *thresholds.MCPServerThreshold),
			Fix:     messages.WarningsTooManyServersFix,
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
				Message: fmt.Sprintf(messages.WarningsMCPConnectFailedFmt, res.Error),
				Fix:     messages.WarningsMCPConnectFix,
			})
			continue
		}

		// Check: MCP_SERVER_TOO_MANY_TOOLS
		if thresholds.MCPServerToolsThreshold != nil && len(res.Tools) > *thresholds.MCPServerToolsThreshold {
			warnings = append(warnings, Warning{
				Code:    CodeMCPServerTooManyTools,
				Subject: res.ServerID,
				Message: fmt.Sprintf(messages.WarningsMCPServerTooManyToolsFmt, *thresholds.MCPServerToolsThreshold, len(res.Tools), *thresholds.MCPServerToolsThreshold),
				Fix:     messages.WarningsMCPServerTooManyToolsFix,
			})
		}

		// Check: MCP_TOOL_SCHEMA_BLOAT_SERVER
		if thresholds.MCPSchemaTokensServerThreshold != nil && res.SchemaTokens > *thresholds.MCPSchemaTokensServerThreshold {
			// Sort tools by tokens (descending)
			sortedTools := make([]ToolDef, len(res.Tools))
			copy(sortedTools, res.Tools)
			sort.Slice(sortedTools, func(i, j int) bool {
				return sortedTools[i].Tokens > sortedTools[j].Tokens
			})

			var details []string
			details = append(details, "Top contributors by token count:")
			limit := 10
			for i, t := range sortedTools {
				if i >= limit {
					details = append(details, fmt.Sprintf("...and %d more", len(sortedTools)-limit))
					break
				}
				details = append(details, fmt.Sprintf("- %s: %d tokens", t.Name, t.Tokens))
			}

			warnings = append(warnings, Warning{
				Code:    CodeMCPToolSchemaBloatServer,
				Subject: res.ServerID,
				Message: fmt.Sprintf(messages.WarningsMCPSchemaBloatServerFmt, *thresholds.MCPSchemaTokensServerThreshold, res.SchemaTokens, *thresholds.MCPSchemaTokensServerThreshold),
				Fix:     messages.WarningsMCPSchemaBloatFix,
				Details: details,
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
			Message: fmt.Sprintf(messages.WarningsMCPTooManyToolsTotalFmt, *thresholds.MCPToolsTotalThreshold, totalTools, *thresholds.MCPToolsTotalThreshold),
			Fix:     messages.WarningsMCPTooManyToolsTotalFix,
		})
	}

	// Check: MCP_TOOL_SCHEMA_BLOAT_TOTAL
	if thresholds.MCPSchemaTokensTotalThreshold != nil && totalSchemaTokens > *thresholds.MCPSchemaTokensTotalThreshold {
		warnings = append(warnings, Warning{
			Code:    CodeMCPToolSchemaBloatTotal,
			Subject: "mcp.tools.schema.total",
			Message: fmt.Sprintf(messages.WarningsMCPSchemaBloatTotalFmt, *thresholds.MCPSchemaTokensTotalThreshold, totalSchemaTokens, *thresholds.MCPSchemaTokensTotalThreshold),
			Fix:     messages.WarningsMCPSchemaBloatFix,
		})
	}

	// Check: MCP_TOOL_NAME_COLLISION
	for name, servers := range toolNames {
		if len(servers) > 1 {
			warnings = append(warnings, Warning{
				Code:    CodeMCPToolNameCollision,
				Subject: name,
				Message: fmt.Sprintf(messages.WarningsMCPToolNameCollisionFmt, servers),
				Fix:     messages.WarningsMCPToolNameCollisionFix,
			})
		}
	}

	return warnings, nil
}

// ToolDef represents a discovered tool from an MCP server.
type ToolDef struct {
	Name   string
	Tokens int
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
	ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult
}

func discoverTools(ctx context.Context, servers []projection.ResolvedMCPServer, connector Connector) []DiscoveryResult {
	results := make([]DiscoveryResult, len(servers))

	// Semaphore for concurrency
	sem := make(chan struct{}, mcpDiscoveryConcurrency(len(servers)))
	var wg sync.WaitGroup

	for i, server := range servers {
		wg.Add(1)
		go func(i int, s projection.ResolvedMCPServer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[i] = connector.ConnectAndDiscover(ctx, s)
		}(i, server)
	}

	wg.Wait()
	return results
}

// mcpDiscoveryConcurrency returns the max number of concurrent MCP discovery calls.
// serverCount is the number of enabled servers; returns 0 when no servers are provided.
func mcpDiscoveryConcurrency(serverCount int) int {
	if serverCount <= 0 {
		return 0
	}

	gomax := runtime.GOMAXPROCS(0)
	if gomax < 1 {
		gomax = 1
	}

	// Use ~2/3 of GOMAXPROCS to leave headroom for other work.
	limit := (gomax * 2) / 3
	if limit < 1 {
		limit = 1
	}
	if serverCount < limit {
		return serverCount
	}
	return limit
}
