package wizard

import (
	"errors"
	"testing"

	toml "github.com/pelletier/go-toml"
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

func TestAppendMissingDefaultMCPServers(t *testing.T) {
	content := "[mcp]\n"
	missing := []string{"tavily"}

	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	err = appendMissingDefaultMCPServers(tree, missing)
	require.NoError(t, err)

	updated, err := tree.ToTomlString()
	require.NoError(t, err)

	assert.Contains(t, updated, "[[mcp.servers]]")
	assert.Contains(t, updated, `id = "tavily"`)
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

func TestAppendMissingDefaultMCPServers_Error(t *testing.T) {
	content := "[mcp]\n"
	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	err = appendMissingDefaultMCPServers(tree, []string{"non-existent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing default MCP server template")
}

func TestAppendMissingDefaultMCPServers_Empty(t *testing.T) {
	content := "[mcp]\n"
	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	// Empty missing list should return nil immediately
	err = appendMissingDefaultMCPServers(tree, []string{})
	assert.NoError(t, err)
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

func TestMcpServerTrees_UnexpectedType(t *testing.T) {
	// Create a tree where mcp.servers is not []*toml.Tree
	content := `[mcp]
servers = "not-an-array"
`
	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	_, err = mcpServerTrees(tree)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected type")
}

func TestMcpServerTrees_Nil(t *testing.T) {
	content := `[mcp]
`
	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	servers, err := mcpServerTrees(tree)
	assert.NoError(t, err)
	assert.Nil(t, servers)
}

func TestAppendMissingDefaultMCPServers_McpServerTreesError(t *testing.T) {
	// Create a tree where mcp.servers has unexpected type
	content := `[mcp]
servers = "bad"
`
	tree, err := toml.LoadBytes([]byte(content))
	require.NoError(t, err)

	err = appendMissingDefaultMCPServers(tree, []string{"tavily"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected type")
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

[agents.claude-vscode]
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

func TestDefaultMCPServerTreesReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := defaultMCPServerTrees()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config template")
}

func TestDefaultMCPServerTreesParseError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return []byte("invalid toml [[["), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := defaultMCPServerTrees()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config template")
}

func TestDefaultMCPServerTreesNoServers(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return []byte(`[mcp]
`), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := defaultMCPServerTrees()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no MCP server blocks")
}

func TestDefaultMCPServerTreesMissingID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return []byte(`[[mcp.servers]]
enabled = true
`), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := defaultMCPServerTrees()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing MCP server id")
}

func TestDefaultMCPServerTreesDuplicateID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return []byte(`[[mcp.servers]]
id = "tavily"
enabled = true

[[mcp.servers]]
id = "tavily"
enabled = true
`), nil
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := defaultMCPServerTrees()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate MCP server id")
}
