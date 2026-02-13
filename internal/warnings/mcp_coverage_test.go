package warnings

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestCheckMCPServers_NilConnector_AssignsRealConnector_NoEnabledServers(t *testing.T) {
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

	warnings, err := CheckMCPServers(context.Background(), cfg, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestCheckMCPServers_SchemaBloatServer_SortsAndTruncatesDetails(t *testing.T) {
	enabled := true
	serverSchemaThreshold := 10
	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{ID: "s1", Enabled: &enabled, Transport: "stdio", Command: "echo"},
				},
			},
			Warnings: config.WarningsConfig{
				MCPSchemaTokensServerThreshold: &serverSchemaThreshold,
			},
		},
		Env: map[string]string{},
	}

	tools := make([]ToolDef, 0, 12)
	for i := 1; i <= 12; i++ {
		tools = append(tools, ToolDef{
			Name:   fmt.Sprintf("t%02d", i),
			Tokens: i,
		})
	}
	mock := &MockConnector{
		Results: map[string]DiscoveryResult{
			"s1": {
				ServerID:     "s1",
				Tools:        tools,
				SchemaTokens: 100,
			},
		},
	}

	warnings, err := CheckMCPServers(context.Background(), cfg, mock, nil)
	require.NoError(t, err)

	var bloat Warning
	for _, w := range warnings {
		if w.Code == CodeMCPToolSchemaBloatServer {
			bloat = w
			break
		}
	}

	require.Equal(t, CodeMCPToolSchemaBloatServer, bloat.Code)
	require.NotEmpty(t, bloat.Details)
	assert.Contains(t, bloat.Details[0], "Top contributors")
	assert.Contains(t, bloat.Details[1], "t12: 12 tokens")
	assert.Contains(t, bloat.Details[len(bloat.Details)-1], "...and 2 more")
}

func TestMCPDiscoveryConcurrency_MinimumOne(t *testing.T) {
	original := runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(original) })

	if got := mcpDiscoveryConcurrency(100); got != 1 {
		t.Fatalf("expected concurrency 1, got %d", got)
	}
}
