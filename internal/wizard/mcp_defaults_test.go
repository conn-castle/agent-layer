package wizard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadDefaultMCPServers(t *testing.T) {
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	assert.NotEmpty(t, defaults)

	// Check for expected defaults
	ids := make(map[string]bool)
	for _, s := range defaults {
		ids[s.ID] = true
	}
	assert.True(t, ids["context7"])
	assert.True(t, ids["tavily"])
	assert.True(t, ids["fetch"])
	assert.True(t, ids["playwright"])
	// ripgrep and filesystem were removed from the catalog: ordinary CLI-backed
	// tools belong in CLI command-based skills, not MCP servers.
	assert.False(t, ids["ripgrep"])
	assert.False(t, ids["filesystem"])
}

func TestLoadDefaultMCPServersReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadDefaultMCPServers()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mcp-catalog.toml")
}

func TestLoadDefaultMCPServersNoServers(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == "mcp-catalog.toml" {
			// Return a syntactically valid catalog file with no [[mcp.servers]] entries.
			return []byte("# empty catalog\n[mcp]\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadDefaultMCPServers()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mcp-catalog.toml contains no MCP servers")
}
