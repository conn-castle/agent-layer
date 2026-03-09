package wizard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestMissingDefaultMCPServers(t *testing.T) {
	defaults := []DefaultMCPServer{
		{ID: "tavily"},
		{ID: "context7"},
		{ID: "tavily"},
		{ID: "fetch"},
		{ID: "ripgrep"},
		{ID: "filesystem"},
	}
	servers := []config.MCPServer{
		{ID: "tavily"},
		{ID: "tavily"},
	}

	missing := missingDefaultMCPServers(defaults, servers)

	assert.Equal(t, []string{"context7", "fetch", "ripgrep", "filesystem"}, missing)
}

func TestLoadDefaultMCPServers(t *testing.T) {
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	assert.NotEmpty(t, defaults)

	// Check for expected defaults
	ids := make(map[string]bool)
	for _, s := range defaults {
		ids[s.ID] = true
	}
	assert.True(t, ids["tavily"])
	assert.True(t, ids["fetch"])
	assert.True(t, ids["ripgrep"])
	assert.True(t, ids["filesystem"])
}

func TestMissingDefaultMCPServers_EmptyID(t *testing.T) {
	defaults := []DefaultMCPServer{
		{ID: "github"},
	}
	// Server with empty ID should be skipped
	servers := []config.MCPServer{
		{ID: ""},
		{ID: "github"},
	}

	missing := missingDefaultMCPServers(defaults, servers)
	assert.Empty(t, missing)
}

func TestLoadDefaultMCPServersReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadDefaultMCPServers()
	assert.Error(t, err)
}

func TestLoadDefaultMCPServersNoServers(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		// Return valid TOML with required fields but no MCP servers
		return []byte(`[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[mcp]
`), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadDefaultMCPServers()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no MCP servers")
}
