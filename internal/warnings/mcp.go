package warnings

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"sync"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envref"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// MCPSummary holds aggregate MCP discovery measurements for reporting (e.g. the doctor
// size summary). It is produced by the same single discovery pass that emits warnings.
type MCPSummary struct {
	// Available is false when enabled servers could not be resolved at all (in which case
	// the remaining counts are meaningless and should not be reported).
	Available bool
	// EnabledServers is the count of enabled, resolvable servers.
	EnabledServers int
	// ReachableServers is the count of servers whose tools were successfully discovered.
	ReachableServers int
	// OAuthUnvalidatedServers is the count of servers using client-managed OAuth, which
	// doctor cannot authenticate. Tool and schema totals exclude them.
	OAuthUnvalidatedServers int
	// TotalTools is the number of tools discovered across reachable servers.
	TotalTools int
	// TotalSchemaTokens is the estimated tool-schema token count across reachable servers.
	TotalSchemaTokens int
}

// CheckMCPServers performs discovery on enabled MCP servers and checks against warning thresholds.
// cfg supplies the configured thresholds; nil thresholds disable the corresponding warnings.
// statusFn is an optional callback invoked with discovery events; it is safe to pass nil.
// It returns the emitted warnings plus an MCPSummary of the aggregate measurements from the
// same discovery pass (summary.Available is false when servers could not be resolved).
func CheckMCPServers(ctx context.Context, cfg *config.ProjectConfig, connector Connector, statusFn MCPDiscoveryStatusFunc) ([]Warning, MCPSummary, error) {
	if connector == nil {
		connector = &RealConnector{}
	}

	// 1. Identify enabled servers
	env := cfg.PlaceholderEnv()
	enabledServers, err := projection.ResolveEffectiveEnabledMCPServers(cfg.Config, env)
	if err != nil {
		subject := mcpServersKey
		var resolveErr *projection.MCPServerResolveError
		if errors.As(err, &resolveErr) && resolveErr.ServerID != "" {
			subject = resolveErr.ServerID
		}
		return []Warning{{
			Code:     CodeMCPServerUnreachable,
			Subject:  subject,
			Message:  fmt.Sprintf(messages.WarningsResolveConfigFailedFmt, err),
			Fix:      messages.WarningsResolveConfigFix,
			Source:   SourceInternal,
			Severity: SeverityCritical,
		}}, MCPSummary{}, nil
	}

	var warnings []Warning
	transportByServerID := make(map[string]string, len(enabledServers))
	for _, server := range enabledServers {
		transportByServerID[server.ID] = server.Transport
	}

	thresholds := cfg.Config.Warnings

	// Check: MCP_TOO_MANY_SERVERS_ENABLED
	if thresholds.MCPServerThreshold != nil && len(enabledServers) > *thresholds.MCPServerThreshold {
		warnings = append(warnings, Warning{
			Code:              CodeMCPTooManyServers,
			Subject:           mcpServersKey,
			Message:           fmt.Sprintf(messages.WarningsTooManyServersFmt, *thresholds.MCPServerThreshold, len(enabledServers), *thresholds.MCPServerThreshold),
			Fix:               messages.WarningsTooManyServersFix,
			Source:            SourceInternal,
			Severity:          SeverityWarning,
			NoiseSuppressible: true,
		})
	}

	// 2. Discovery (Parallel)
	results := discoverTools(ctx, enabledServers, connector, statusFn, mcpSecretPlaceholders(projection.ReceivedMCPServers(cfg.Config), env))

	// 3. Process results
	var totalTools int
	var totalSchemaTokens int
	var reachableServers int
	var oauthUnvalidatedServers int
	toolNames := make(map[string][]string) // name -> serverIDs

	for _, res := range results {
		if res.OAuthUnvalidated {
			oauthUnvalidatedServers++
			continue
		}

		if res.Error != nil {
			source := SourceExternalDependency
			if transportByServerID[res.ServerID] == config.TransportHTTP {
				source = SourceNetwork
			}

			warnings = append(warnings, Warning{
				Code:     CodeMCPServerUnreachable,
				Subject:  res.ServerID,
				Message:  fmt.Sprintf(messages.WarningsMCPConnectFailedFmt, res.Error),
				Fix:      messages.WarningsMCPConnectFix,
				Source:   source,
				Severity: SeverityCritical,
			})
			continue
		}
		reachableServers++

		// Check: MCP_SERVER_TOO_MANY_TOOLS
		if thresholds.MCPServerToolsThreshold != nil && len(res.Tools) > *thresholds.MCPServerToolsThreshold {
			warnings = append(warnings, Warning{
				Code:              CodeMCPServerTooManyTools,
				Subject:           res.ServerID,
				Message:           fmt.Sprintf(messages.WarningsMCPServerTooManyToolsFmt, *thresholds.MCPServerToolsThreshold, len(res.Tools), *thresholds.MCPServerToolsThreshold),
				Fix:               messages.WarningsMCPServerTooManyToolsFix,
				Source:            SourceInternal,
				Severity:          SeverityWarning,
				NoiseSuppressible: true,
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
				Code:              CodeMCPToolSchemaBloatServer,
				Subject:           res.ServerID,
				Message:           fmt.Sprintf(messages.WarningsMCPSchemaBloatServerFmt, *thresholds.MCPSchemaTokensServerThreshold, res.SchemaTokens, *thresholds.MCPSchemaTokensServerThreshold),
				Fix:               messages.WarningsMCPSchemaBloatFix,
				Details:           details,
				Source:            SourceInternal,
				Severity:          SeverityWarning,
				NoiseSuppressible: true,
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
			Code:              CodeMCPTooManyToolsTotal,
			Subject:           "mcp.tools.total",
			Message:           fmt.Sprintf(messages.WarningsMCPTooManyToolsTotalFmt, *thresholds.MCPToolsTotalThreshold, totalTools, *thresholds.MCPToolsTotalThreshold),
			Fix:               messages.WarningsMCPTooManyToolsTotalFix,
			Source:            SourceInternal,
			Severity:          SeverityWarning,
			NoiseSuppressible: true,
		})
	}

	// Check: MCP_TOOL_SCHEMA_BLOAT_TOTAL
	if thresholds.MCPSchemaTokensTotalThreshold != nil && totalSchemaTokens > *thresholds.MCPSchemaTokensTotalThreshold {
		warnings = append(warnings, Warning{
			Code:              CodeMCPToolSchemaBloatTotal,
			Subject:           "mcp.tools.schema.total",
			Message:           fmt.Sprintf(messages.WarningsMCPSchemaBloatTotalFmt, *thresholds.MCPSchemaTokensTotalThreshold, totalSchemaTokens, *thresholds.MCPSchemaTokensTotalThreshold),
			Fix:               messages.WarningsMCPSchemaBloatFix,
			Source:            SourceInternal,
			Severity:          SeverityWarning,
			NoiseSuppressible: true,
		})
	}

	// Check: MCP_TOOL_NAME_COLLISION
	collisionNames := make([]string, 0, len(toolNames))
	for name, servers := range toolNames {
		if len(servers) > 1 {
			collisionNames = append(collisionNames, name)
		}
	}
	sort.Strings(collisionNames)
	for _, name := range collisionNames {
		servers := append([]string(nil), toolNames[name]...)
		sort.Strings(servers)
		warnings = append(warnings, Warning{
			Code:     CodeMCPToolNameCollision,
			Subject:  name,
			Message:  fmt.Sprintf(messages.WarningsMCPToolNameCollisionFmt, servers),
			Fix:      messages.WarningsMCPToolNameCollisionFix,
			Source:   SourceInternal,
			Severity: SeverityWarning,
		})
	}

	summary := MCPSummary{
		Available:               true,
		EnabledServers:          len(enabledServers),
		ReachableServers:        reachableServers,
		OAuthUnvalidatedServers: oauthUnvalidatedServers,
		TotalTools:              totalTools,
		TotalSchemaTokens:       totalSchemaTokens,
	}

	return warnings, summary, nil
}

// ToolDef represents a discovered tool from an MCP server.
type ToolDef struct {
	Name   string
	Tokens int
}

// DiscoveryResult contains the results of discovering tools from an MCP server.
type DiscoveryResult struct {
	ServerID         string
	Tools            []ToolDef
	SchemaTokens     int
	OAuthUnvalidated bool
	Error            error
}

// MCPDiscoveryStatus is the status of a discovery event for an MCP server.
type MCPDiscoveryStatus string

const (
	// MCPDiscoveryStatusStart indicates a server discovery has started.
	MCPDiscoveryStatusStart MCPDiscoveryStatus = "start"
	// MCPDiscoveryStatusDone indicates a server discovery completed successfully.
	MCPDiscoveryStatusDone MCPDiscoveryStatus = "done"
	// MCPDiscoveryStatusAuthNotValidated indicates that doctor skipped an OAuth server it cannot authenticate.
	MCPDiscoveryStatusAuthNotValidated MCPDiscoveryStatus = "auth_not_validated"
	// MCPDiscoveryStatusError indicates a server discovery completed with an error.
	MCPDiscoveryStatusError MCPDiscoveryStatus = "error"
)

// MCPDiscoveryEvent describes a discovery event for a single MCP server.
type MCPDiscoveryEvent struct {
	ServerID string
	Status   MCPDiscoveryStatus
	Err      error
}

// MCPDiscoveryStatusFunc handles a discovery event emitted during MCP server discovery.
// The function may be invoked concurrently from multiple goroutines.
type MCPDiscoveryStatusFunc func(event MCPDiscoveryEvent)

// Connector interface for mocking.
type Connector interface {
	ConnectAndDiscover(ctx context.Context, server projection.ResolvedMCPServer) DiscoveryResult
}

// discoverTools runs discovery for servers concurrently. placeholders maps each
// resolved configuration value to its placeholder text; discovery errors are
// redacted with it before they reach statusFn or the returned results.
func discoverTools(ctx context.Context, servers []projection.ResolvedMCPServer, connector Connector, statusFn MCPDiscoveryStatusFunc, placeholders map[string]string) []DiscoveryResult {
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

			if statusFn != nil {
				statusFn(MCPDiscoveryEvent{ServerID: s.ID, Status: MCPDiscoveryStatusStart})
			}

			res := connector.ConnectAndDiscover(ctx, s)
			res.Error = redactDiscoveryError(res.Error, placeholders)
			results[i] = res

			if statusFn != nil {
				status := MCPDiscoveryStatusDone
				if res.OAuthUnvalidated {
					status = MCPDiscoveryStatusAuthNotValidated
				} else if res.Error != nil {
					status = MCPDiscoveryStatusError
				}
				statusFn(MCPDiscoveryEvent{ServerID: s.ID, Status: status, Err: res.Error})
			}
		}(i, server)
	}

	wg.Wait()
	return results
}

// mcpSecretPlaceholders maps each value a placeholder in servers resolves to in
// env back to that placeholder's text. Discovery hands resolved URLs, headers,
// commands, arguments, and environment values to transports whose errors echo
// them (Go's HTTP client quotes the full request URL), so every substituted
// value is treated as a secret. Those errors may quote a value (`%q`) or
// re-encode it as part of a URL, so each encoded form maps to the placeholder
// too. Expanded command and argument paths map back to their templates because
// path cleaning can remove parts of a substituted value. Built-in placeholders
// such as AL_REPO_ROOT name non-secret paths and are left visible.
func mcpSecretPlaceholders(servers []config.MCPServer, env map[string]string) map[string]string {
	placeholders := make(map[string]string)
	addForms := func(value, placeholder string) {
		quoted := strconv.Quote(value)
		forms := []string{value, quoted[1 : len(quoted)-1], url.QueryEscape(value), url.PathEscape(value)}
		if unescaped, err := url.PathUnescape(value); err == nil {
			forms = append(forms, unescaped)
		}
		for _, form := range forms {
			placeholders[form] = placeholder
		}
	}
	add := func(text string) bool {
		hasSecret := false
		for _, name := range envref.Names(text) {
			if config.IsBuiltInEnvVar(name) {
				continue
			}
			value := env[name]
			if value == "" {
				continue
			}
			hasSecret = true
			addForms(value, "${"+name+"}")
		}
		return hasSecret
	}
	addPath := func(text string) {
		if !add(text) || !config.ShouldExpandPath(text) {
			return
		}
		substituted, err := config.SubstituteEnvVars(text, env)
		if err != nil {
			return
		}
		// Use the same expansion as projection, including traversal across the
		// template's literal path segments, rather than cleaning the secret alone.
		expanded, err := config.ExpandPathIfNeeded(text, substituted, env[config.BuiltinRepoRootEnvVar])
		if err != nil || expanded == substituted {
			return
		}
		template, err := config.SubstituteEnvVarsWith(text, env, projection.ClientPlaceholderResolver("${%s}"))
		if err == nil {
			addForms(expanded, template)
		}
	}
	for _, server := range servers {
		add(server.URL)
		addPath(server.Command)
		for _, arg := range server.Args {
			addPath(arg)
		}
		for _, value := range server.Headers {
			add(value)
		}
		for _, value := range server.Env {
			add(value)
		}
	}
	return placeholders
}

// redactDiscoveryError replaces resolved secret values in err's message with
// their placeholders. The redacted error deliberately drops the original from
// its chain so no caller can unwrap back to the secret.
func redactDiscoveryError(err error, placeholders map[string]string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	redacted := envref.Redact(message, placeholders)
	if redacted == message {
		return err
	}
	return errors.New(redacted)
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
